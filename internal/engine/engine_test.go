package engine

import (
	"os"
	"slices"
	"testing"
	"time"

	"sokki/internal/logx"
	"sokki/internal/model"
)

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "sokki-log")
	logx.SetDir(dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func matches(triggers ...string) []model.Match {
	var ms []model.Match
	for _, t := range triggers {
		ms = append(ms, model.Match{ID: model.NewID(), Trigger: t, Replace: "<" + t + ">", Enabled: true})
	}
	return ms
}

func feedAll(m *Matcher, s string) *MatchResult {
	var last *MatchResult
	for _, r := range s {
		if res := m.Feed(r); res != nil {
			last = res
		}
	}
	return last
}

func TestMatcherFiresAtEndOfTrigger(t *testing.T) {
	m := NewMatcher(matches(":hello", ":hi"))
	if res := feedAll(m, "say :hello"); res == nil || res.Match.Trigger != ":hello" || res.TriggerLength != 6 {
		t.Fatalf("got %+v", res)
	}
	if res := feedAll(m, "x:hi"); res == nil || res.Match.Trigger != ":hi" {
		t.Fatalf("got %+v", res)
	}
}

func TestMatcherHoldsShorterTrigger(t *testing.T) {
	m := NewMatcher(matches(";d", ";done"))
	if res := feedAll(m, ";d"); res != nil {
		t.Fatalf("fired early: %+v", res)
	}
	if p := m.Pending(); p == nil || p.Match.Trigger != ";d" {
		t.Fatalf("pending %+v", p)
	}
	// A character that continues no trigger fires the pending one.
	if res := m.Feed('x'); res == nil || res.Match.Trigger != ";d" {
		t.Fatalf("got %+v", res)
	}
	m.Reset()
	if res := feedAll(m, ";done"); res == nil || res.Match.Trigger != ";done" {
		t.Fatalf("got %+v", res)
	}
	m.Reset()
	feedAll(m, ";do") // past ;d: the pending is dropped
	if m.Pending() != nil {
		t.Fatal("pending kept past its trigger")
	}
}

func TestMatcherIgnoresDisabled(t *testing.T) {
	ms := matches(":a")
	ms[0].Enabled = false
	if res := feedAll(NewMatcher(ms), ":a"); res != nil {
		t.Fatal("disabled match fired")
	}
}

func TestMatcherIgnoresPrompts(t *testing.T) {
	ms := append(matches(":a"), model.Match{ID: model.NewID(), Label: "Prompt", Replace: "Review", Enabled: true})
	m := NewMatcher(ms)
	if m.root.match != nil {
		t.Fatal("a match without a trigger is in the trie")
	}
	if res := feedAll(m, "x:a"); res == nil || res.Match.Trigger != ":a" {
		t.Fatalf("got %+v", res)
	}
}

func TestBuffer(t *testing.T) {
	b := NewBuffer(4)
	for _, r := range "abcdef" {
		b.Append(r)
	}
	if b.String() != "cdef" || b.Tail(2) != "ef" || b.Tail(5) != "" {
		t.Fatalf("buffer %q", b.String())
	}
	b.PopLast()
	if b.String() != "cde" {
		t.Fatalf("after pop %q", b.String())
	}
	b.Clear()
	if b.PopLast() || b.Len() != 0 {
		t.Fatal("clear")
	}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newCtx(triggers ...string) (*Context, *clock) {
	ck := &clock{t: time.Unix(1_700_000_000, 0)}
	c := NewContext(func() bool { return false })
	c.Now = ck.now
	c.SetMatches(matches(triggers...))
	return c, ck
}

func typeText(c *Context, s string) Result {
	var last Result
	for _, r := range s {
		res := c.Feed(r, 0, 0)
		if res.Action != ActionNone {
			last = res
		}
	}
	return last
}

func TestContextBackspaceRewind(t *testing.T) {
	c, _ := newCtx(":hello")
	typeText(c, ":helx")
	c.Feed('\b', KeyBackspace, 0)
	if res := typeText(c, "lo"); res.Action != ActionMatched || res.Match.Trigger != ":hello" {
		t.Fatalf("got %+v", res)
	}
}

func TestContextUndo(t *testing.T) {
	c, ck := newCtx(":hi")
	c.RecordExpansion(":hi", "hey")
	c.Feed('\b', KeyBackspace, 0)
	c.Feed('\b', KeyBackspace, 0)
	if res := c.Feed('\b', KeyBackspace, 0); res.Action != ActionUndo || res.Trigger != ":hi" {
		t.Fatalf("got %+v", res)
	}
	c.RecordExpansion(":hi", "hey")
	ck.t = ck.t.Add(4 * time.Second)
	for range 3 {
		if res := c.Feed('\b', KeyBackspace, 0); res.Action == ActionUndo {
			t.Fatal("undo after the timeout")
		}
	}
}

func TestContextCaretKeysClear(t *testing.T) {
	c, _ := newCtx(":hello")
	typeText(c, ":hel")
	c.Feed(0, 0x7B, 0) // ←
	if res := typeText(c, "lo"); res.Action == ActionMatched {
		t.Fatal("matched across a caret move")
	}
}

func TestContextPasteWindow(t *testing.T) {
	c, ck := newCtx(":a")
	c.PasteDetected()
	if res := c.Feed(':', 0, 0); res.Action != ActionBlockedTooSoon {
		t.Fatalf("got %+v", res)
	}
	if res := c.Feed('a', 0, 0); res.Action != ActionNone {
		t.Fatalf("second block surfaced: %+v", res)
	}
	ck.t = ck.t.Add(time.Second)
	if res := typeText(c, ":a"); res.Action != ActionMatched {
		t.Fatalf("got %+v", res)
	}
}

func TestContextSecureInput(t *testing.T) {
	secure := false
	c, _ := newCtx(":a")
	c.IsSecure = func() bool { return secure }
	typeText(c, ":")
	secure = true
	if res := c.Feed('a', 0, 0); res.Action != ActionSecureInputChanged || !res.Secure {
		t.Fatalf("got %+v", res)
	}
	if res := typeText(c, ":a"); res.Action != ActionNone {
		t.Fatalf("expanded in a secure field: %+v", res)
	}
	secure = false
	if res := c.Feed('x', 0, 0); res.Action != ActionSecureInputChanged || res.Secure {
		t.Fatalf("got %+v", res)
	}
}

func TestContextDisabledAndToggle(t *testing.T) {
	c, ck := newCtx(":a")
	cfg := model.DefaultConfig()
	cfg.ToggleShortcut = "double_option"
	c.SetConfig(cfg)
	c.ModifierChanged(FlagAlternate)
	ck.t = ck.t.Add(100 * time.Millisecond)
	if res := c.ModifierChanged(FlagAlternate); res.Action != ActionToggled || res.Enabled {
		t.Fatalf("got %+v", res)
	}
	if res := c.Feed(':', 0, 0); res.Action != ActionBlockedDisabled {
		t.Fatalf("got %+v", res)
	}
	if res := c.Feed('a', 0, 0); res.Action != ActionNone {
		t.Fatalf("got %+v", res)
	}
	ck.t = ck.t.Add(time.Second)
	c.ModifierChanged(FlagAlternate)
	ck.t = ck.t.Add(400 * time.Millisecond) // too slow
	if res := c.ModifierChanged(FlagAlternate); res.Action == ActionToggled {
		t.Fatal("toggled on a slow double tap")
	}
}

func TestContextPendingFiresAfterDelay(t *testing.T) {
	c, _ := newCtx(";d", ";done")
	cfg := model.DefaultConfig()
	cfg.ConflictResolveDelayMs = 50
	c.SetConfig(cfg)
	fired := make(chan string, 1)
	c.OnPendingFired(func(m model.Match, n int) { fired <- m.Trigger })
	typeText(c, ";d")
	select {
	case got := <-fired:
		if got != ";d" {
			t.Fatalf("fired %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending match never fired")
	}
	typeText(c, ";d")
	c.CancelPending("test")
	select {
	case got := <-fired:
		t.Fatalf("canceled pending fired %q", got)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestContextSuppressAndPalette(t *testing.T) {
	c, _ := newCtx(":a")
	c.SetSuppress(true)
	if res := typeText(c, ":a"); res.Action != ActionNone {
		t.Fatal("matched while suppressed")
	}
	c.SetSuppress(false)
	cfg := model.DefaultConfig()
	cfg.PaletteShortcut = "opt+space"
	c.SetConfig(cfg)
	if !c.PaletteChord(KeySpace, FlagAlternate) || c.PaletteChord(KeySpace, FlagAlternate|FlagShift) {
		t.Fatal("palette chord")
	}
	if c.CaptureChord(KeyP, FlagControl|FlagAlternate) {
		t.Fatal("capture chord while none")
	}
	cfg.CaptureShortcut = "ctrl+opt+p"
	c.SetConfig(cfg)
	if !c.CaptureChord(KeyP, FlagControl|FlagAlternate) || c.CaptureChord(KeyS, FlagControl|FlagAlternate) || c.PaletteChord(KeyP, FlagControl|FlagAlternate) {
		t.Fatal("capture chord")
	}
}

func TestRender(t *testing.T) {
	now := time.Date(2026, 3, 4, 15, 6, 7, 0, time.UTC)
	clip := func() string { return "CLIP" }
	cases := map[string]string{
		"Pasted: {{clipboard}}":        "Pasted: CLIP",
		"{{date}}":                     "2026-03-04",
		"{{date:yyyy-MM-dd HH:mm:ss}}": "2026-03-04 15:06:07",
		"{{date:%Y-%m-%d}}":            "2026-03-04",
		"{{date:EEEE, MMM d}}":         "Wednesday, Mar 4",
		"{{date:h:mm a}}":              "3:06 PM",
		"{{date:'week' w}}":            "week 10",
		"{{unknown}} {{other:x}}":      "{{unknown}} {{other:x}}",
		"no vars":                      "no vars",
		"{{cursor}}":                   "{{cursor}}",
	}
	for in, want := range cases {
		if got := Render(in, clip, now); got != want {
			t.Errorf("Render(%q) = %q, want %q", in, got, want)
		}
	}
	if got := RenderPreview("a{{cursor}}b {{clipboard}}", "SNAP", now); got != "a"+CursorGlyph+"b SNAP" {
		t.Errorf("preview %q", got)
	}
}

func TestFuzzyScore(t *testing.T) {
	if _, ok := FuzzyScore("xyz", "hello"); ok {
		t.Fatal("matched missing letters")
	}
	start, _ := FuzzyScore("he", "hello")
	mid, _ := FuzzyScore("ll", "hello")
	if start <= mid {
		t.Fatalf("start %d should beat middle %d", start, mid)
	}
	if s, ok := FuzzyScore("", "x"); !ok || s != 0 {
		t.Fatal("empty query")
	}
}

func TestVariables(t *testing.T) {
	s := "Hi {{clipboard}}, it is {{date:yyyy}} {{x"
	got := Variables(s)
	if len(got) != 2 || s[got[0][0]:got[0][1]] != "{{clipboard}}" || s[got[1][0]:got[1][1]] != "{{date:yyyy}}" {
		t.Fatalf("Variables = %v", got)
	}
	if Variables("plain") != nil {
		t.Fatal("plain text has variables")
	}
}

func TestFuzzyPositions(t *testing.T) {
	if got := FuzzyPositions(Fold("hlo"), Fold("Hello")); !slices.Equal(got, []int{0, 2, 4}) {
		t.Fatalf("positions %v", got)
	}
	if FuzzyPositions(Fold("xyz"), Fold("Hello")) != nil {
		t.Fatal("matched what is not there")
	}
}
