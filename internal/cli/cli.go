// Package cli is the sokki command, which does from the terminal what
// Sokki's windows and menu do, by asking the running app: its commands,
// the protocol it speaks with the app, and putting it on the shell's path.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Exit codes.
const (
	ExitOK         = 0
	ExitFailed     = 1 // the app couldn't do it
	ExitUsage      = 2 // the command is wrong
	ExitNotRunning = 3 // the app isn't running
)

// Env is what Main needs of its process.
type Env struct {
	Stdout, Stderr io.Writer
	// Stdin is where a match's text is read from when it is piped in;
	// nil for the process's, when it isn't a terminal.
	Stdin io.Reader
	// Dev talks to the development build, as --dev does.
	Dev bool
	// Call sends a request to the app; nil for the socket's.
	Call func(ctx context.Context, req Request) (json.RawMessage, error)
}

// Usage is the sokki command's help.
const Usage = `sokki: do from the terminal what Sokki's windows and menu do. It talks to
Sokki running on this Mac, and prints JSON; --text prints for people.
A match is named by its trigger, or a prompt by its title.

Status
  sokki status                                on or paused, Accessibility, this Mac's matches
  sokki pause | resume                        stop and start expanding triggers

Matches and prompts
  sokki matches [--file F] [--prompts|--triggers] [--disabled] [--query Q]
  sokki files                                 the match files, with their counts
  sokki match show <key>
  sokki match add [--trigger T] [--label L] [--file F] [--disabled] [<text>]
                                              the text, else what is piped in; no
                                              trigger makes a prompt
  sokki match edit <key> [--trigger T] [--label L] [--file F] [--replace TEXT]
                                              --trigger "" makes it a prompt;
                                              --replace - reads what is piped in
  sokki match duplicate <key> [--trigger T] [--label L] [--file F]
  sokki match enable|disable <key>
  sokki match remove <key>
  sokki prompt add [--label L] [--trigger T] [<text>]
                                              a prompt of prompts.yml, as the palette's
                                              New Prompt

Palette
  sokki search <query> [--limit N]            the enabled matches, best first
  sokki render <key>                          its text, variables filled in
  sokki copy <key>                            the same, put on the clipboard

Settings
  sokki config                                every setting
  sokki config get <key>
  sokki config set <key> <value>              enable, launch_at_login, inject_delay,
                                              conflict_resolve_delay_ms,
                                              clipboard_threshold, preserve_clipboard,
                                              restore_clipboard_delay, log_level,
                                              palette_shortcut, capture_shortcut,
                                              toggle_shortcut, hud_shortcut
  sokki privacy                               crash reports, usage stats, anonymous ID
  sokki privacy crash-reports|usage-stats on|off
  sokki privacy reset-id

Insights and Recent Activity
  sokki insights                              the past 7 days' expansions
  sokki insights clear                        delete the usage data
  sokki activity [--reveal]                   what Sokki did with the last things
                                              typed, masked unless --reveal

Account (settings sync)
  sokki account                               signed in, syncing, the last sync
  sokki signin [--wait]                       prints the code and opens the browser;
                                              --wait until it's approved
  sokki signout
  sokki sync                                  Sync Now, waiting for it

Troubleshooting
  sokki reload                                read the match files again
  sokki log                                   the log file and the config folder
  sokki restart | quit

Options
  --text   print for people rather than JSON
  --dev    talk to the development build (scripts/dev.sh)

Exit codes: 0 done, 1 failed, 2 wrong command, 3 Sokki isn't running.
`

