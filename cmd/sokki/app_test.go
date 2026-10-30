package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"sokki/internal/cloudsync"
	"sokki/internal/config"
	"sokki/internal/expander"
	"sokki/internal/logx"
	"sokki/internal/model"
	"sokki/internal/mon"
	"sokki/internal/prefs"
	"sokki/internal/telemetry"
	"sokki/internal/usage"
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
	a.usage = usage.Open(root)
	a.prefs = prefs.Open(filepath.Join(root, "prefs.json"))
	a.exp = expander.New(expander.Handlers{})
	a.store.OnMatchesChanged = func() { a.exp.SetMatches(a.store.Matches) }
	a.settings = newSettingsWindow(a)
	a.editor = &editorWindow{a: a, headless: true}
	a.palette = newPalette(a)
	a.hud = newHUDPanel(a)
	a.cloud = newCloud(a)
	a.store.Load()
	return a
}

// settle is long enough for the palette's motion to end.
const settle = 300 * time.Millisecond

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

func TestDeleteUndoes(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.settings.view, 780, 560)
	if err := tt.RightClick(":paste"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatalf("%v: %q", err, tt.Menu())
	}
	tt.Frame()
	if len(a.store.Matches) != 3 {
		t.Fatalf("not deleted: %+v", a.store.Matches)
	}
	if !tt.HasText("Deleted :paste") {
		t.Fatalf("no undo toast: %q", tt.Texts())
	}
	tt.Advance(500 * time.Millisecond)
	shot(t, tt, "delete")
	if err := tt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.store.Matches) != 4 || a.store.Matches[2].Trigger != ":paste" {
		t.Fatalf("not put back in place: %+v", a.store.Matches)
	}
}

func TestDuplicateOpensEditor(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.settings.view, 780, 560)
	tt.Key(ui.Cmd, ui.KeyD)
	tt.Frame()
	e := a.editor
	if e.existing != 0 || e.trigger != ":hello" || e.replace != "Hi there!" || e.title() != "Duplicate Match" {
		t.Fatalf("not a duplicate: %+v", e)
	}
	et := ui.NewTester(e.view, 560, 540)
	if !et.HasText("Another match in default.yml has this trigger.") {
		t.Fatalf("no conflict warning: %q", et.Texts())
	}
}

