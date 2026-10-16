// Package ai runs AI prompts through the user's Claude Code CLI, `claude`:
// it finds the binary, settles its arguments, runs it and reads its JSON.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"sokki/internal/logx"
)

// SystemPromptAppend asks for the transformed text alone.
const SystemPromptAppend = "Output only the transformed text. Do not add preamble, quotes, explanations, or commentary."

// Request is a prompt to run.
type Request struct {
	Prompt   string
	Model    string
	Timeout  time.Duration
	UserArgs []string // nil for the defaults
	// Binary overrides where `claude` is, when set.
	Binary string
}

// ReasonKind is why a run failed.
type ReasonKind int

const (
	ReasonTimeout ReasonKind = iota
	ReasonBinaryMissing
	ReasonAuthRequired
	ReasonNonZeroExit
	ReasonProtocol
)

// Reason is why a run failed, with what goes with it.
type Reason struct {
	Kind   ReasonKind
	Code   int
	Tail   string // stderr's end, or the CLI's message
	Detail string // for ReasonProtocol
}

// String is the reason as the HUD shows it.
func (r Reason) String() string {
	switch r.Kind {
	case ReasonTimeout:
		return "timeout"
	case ReasonBinaryMissing:
		return "`claude` binary not found"
	case ReasonAuthRequired:
		return "run `claude auth login` to sign in"
	case ReasonNonZeroExit:
		return fmt.Sprintf("exit %d: %s", r.Code, truncRunes(r.Tail, 120))
	}
	return "protocol: " + r.Detail
}

// Outcome is how a run ended.
type Outcome int

const (
	Completed Outcome = iota
	Failed
	Canceled
)

// Result is what a run returned.
type Result struct {
	Outcome Outcome
	Text    string
	Reason  Reason
}

// Rewriter runs one prompt at a time; a new run cancels the one before.
type Rewriter struct {
	Resolver *Resolver
	// ScratchDir is the working directory of the CLI, which keeps it out
	// of any project.
	ScratchDir string

	mu     sync.Mutex
	cancel context.CancelFunc
	gen    uint64
}

// ScratchDir returns ~/Library/Application Support/Sokki/ai-scratch, made.
func ScratchDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "Sokki", "ai-scratch")
	os.MkdirAll(dir, 0o755)
	return dir
}

// Run runs req and waits for it.
func (w *Rewriter) Run(req Request) Result {
	ctx, cancel := context.WithCancel(context.Background())
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
	}
	w.cancel = cancel
	w.gen++
	gen := w.gen
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		if w.gen == gen {
			w.cancel = nil
		}
		w.mu.Unlock()
		cancel()
	}()

	binary, ok := w.Resolver.Resolve(req.Binary)
	if !ok {
		logx.Warnf("[AI] binary not found (override=%s)", orNil(req.Binary))
		return Result{Outcome: Failed, Reason: Reason{Kind: ReasonBinaryMissing}}
	}
	args, logs := CanonicalArgs(req.UserArgs, req.Model, SystemPromptAppend)
	for _, l := range logs {
		logx.Debugf("%s", l)
	}
	logx.Debugf("[AI] run binary=%s model=%s timeout=%ds promptLen=%d gen=%d",
		binary, req.Model, int(req.Timeout.Seconds()), len([]rune(req.Prompt)), gen)

	tctx, tcancel := context.WithTimeout(ctx, req.Timeout)
	defer tcancel()
	cmd := exec.CommandContext(tctx, binary, args...)
	cmd.Dir = w.ScratchDir
	cmd.Env = withPath(os.Environ())
	cmd.Stdin = strings.NewReader(req.Prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// The CLI is a script whose children hold its pipes: end its whole
	// process group, and stop waiting for the pipes soon after.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	switch {
	case ctx.Err() != nil:
		return Result{Outcome: Canceled}
	case tctx.Err() == context.DeadlineExceeded:
		return Result{Outcome: Failed, Reason: Reason{Kind: ReasonTimeout}}
	}
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
				return Result{Outcome: Failed, Reason: Reason{Kind: ReasonBinaryMissing}}
			}
			return Result{Outcome: Failed, Reason: Reason{Kind: ReasonProtocol, Detail: "launch failed: " + err.Error()}}
		}
		code = exit.ExitCode()
	}
	tail := stderr.String()
	if r := []rune(tail); len(r) > 500 {
		tail = string(r[len(r)-500:])
	}
	return Parse(code, stdout.Bytes(), strings.TrimSpace(tail))
}