// Main runs the sokki command with args, without the command's name, and
// returns its exit code.
func Main(args []string, env Env) int {
	if env.Stdout == nil {
		env.Stdout = os.Stdout
	}
	if env.Stderr == nil {
		env.Stderr = os.Stderr
	}
	pos, fl, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(env.Stderr, "sokki:", err)
		return ExitUsage
	}
	if len(pos) == 0 || pos[0] == "help" || fl.bool("help") {
		fmt.Fprint(env.Stdout, Usage)
		return ExitOK
	}
	req, timeout, err := build(pos, fl, stdin(env))
	if err != nil {
		fmt.Fprintln(env.Stderr, "sokki:", err)
		fmt.Fprintln(env.Stderr, "Run `sokki help` for the commands.")
		return ExitUsage
	}
	call := env.Call
	if call == nil {
		socket := SocketPath(env.Dev || fl.bool("dev"))
		call = func(ctx context.Context, req Request) (json.RawMessage, error) { return Call(ctx, socket, req) }
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := call(ctx, req)
	if err != nil {
		fmt.Fprintln(env.Stderr, "sokki:", err)
		if errors.Is(err, ErrNotRunning) {
			return ExitNotRunning
		}
		return ExitFailed
	}
	if fl.bool("text") {
		printText(env.Stdout, out)
		return ExitOK
	}
	var b bytes.Buffer
	if json.Indent(&b, out, "", "  ") != nil {
		b.Reset()
		b.Write(out)
	}
	b.WriteByte('\n')
	env.Stdout.Write(b.Bytes())
	return ExitOK
}

// stdin returns what is piped in, read once as asked: "" with nothing
// piped, as from a terminal.
func stdin(env Env) func() (string, error) {
	return func() (string, error) {
		r := env.Stdin
		if r == nil {
			fi, err := os.Stdin.Stat()
			if err != nil || fi.Mode()&os.ModeCharDevice != 0 {
				return "", nil
			}
			r = os.Stdin
		}
		b, err := io.ReadAll(io.LimitReader(r, 1<<20))
		return string(b), err
	}
}

// flags are a command's options: each --name's values, in order.
type flags map[string][]string

func (f flags) str(name string) string {
	if v := f[name]; len(v) > 0 {
		return v[len(v)-1]
	}
	return ""
}

func (f flags) bool(name string) bool { _, ok := f[name]; return ok }

// boolFlags take no value.
var boolFlags = []string{"text", "dev", "help", "prompts", "triggers", "disabled", "reveal", "wait"}

// parseArgs splits args into words and options, which may come anywhere;
// "--" ends the options.
func parseArgs(args []string) (pos []string, fl flags, err error) {
	fl = flags{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if a == "-h" {
			fl["help"] = nil
			continue
		}
		if !strings.HasPrefix(a, "--") {
			pos = append(pos, a)
			continue
		}
		name, val, hasVal := strings.Cut(a[2:], "=")
		if slices.Contains(boolFlags, name) {
			if hasVal {
				return nil, nil, fmt.Errorf("--%s takes no value", name)
			}
			fl[name] = nil
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("--%s needs a value", name)
			}
			i++
			val = args[i]
		}
		fl[name] = append(fl[name], val)
	}
	return pos, fl, nil
}

// callTimeout is how long a call may take; waitTimeout one that waits for
// a sync or a sign-in.
const (
	callTimeout = 30 * time.Second
	waitTimeout = 16 * time.Minute
)

