package history

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestClean(t *testing.T) {
	keep := []string{
		"commit & push the branch",
		"  Review this diff for bugs.\n",
		"/Users/me/notes.md has the plan, read it",
		"用中文解释这个函数的作用是什么",
	}
	for _, s := range keep {
		if _, ok := Clean(s, Settings{}); !ok {
			t.Errorf("left out %q", s)
		}
	}
	drop := []string{
		"continue", "hi", "/model", "/review the diff please", "!open . in finder",
		"[Pasted text #1 +3 lines]",
		"use the key sk-abcdefghijklmnopqrstuvwxyz123456 to call it",
		`<hc from="t127 ikaga">say hi only to the person</hc>`,
		"<command-name>/effort</command-name>\n<command-args></command-args>",
	}
	for _, s := range drop {
		if _, ok := Clean(s, Settings{}); ok {
			t.Errorf("kept %q", s)
		}
	}
	if _, ok := Clean("If the analysis found nothing", Settings{Ignore: []string{"IF THE ANALYSIS"}}); ok {
		t.Error("ignore list not applied")
	}
	if got, _ := Clean("  spaced out prompt here  ", Settings{}); got != "spaced out prompt here" {
		t.Errorf("not trimmed: %q", got)
	}
}

func TestImportMergesAcrossAgents(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s := openAt(t.TempDir(), func() time.Time { return now })
	n := s.Import([]Item{
		{Source: "claude", Text: "Review this diff for bugs", Project: "/p/a", Time: now.Add(-48 * time.Hour)},
		{Source: "codex", Text: "Review this  diff\nfor bugs ", Project: "/p/b", Time: now.Add(-time.Hour)},
		{Source: "codex", Text: "Write the changelog entry", Time: now.Add(-2 * time.Hour)},
		{Source: "codex", Text: "ok"},
	}, map[string]Cursor{"x": {Offset: 10}})
	if n != 2 {
		t.Fatalf("new %d", n)
	}
	es := s.Entries()
	if len(es) != 2 {
		t.Fatalf("entries %d", len(es))
	}
	e := es[0]
	if e.Count != 2 || !slices.Equal(e.Sources, []string{"claude", "codex"}) || e.Project() != "b" || len(e.Uses) != 2 {
		t.Fatalf("merged %+v", e)
	}
	if e.Text != "Review this diff for bugs" {
		t.Fatalf("kept the first text: %q", e.Text)
	}
	if s.cursors()["x"].Offset != 10 {
		t.Fatal("cursor not kept")
	}

	s.Recall(e.ID, "/p/c")
	e, _ = s.Entry(e.ID)
	if e.Count != 3 || e.Recalls != 1 || e.Project() != "c" || slices.Contains(e.Sources, RecallSource) {
		t.Fatalf("recall %+v", e)
	}

	s.Hide(e.ID)
	if len(s.Entries()) != 1 {
		t.Fatal("hidden still shows")
	}
	s.Import([]Item{{Source: "claude", Text: "Review this diff for bugs", Time: now}}, nil)
	if len(s.Entries()) != 1 {
		t.Fatal("a hidden entry came back")
	}
	s.Unhide(e.ID)
	if len(s.Entries()) != 2 {
		t.Fatal("unhide")
	}

	s.SetSettings(Settings{Ignore: []string{"changelog"}})
	if len(s.Entries()) != 1 {
		t.Fatal("ignore not applied to the entries")
	}

	// It comes back from the file.
	s.Flush()
	s2 := openAt(filepath.Dir(s.path), func() time.Time { return now })
	if len(s2.Entries()) != 1 || s2.Entries()[0].Count != 4 || s2.Settings().Ignore[0] != "changelog" {
		t.Fatalf("reloaded %+v", s2.Entries())
	}

	s2.Clear(false)
	if len(s2.Entries()) != 0 || s2.cursors()["x"].Offset != 10 {
		t.Fatal("clear")
	}
	s2.Clear(true)
	if len(s2.cursors()) != 0 {
		t.Fatal("reimport keeps cursors")
	}
}

func TestFrecency(t *testing.T) {
	now := time.Now()
	recent := []int64{now.Add(-time.Hour).Unix()}
	old := []int64{now.Add(-200 * 24 * time.Hour).Unix()}
	month := []int64{now.Add(-20 * 24 * time.Hour).Unix()}
	if Frecency(1, recent, now) <= Frecency(1, old, now) {
		t.Fatal("recent not above old")
	}
	if Frecency(10, month, now) <= Frecency(1, recent, now) {
		t.Fatal("often not above once")
	}
	if Boost(0) != 0 || Boost(1000) <= Boost(100) {
		t.Fatal("boost")
	}
}

func TestBumpMatch(t *testing.T) {
	s := Open(t.TempDir())
	for range 20 {
		s.BumpMatch(":sig")
	}
	m := s.MatchUses()[":sig"]
	if m.Count != 20 || len(m.Times) != keepUses {
		t.Fatalf("%+v", m)
	}
}

