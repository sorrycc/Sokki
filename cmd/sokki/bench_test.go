package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sokki/internal/model"
	"sokki/internal/usage"
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

// BenchmarkInsightsFrame is a frame of the Insights page over a large
// configuration and a busy week, as a hover redraws it.
func BenchmarkInsightsFrame(b *testing.B) {
	a := testApp(b)
	manyMatches(a, 2000)
	logWeek(a, 5000)
	a.settings.setPage(insightsPage)
	tt := ui.NewTester(a.settings.view, 780, 900)
	tt.Advance(time.Second)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		tt.Frame()
	}
}

// logWeek gives a's usage about n expansions over the last 7 days, of
// the first triggers of manyMatches, the first ones most, in a few apps.
func logWeek(a *app, n int) {
	now := time.Now()
	weights := [7]int{6, 9, 4, 11, 13, 8, 10} // a week ago to today
	apps := []string{"Mail", "Slack", "Notes", "Safari", "Xcode", "Messages", "Notion", "Terminal", "Figma"}
	var events []usage.Event
	seed := uint32(7)
	random := func() float32 { // an LCG, the same week each run
		seed = seed*1664525 + 1013904223
		return float32(seed>>8) / (1 << 24)
	}
	for d, w := range weights {
		for i := range n * w / 61 {
			r := random()
			app := apps[int(float32(len(apps))*r*r)]
			events = append(events, usage.Event{
				Time:    now.AddDate(0, 0, d-6).Add(-time.Duration(i) * time.Second),
				Trigger: fmt.Sprintf(":t%04d", int(150*random()*random())),
				App:     "com.example." + strings.ToLower(app), AppName: app,
				Palette: random() < 0.2, Saved: 10 + int(60*random()),
			})
		}
	}
	dir, _ := os.MkdirTemp("", "usage")
	data, _ := json.Marshal(events)
	os.WriteFile(filepath.Join(dir, usage.File), data, 0o644)
	a.usage = usage.Open(dir)
	os.RemoveAll(dir)
}
