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
	score := func(q, name string) (int, bool) {
		d := docs[name]
		return Parse(q).Score(&d)
	}
	if s, ok := score("sig", "sig"); !ok || s < 40 {
		t.Fatalf("trigger prefix %d", s)
	}
	a, ok1 := score("review diff", "review")
	b, ok2 := score("review diff", "later")
	if !ok1 || !ok2 || a <= b {
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