// build turns the words and options of a command into the app's request,
// and how long to wait for it; piped reads the text piped in.
func build(pos []string, fl flags, piped func() (string, error)) (Request, time.Duration, error) {
	cmd, rest := pos[0], pos[1:]
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	args := map[string]any{}
	fail := func(err error) (Request, time.Duration, error) { return Request{}, 0, err }
	req := func(name string) (Request, time.Duration, error) {
		return Request{Cmd: name, Args: args}, callTimeout, nil
	}
	allowed := func(names ...string) error {
		for k := range fl {
			if !slices.Contains(names, k) && k != "text" && k != "dev" {
				return fmt.Errorf("unknown option --%s", k)
			}
		}
		return nil
	}
	// words checks that the command has n words after its name.
	words := func(n int, what string) error {
		switch {
		case len(rest) < n:
			return fmt.Errorf("missing %s", what)
		case len(rest) > n:
			return fmt.Errorf("too many words: %q", strings.Join(rest[n:], " "))
		}
		return nil
	}
	// setStr sets the options given, "" too: --trigger "" clears it.
	setStr := func(names ...string) {
		for _, n := range names {
			if fl.bool(n) {
				args[n] = fl.str(n)
			}
		}
	}
	// text is the match's text: the word given, else what is piped in.
	text := func(words []string) (string, error) {
		switch {
		case len(words) > 1:
			return "", fmt.Errorf("too many words: quote the text")
		case len(words) == 1 && words[0] != "-":
			return words[0], nil
		}
		s, err := piped()
		if err != nil {
			return "", err
		}
		// As $(…) does, the newline that ends the last line goes.
		s = strings.TrimSuffix(strings.TrimSuffix(s, "\n"), "\r")
		if s == "" {
			return "", errors.New("missing the text: give it, or pipe it in")
		}
		return s, nil
	}
	simple := func(name string) (Request, time.Duration, error) {
		if err := firstErr(allowed(), words(0, "")); err != nil {
			return fail(err)
		}
		return req(name)
	}

	switch cmd {
	case "status", "pause", "resume", "files", "reload", "log", "restart", "quit", "account", "signout":
		return simple(cmd)
	case "sync":
		r, _, err := simple(cmd)
		return r, waitTimeout, err
	case "signin":
		if err := firstErr(allowed("wait"), words(0, "")); err != nil {
			return fail(err)
		}
		if fl.bool("wait") {
			args["wait"] = true
			return Request{Cmd: "signin", Args: args}, waitTimeout, nil
		}
		return req("signin")
	case "matches":
		if err := firstErr(allowed("file", "prompts", "triggers", "disabled", "query"), words(0, "")); err != nil {
			return fail(err)
		}
		if fl.bool("prompts") && fl.bool("triggers") {
			return fail(errors.New("--prompts and --triggers leave nothing: give one"))
		}
		setStr("file", "query")
		for _, b := range []string{"prompts", "triggers", "disabled"} {
			if fl.bool(b) {
				args[b] = true
			}
		}
		return req("match.list")
	case "match":
		switch sub {
		case "show", "remove", "enable", "disable":
			if err := firstErr(allowed(), words(2, "the match's trigger or title")); err != nil {
				return fail(err)
			}
			args["key"] = rest[1]
			return req("match." + sub)
		case "add":
			if err := allowed("trigger", "label", "file", "disabled"); err != nil {
				return fail(err)
			}
			s, err := text(rest[1:])
			if err != nil {
				return fail(err)
			}
			args["replace"] = s
			setStr("trigger", "label", "file")
			if fl.bool("disabled") {
				args["disabled"] = true
			}
			return req("match.add")
		case "edit", "duplicate":
			if err := firstErr(allowed("trigger", "label", "file", "replace"), words(2, "the match's trigger or title")); err != nil {
				return fail(err)
			}
			if sub == "duplicate" && fl.bool("replace") {
				return fail(errors.New("unknown option --replace"))
			}
			args["key"] = rest[1]
			setStr("trigger", "label", "file", "replace")
			if fl.str("replace") == "-" {
				s, err := text(nil)
				if err != nil {
					return fail(err)
				}
				args["replace"] = s
			}
			if sub == "edit" && len(args) == 1 {
				return fail(errors.New("nothing to change: give --trigger, --label, --file or --replace"))
			}
			return req("match." + sub)
		case "":
			return fail(errors.New("match takes show, add, edit, duplicate, enable, disable or remove"))
		}
		return fail(fmt.Errorf("unknown command: match %s", sub))
	case "prompt":
		if sub != "add" {
			return fail(errors.New("prompt takes add"))
		}
		if err := allowed("trigger", "label"); err != nil {
			return fail(err)
		}
		s, err := text(rest[1:])
		if err != nil {
			return fail(err)
		}
		args["replace"], args["prompt"] = s, true
		setStr("trigger", "label")
		return req("match.add")
	case "search":
		if err := allowed("limit"); err != nil {
			return fail(err)
		}
		args["query"] = strings.Join(rest, " ")
		if v := fl.str("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return fail(errors.New("--limit takes a number"))
			}
			args["limit"] = n
		}
		return req("search")
	case "render", "copy":
		if err := firstErr(allowed(), words(1, "the match's trigger or title")); err != nil {
			return fail(err)
		}
		args["key"] = rest[0]
		return req(cmd)
	case "config":
		if err := allowed(); err != nil {
			return fail(err)
		}
		switch sub {
		case "":
			return req("config.get")
		case "get":
			if err := words(2, "the setting's name"); err != nil {
				return fail(err)
			}
			args["key"] = rest[1]
			return req("config.get")
		case "set":
			if err := words(3, "the setting's name and value"); err != nil {
				return fail(err)
			}
			args["key"], args["value"] = rest[1], rest[2]
			return req("config.set")
		}
		return fail(fmt.Errorf("unknown command: config %s", sub))
	case "privacy":
		if err := allowed(); err != nil {
			return fail(err)
		}
		switch sub {
		case "":
			return req("privacy.get")
		case "reset-id":
			if err := words(1, ""); err != nil {
				return fail(err)
			}
			return req("privacy.reset-id")
		case "crash-reports", "usage-stats":
			if len(rest) != 2 || (rest[1] != "on" && rest[1] != "off") {
				return fail(fmt.Errorf("privacy %s takes on or off", sub))
			}
			args["key"], args["on"] = sub, rest[1] == "on"
			return req("privacy.set")
		}
		return fail(fmt.Errorf("unknown command: privacy %s", sub))
	case "insights":
		if err := allowed(); err != nil {
			return fail(err)
		}
		switch sub {
		case "":
			return req("insights")
		case "clear":
			if err := words(1, ""); err != nil {
				return fail(err)
			}
			return req("insights.clear")
		}
		return fail(fmt.Errorf("unknown command: insights %s", sub))
	case "activity":
		if err := firstErr(allowed("reveal"), words(0, "")); err != nil {
			return fail(err)
		}
		if fl.bool("reveal") {
			args["reveal"] = true
		}
		return req("activity")
	}
	return fail(fmt.Errorf("unknown command: %s", cmd))
}