// write writes files under home, by path.
func write(t *testing.T, home string, files map[string]string) {
	t.Helper()
	for p, s := range files {
		p = filepath.Join(home, p)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSources(t *testing.T) {
	home := t.TempDir()
	write(t, home, map[string]string{
		".claude/history.jsonl": `{"display":"/model","pastedContents":{},"timestamp":1784531156885,"project":"/p/x"}
{"display":"Fix the failing test in the parser","pastedContents":{},"timestamp":1784531162347,"project":"/p/sokki"}
{"display":"Summarize [Pasted text #1 +3 lines] in one line","pastedContents":{"1":{"id":1,"type":"text","contentHash":"abc123"}},"timestamp":1784531162348,"project":"/p/sokki"}
{"display":"Explain [Pasted text #1 +1 lines] briefly","pastedContents":{"1":{"id":1,"type":"text","content":"inline words here"}},"timestamp":1784531162349,"project":"/p/sokki"}
`,
		".claude/paste-cache/abc123.txt": "the pasted log\nof three\nlines",
		".codex/history.jsonl":           `{"session_id":"s","ts":1785809189,"text":"Fix the failing test in the parser"}` + "\n",
		".gemini/antigravity-cli/history.jsonl": `{"display":"/model","timestamp":1787215945553,"type":"slash_command"}
{"display":"how to call agy headlessly?","timestamp":1787215986825,"workspace":"/p/q"}
`,
		".gemini/tmp/abc/logs.json": `[{"type":"user","message":"Add a test for the scanner","timestamp":"2026-01-02T03:04:05Z"},{"type":"model","message":"ok"}]`,
		".qoder/projects/-p-q/s1.jsonl": `{"type":"user","timestamp":"2026-11-13T11:15:15.634Z","cwd":"/p/q","origin":{"kind":"human"},"humanInput":{"text":"Follow up on the PR review","mode":"prompt"}}
{"type":"user","timestamp":"2026-11-13T11:15:16Z","cwd":"/p/q","origin":{"kind":"tool"},"humanInput":{"text":"tool result text","mode":"prompt"}}
{"type":"user","timestamp":"2026-11-13T11:15:17Z","cwd":"/p/q","origin":{"kind":"human"},"humanInput":{"text":"ls -la the folder","mode":"bash"}}
`,
		".grok/sessions/%2Fp%2Fg/01/chat_history.jsonl": `{"type":"system","content":"You are Grok"}
{"type":"user","content":[{"type":"text","text":"<user_info>x</user_info>\n<user_query>\nRename the module to sokki\n</user_query>"}],"prompt_index":0}
{"type":"user","content":[{"type":"text","text":"<user_query>made up by grok itself</user_query>"}],"synthetic_reason":"x"}
`,
		".local/state/opencode/prompt-history.jsonl": `{"input":"Port the scanner to Go please","parts":[],"mode":"normal"}` + "\n",
		"proj/.aider.input.history":                  "\n# 2026-01-02 10:00:00.123456\n+Refactor the parser\n+into two files\n\n# 2026-01-02 10:01:00.000000\n+/add main.go\n",
	})
	s := Open(t.TempDir())
	n := s.Scan(Env{Home: home, Projects: []string{filepath.Join(home, "proj")}})
	var texts []string
	for _, e := range s.Entries() {
		texts = append(texts, e.Text)
	}
	want := []string{
		"Fix the failing test in the parser",
		"Summarize the pasted log\nof three\nlines in one line",
		"Explain inline words here briefly",
		"how to call agy headlessly?",
		"Add a test for the scanner",
		"Follow up on the PR review",
		"Rename the module to sokki",
		"Port the scanner to Go please",
		"Refactor the parser\ninto two files",
	}
	for _, w := range want {
		if !slices.Contains(texts, w) {
			t.Errorf("missing %q in %q", w, texts)
		}
	}
	if n != len(want) || len(texts) != len(want) {
		t.Fatalf("new %d, entries %q", n, texts)
	}
	fix := s.Entries()[0]
	if fix.Count != 2 || !slices.Equal(fix.Sources, []string{"claude", "codex"}) {
		t.Fatalf("fix %+v", fix)
	}
	for _, e := range s.Entries() {
		if e.Text == "Rename the module to sokki" && e.Project() != "g" {
			t.Fatalf("grok project %q", e.Projects)
		}
	}

	// A second scan reads only what was added.
	if n := s.Scan(Env{Home: home}); n != 0 {
		t.Fatalf("scanned again: %d", n)
	}
	f, _ := os.OpenFile(filepath.Join(home, ".codex/history.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"session_id":"s","ts":1785809190,"text":"Fix the failing test in the parser"}` + "\n" + `{"ts":1,"text":"half a li`)
	f.Close()
	s.Scan(Env{Home: home})
	if e, _ := s.Entry(fix.ID); e.Count != 3 {
		t.Fatalf("appended use: %d", e.Count)
	}

	// A source turned off is not read.
	st := s.Settings()
	st.Off = []string{"codex"}
	s.SetSettings(st)
	f, _ = os.OpenFile(filepath.Join(home, ".codex/history.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("ne\"}\n" + `{"ts":2,"text":"Fix the failing test in the parser"}` + "\n")
	f.Close()
	s.Scan(Env{Home: home})
	if e, _ := s.Entry(fix.ID); e.Count != 3 {
		t.Fatalf("off source read: %d", e.Count)
	}
}

func TestSourceNames(t *testing.T) {
	for _, src := range Sources {
		if SourceName(src.ID) != src.Name || strings.TrimSpace(src.Where) == "" {
			t.Fatal(src.ID)
		}
	}
}
