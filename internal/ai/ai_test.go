package ai

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"sokki/internal/logx"
)

const sys = "Output only transformed text."

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "sokki-log")
	logx.SetDir(dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func flagValue(args []string, flag string) string {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

func TestCanonicalArgsDefaults(t *testing.T) {
	args, logs := CanonicalArgs(nil, "haiku", sys)
	if len(logs) != 0 {
		t.Fatalf("logs %v", logs)
	}
	if !slices.Contains(args, "--print") || flagValue(args, "--output-format") != "json" ||
		flagValue(args, "--append-system-prompt") != sys || flagValue(args, "--model") != "haiku" {
		t.Fatalf("args %q", args)
	}
}

func TestCanonicalArgsFloor(t *testing.T) {
	args, logs := CanonicalArgs([]string{}, "haiku", sys)
	inserted := 0
	for _, l := range logs {
		if strings.Contains(l, "inserted") {
			inserted++
		}
	}
	if inserted != 3 || flagValue(args, "--model") != "haiku" {
		t.Fatalf("args %q logs %v", args, logs)
	}
}

func TestCanonicalArgsOverrides(t *testing.T) {
	args, logs := CanonicalArgs([]string{"--output-format", "text"}, "haiku", sys)
	if flagValue(args, "--output-format") != "json" ||
		!slices.ContainsFunc(logs, func(l string) bool {
			return strings.Contains(l, "overrode --output-format=json") && strings.Contains(l, "user had: text")
		}) {
		t.Fatalf("args %q logs %v", args, logs)
	}
	long := "Be very long and chatty — say everything you think and then some, really elaborately"
	args, logs = CanonicalArgs([]string{"--append-system-prompt", long}, "haiku", sys)
	if flagValue(args, "--append-system-prompt") != sys ||
		!slices.ContainsFunc(logs, func(l string) bool {
			return strings.Contains(l, "overrode --append-system-prompt") && strings.Contains(l, "…")
		}) {
		t.Fatalf("args %q logs %v", args, logs)
	}
	_, logs = CanonicalArgs([]string{"--print", "--output-format", "json", "--append-system-prompt", sys}, "haiku", sys)
	if len(logs) != 0 {
		t.Fatalf("logs on exact match: %v", logs)
	}
}

func TestCanonicalArgsModel(t *testing.T) {
	args, _ := CanonicalArgs([]string{"--model", "sonnet"}, "haiku", sys)
	n := 0
	for _, a := range args {
		if a == "--model" {
			n++
		}
	}
	if n != 1 || flagValue(args, "--model") != "sonnet" {
		t.Fatalf("args %q", args)
	}
	if args, _ := CanonicalArgs([]string{"--print"}, "", sys); slices.Contains(args, "--model") {
		t.Fatal("model injected while empty")
	}
	args, logs := CanonicalArgs([]string{"--output-format", "json", "--append-system-prompt", sys}, "haiku", sys)
	if !slices.Contains(args, "--print") || !slices.ContainsFunc(logs, func(l string) bool { return strings.Contains(l, "inserted --print") }) {
		t.Fatalf("args %q logs %v", args, logs)
	}
}

func TestParse(t *testing.T) {
	if r := Parse(0, []byte(`{"result":"hi","is_error":false}`), ""); r.Outcome != Completed || r.Text != "hi" {
		t.Fatalf("%+v", r)
	}
	if r := Parse(1, nil, "Please log in first"); r.Reason.Kind != ReasonAuthRequired {
		t.Fatalf("%+v", r)
	}
	if r := Parse(0, []byte(`{"result":"Not logged in · Please run /login","is_error":true}`), ""); r.Reason.Kind != ReasonAuthRequired {
		t.Fatalf("%+v", r)
	}
	if r := Parse(2, nil, "boom"); r.Reason.Kind != ReasonNonZeroExit || r.Reason.Code != 2 {
		t.Fatalf("%+v", r)
	}
	if r := Parse(0, []byte(`[1]`), ""); r.Reason.Detail != "non-object JSON" {
		t.Fatalf("%+v", r)
	}
	if r := Parse(0, []byte(`{"x":1}`), ""); r.Reason.Detail != "missing `result` field" {
		t.Fatalf("%+v", r)
	}
}

func fakeClaude(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunWithFakeCLI(t *testing.T) {
	bin := fakeClaude(t, `cat > /dev/null; echo '{"result":"rewritten","is_error":false}'`)
	w := &Rewriter{Resolver: &Resolver{}, ScratchDir: t.TempDir()}
	r := w.Run(Request{Prompt: "x", Model: "haiku", Timeout: 5 * time.Second, Binary: bin})
	if r.Outcome != Completed || r.Text != "rewritten" {
		t.Fatalf("%+v", r)
	}
}

func TestRunTimeoutAndCancel(t *testing.T) {
	bin := fakeClaude(t, `sleep 5`)
	w := &Rewriter{Resolver: &Resolver{}, ScratchDir: t.TempDir()}
	if r := w.Run(Request{Prompt: "x", Timeout: 200 * time.Millisecond, Binary: bin}); r.Reason.Kind != ReasonTimeout {
		t.Fatalf("%+v", r)
	}
	done := make(chan Result, 1)
	go func() { done <- w.Run(Request{Prompt: "x", Timeout: 10 * time.Second, Binary: bin}) }()
	time.Sleep(200 * time.Millisecond)
	w.Cancel()
	select {
	case r := <-done:
		if r.Outcome != Canceled {
			t.Fatalf("%+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not end the run")
	}
}
