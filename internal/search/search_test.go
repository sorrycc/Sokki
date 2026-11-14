package search

import (
	"slices"
	"testing"
)

func TestScoreRanks(t *testing.T) {
	docs := map[string]Doc{
		"sig":    NewDoc(":sig", "Signature", "Best,\nMe", nil, nil),
		"review": NewDoc("", "Review this diff for bugs", "Review this diff for bugs. Be brief.", []string{"claude", "Claude Code"}, []string{"sokki"}),
		"later":  NewDoc("", "Write tests", "Write tests, then review the diff at the end", []string{"codex"}, nil),
		"zh":     NewDoc("", "用中文解释这个函数", "用中文解释这个函数", []string{"codex"}, nil),
	}
	score := func(q, name string) (Fit, bool) {
		d := docs[name]
		return Parse(q).Score(&d)
	}
	if s, ok := score("sig", "sig"); !ok || s.Tier != TierTrigger {
		t.Fatalf("trigger %+v", s)
	}
	a, ok1 := score("review diff", "review")
	b, ok2 := score("review diff", "later")
	if !ok1 || !ok2 || !a.better(b) {
		t.Fatalf("title %d vs body %d", a, b)
	}
	if _, ok := score("review zzz", "review"); ok {
		t.Fatal("every word must be found")
	}
	if _, ok := score("rvw", "review"); !ok {
		t.Fatal("one word matches a title fuzzily")
	}
	if _, ok := score("rvw dff", "review"); ok {
		t.Fatal("several words don't match fuzzily")
	}
	if _, ok := score("解释", "zh"); !ok {
		t.Fatal("chinese")
	}
	if _, ok := score("@claude review", "review"); !ok {
		t.Fatal("agent filter")
	}
	if _, ok := score("@claude review", "later"); ok {
		t.Fatal("agent filter lets others through")
	}
	if _, ok := score("#sok", "review"); !ok {
		t.Fatal("project filter")
	}
	if _, ok := score("#sok", "sig"); ok {
		t.Fatal("project filter lets matches through")
	}
}

// An exact match ranks above a fuzzy one, and letters strewn over a long
// title match nothing.
func TestScoreExactFirst(t *testing.T) {
	read := NewDoc("", "Read ~/Downloads/qoder-feature-cleanup.md and tell me what to do", "", nil, nil)
	refact := NewDoc("", "refact the README.md", "refact the README.md", nil, nil)
	refactor := NewDoc("", "refactor the parser", "refactor the parser", nil, nil)
	q := Parse("refact")
	if f, ok := q.Score(&read); ok {
		t.Fatalf("strewn letters match: %+v", f)
	}
	a, _ := q.Score(&refact)
	b, _ := q.Score(&refactor)
	if a.Tier != TierTitleStart || !a.better(b) {
		t.Fatalf("whole word %+v vs prefix %+v", a, b)
	}
	if f, ok := Parse("readme refact").Score(&refact); !ok || f.Tier < TierWord {
		t.Fatalf("words in any order %+v %v", f, ok)
	}
	fz, ok := Parse("rfct").Score(&refact)
	if !ok || fz.Tier != TierFuzzy || !b.better(fz) {
		t.Fatalf("fuzzy %+v %v", fz, ok)
	}
	gc := NewDoc("", "git commit all", "", nil, nil)
	if _, ok := Parse("gca").Score(&gc); !ok {
		t.Fatal("word starts")
	}
}

// Words close together, in the order typed, score more than far apart.
func TestScoreNear(t *testing.T) {
	close := NewDoc("", "", "fix the flaky tests in the parser", nil, nil)
	far := NewDoc("", "", "flaky runs of the build are worse than any slow test, so fix them in the parser", nil, nil)
	q := Parse("flaky parser")
	a, ok1 := q.Score(&close)
	b, ok2 := q.Score(&far)
	if !ok1 || !ok2 || !a.better(b) {
		t.Fatalf("near %+v far %+v", a, b)
	}
	c, _ := Parse("parser flaky").Score(&close)
	if !a.better(c) {
		t.Fatalf("in order %+v out of order %+v", a, c)
	}
}

func TestNarrows(t *testing.T) {
	cases := []struct {
		was, q string
		want   bool
	}{
		{"rev", "revi", true},
		{"rev", "rev diff", true},
		{"review d", "review di", true},
		{"rev", "re", false},
		{"", "a", false},
		{"@cl", "@cla x", true},
		{"rev", "@claude rev", false},
	}
	for _, c := range cases {
		if got := Parse(c.q).Narrows(Parse(c.was)); got != c.want {
			t.Errorf("%q after %q: %v", c.q, c.was, got)
		}
	}
}

func TestMarks(t *testing.T) {
	if m := Parse("diff rev").Marks("Review this diff"); !slices.Equal(m, []int{0, 1, 2, 12, 13, 14, 15}) {
		t.Fatalf("%v", m)
	}
	if m := Parse("rvw").Marks("Review"); !slices.Equal(m, []int{0, 2, 5}) {
		t.Fatalf("fuzzy %v", m)
	}
}

func TestRanges(t *testing.T) {
	q := Parse("the test @codex")
	got := q.Ranges("Fix the TEST, then the tests", 100)
	want := [][2]int{{4, 7}, {8, 12}, {14, 17}, {19, 22}, {23, 27}}
	if !slices.Equal(got, want) {
		t.Fatalf("ranges %v", got)
	}
	if got := Parse("ab").Ranges("abab", 1); len(got) != 1 {
		t.Fatalf("limit: %v", got)
	}
	if got := Parse("bc abc").Ranges("xabcx", 10); !slices.Equal(got, [][2]int{{1, 4}}) {
		t.Fatalf("merged: %v", got)
	}
}
