package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sokki/internal/config"
	"sokki/internal/expander"
	"sokki/internal/logx"
	"sokki/internal/model"
	"sokki/internal/prefs"
	"sokki/internal/telemetry"
)

// testApp is an app over a temporary configuration, with no windows: the
// views are run by testers.
func testApp(t testing.TB) *app {
	t.Helper()
	root := t.TempDir()
	logx.SetDir(filepath.Join(root, "logs"))
	a := &app{dir: config.Dir(filepath.Join(root, "sokki")), mainQueue: make(chan func(), 16), trusted: true}
	a.dir.Bootstrap()
	a.store = config.NewStore(a.dir)
	a.recency = config.NewRecency(a.dir)
	a.prefs = prefs.Open(filepath.Join(root, "prefs.json"))
	a.exp = expander.New(expander.Handlers{})
	a.store.OnMatchesChanged = func() { a.exp.SetMatches(a.store.Matches) }
	a.settings = newSettingsWindow(a)
	a.editor = &editorWindow{a: a}
	a.palette = newPalette(a)
	a.hud = newHUDPanel(a)
	a.store.Load()
	return a
}

// shot saves what the tester shows to $SOKKI_SHOTS/name.png, when set.
func shot(t testing.TB, tt *ui.Tester, name string) {
	dir := os.Getenv("SOKKI_SHOTS")
	if dir == "" {
		return
	}
	os.MkdirAll(dir, 0o755)
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, tt.Image())
}

func TestMatchesTab(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.settings.view, 780, 560)
	for _, want := range []string{"All Matches", ":hello", "Hi there!", ":sig", "default.yml"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q in %q", want, tt.Texts())
		}
	}
	shot(t, tt, "matches")

	// Searching filters.
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	tt.Type("today")
	if tt.HasText(":hello") || !tt.HasText(":today") {
		t.Fatalf("search did not filter: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	if !tt.HasText(":hello") {
		t.Fatalf("escape did not clear: %q", tt.Texts())
	}
}

func TestMatchesContextMenuDisables(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.settings.view, 780, 560)
	if err := tt.RightClick(":hello"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Turn Off"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	tt.Frame()
	m := a.store.Matches[0]
	if m.Trigger != ":hello" || m.Enabled {
		t.Fatalf("not disabled: %+v", m)
	}
	if !tt.HasText("Off") {
		t.Fatal("no Off badge")
	}
}

func TestDeleteAsksFirst(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.settings.view, 780, 560)
	if err := tt.RightClick(":hello"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Delete…"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	tt.Frame()
	if !tt.HasText("Delete Match?") {
		t.Fatalf("no confirmation: %q", tt.Texts())
	}
	shot(t, tt, "delete")
	tt.Click("Delete")
	tt.Frame()
	for _, m := range a.store.Matches {
		if m.Trigger == ":hello" {
			t.Fatal("not deleted")
		}
	}
}

