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
func testApp(t *testing.T) *app {
	t.Helper()
	root := t.TempDir()
	logx.SetDir(filepath.Join(root, "logs"))
	a := &app{dir: config.Dir(filepath.Join(root, "sokki")), mainQueue: make(chan func(), 16)}
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
func shot(t *testing.T, tt *ui.Tester, name string) {
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
	tt := ui.NewTester(a.settings.view, 620, 520)
	for _, want := range []string{"Matches", ":hello", "Hi there!", ":sig", "default.yml"} {
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
	tt := ui.NewTester(a.settings.view, 620, 520)
	if err := tt.RightClick(":hello"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Disable"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	tt.Frame()
	m := a.store.Matches[0]
	if m.Trigger != ":hello" || m.Enabled {
		t.Fatalf("not disabled: %+v", m)
	}
	if !tt.HasText("Disabled") {
		t.Fatal("no Disabled badge")
	}
}

func TestDeleteAsksFirst(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.settings.view, 620, 520)
	tt.Click(":hello")
	if err := tt.Click("Remove match"); err != nil {
		t.Fatal(err)
	}
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

func TestSettingsTabs(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.settings.view, 620, 640)
	tt.Key(ui.Cmd, ui.Key2)
	for _, want := range []string{"Behavior", "Clipboard threshold (chars)", "Quick toggle", "Open palette"} {
		if !tt.HasText(want) {
			t.Fatalf("General lacks %q: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "general")
	tt.Key(ui.Cmd, ui.Key3)
	if !tt.HasText("Claude binary") || !tt.HasText("Test connection") {
		t.Fatalf("AI tab: %q", tt.Texts())
	}
	shot(t, tt, "ai")
	tt.Key(ui.Cmd, ui.Key4)
	if !tt.HasText("Telemetry is only active in packaged release builds.") {
		t.Fatalf("Privacy tab: %q", tt.Texts())
	}
	a.telemetry = &telemetry.Config{EndpointURL: "https://example.com", IngestKey: "k"}
	tt.Frame()
	if !tt.HasText("Send crash reports automatically") || !tt.HasText("Anonymous ID") {
		t.Fatalf("Privacy tab: %q", tt.Texts())
	}
	shot(t, tt, "privacy")
}

func TestGeneralSavesConfig(t *testing.T) {
	a := testApp(t)
	a.settings.tab = 1
	tt := ui.NewTester(a.settings.view, 620, 640)
	if err := tt.Click("Double Option"); err != nil {
		// The select shows its choice, None by default.
		if err := tt.Click("None"); err != nil {
			t.Fatal(err)
		}
	}
	if err := tt.Click("Double Shift"); err != nil {
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
	e.existing, e.trigger, e.file, e.enabled = 0, ":", config.DefaultMatchFile, true
	tt := ui.NewTester(e.view, 500, 480)
	if !tt.HasText("New Match") || !tt.HasText("Replacement") {
		t.Fatalf("editor: %q", tt.Texts())
	}
	shot(t, tt, "editor")
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

func TestEditorRejectsBadPrompt(t *testing.T) {
	a := testApp(t)
	e := a.editor
	e.existing, e.trigger, e.file, e.enabled, e.isAI, e.prompt = 0, ":ai", config.DefaultMatchFile, true, true, "Fix {{nope}}"
	tt := ui.NewTester(e.view, 500, 480)
	tt.Click("Save")
	if e.promptErr == "" || !tt.HasText(e.promptErr) {
		t.Fatalf("no error shown: %q", tt.Texts())
	}
	shot(t, tt, "editor-ai")
	for _, m := range a.store.Matches {
		if m.Trigger == ":ai" {
			t.Fatal("saved a bad prompt")
		}
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
	if !tt.HasText(":hello") || !tt.HasText("Hi there!") {
		t.Fatalf("palette: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyDown)
	if p.chosen != 1 {
		t.Fatalf("chosen %d", p.chosen)
	}
	shot(t, tt, "palette")
	p.query = "zzz"
	p.filter()
	tt.Frame()
	if !tt.HasText("No results") {
		t.Fatalf("empty: %q", tt.Texts())
	}
}

func TestPaletteShowsAIPrompt(t *testing.T) {
	a := testApp(t)
	prompt := "Fix the grammar: {{s|c}}"
	a.store.Add(model.Match{Trigger: ":fix", Prompt: &prompt, Label: "Fix grammar", Enabled: true})
	p := a.palette
	p.query = "fix"
	p.filter()
	tt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	if !tt.HasText("Fix grammar") || !tt.HasText("PROMPT") || !tt.HasText("AI") {
		t.Fatalf("palette: %q", tt.Texts())
	}
	shot(t, tt, "palette-ai")
}

func TestHUD(t *testing.T) {
	a := testApp(t)
	h := a.hud
	tt := ui.NewTester(h.view, 380, 420)
	if !tt.HasText("No recent activity") {
		t.Fatalf("empty: %q", tt.Texts())
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	h.record(model.HUDEvent{ID: 1, Time: now, Verdict: model.VerdictTooSoon, Buffer: "secret word"})
	h.record(model.HUDEvent{ID: 2, Time: now, Verdict: model.VerdictMatched, Buffer: ":hi", MatchedTrigger: ":hi"})
	h.record(model.HUDEvent{ID: 3, Time: now, Verdict: model.VerdictAIFailed, Buffer: ":fix", MatchedTrigger: ":fix", Note: "timeout"})
	tt.Frame()
	if !tt.HasText("•••••• ••••") || tt.HasText("secret word") || !tt.HasText("Too soon") || !tt.HasText("timeout") {
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
