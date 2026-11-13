package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
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
	shotDark(t, tt, "palette-edit-edited-dark")
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
	r, ok := tt.Find("Summarize what changed since the last release")
	if !ok {
		t.Fatalf("not shown: %q", tt.Texts())
	}
	// It fades in, then shows as the others do.
	ink := func() (n int) {
		img := tt.Image()
		s := float32(img.Bounds().Dx()) / paletteWidth
		for y := int(r.Y * s); y < int((r.Y+r.H)*s); y++ {
			for x := int(r.X * s); x < int((r.X+r.W)*s); x++ {
				n += int(img.RGBAAt(x, y).R)
			}
		}
		return n
	}
	first := ink()
	tt.Advance(arriveFor / 6)
	shot(t, tt, "palette-history-arriving")
	mid := ink()
	tt.Advance(arriveFor)
	last := ink()
	if first == mid || mid == last {
		t.Fatalf("no fade: %d, %d, %d", first, mid, last)
	}
	if len(p.arrived) != 0 || tt.FramePending() {
		t.Fatalf("still arriving: %v", p.arrived)
	}
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
	for _, want := range []string{"Agent History", "3 prompts from 3 agents", "~/.claude*/history.jsonl", "2 prompts", "Shorter than"} {
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

func TestPaletteGroupsHistoryByDay(t *testing.T) {
	a := testApp(t)
	y, m, d := time.Now().Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	a.history.Import([]history.Item{
		{Source: "claude", Text: "Sent early this morning, a prompt", Time: midnight},
		{Source: "codex", Text: "Sent last night, before midnight", Time: midnight.Add(-time.Hour)},
		{Source: "codex", Text: "Sent last night, before midnight", Time: midnight.Add(-2 * time.Hour)},
		{Source: "gemini", Text: "Sent days ago, and often since", Time: midnight.Add(-30 * time.Hour)},
		{Source: "gemini", Text: "Sent days ago, and often since", Time: midnight.Add(-50 * time.Hour)},
		{Source: "gemini", Text: "Sent days ago, and often since", Time: midnight.Add(-70 * time.Hour)},
		{Source: "qoder", Text: "Sent the day before yesterday", Time: midnight.Add(-26 * time.Hour)},
	}, nil)
	p := a.palette
	tt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	tt.Key(ui.Cmd, ui.Key3)
	// The most used first, but under the day it was last sent.
	if want := []int{-1 - dayToday, 0, -1 - dayYesterday, 1, -1 - dayEarlier, 2, 3}; !slices.Equal(p.days, want) {
		t.Fatalf("rows %v", p.days)
	}
	if p.results[2].e.Count != 3 {
		t.Fatalf("earlier not by use: %+v", p.results[2].e)
	}
	for _, want := range []string{"Today", "Yesterday", "Earlier"} {
		if !tt.HasText(want) {
			t.Fatalf("lacks %q: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "palette-history-days")
	shotDark(t, tt, "palette-history-days-dark")
	// The keys go from prompt to prompt, over the headings.
	for i := 1; i < 4; i++ {
		tt.Key(0, ui.KeyDown)
		if p.chosen != i {
			t.Fatalf("down %d chose %d", i, p.chosen)
		}
	}
	tt.Key(0, ui.KeyDown)
	if p.chosen != 3 {
		t.Fatalf("down past the end chose %d", p.chosen)
	}
	tt.Key(0, ui.KeyUp)
	if r, _ := p.selected(); r.e.Text != "Sent days ago, and often since" {
		t.Fatalf("up chose %q", r.e.Text)
	}
	// A search ranks without days, as do the other scopes.
	p.query = "sent"
	p.filter()
	if p.days != nil {
		t.Fatal("days in a search")
	}
	p.query = ""
	tt.Key(ui.Cmd, ui.Key1)
	if p.days != nil {
		t.Fatal("days in All")
	}
}

func TestReadNowSaysWhatItFound(t *testing.T) {
	a := testApp(t)
	withHistory(a)
	tt := ui.NewTester(a.settings.view, 780, 900)
	a.settings.setPage(historyPage)
	tt.Frame()
	a.lastRead = historyRead{found: 2, at: time.Now(), fresh: true}
	tt.Frame()
	tt.Advance(settle)
	if !tt.HasText("2 new prompts read just now.") {
		t.Fatalf("no news: %q", tt.Texts())
	}
	shot(t, tt, "history-page-found")
	shotDark(t, tt, "history-page-found-dark")
	tt.Advance(readNoteFor)
	if tt.HasText("2 new prompts read just now.") || !tt.HasText("Read just now. Checked again every 2 minutes.") {
		t.Fatalf("news stays: %q", tt.Texts())
	}
	a.lastRead = historyRead{at: time.Now(), fresh: true}
	tt.Frame()
	if !tt.HasText("Up to date: nothing new since the last reading.") {
		t.Fatalf("not up to date: %q", tt.Texts())
	}
	// A reading Settings did not show at the time says nothing later.
	tt.Advance(readNoteFor)
	a.lastRead = historyRead{found: 5, at: time.Now().Add(-time.Minute), fresh: true}
	tt.Frame()
	if tt.HasText("5 new prompts read just now.") {
		t.Fatalf("stale news: %q", tt.Texts())
	}

	// The palette's footer says what came in.
	p := a.palette
	p.filter()
	pt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	p.news = 3
	pt.Frame()
	pt.Advance(settle)
	if !pt.HasText("3 new prompts from your agents") {
		t.Fatalf("no footer news: %q", pt.Texts())
	}
	shot(t, pt, "palette-history-news")
}

func TestHistoryPageSaysWhatIsNotFound(t *testing.T) {
	a := testApp(t)
	p := filepath.Join(a.historyHome, ".codex", "history.jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"ts":1785809189,"text":"Bump the version and tag the release"}`+"\n"), 0o644)
	a.history.Scan(a.historyEnv())
	tt := ui.NewTester(a.settings.view, 780, 900)
	a.settings.setPage(historyPage)
	tt.Frame()
	tt.SetDark(true)
	tt.Frame()
	tt.Advance(settle)
	if n := strings.Count(strings.Join(tt.Texts(), "\n"), "Not found"); n != len(history.Sources)-1 {
		t.Fatalf("%d not found: %q", n, tt.Texts())
	}
	shot(t, tt, "history-page-not-found-dark")
	tt.SetDark(false)
	tt.Frame()
	tt.Advance(settle)
	shot(t, tt, "history-page-not-found")

	// The Finder shows the file read, or where an agent would keep it.
	var shown []string
	showInFinder = func(path string) { shown = append(shown, path) }
	defer func() { showInFinder = mygo.Shell.ShowItemInFolder }()
	if err := tt.Click("Show Codex’s History in Finder"); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(a.historyHome, ".gemini", "tmp"), 0o755)
	a.history.Scan(a.historyEnv())
	tt.Frame()
	if err := tt.Click("Show Where Gemini CLI Keeps Its History in Finder"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("Show Where Claude Code Keeps Its History in Finder") {
		t.Fatal("a way to the home folder")
	}
	if want := []string{p, filepath.Join(a.historyHome, ".gemini", "tmp")}; !slices.Equal(shown, want) {
		t.Fatalf("shown %q", shown)
	}
}

func TestHistoryPageSaysTheNewestDay(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.settings.view, 780, 900)
	a.settings.setPage(historyPage)
	tt.Frame()
	if !tt.HasText("No prompts yet") {
		t.Fatalf("not empty: %q", tt.Texts())
	}
	now := time.Now()
	a.history.Import([]history.Item{
		{Source: "claude", Text: "Sent a week ago, one of two", Time: now.AddDate(0, 0, -7)},
		{Source: "claude", Text: "Sent a week ago, two of two", Time: now.AddDate(0, 0, -7)},
		{Source: "claude", Text: "Sent two weeks ago, alone", Time: now.AddDate(0, 0, -14)},
	}, nil)
	tt.Frame()
	if want := "2 on " + now.AddDate(0, 0, -7).Format("Jan 2"); !tt.HasText(want) || !tt.HasText("3 prompts from Claude Code") {
		t.Fatalf("lacks %q: %q", want, tt.Texts())
	}
	a.history.Import([]history.Item{{Source: "codex", Text: "Sent just now, and new", Time: now}}, nil)
	tt.Frame()
	tt.Advance(settle)
	if !tt.HasText("1 today") || !tt.HasText("4 prompts from 2 agents") {
		t.Fatalf("not today: %q", tt.Texts())
	}
	shot(t, tt, "history-page-today")
}

func TestPaletteSaysWhyHistoryIsEmpty(t *testing.T) {
	a := testApp(t)
	p := a.palette
	tt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	tt.Key(ui.Cmd, ui.Key3)
	if !tt.HasText("No Agent History") {
		t.Fatalf("lacks the empty state: %q", tt.Texts())
	}
	a.history.Scan(a.historyEnv())
	tt.Frame()
	if !tt.HasText("No Agents Found") {
		t.Fatalf("agents found: %q", tt.Texts())
	}
	shot(t, tt, "palette-history-no-agents")
	a.scanning.Store(true)
	tt.Frame()
	if !tt.HasText("Reading Your Agents…") {
		t.Fatalf("not reading: %q", tt.Texts())
	}
	a.scanning.Store(false)

	withHistory(a)
	p.query = "nothing like this"
	p.filter()
	tt.Frame()
	tt.Advance(settle)
	if !tt.HasText("Nothing in the agents’ history matches “nothing like this”.") {
		t.Fatalf("lacks no results: %q", tt.Texts())
	}
	shot(t, tt, "palette-history-no-results")
	shotDark(t, tt, "palette-history-no-results-dark")
	if err := tt.Click("Search Everything"); err != nil {
		t.Fatal(err)
	}
	if p.scope != scopeAll {
		t.Fatalf("scope %d", p.scope)
	}

	p.query = ""
	tt.Key(ui.Cmd, ui.Key3)
	for _, e := range a.history.Entries() {
		a.history.Hide(e.ID)
	}
	p.filter()
	tt.Frame()
	if !tt.HasText("Every Prompt Is Hidden") {
		t.Fatalf("not hidden: %q", tt.Texts())
	}
	st := a.history.Settings()
	for _, src := range history.Sources {
		st.Off = append(st.Off, src.ID)
	}
	a.history.SetSettings(st)
	tt.Frame()
	if !tt.HasText("Every Agent Is Off") {
		t.Fatalf("not off: %q", tt.Texts())
	}
}

func TestReadNowAskedAsAReadingRuns(t *testing.T) {
	a := testApp(t)
	a.scanning.Store(true)
	a.readHistoryNow()
	if !a.readAsked.Load() {
		t.Fatal("the ask was lost")
	}
}

func TestPreviewMarksWhereTheSearchFound(t *testing.T) {
	a := testApp(t)
	var b strings.Builder
	for i := range 50 {
		fmt.Fprintf(&b, "Step %d: read the next part of the log and say what it does.\n", i)
		if i == 30 {
			b.WriteString("Then write the summary of the incident for the team.\n")
		}
	}
	a.history.Import([]history.Item{{Source: "claude", Text: b.String(), Time: time.Now()}}, nil)
	p := a.palette
	tt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	tt.Key(ui.Cmd, ui.Key3)
	tt.Frame()
	if p.previewScroll.Y != 0 || p.hits.ranges != nil {
		t.Fatalf("scrolled %v without a query", p.previewScroll.Y)
	}
	p.query = "incident"
	p.filter()
	tt.Frame()
	tt.Advance(settle)
	if len(p.hits.ranges) != 1 || p.previewScroll.Y < 600 || p.previewScroll.Y >= p.previewScroll.MaxY {
		t.Fatalf("hits %v, scrolled %v", p.hits.ranges, p.previewScroll.Y)
	}
	if !tt.HasText("incident") {
		t.Fatalf("match not in view: %q", tt.Texts())
	}
	shot(t, tt, "palette-history-preview-hits")
	shotDark(t, tt, "palette-history-preview-hits-dark")
}

// shotDark saves a shot of what tt shows in dark mode, then goes back to
// light.
func shotDark(t *testing.T, tt *ui.Tester, name string) {
	if os.Getenv("SOKKI_SHOTS") == "" {
		return
	}
	tt.SetDark(true)
	tt.Frame()
	tt.Advance(settle)
	shot(t, tt, name)
	tt.SetDark(false)
	tt.Frame()
	tt.Advance(settle)
}