func TestSettingsPages(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.settings.view, 780, 640)
	tt.Key(ui.Cmd, ui.Key2)
	for _, want := range []string{"General", "Launch at login", "Paste longer than", "Overlap delay", "Log level"} {
		if !tt.HasText(want) {
			t.Fatalf("General lacks %q: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "general")
	tt.Key(ui.Cmd, ui.Key3)
	for _, want := range []string{"Search matches", "Pause or resume", "Recent Activity"} {
		if !tt.HasText(want) {
			t.Fatalf("Shortcuts lacks %q: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "shortcuts")
	tt.Key(ui.Cmd, ui.Key4)
	if !tt.HasText("Telemetry is off in this build") {
		t.Fatalf("Privacy: %q", tt.Texts())
	}
	a.telemetry = &telemetry.Config{EndpointURL: "https://example.com", IngestKey: "k"}
	tt.Frame()
	if !tt.HasText("Send crash reports") || !tt.HasText("Anonymous ID") {
		t.Fatalf("Privacy: %q", tt.Texts())
	}
	shot(t, tt, "privacy")
	tt.Key(ui.Cmd, ui.Key5)
	if !tt.HasText("Sokki") || !tt.HasText("Release Notes") {
		t.Fatalf("About: %q", tt.Texts())
	}
	shot(t, tt, "about")
	tt.SetDark(true)
	tt.Key(ui.Cmd, ui.Key1)
	shot(t, tt, "matches-dark")
}

func TestSidebarShowsAFile(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.settings.view, 780, 560)
	if err := tt.Click("default.yml"); err != nil {
		t.Fatal(err)
	}
	if a.settings.page != filePage+"default.yml" || a.settings.matches.file != "default.yml" {
		t.Fatalf("page %q, file %q", a.settings.page, a.settings.matches.file)
	}
	if !tt.HasText("4 matches") {
		t.Fatalf("file page: %q", tt.Texts())
	}
}

func TestGeneralSavesConfig(t *testing.T) {
	a := testApp(t)
	a.settings.setPage(shortcutsPage)
	tt := ui.NewTester(a.settings.view, 780, 640)
	// The selects show their choice: only the toggle's is None.
	a.store.Config.PaletteShortcut, a.store.Config.HUDShortcut = "opt+space", "double_control"
	tt.Frame()
	if err := tt.Click("None"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	if err := tt.Click("Double-tap ⇧ Shift"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	if a.store.Config.ToggleShortcut != "double_shift" {
		t.Fatalf("toggle %q", a.store.Config.ToggleShortcut)
	}
	if again := a.dir.Load(); again.Config.ToggleShortcut != "double_shift" {
		t.Fatalf("not saved: %q", again.Config.ToggleShortcut)
	}
}

func TestEditorSavesNewMatch(t *testing.T) {
	a := testApp(t)
	e := a.editor
	e.existing, e.trigger, e.file, e.enabled, e.title = 0, ":", config.DefaultMatchFile, true, "New Match"
	tt := ui.NewTester(e.view, 560, 540)
	tt.SetTitleBar(ui.TitleBar{Height: 28, Left: 78})
	if !tt.HasText("Trigger") || !tt.HasText("Replacement") {
		t.Fatalf("editor: %q", tt.Texts())
	}
	shot(t, tt, "editor")
	e.trigger, e.replace = ":hello", "Today is {{date:yyyy}}"
	tt.Frame()
	if !tt.HasText("Another match in default.yml has this trigger.") || !tt.HasText("Preview") {
		t.Fatalf("editor: %q", tt.Texts())
	}
	shot(t, tt, "editor-preview")
	e.trigger = ":bye"
	e.replace = "Goodbye!"
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	m := a.store.Matches[len(a.store.Matches)-1]
	if m.Trigger != ":bye" || m.Replace != "Goodbye!" || m.SourceFile != "default.yml" {
		t.Fatalf("saved %+v", m)
	}
}

func TestEditorInsertsAtCaret(t *testing.T) {
	a := testApp(t)
	e := a.editor
	e.trigger, e.replace, e.file, e.enabled = ":x", "ab", config.DefaultMatchFile, true
	e.insert("{{clipboard}}") // never focused: at the end
	if e.replace != "ab{{clipboard}}" {
		t.Fatalf("replace %q", e.replace)
	}
	e.selStart, e.selEnd, e.selKnown = 1, 1, true
	e.insert("é")
	if e.replace != "aéb{{clipboard}}" || e.caret != 2 {
		t.Fatalf("replace %q caret %d", e.replace, e.caret)
	}
	tt := ui.NewTester(e.view, 560, 540)
	tt.Frame()
	if !tt.HasText("Preview") {
		t.Fatalf("editor: %q", tt.Texts())
	}
}

func TestNewFileNames(t *testing.T) {
	files := []string{"default.yml", "Work.yaml"}
	cases := map[string]string{"notes": "notes.yml", "x.YAML": "x.YAML", ".hidden": "", "a/b": "", "  ": ""}
	for in, want := range cases {
		if got := NormalizeFilename(in); got != want {
			t.Errorf("NormalizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
	if got := existingFile("work", files); got != "Work.yaml" {
		t.Fatalf("existing %q", got)
	}
}

func TestPaletteFiltersAndRanks(t *testing.T) {
	a := testApp(t)
	p := a.palette
	p.query = "sig"
	p.filter()
	if len(p.results) == 0 || p.results[0].Trigger != ":sig" {
		t.Fatalf("results %+v", p.results)
	}
	p.query = ""
	p.filter()
	if len(p.results) != 4 {
		t.Fatalf("all: %d", len(p.results))
	}
	tt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	if !tt.HasText(":hello") || !tt.HasText("Hi there!") || !tt.HasText("Insert") {
		t.Fatalf("palette: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyDown)
	if p.chosen != 1 {
		t.Fatalf("chosen %d", p.chosen)
	}
	shot(t, tt, "palette")
	p.query = "hi"
	p.filter()
	tt.Frame()
	shot(t, tt, "palette-search")
	p.query = "zzz"
	p.filter()
	tt.Frame()
	if !tt.HasText("No Results") {
		t.Fatalf("empty: %q", tt.Texts())
	}
	shot(t, tt, "palette-empty")
	tt.Key(0, ui.KeyEscape)
	if p.query != "" || len(p.results) != 4 {
		t.Fatalf("escape did not clear: %q", p.query)
	}

	// An edit of the matches is searched at once.
	m := a.store.Matches[0]
	m.Label = "Greeting"
	a.store.Update(m)
	p.query = "greet"
	p.filter()
	if len(p.results) != 1 || p.results[0].Trigger != ":hello" {
		t.Fatalf("edited label not searched: %+v", p.results)
	}
}

func TestMatchesDetail(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.settings.view, 780, 560)
	for _, want := range []string{"Replacement", "Typed key by key", "Edit…"} {
		if !tt.HasText(want) {
			t.Fatalf("detail lacks %q: %q", want, tt.Texts())
		}
	}
	// The chosen match's switch turns it off.
	if err := tt.Click("Enabled"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m := a.store.Matches[0]; m.Enabled {
		t.Fatalf("not turned off: %+v", m)
	}
	// A match with variables shows them filled in.
	if err := tt.Click("{{date:yyyy-MM-dd}}"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Preview") {
		t.Fatalf("no preview for :today: %q", tt.Texts())
	}
	shot(t, tt, "matches-variables")
}

func TestHUD(t *testing.T) {
	a := testApp(t)
	h := a.hud
	tt := ui.NewTester(h.view, 380, 460)
	tt.SetTitleBar(ui.TitleBar{Height: 28, Left: 58})
	if !tt.HasText("No Recent Activity") {
		t.Fatalf("empty: %q", tt.Texts())
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	h.record(model.HUDEvent{ID: 1, Time: now, Verdict: model.VerdictTooSoon, Buffer: "secret word"})
	h.record(model.HUDEvent{ID: 2, Time: now, Verdict: model.VerdictMatched, Buffer: ":hi", MatchedTrigger: ":hi"})
	h.record(model.HUDEvent{ID: 3, Time: now, Verdict: model.VerdictMatched, Buffer: ":fix", MatchedTrigger: ":fix", Note: "timeout"})
	tt.Frame()
	if !tt.HasText("•••••• ••••") || tt.HasText("secret word") || !tt.HasText("Too soon after a paste") || !tt.HasText("timeout") {
		t.Fatalf("hud: %q", tt.Texts())
	}
	shot(t, tt, "hud")
	tt.Click("•••••• ••••")
	if !tt.HasText("secret word") {
		t.Fatalf("click did not reveal: %q", tt.Texts())
	}
	for range hudCapacity + 5 {
		h.record(model.HUDEvent{ID: model.NewID(), Time: now, Verdict: model.VerdictMatched})
	}
	if len(h.events) != hudCapacity {
		t.Fatalf("kept %d", len(h.events))
	}
}

func TestTrayIcon(t *testing.T) {
	img, err := png.Decode(strings.NewReader(string(trayIcon(1))))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 36 {
		t.Fatalf("size %v", img.Bounds())
	}
	inked := 0
	for y := range 36 {
		for x := range 36 {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				inked++
			}
		}
	}
	if inked < 100 || inked > 900 {
		t.Fatalf("inked %d pixels", inked)
	}
	if dir := os.Getenv("SOKKI_SHOTS"); dir != "" {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "tray.png"), trayIcon(1), 0o644)
	}
}

func TestAccessibilityNeeded(t *testing.T) {
	a := testApp(t)
	a.trusted = false
	tt := ui.NewTester(a.settings.view, 780, 560)
	for _, want := range []string{"Sokki needs Accessibility access", "Open System Settings", "Needs access", "Grant Access…"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q in %q", want, tt.Texts())
		}
	}
	if st := a.status(); !st.attention || st.on {
		t.Fatalf("status %+v", st)
	}
	shot(t, tt, "needs-access")
	a.trusted = true
	tt.Frame()
	if tt.HasText("Open System Settings") || !tt.HasText("Sokki is on") {
		t.Fatalf("still asks: %q", tt.Texts())
	}
	// Its switch pauses Sokki, and the pause is saved.
	if err := tt.Click("Expand triggers"); err != nil {
		t.Fatal(err)
	}
	if a.store.Config.Enabled || a.dir.Load().Config.Enabled {
		t.Fatal("not paused")
	}
	if !tt.HasText("Paused") {
		t.Fatalf("status: %q", tt.Texts())
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local)
	cases := map[time.Duration]string{
		10 * time.Second:    "just now",
		5 * time.Minute:     "5 minutes ago",
		1 * time.Hour:       "1 hour ago",
		30 * time.Hour:      "yesterday",
		72 * time.Hour:      "3 days ago",
		30 * 24 * time.Hour: "September 18",
	}
	for d, want := range cases {
		if got := ago(now.Add(-d), now); got != want {
			t.Errorf("ago(%v) = %q, want %q", d, got, want)
		}
	}
}