// firstErr is the first error of errs.
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// printText prints a result for people: an object's fields a line each,
// a list's items apart.
func printText(w io.Writer, raw json.RawMessage) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		fmt.Fprintln(w, string(raw))
		return
	}
	var b strings.Builder
	writeText(&b, v, "")
	fmt.Fprint(w, b.String())
}

func writeText(b *strings.Builder, v any, indent string) {
	switch v := v.(type) {
	case []any:
		for i, item := range v {
			if i > 0 && indent == "" {
				b.WriteString("\n")
			}
			if _, ok := item.(map[string]any); ok {
				if indent == "" {
					writeText(b, item, "")
					continue
				}
				b.WriteString(indent + "-\n")
				writeText(b, item, indent+"  ")
				continue
			}
			b.WriteString(indent + "- " + scalar(item) + "\n")
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch x := v[k].(type) {
			case nil, string, bool, float64:
				if s := scalar(x); s != "" {
					s = strings.ReplaceAll(s, "\n", "\n"+indent+"  ")
					b.WriteString(indent + k + ": " + s + "\n")
				}
			case []any:
				if len(x) > 0 {
					b.WriteString(indent + k + ":\n")
					writeText(b, x, indent+"  ")
				}
			default:
				b.WriteString(indent + k + ":\n")
				writeText(b, x, indent+"  ")
			}
		}
	default:
		b.WriteString(indent + scalar(v) + "\n")
	}
}

func scalar(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}
