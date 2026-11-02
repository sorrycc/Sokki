package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// run runs the command with args against a fake app that answers result,
// and returns the request it got, its exit code and what it printed.
func run(t *testing.T, stdin string, result any, args ...string) (Request, int, string, string) {
	t.Helper()
	var got Request
	var out, errOut bytes.Buffer
	env := Env{Stdout: &out, Stderr: &errOut, Stdin: strings.NewReader(stdin),
		Call: func(_ context.Context, req Request) (json.RawMessage, error) {
			got = req
			if err, ok := result.(error); ok {
				return nil, err
			}
			return json.Marshal(result)
		}}
	code := Main(args, env)
	return got, code, out.String(), errOut.String()
}

func TestBuildsRequests(t *testing.T) {
	cases := []struct {
		args  []string
		stdin string
		cmd   string
		want  map[string]any
	}{
		{[]string{"status"}, "", "status", map[string]any{}},
		{[]string{"pause"}, "", "pause", map[string]any{}},
		{[]string{"matches", "--prompts", "--file", "work"}, "", "match.list", map[string]any{"prompts": true, "file": "work"}},
		{[]string{"match", "show", ":hi"}, "", "match.show", map[string]any{"key": ":hi"}},
		{[]string{"match", "add", "--trigger", ":hi", "Hello"}, "", "match.add", map[string]any{"trigger": ":hi", "replace": "Hello"}},
		{[]string{"match", "add", "--trigger", ":sig"}, "Best,\nMe\n", "match.add", map[string]any{"trigger": ":sig", "replace": "Best,\nMe"}},
		{[]string{"match", "edit", ":hi", "--trigger", ""}, "", "match.edit", map[string]any{"key": ":hi", "trigger": ""}},
		{[]string{"match", "edit", ":hi", "--replace", "-"}, "piped", "match.edit", map[string]any{"key": ":hi", "replace": "piped"}},
		{[]string{"match", "disable", ":hi"}, "", "match.disable", map[string]any{"key": ":hi"}},
		{[]string{"prompt", "add", "--label", "Review", "Review this"}, "", "match.add", map[string]any{"label": "Review", "replace": "Review this", "prompt": true}},
		{[]string{"search", "hel", "lo", "--limit", "3"}, "", "search", map[string]any{"query": "hel lo", "limit": float64(3)}},
		{[]string{"copy", ":hi"}, "", "copy", map[string]any{"key": ":hi"}},
		{[]string{"config"}, "", "config.get", map[string]any{}},
		{[]string{"config", "set", "inject_delay", "5"}, "", "config.set", map[string]any{"key": "inject_delay", "value": "5"}},
		{[]string{"privacy", "usage-stats", "off"}, "", "privacy.set", map[string]any{"key": "usage-stats", "on": false}},
		{[]string{"insights", "clear"}, "", "insights.clear", map[string]any{}},
		{[]string{"activity", "--reveal"}, "", "activity", map[string]any{"reveal": true}},
		{[]string{"signin", "--wait"}, "", "signin", map[string]any{"wait": true}},
	}
	for _, c := range cases {
		got, code, _, errOut := run(t, c.stdin, map[string]any{}, c.args...)
		if code != ExitOK {
			t.Errorf("%v: exit %d: %s", c.args, code, errOut)
			continue
		}
		// As the app decodes them.
		raw, _ := json.Marshal(got.Args)
		args := map[string]any{}
		json.Unmarshal(raw, &args)
		if got.Cmd != c.cmd || !reflect.DeepEqual(args, c.want) {
			t.Errorf("%v: got %s %v, want %s %v", c.args, got.Cmd, args, c.cmd, c.want)
		}
	}
}

