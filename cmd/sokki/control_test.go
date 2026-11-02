package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sokki/internal/cli"
	"sokki/internal/config"
	"sokki/internal/model"
	"sokki/internal/usage"
)

// controlApp is a test app whose main queue runs, for the commands to
// hand their work to.
func controlApp(t *testing.T) *app {
	a := testApp(t)
	login := false
	openAtLogin = func() bool { return login }
	setOpenAtLogin = func(on bool) error { login = on; return nil }
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case fn := <-a.mainQueue:
				fn()
			case <-stop:
				return
			}
		}
	}()
	return a
}

// sokki runs the sokki command against a, and returns what it printed,
// decoded, and its exit code.
func sokki(t *testing.T, a *app, stdin string, args ...string) (any, int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cli.Main(args, cli.Env{Stdout: &out, Stderr: &errOut, Stdin: strings.NewReader(stdin),
		Call: func(ctx context.Context, req cli.Request) (json.RawMessage, error) {
			// Through JSON, as over the socket.
			raw, _ := json.Marshal(req)
			var r cli.Request
			json.Unmarshal(raw, &r)
			v, err := a.control(ctx, r)
			if err != nil {
				return nil, err
			}
			return json.Marshal(v)
		}})
	var v any
	json.Unmarshal(out.Bytes(), &v)
	return v, code, errOut.String()
}

func field(v any, name string) any {
	m, _ := v.(map[string]any)
	return m[name]
}

func TestCommandManagesMatches(t *testing.T) {
	a := controlApp(t)
	if v, code, msg := sokki(t, a, "", "match", "add", "--trigger", ":addr", "1 Main St"); code != 0 || field(v, "file") != "default.yml" {
		t.Fatalf("add: %v %d %s", v, code, msg)
	}
	if _, code, msg := sokki(t, a, "", "match", "add", "--trigger", ":addr", "again"); code != cli.ExitFailed || !strings.Contains(msg, "has the trigger") {
		t.Errorf("duplicate trigger: %d %s", code, msg)
	}
	if _, code, msg := sokki(t, a, "", "match", "add", "--trigger", "x", "text"); code != cli.ExitFailed || !strings.Contains(msg, "at least 2") {
		t.Errorf("short trigger: %d %s", code, msg)
	}
	v, code, msg := sokki(t, a, "Please review the diff.\nThanks", "prompt", "add")
	if code != 0 || field(v, "kind") != "prompt" || field(v, "file") != config.PromptFile || field(v, "title") != "Please review the diff." {
		t.Fatalf("prompt: %v %d %s", v, code, msg)
	}
	if v, code, msg := sokki(t, a, "", "match", "edit", ":addr", "--trigger", ":home", "--label", "Home", "--file", "personal"); code != 0 ||
		field(v, "trigger") != ":home" || field(v, "file") != "personal.yml" {
		t.Fatalf("edit: %v %d %s", v, code, msg)
	}
	if _, code, _ := sokki(t, a, "", "match", "disable", ":home"); code != 0 {
		t.Fatal("disable")
	}
	if m, _ := a.store.WithKey(":home"); m.Enabled || m.Label != "Home" {
		t.Errorf("stored: %+v", m)
	}
	data, _ := os.ReadFile(filepath.Join(a.dir.MatchDir(), "personal.yml"))
	if !strings.Contains(string(data), ":home") {
		t.Errorf("personal.yml: %s", data)
	}
	if v, code, _ := sokki(t, a, "", "matches", "--disabled"); code != 0 || len(v.([]any)) != 1 {
		t.Errorf("disabled: %v", v)
	}
	if v, _, _ := sokki(t, a, "", "matches", "--prompts"); len(v.([]any)) != 1 {
		t.Errorf("prompts: %v", v)
	}
	if v, code, msg := sokki(t, a, "", "match", "duplicate", ":hello", "--trigger", ":hey"); code != 0 || field(v, "replace") != "Hi there!" {
		t.Errorf("duplicate: %v %d %s", v, code, msg)
	}
	if _, code, msg := sokki(t, a, "", "match", "duplicate", ":hello"); code != cli.ExitFailed || !strings.Contains(msg, "--trigger") {
		t.Errorf("duplicate without trigger: %d %s", code, msg)
	}
	if _, code, _ := sokki(t, a, "", "match", "remove", ":hey"); code != 0 {
		t.Fatal("remove")
	}
	if _, ok := a.store.WithKey(":hey"); ok {
		t.Error("still there")
	}
	if _, code, msg := sokki(t, a, "", "match", "show", ":nope"); code != cli.ExitFailed || !strings.Contains(msg, "no match") {
		t.Errorf("show missing: %d %s", code, msg)
	}
	if v, _, _ := sokki(t, a, "", "files"); len(v.([]any)) != 3 {
		t.Errorf("files: %v", v)
	}
}

