package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"sokki/internal/model"
)

// manyMatches gives a's store n matches over a few files, as a large
// configuration has, without writing them.
func manyMatches(a *app, n int) {
	ms := make([]model.Match, n)
	for i := range ms {
		ms[i] = model.Match{
			ID:         int64(i + 1),
			Trigger:    fmt.Sprintf(":t%04d", i),
			Label:      fmt.Sprintf("Snippet number %d", i),
			Replace:    strings.Repeat(fmt.Sprintf("Line %d of a longer replacement {{date:yyyy}}\n", i), 1+i%4),
			Enabled:    i%7 != 0,
			SourceFile: fmt.Sprintf("file%d.yml", i%5),
		}
	}
	a.store.Matches = ms
	a.store.Gen++
}

// BenchmarkMatchesFrame is a frame of Settings' Matches page that changes
// nothing, as a hover or a blink of the caret redraws it.
func BenchmarkMatchesFrame(b *testing.B) {
	a := testApp(b)
	manyMatches(a, 2000)
	tt := ui.NewTester(a.settings.view, 780, 560)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		tt.Frame()
	}
}

// BenchmarkPaletteType is a key typed in the palette's search over many
// matches: the filter and the frame after it.
func BenchmarkPaletteType(b *testing.B) {
	a := testApp(b)
	manyMatches(a, 2000)
	p := a.palette
	tt := ui.NewTester(p.view, paletteWidth, paletteHeight)
	queries := []string{"s", "sn", "sni", "snip", "1", "12", ""}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		p.query = queries[i%len(queries)]
		p.filter()
		tt.Frame()
	}
}

// BenchmarkPaletteFilter is the palette's ranking alone.
func BenchmarkPaletteFilter(b *testing.B) {
	a := testApp(b)
	manyMatches(a, 2000)
	p := a.palette
	queries := []string{"s", "sn", "sni", "snip", "1", "12", ""}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		p.query = queries[i%len(queries)]
		p.filter()
	}
}