// Cancel ends the run under way, if any.
func (w *Rewriter) Cancel() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		logx.Debugf("[AI] cancelCurrent")
		w.cancel()
		w.cancel = nil
	}
}

func orNil(s string) string {
	if s == "" {
		return "nil"
	}
	return s
}

// withPath puts the usual binary directories first on PATH, so that the
// node and shims the claude script runs are found from a GUI app.
func withPath(env []string) []string {
	path := ""
	out := env[:0:0]
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
			continue
		}
		out = append(out, kv)
	}
	var dirs []string
	for _, d := range append([]string{"/usr/local/bin", "/opt/homebrew/bin", "/usr/bin", "/bin"}, strings.Split(path, ":")...) {
		if d != "" && !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	return append(out, "PATH="+strings.Join(dirs, ":"))
}

var authHints = []string{"log in", "logged in", "authenticate", "not signed in", "sign in"}

// Parse reads what the CLI printed with --output-format json.
func Parse(code int, stdout []byte, stderrTail string) Result {
	logx.Debugf("[AI] process exit=%d stdoutBytes=%d stderrLen=%d", code, len(stdout), len(stderrTail))
	fail := func(r Reason) Result { return Result{Outcome: Failed, Reason: r} }
	lower := strings.ToLower(stderrTail)
	if code != 0 && slices.ContainsFunc(authHints, func(h string) bool { return strings.Contains(lower, h) }) {
		logx.Warnf("[AI] parse: authRequired stderrTail=%s", truncRunes(stderrTail, 200))
		return fail(Reason{Kind: ReasonAuthRequired})
	}
	if len(stdout) == 0 {
		if code == 0 {
			logx.Warnf("[AI] parse: empty stdout with exit=0")
			return fail(Reason{Kind: ReasonProtocol, Detail: "empty stdout"})
		}
		logx.Warnf("[AI] parse: nonZeroExit code=%d stderrTail=%s", code, truncRunes(stderrTail, 200))
		return fail(Reason{Kind: ReasonNonZeroExit, Code: code, Tail: stderrTail})
	}
	var obj map[string]any
	if err := json.Unmarshal(stdout, &obj); err != nil {
		var any_ any
		if json.Unmarshal(stdout, &any_) == nil {
			logx.Warnf("[AI] parse: non-object JSON")
			return fail(Reason{Kind: ReasonProtocol, Detail: "non-object JSON"})
		}
		logx.Warnf("[AI] parse: JSON decode %v", err)
		return fail(Reason{Kind: ReasonProtocol, Detail: "JSON decode: " + err.Error()})
	}
	isError, _ := obj["is_error"].(bool)
	text, _ := obj["result"].(string)
	if isError {
		msg := text
		if msg == "" {
			msg = stderrTail
		}
		ml := strings.ToLower(msg)
		if strings.Contains(ml, "not logged in") || strings.Contains(ml, "please run /login") ||
			strings.Contains(ml, "log in") || strings.Contains(ml, "authenticate") {
			logx.Warnf("[AI] parse: authRequired (json) msg=%s", truncRunes(msg, 200))
			return fail(Reason{Kind: ReasonAuthRequired})
		}
		logx.Warnf("[AI] parse: is_error=true msg=%s", truncRunes(msg, 200))
		return fail(Reason{Kind: ReasonNonZeroExit, Code: code, Tail: msg})
	}
	if text == "" {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		logx.Warnf("[AI] parse: missing `result` field keys=%s", strings.Join(keys, ","))
		return fail(Reason{Kind: ReasonProtocol, Detail: "missing `result` field"})
	}
	logx.Debugf("[AI] parse: ok resultLen=%d cliDurationMs=%v costUSD=%v", len([]rune(text)), obj["duration_ms"], obj["total_cost_usd"])
	return Result{Outcome: Completed, Text: text}
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func quoted(s string) string {
	r := []rune(s)
	if len(r) <= 40 {
		return `"` + s + `"`
	}
	return `"` + string(r[:40]) + `…"`
}

// CanonicalArgs settles the CLI's arguments: the user's (or defaults),
// with --print, --output-format json and the system prompt forced, and
// --model added when the user did not give one. logs say what changed.
func CanonicalArgs(userArgs []string, model, systemPrompt string) (args, logs []string) {
	if userArgs == nil {
		args = []string{"--print", "--tools", "", "--output-format", "json", "--max-turns", "1",
			"--no-session-persistence", "--append-system-prompt", systemPrompt}
	} else {
		args = slices.Clone(userArgs)
	}
	forceValue := func(flag, required string, redact bool) {
		show := func(s string) string {
			if redact {
				return quoted(s)
			}
			return s
		}
		i := slices.Index(args, flag)
		switch {
		case i < 0:
			args = append(args, flag, required)
			logs = append(logs, fmt.Sprintf("[AI] inserted %s=%s (user did not set)", flag, show(required)))
		case i+1 < len(args):
			if args[i+1] == required {
				return
			}
			old := args[i+1]
			args[i+1] = required
			logs = append(logs, fmt.Sprintf("[AI] overrode %s=%s (user had: %s)", flag, show(required), show(old)))
		default:
			args = append(args, required)
			logs = append(logs, fmt.Sprintf("[AI] inserted value for dangling %s=%s", flag, show(required)))
		}
	}
	forceValue("--output-format", "json", false)
	forceValue("--append-system-prompt", systemPrompt, true)
	if !slices.Contains(args, "--print") {
		args = append(args, "--print")
		logs = append(logs, "[AI] inserted --print (user did not set)")
	}
	if model != "" && !slices.Contains(args, "--model") {
		args = append(args, "--model", model)
	}
	return args, logs
}

// Resolver finds the claude binary, and remembers where.
type Resolver struct {
	mu     sync.Mutex
	cached string
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// Resolve returns where claude is: the override, the usual places, else
// what the login shell finds.
func (r *Resolver) Resolve(override string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cached != "" && isExecutable(r.cached) {
		return r.cached, true
	}
	home, _ := os.UserHomeDir()
	var candidates []string
	if override != "" {
		if rest, ok := strings.CutPrefix(override, "~/"); ok {
			override = filepath.Join(home, rest)
		}
		candidates = append(candidates, override)
	}
	candidates = append(candidates,
		filepath.Join(home, ".claude/local/claude"),
		filepath.Join(home, ".local/bin/claude"),
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude")
	for _, p := range candidates {
		if isExecutable(p) {
			r.cached = p
			logx.Debugf("[AI:resolve] found at %s", p)
			return p, true
		}
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-lc", "command -v claude").Output()
	if p := strings.TrimSpace(string(out)); err == nil && p != "" && isExecutable(p) {
		r.cached = p
		logx.Debugf("[AI:resolve] found via login shell at %s", p)
		return p, true
	}
	logx.Errorf("[AI:resolve] claude binary not found; checked=%s", strings.Join(candidates, ","))
	return "", false
}

// Invalidate forgets where claude was.
func (r *Resolver) Invalidate() {
	r.mu.Lock()
	r.cached = ""
	r.mu.Unlock()
}