func TestEditorAsksBeforeDiscarding(t *testing.T) {
	a := testApp(t)
	e := a.editor
	e.edit(a.store.Matches[0])
	tt := ui.NewTester(e.view, 560, 540)
	// Unchanged, Escape just closes.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if e.confirmDiscard {
		t.Fatal("asked with nothing to lose")
	}
	e.edit(a.store.Matches[0])
	e.replace = "Changed"
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if !tt.HasText("Discard Changes?") {
		t.Fatalf("did not ask: %q", tt.Texts())
	}
	shot(t, tt, "editor-discard")
	if err := tt.Click("Keep Editing"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if e.confirmDiscard || e.replace != "Changed" {
		t.Fatalf("lost the edit: %q", e.replace)
	}
	if a.store.Matches[0].Replace == "Changed" {
		t.Fatal("saved without Save")
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
	if !tt.HasText("No expansions yet") {
		t.Fatalf("Insights: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.Key4)
	for _, want := range []string{"Search matches", "Pause or resume", "Recent Activity"} {
		if !tt.HasText(want) {
			t.Fatalf("Shortcuts lacks %q: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "shortcuts")
	tt.Key(ui.Cmd, ui.Key5)
	if !tt.HasText("Sign in to sync") {
		t.Fatalf("Account: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.Key6)
	if !tt.HasText("Telemetry is off in this build") {
		t.Fatalf("Privacy: %q", tt.Texts())
	}
	a.telemetry = &telemetry.Config{EndpointURL: "https://example.com", IngestKey: "k"}
	tt.Frame()
	if !tt.HasText("Send crash reports") || !tt.HasText("Anonymous ID") {
		t.Fatalf("Privacy: %q", tt.Texts())
	}
	shot(t, tt, "privacy")
	tt.Key(ui.Cmd, ui.Key7)
	if !tt.HasText("Sokki") || !tt.HasText("Release Notes") {
		t.Fatalf("About: %q", tt.Texts())
	}
	shot(t, tt, "about")
	tt.SetDark(true)
	tt.Key(ui.Cmd, ui.Key1)
	shot(t, tt, "matches-dark")
}

func TestInsights(t *testing.T) {
	a := testApp(t)
	a.usage.Log(usage.Event{Trigger: ":hello", App: "com.apple.Notes", AppName: "Notes", Saved: 8})
	a.usage.Log(usage.Event{Trigger: ":hello", App: "com.apple.mail", AppName: "Mail", Saved: 8})
	a.usage.Log(usage.Event{Trigger: ":sig", App: "com.apple.mail", AppName: "Mail", Palette: true, Saved: 300})
	a.settings.setPage(insightsPage)
	tt := ui.NewTester(a.settings.view, 780, 1200)
	tt.Advance(time.Second)
	for _, want := range []string{"Expansions", "Characters saved", "316", "about 2 min of typing",
		"Most Used", ":hello", "Typed vs Palette", "Mail", "Notes", "2 matches not used in 7 days"} {
		if !tt.HasText(want) {
			t.Fatalf("Insights lacks %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Unused matches"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(":today") {
		t.Fatalf("unused not listed: %q", tt.Texts())
	}
	if !tt.HasText("your first day") || !tt.HasText("Early days") {
		t.Fatalf("Insights lacks the first-day notes: %q", tt.Texts())
	}
	tt.Advance(settle)
	shot(t, tt, "insights")
	tt.SetDark(true)
	tt.Advance(time.Second)
	shot(t, tt, "insights-dark")
	tt.SetDark(false)
	if err := tt.Click("Edit :today"); err != nil {
		t.Fatal(err)
	}
	if a.editor.trigger != ":today" {
		t.Fatalf("editor has %q", a.editor.trigger)
	}
	if err := tt.Click("Clear Usage Data…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Clear"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.usage.Total() != 0 || !tt.HasText("No expansions yet") || !tt.HasText("Show Matches") {
		t.Fatalf("not cleared: %q", tt.Texts())
	}
	shot(t, tt, "insights-empty")
}

// TestInsightsWeek is the Insights page over a busy week: the average,
// the top 10 of many, and only some of thousands of unused matches.
func TestInsightsWeek(t *testing.T) {
	a := testApp(t)
	manyMatches(a, 600)
	logWeek(a, 900)
	a.settings.setPage(insightsPage)
	tt := ui.NewTester(a.settings.view, 780, 1400)
	tt.Advance(time.Second)
	for _, want := range []string{"Daily average", "Top 10 of", "above average"} {
		if !tt.HasText(want) {
			t.Fatalf("Insights lacks %q: %q", want, tt.Texts())
		}
	}
	if tt.HasText("Early days") {
		t.Fatal("early note over a full week")
	}
	if err := tt.Click("Unused matches"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if !tt.HasText(fmt.Sprintf("and %d more", len(a.settings.insightsCache.idle)-maxUnused)) {
		t.Fatalf("unused not capped: %q", tt.Texts())
	}
	shot(t, tt, "insights-week")
	tt.SetDark(true)
	tt.Advance(time.Second)
	shot(t, tt, "insights-week-dark")
}

func TestTypingTime(t *testing.T) {
	for n, want := range map[int]string{0: "under a minute of typing", 600: "about 3 min of typing", 18000: "about 1.5 h of typing"} {
		if got := typingTime(n); got != want {
			t.Errorf("typingTime(%d) = %q, want %q", n, got, want)
		}
	}
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
	a.store.Config.CaptureShortcut = "ctrl+opt+p"
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
	e.existing, e.trigger, e.file, e.enabled, e.verb = 0, ":", config.DefaultMatchFile, true, "New"
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
	tt.SetDark(true)
	tt.Frame()
	shot(t, tt, "palette-dark")
	tt.SetDark(false)
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

// The Matches page's keys work wherever the focus is.
func TestMatchesShortcutsFromAnywhere(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.settings.view, 780, 560)
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()
	if a.editor.title() != "New Match" {
		t.Fatalf("⌘N: %q", a.editor.title())
	}
}

func TestEditorRefusesOneCharacterTrigger(t *testing.T) {
	a := testApp(t)
	e := a.editor
	e.create(config.DefaultMatchFile, "")
	e.replace = "x"
	if e.canSave() {
		t.Fatal("saves the lone colon")
	}
	e.trigger = ";"
	tt := ui.NewTester(e.view, 560, 540)
	if !tt.HasText("A trigger needs at least 2 characters, so that it doesn't expand by accident.") {
		t.Fatalf("no hint: %q", tt.Texts())
	}
	e.trigger = ";x"
	if !e.canSave() {
		t.Fatal("refuses two characters")
	}
}

func TestAccountPage(t *testing.T) {
	a := testApp(t)
	a.settings.setPage(accountPage)
	tt := ui.NewTester(a.settings.view, 780, 560)
	if !tt.HasText("Sign In…") {
		t.Fatalf("no Sign In: %q", tt.Texts())
	}
	shot(t, tt, "account-signed-out")

	cl := a.cloud
	cl.signin = &signin{cancel: func() {}, code: "WXYZ-2345", url: "https://mon.sorrycc.dev/device"}
	tt.Frame()
	if !tt.HasText("WXYZ-2345") {
		t.Fatalf("no code: %q", tt.Texts())
	}
	shot(t, tt, "account-signing-in")
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if cl.signin != nil {
		t.Fatal("not cancelled")
	}

	if err := cl.engine.SignedIn(&mon.User{ID: "u1", Name: "Ada Lovelace", Email: "ada@example.com"}); err != nil {
		t.Fatal(err)
	}
	cl.client.Token = "mon_test"
	tt.Frame()
	for _, want := range []string{"Ada Lovelace", "ada@example.com", "Sync Now", "Not synced yet", "Sign Out…"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q in %q", want, tt.Texts())
		}
	}
	shot(t, tt, "account-signed-in")
}

func TestSyncAppliesToTheFiles(t *testing.T) {
	a := testApp(t)
	go func() {
		for fn := range a.mainQueue {
			fn()
		}
	}()
	l := cloudLocal{a.cloud}
	read, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !l.IsDefault(read) {
		t.Error("the starter matches are not the default")
	}
	s := read
	s.Matches = append(slices.Clone(read.Matches), cloudsync.Match{File: "work.yml", Trigger: ":addr", Replace: "1 Main St", Enabled: true})
	s.Matches[0].Replace = "Hello from the account"
	if err := l.Apply(read, s); err != nil {
		t.Fatal(err)
	}
	got, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(s) {
		t.Fatalf("files = %+v, want %+v", got.Matches, s.Matches)
	}
	if _, err := os.Stat(filepath.Join(a.dir.MatchDir(), "work.yml")); err != nil {
		t.Error(err)
	}
	// What was read is no longer: applying it again changes nothing.
	if err := l.Apply(read, read); err != cloudsync.ErrStale {
		t.Errorf("err = %v, want ErrStale", err)
	}
}

// ⌘N in the palette makes a prompt of the selection, else the clipboard,
// in prompts.yml, without leaving the palette.
func TestPaletteQuickAddsAPrompt(t *testing.T) {
	a := testApp(t)
	p := a.palette
	p.selectedText = func() (string, string) { return "Review this diff for bugs.\nBe brief.", "Safari" }
	p.query = "Review diff"
	p.filter()
	tt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()
	if p.mode != modeAdd || p.add.label != "Review diff" || p.add.trigger != "" || p.add.file != config.PromptFile {
		t.Fatalf("form %+v", p.add)
	}
	if !tt.HasText("New Prompt") || !tt.HasText("From the selection in Safari") {
		t.Fatalf("form: %q", tt.Texts())
	}
	tt.Advance(settle)
	shot(t, tt, "palette-new-prompt")
	tt.SetDark(true)
	tt.Frame()
	shot(t, tt, "palette-new-prompt-dark")
	tt.SetDark(false)
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	if p.mode != modeSearch {
		t.Fatalf("still in the form: %q", tt.Texts())
	}
	m, ok := a.store.WithKey("Review diff")
	if !ok || m.Trigger != "" || m.SourceFile != config.PromptFile || m.Replace != "Review this diff for bugs.\nBe brief." {
		t.Fatalf("saved %+v %v", m, ok)
	}
	if got, ok := p.selected(); !ok || got.ID != m.ID {
		t.Fatalf("chosen %+v", got)
	}
	if !tt.HasText("Palette") {
		t.Fatalf("no Palette badge: %q", tt.Texts())
	}
	// The footer says where it went, and the row is marked, for a moment.
	if !tt.HasText("Saved to prompts.yml") || !slices.Contains(tt.Texts(), "New") {
		t.Fatalf("no confirmation: %q", tt.Texts())
	}
	tt.Advance(settle)
	shot(t, tt, "palette-saved")
	tt.Advance(flashFor)
	if tt.HasText("Saved to prompts.yml") || slices.Contains(tt.Texts(), "New") {
		t.Fatalf("confirmation stayed: %q", tt.Texts())
	}
	// It is in its own file, and loads again.
	a.store.Load()
	if again, ok := a.store.WithKey("Review diff"); !ok || again.ID != m.ID {
		t.Fatalf("reloaded %+v %v", again, ok)
	}
	data, err := os.ReadFile(filepath.Join(a.dir.MatchDir(), config.PromptFile))
	if err != nil || strings.Contains(string(data), "trigger") || !strings.Contains(string(data), "label: Review diff") {
		t.Fatalf("prompts.yml: %v\n%s", err, data)
	}

	// Without a selection the clipboard is used; a query like a trigger
	// is the trigger; the same title twice is refused.
	p.selectedText = func() (string, string) { return "", "" }
	p.clipboard = "Explain it slowly"
	p.query = ";explain"
	p.quickAdd()
	if p.add.trigger != ";explain" || p.add.label != "" || p.add.source != "From the clipboard" {
		t.Fatalf("form %+v", p.add)
	}
	p.add.trigger, p.add.label = "", "Review diff"
	if p.addProblem() == "" {
		t.Fatal("saves a second prompt of the same title")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if p.mode != modeSearch {
		t.Fatal("escape did not go back")
	}
}

func TestCaptureSavesTheSelection(t *testing.T) {
	a := testApp(t)
	p := a.palette
	p.selectedText = func() (string, string) { return "  \n", "Notes" }
	p.captureNow()
	if p.captured.ok {
		t.Fatal("saved blank text")
	}
	p.mode = modeSaved
	tt := ui.NewTester(p.view, paletteWidth, savedHeight)
	if !tt.HasText("Nothing selected") {
		t.Fatalf("saved: %q", tt.Texts())
	}
	long := strings.Repeat("word ", 20) + "\nsecond line"
	p.selectedText = func() (string, string) { return long, "Notes" }
	p.captureNow()
	p.captureNow()
	first, second := promptLabel(long), promptLabel(long)+" 2"
	if utf8.RuneCountInString(first) != 60 {
		t.Fatalf("label %q", first)
	}
	for _, key := range []string{first, second} {
		if m, ok := a.store.WithKey(key); !ok || m.Replace != long || m.SourceFile != config.PromptFile {
			t.Fatalf("%q: %+v %v", key, m, ok)
		}
	}
	tt.Frame()
	if !tt.HasText("Saved as a prompt") || !tt.HasText("Undo") {
		t.Fatalf("saved: %q", tt.Texts())
	}
	tt.Advance(settle)
	shot(t, tt, "palette-captured")
	tt.SetDark(true)
	tt.Frame()
	shot(t, tt, "palette-captured-dark")
	tt.SetDark(false)
	if err := tt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.store.WithKey(second); ok {
		t.Fatal("undo kept the prompt")
	}

	// ⌘C says what it copied, without Undo.
	p.captured = capture{gen: p.captured.gen + 1, copied: "Hi there!", shown: copiedFor}
	p.opened++
	tt.Frame()
	if !tt.HasText("Copied to the clipboard") || tt.HasText("Undo") {
		t.Fatalf("copied: %q", tt.Texts())
	}
	tt.Advance(settle)
	shot(t, tt, "palette-copied")
}

func TestPaletteWithoutMatches(t *testing.T) {
	a := testApp(t)
	p := a.palette
	a.store.Matches = nil
	a.store.Gen++
	p.filter()
	p.selectedText = func() (string, string) { return "", "" }
	tt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	if !tt.HasText("No Prompts Yet") {
		t.Fatalf("empty: %q", tt.Texts())
	}
	shot(t, tt, "palette-no-prompts")
	if err := tt.Click("New Prompt"); err != nil {
		t.Fatal(err)
	}
	if p.mode != modeAdd {
		t.Fatal("New Prompt did not open the form")
	}
}

func TestEditorSavesAPromptWithoutTrigger(t *testing.T) {
	a := testApp(t)
	e := a.editor
	e.compose(model.Match{Label: "Plan", Replace: "Plan it first."})
	if e.title() != "New Prompt" || e.file != config.PromptFile || !e.canSave() {
		t.Fatalf("editor %+v", e)
	}
	tt := ui.NewTester(e.view, 560, 540)
	if !tt.HasText("No trigger: a prompt, inserted from the palette.") {
		t.Fatalf("editor: %q", tt.Texts())
	}
	e.label = ""
	e.replace = "\n\n:hello"
	tt.Frame()
	if !tt.HasText("Another match in default.yml has this title. Give this one a label.") {
		t.Fatalf("no conflict: %q", tt.Texts())
	}
	e.label, e.replace = "Plan", "Plan it first."
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if m, ok := a.store.WithKey("Plan"); !ok || m.Trigger != "" {
		t.Fatalf("saved %+v %v", m, ok)
	}
	tt = ui.NewTester(a.settings.view, 780, 560)
	if err := tt.Click("Plan"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("From the palette, typed") {
		t.Fatalf("detail: %q", tt.Texts())
	}
	shot(t, tt, "matches-prompt")
}

// TestBannerKeep is the pointer over the capture banner keeping it, and
// a countdown starting again once the pointer leaves.
func TestBannerKeep(t *testing.T) {
	p := testApp(t).palette
	p.mode = modeSaved
	p.keep(true)
	p.keep(true)
	if !p.captured.kept || p.captured.timers != 0 {
		t.Fatalf("hovered: %+v", p.captured)
	}
	p.keep(false)
	p.keep(false)
	if p.captured.kept || p.captured.timers != 1 || p.captured.shown != leftFor {
		t.Fatalf("left: %+v", p.captured)
	}
}

// TestMatchesShowsPrompts is the scope bar of Matches: prompts alone,
// none yet with a button making one, and New making a prompt there.
func TestMatchesShowsPrompts(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.settings.view, 780, 560)
	if err := tt.Click("Prompts"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("No Prompts") || tt.HasText(":hello") {
		t.Fatalf("prompts: %q", tt.Texts())
	}
	shot(t, tt, "matches-no-prompts")
	if err := tt.Click("New Prompt"); err != nil {
		t.Fatal(err)
	}
	e := a.editor
	if e.title() != "New Prompt" || e.file != config.PromptFile || e.trigger != "" {
		t.Fatalf("editor %q %q %q", e.title(), e.file, e.trigger)
	}
	a.store.Add(model.Match{ID: model.NewID(), Label: "Review a diff", Replace: "Review this diff.\nBe brief.", Enabled: true, SourceFile: config.PromptFile})
	tt.Frame()
	if !tt.HasText("Review a diff") || tt.HasText(":hello") {
		t.Fatalf("prompts: %q", tt.Texts())
	}
	if m, ok := a.settings.matches.selected(); !ok || m.Label != "Review a diff" {
		t.Fatalf("chosen %+v %v", m, ok)
	}
	shot(t, tt, "matches-prompts")
	tt.SetDark(true)
	tt.Frame()
	shot(t, tt, "matches-prompts-dark")
	tt.SetDark(false)
	if err := tt.Click("Triggers"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("Review a diff") || !tt.HasText(":hello") {
		t.Fatalf("triggers: %q", tt.Texts())
	}
}

// TestEditorFollowsTheTrigger is the editor calling what it edits a
// prompt while the trigger is empty, and a match once one is typed.
func TestEditorFollowsTheTrigger(t *testing.T) {
	a := testApp(t)
	a.store.Config.PaletteShortcut = "opt+space"
	e := a.editor
	e.compose(model.Match{Replace: "Summarize this thread.\nKeep it short."})
	tt := ui.NewTester(e.view, 560, 540)
	tt.SetTitleBar(ui.TitleBar{Height: 28, Left: 78})
	if !tt.HasText("New Prompt") || !tt.HasText("No trigger: a prompt, in the palette as “Summarize this thread.”") || !tt.HasText("⌥ Space") {
		t.Fatalf("prompt: %q", tt.Texts())
	}
	tt.Advance(settle)
	shot(t, tt, "editor-prompt")
	tt.SetDark(true)
	tt.Frame()
	shot(t, tt, "editor-prompt-dark")
	tt.SetDark(false)
	e.trigger = ":sum"
	tt.Frame()
	if !tt.HasText("New Match") || tt.HasText("⌥ Space") {
		t.Fatalf("match: %q", tt.Texts())
	}
}

func TestAccelerator(t *testing.T) {
	for value, want := range map[string]string{
		"ctrl+opt+p": "Ctrl+Alt+P", "opt+space": "Alt+Space", "ctrl+shift+space": "Ctrl+Shift+Space", "none": "", "": "",
	} {
		if got := accelerator(value); got != want {
			t.Errorf("accelerator(%q) = %q, want %q", value, got, want)
		}
	}
}
