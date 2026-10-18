package engine

import (
	"fmt"
	"testing"

	"sokki/internal/model"
)

// benchMatches is a set of triggers the size of a large user's.
func benchMatches(n int) []model.Match {
	ms := make([]model.Match, 0, n)
	for i := range n {
		ms = append(ms, model.Match{ID: model.NewID(), Trigger: fmt.Sprintf(":t%03d", i), Replace: "x", Enabled: true})
	}
	return ms
}

// BenchmarkFeed is a key typed on the event tap's thread: prose, which
// starts no trigger, and the colon of one.
func BenchmarkFeed(b *testing.B) {
	c := NewContext(func() bool { return false })
	c.SetMatches(benchMatches(500))
	text := []rune("the quick brown fox :t0 jumps over the lazy dog ")
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		c.Feed(text[i%len(text)], 0, 0)
	}
}

func BenchmarkFuzzyScore(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		FuzzyScore("sig", "Best regards, and a long replacement text that goes on")
	}
}