func TestRejectsWrongCommands(t *testing.T) {
	for _, args := range [][]string{
		{"nope"},
		{"status", "extra"},
		{"matches", "--bogus", "x"},
		{"matches", "--prompts", "--triggers"},
		{"match"},
		{"match", "show"},
		{"match", "add", "--trigger", ":x"}, // no text, nothing piped
		{"match", "edit", ":hi"},            // nothing to change
		{"match", "duplicate", ":hi", "--replace", "x"},
		{"config", "set", "inject_delay"},
		{"privacy", "crash-reports", "maybe"},
		{"search", "x", "--limit", "zero"},
		{"--text=yes", "status"},
	} {
		if _, code, _, _ := run(t, "", nil, args...); code != ExitUsage {
			t.Errorf("%v: exit %d, want %d", args, code, ExitUsage)
		}
	}
}

func TestHelpNeedsNoApp(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"match", "-h"}} {
		got, code, out, _ := run(t, "", errors.New("called"), args...)
		if code != ExitOK || got.Cmd != "" || !strings.Contains(out, "sokki match add") {
			t.Errorf("%v: exit %d, called %q", args, code, got.Cmd)
		}
	}
}

func TestExitCodes(t *testing.T) {
	if _, code, _, errOut := run(t, "", ErrNotRunning, "status"); code != ExitNotRunning || !strings.Contains(errOut, "isn’t running") {
		t.Errorf("not running: exit %d, %q", code, errOut)
	}
	if _, code, _, errOut := run(t, "", errors.New("no match"), "match", "show", "x"); code != ExitFailed || !strings.Contains(errOut, "no match") {
		t.Errorf("failed: exit %d, %q", code, errOut)
	}
}

func TestPrintsText(t *testing.T) {
	_, _, out, _ := run(t, "", map[string]any{"enabled": false, "trigger": ":hi", "replace": "a\nb"}, "match", "show", ":hi", "--text")
	want := "enabled: false\nreplace: a\n  b\ntrigger: :hi\n"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestSocketRoundTrip(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "s.sock")
	if _, err := Call(context.Background(), socket, Request{Cmd: "status"}); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("no app: %v", err)
	}
	l, err := Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go http.Serve(l, Handler(func(_ context.Context, req Request) (any, error) {
		if req.Cmd == "fail" {
			return nil, errors.New("it failed")
		}
		return map[string]any{"cmd": req.Cmd, "key": req.Args["key"]}, nil
	}))
	raw, err := Call(context.Background(), socket, Request{Cmd: "match.show", Args: map[string]any{"key": ":hi"}})
	if err != nil || string(raw) != `{"cmd":"match.show","key":":hi"}` {
		t.Errorf("got %s, %v", raw, err)
	}
	if _, err := Call(context.Background(), socket, Request{Cmd: "fail"}); err == nil || err.Error() != "it failed" {
		t.Errorf("error: %v", err)
	}
	if fi, err := os.Stat(socket); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("socket mode: %v %v", fi.Mode(), err)
	}
	if _, err := Listen(socket); err == nil {
		t.Error("a second app listened at the same socket")
	}
}

func TestInstallsAndRepairs(t *testing.T) {
	dir := t.TempDir()
	dirs := []string{filepath.Join(dir, "bin")}
	bin := filepath.Join(dir, "Sokki")
	os.WriteFile(bin, nil, 0o755)
	p, err := Install(dirs, bin)
	if err != nil {
		t.Fatal(err)
	}
	if got, b := Installed(dirs); got != p || b != bin {
		t.Errorf("installed %q running %q", got, b)
	}
	// The app moved: the command follows it.
	moved := filepath.Join(dir, "Moved", "Sokki")
	os.Remove(bin)
	if !Repair(dirs, moved) {
		t.Error("didn't repair")
	}
	if _, b := Installed(dirs); b != moved {
		t.Errorf("runs %q", b)
	}
	if removed, err := Uninstall(dirs); err != nil || len(removed) != 1 {
		t.Errorf("uninstall: %v %v", removed, err)
	}
	// Another program's sokki is left alone.
	os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	if _, err := Install(dirs, bin); err == nil {
		t.Error("overwrote another program")
	}
}