func TestCommandSearchesAndRenders(t *testing.T) {
	a := controlApp(t)
	v, code, _ := sokki(t, a, "", "search", "sig")
	if list, _ := v.([]any); code != 0 || len(list) == 0 || field(list[0], "trigger") != ":sig" {
		t.Fatalf("search: %v", v)
	}
	v, code, msg := sokki(t, a, "", "render", ":today")
	if code != 0 || field(v, "text") != time.Now().Format("2006-01-02") {
		t.Errorf("render: %v %d %s", v, code, msg)
	}
}

func TestCommandSetsSettings(t *testing.T) {
	a := controlApp(t)
	if v, code, msg := sokki(t, a, "", "config", "set", "inject_delay", "12"); code != 0 || field(v, "inject_delay") != float64(12) {
		t.Fatalf("set: %v %d %s", v, code, msg)
	}
	if got := a.dir.Load().Config.InjectDelay; got != 12 {
		t.Errorf("saved %d", got)
	}
	for _, bad := range [][]string{{"inject_delay", "5000"}, {"palette_shortcut", "cmd+x"}, {"nope", "1"}, {"preserve_clipboard", "maybe"}} {
		if _, code, _ := sokki(t, a, "", "config", "set", bad[0], bad[1]); code != cli.ExitFailed {
			t.Errorf("%v: exit %d", bad, code)
		}
	}
	if _, code, _ := sokki(t, a, "", "config", "set", "palette_shortcut", "ctrl+space"); code != 0 || a.store.Config.PaletteShortcut != "ctrl+space" {
		t.Error("palette shortcut")
	}
	if v, _, _ := sokki(t, a, "", "pause"); field(v, "enabled") != false || a.dir.Load().Config.Enabled {
		t.Errorf("pause: %v", v)
	}
	if v, _, _ := sokki(t, a, "", "resume"); field(v, "enabled") != true {
		t.Errorf("resume: %v", v)
	}
	if v, _, _ := sokki(t, a, "", "privacy", "usage-stats", "off"); field(v, "usage_stats") != false || a.prefs.Get().DailyPingOn() {
		t.Errorf("privacy: %v", v)
	}
	id := a.prefs.InstallID()
	if v, _, _ := sokki(t, a, "", "privacy", "reset-id"); field(v, "anonymous_id") == id {
		t.Error("id kept")
	}
}

func TestCommandShowsInsightsAndActivity(t *testing.T) {
	a := controlApp(t)
	a.usage.Log(usage.Event{Time: time.Now(), Trigger: ":hello", App: "com.apple.TextEdit", AppName: "TextEdit", Saved: 8})
	v, _, _ := sokki(t, a, "", "insights")
	if field(v, "expansions") != float64(1) || field(v, "characters_saved") != float64(8) {
		t.Errorf("insights: %v", v)
	}
	if v, _, _ := sokki(t, a, "", "insights", "clear"); field(v, "expansions") != float64(0) {
		t.Errorf("cleared: %v", v)
	}
	a.hud.record(model.HUDEvent{ID: 1, Time: time.Now(), Verdict: model.VerdictTooSoon, Buffer: "secret word"})
	v, _, _ = sokki(t, a, "", "activity")
	if list, _ := v.([]any); len(list) != 1 || field(list[0], "typed") != "•••••• ••••" {
		t.Errorf("masked: %v", v)
	}
	v, _, _ = sokki(t, a, "", "activity", "--reveal")
	if list, _ := v.([]any); len(list) != 1 || field(list[0], "typed") != "secret word" {
		t.Errorf("revealed: %v", v)
	}
}

func TestCommandNeedsAccountToSync(t *testing.T) {
	a := controlApp(t)
	if _, code, msg := sokki(t, a, "", "sync"); code != cli.ExitFailed || !strings.Contains(msg, "signin") {
		t.Errorf("sync: %d %s", code, msg)
	}
	if v, code, _ := sokki(t, a, "", "account"); code != 0 || field(v, "signed_in") != false {
		t.Errorf("account: %v", v)
	}
}
