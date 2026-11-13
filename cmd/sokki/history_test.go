package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sokki/internal/history"
)

// withHistory gives a's history prompts sent in agents, as if read.
func withHistory(a *app) {
	now := time.Now()
	a.history.Import([]history.Item{
		{Source: "claude", Text: "Review this diff for bugs and be brief", Project: "/p/sokki", Time: now.Add(-time.Hour)},
		{Source: "codex", Text: "Review this diff for bugs and be brief", Project: "/p/sokki", Time: now.Add(-30 * time.Minute)},
		{Source: "codex", Text: "Write the changelog entry for this change", Project: "/p/web", Time: now.Add(-48 * time.Hour)},
		{Source: "gemini", Text: "Explain the failing test\nthen fix it", Time: now.Add(-20 * time.Hour)},
	}, nil)
}

func TestPaletteFindsAgentHistory(t *testing.T) {
	a := testApp(t)
	withHistory(a)
	p := a.palette
	p.filter()
	matches := len(a.store.Matches)
	if len(p.results) != matches+3 {
		t.Fatalf("results %d", len(p.results))
	}
	// Without a query, the most used and latest come first.
	if r := p.results[0]; !r.entry || r.e.Count != 2 {
		t.Fatalf("first %+v", r)
	}
	tt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	for _, want := range []string{"Review this diff for bugs and be brief", "Claude Code +1", "Save as Prompt", "Hide", "Sent twice"} {
		if !tt.HasText(want) {
			t.Fatalf("palette lacks %q: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "palette-history")
	tt.SetDark(true)
	tt.Frame()
	shot(t, tt, "palette-history-dark")
	tt.SetDark(false)

	for q, want := range map[string]int{"changelog": 1, "@gemini": 1, "#web": 1, "@codex review": 1, "review diff": 1} {
		p.query = q
		p.filter()
		if len(p.results) != want {
			t.Fatalf("%q: %d results", q, len(p.results))
		}
	}

	// The scopes.
	p.query = ""
	tt.Key(ui.Cmd, ui.Key2)
	if len(p.results) != matches || p.results[0].entry {
		t.Fatalf("matches scope: %d", len(p.results))
	}
	tt.Key(ui.Cmd, ui.Key3)
	if len(p.results) != 3 {
		t.Fatalf("history scope: %d", len(p.results))
	}
	shot(t, tt, "palette-history-scope")
	a.history.Clear(false)
	p.filter()
	tt.Frame()
	if !tt.HasText("No Agent History") {
		t.Fatalf("empty history: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.Key1)
	if p.scope != scopeAll || len(p.results) != matches {
		t.Fatalf("all scope: %d", len(p.results))
	}
}

func TestPaletteHidesAnEntry(t *testing.T) {
	a := testApp(t)
	withHistory(a)
	p := a.palette
	p.filter()
	tt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	id := p.results[0].id()
	tt.Key(ui.Cmd, ui.KeyBackspace)
	if _, ok := a.history.Entry(id); !ok || len(a.history.Entries()) != 2 {
		t.Fatal("not hidden")
	}
	if !tt.HasText("Hidden from history · ⌘Z to undo") {
		t.Fatalf("no undo: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if len(a.history.Entries()) != 3 {
		t.Fatal("not shown again")
	}
	if r, _ := p.selected(); r.id() != id {
		t.Fatal("not chosen again")
	}
	// A match is not hidden by it.
	tt.Key(ui.Cmd, ui.Key2)
	tt.Key(ui.Cmd, ui.KeyBackspace)
	if len(p.results) != len(a.store.Matches) {
		t.Fatal("a match went")
	}
}

func TestPaletteEditsBeforeInserting(t *testing.T) {
	a := testApp(t)
	withHistory(a)
	p := a.palette
	p.filter()
	tt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	tt.Key(0, ui.KeyTab)
	if p.mode != modeEdit || p.edit.text != "Review this diff for bugs and be brief" {
		t.Fatalf("mode %d, text %q", p.mode, p.edit.text)
	}
	tt.Advance(settle)
	for _, want := range []string{"Edit, Then Insert", "Insert and Send", "The history keeps the prompt as it was sent."} {
		if !tt.HasText(want) {
			t.Fatalf("edit lacks %q: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "palette-edit")
	p.edit.text += ", and list them"
	tt.Advance(settle)
	if !tt.HasText("Edited") {
		t.Fatalf("not edited: %q", tt.Texts())
	}
	shot(t, tt, "palette-edit-edited")
	if err := tt.Click("Revert"); err != nil {
		t.Fatal(err)
	}
	if p.edit.text != "Review this diff for bugs and be brief" {
		t.Fatalf("not reverted: %q", p.edit.text)
	}
	tt.Key(0, ui.KeyEscape)
	if p.mode != modeSearch {
		t.Fatal("escape did not go back")
	}

	// A match is edited with its variables filled in.
	tt.Key(ui.Cmd, ui.Key2)
	p.query = "sig"
	p.filter()
	tt.Key(0, ui.KeyTab)
	if p.mode != modeEdit || p.edit.of.entry || p.edit.text == "" {
		t.Fatalf("match edit %+v", p.edit)
	}
}

func TestPaletteFadesInWhatArrives(t *testing.T) {
	a := testApp(t)
	withHistory(a)
	p := a.palette
	p.filter()
	tt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	tt.Key(0, ui.KeyDown)
	chosen, _ := p.selected()
	a.history.Import([]history.Item{{Source: "qoder", Text: "Summarize what changed since the last release", Time: time.Now()}}, nil)
	p.refilter()
	if len(p.arrived) != 1 {
		t.Fatalf("arrived %v", p.arrived)
	}
	if r, _ := p.selected(); r.id() != chosen.id() {
		t.Fatal("the choice moved")
	}
	tt.Frame()
	if !tt.HasText("Summarize what changed since the last release") {
		t.Fatalf("not shown: %q", tt.Texts())
	}
	tt.Advance(settle)
	shot(t, tt, "palette-history-arrived")
}

func TestPaletteSavesAnEntryAsAPrompt(t *testing.T) {
	a := testApp(t)
	withHistory(a)
	p := a.palette
	p.filter()
	tt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	tt.Key(ui.Cmd, ui.KeyS)
	if p.mode != modeAdd || p.add.body != "Review this diff for bugs and be brief" || p.add.label == "" {
		t.Fatalf("form %+v", p.add)
	}
	tt.Advance(settle)
	if !tt.HasText("From Claude Code, Codex") {
		t.Fatalf("no source: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	if p.mode != modeSearch {
		t.Fatalf("not saved: %q", p.addProblem())
	}
	// It shows once, as the prompt.
	n := 0
	for _, r := range p.results {
		if r.text() == "Review this diff for bugs and be brief" {
			n++
			if r.entry {
				t.Fatal("shows as history")
			}
		}
	}
	if n != 1 {
		t.Fatalf("shows %d times", n)
	}
}

func TestPaletteRanksByUse(t *testing.T) {
	a := testApp(t)
	p := a.palette
	p.query = ":" // every trigger, as well as each other
	p.filter()
	first := p.results[0].m.Key()
	var other string
	for _, r := range p.results[1:] {
		other = r.m.Key()
	}
	for range 5 {
		a.history.BumpMatch(other)
	}
	p.filter()
	if p.results[0].m.Key() != other {
		t.Fatalf("used %s not first over %s: %v", other, first, p.results[0].m.Key())
	}
}

func TestHistoryPage(t *testing.T) {
	a := testApp(t)
	withHistory(a)
	tt := ui.NewTester(a.settings.view, 780, 900)
	a.settings.setPage(historyPage)
	tt.Frame()
	for _, want := range []string{"Agent History", "3 prompts from 3 agents", "~/.claude/history.jsonl", "2 prompts", "Shorter than"} {
		if !tt.HasText(want) {
			t.Fatalf("page lacks %q: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "history-page")
	a.scanning.Store(true)
	tt.Frame()
	tt.Advance(settle)
	if !tt.HasText("Reading the agents’ history…") {
		t.Fatalf("not reading: %q", tt.Texts())
	}
	shot(t, tt, "history-page-reading")
	a.scanning.Store(false)
	tt.Frame()
	tt.Advance(settle)
	if err := tt.Click("Read Claude Code"); err != nil {
		t.Fatal(err)
	}
	if a.history.Settings().On("claude") {
		t.Fatal("claude still on")
	}
	a.settings.historyForm.ignore = "changelog"
	tt.Frame()
	if err := tt.Click("Apply"); err != nil {
		t.Fatal(err)
	}
	if len(a.history.Entries()) != 2 {
		t.Fatalf("ignore not applied: %d", len(a.history.Entries()))
	}
}

func TestHistoryCommand(t *testing.T) {
	a := controlApp(t)
	p := filepath.Join(a.historyHome, ".codex", "history.jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"ts":1785809189,"text":"Bump the version and tag the release"}`+"\n"), 0o644)

	out, code, errOut := sokki(t, a, "", "history", "import")
	if code != 0 || out.(map[string]any)["new"] != float64(1) {
		t.Fatalf("import %v %d %s", out, code, errOut)
	}
	out, code, _ = sokki(t, a, "", "history", "bump")
	rows := out.([]any)
	if code != 0 || len(rows) != 1 {
		t.Fatalf("search %v", out)
	}
	id := rows[0].(map[string]any)["id"].(string)
	if rows[0].(map[string]any)["agents"].([]any)[0] != "Codex" {
		t.Fatalf("agents %v", rows[0])
	}
	if out, code, _ = sokki(t, a, "", "history", "show", id[:4]); code != 0 || out.(map[string]any)["text"] != "Bump the version and tag the release" {
		t.Fatalf("show %v", out)
	}
	if _, code, _ = sokki(t, a, "", "history", "hide", id); code != 0 || len(a.history.Entries()) != 0 {
		t.Fatal("hide")
	}
	if _, code, _ = sokki(t, a, "", "history", "unhide", id); code != 0 || len(a.history.Entries()) != 1 {
		t.Fatal("unhide")
	}
	if _, code, errOut = sokki(t, a, "", "history", "show", "zzzz"); code != 1 || errOut == "" {
		t.Fatalf("unknown id: %d", code)
	}
	if _, code, _ = sokki(t, a, "", "history", "off", "codex"); code != 0 || a.history.Settings().On("codex") {
		t.Fatal("off")
	}
	if _, code, errOut = sokki(t, a, "", "history", "off", "nope"); code != 1 {
		t.Fatalf("unknown agent: %s", errOut)
	}
	out, _, _ = sokki(t, a, "", "history", "sources")
	if len(out.(map[string]any)["sources"].([]any)) != len(history.Sources) {
		t.Fatalf("sources %v", out)
	}
	if _, code, _ = sokki(t, a, "", "history", "clear"); code != 0 || len(a.history.Entries()) != 0 {
		t.Fatal("clear")
	}
}
