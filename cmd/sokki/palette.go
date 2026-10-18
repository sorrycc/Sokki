package main

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sokki/internal/config"
	"sokki/internal/engine"
	"sokki/internal/logx"
	"sokki/internal/macos"
	"sokki/internal/model"
)

const paletteWidth, paletteHeight = 760, 460

// palette is the Spotlight-style finder of matches: a panel over every
// app that takes the keyboard without taking the front from the app the
// user is in, which keeps its focus for the injection.
type palette struct {
	a     *app
	win   *mygo.Window
	query string
	input ui.Handle
	list  ui.ListState

	results   []model.Match
	chosen    int
	clipboard string // a snapshot, for the preview

	// previewText is the preview of the match previewID, whose replacement
	// was previewOf, kept while the choice stays.
	previewID   int64
	previewOf   string
	previewText string
}

func newPalette(a *app) *palette { return &palette{a: a} }

func (p *palette) invalidate() {
	if p != nil && p.win != nil && p.win.IsVisible() {
		p.filter()
		p.win.Invalidate()
	}
}

func (p *palette) ensure() {
	if p.win != nil {
		return
	}
	p.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         "Sokki Palette",
		Panel:         true,
		Hidden:        true,
		Frameless:     true,
		Transparent:   true,
		Vibrancy:      mygo.VibrancyPopover,
		Width:         paletteWidth,
		Height:        paletteHeight,
		DisableResize: true,
		Content:       ui.View(p.view),
	})
	p.a.hideOnClose(p.win, p.hide)
	p.win.OnBlur(func() { // a click in another app
		logx.Debugf("[Palette] blurred — hiding")
		p.hide()
	})
}

func (p *palette) toggle() {
	if p.win != nil && p.win.IsVisible() {
		p.hide()
	} else {
		p.show()
	}
}

func (p *palette) show() {
	p.ensure()
	p.a.exp.Ctx.CancelPending("palette-open")
	p.clipboard, _ = macos.PasteboardString()
	p.previewID = 0
	// On the display with the pointer, its top a quarter of the way down.
	d := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	w, _ := p.win.Size()
	p.win.SetPosition(d.WorkArea.X+(d.WorkArea.Width-w)/2, d.WorkArea.Y+d.WorkArea.Height/4)
	p.query = ""
	p.filter()
	p.input.Focus()
	p.win.Show()
	logx.Debugf("[Palette] show results=%d", len(p.results))
}

func (p *palette) hide() {
	if p.win != nil && p.win.IsVisible() {
		p.win.Hide()
		logx.Debugf("[Palette] hide")
	}
}

type scored struct {
	m     model.Match
	score int
}

// filter ranks the enabled matches by how well the query fits their
// trigger, label (a little more) or replacement (a little less), then by
// when they were last used, then by trigger.
func (p *palette) filter() {
	q := strings.TrimSpace(p.query)
	recent := p.a.recency.Snapshot()
	var found []scored
	for _, m := range p.a.store.Matches {
		if !m.Enabled {
			continue
		}
		if s, ok := matchScore(q, m); ok {
			found = append(found, scored{m, s})
		}
	}
	slices.SortStableFunc(found, func(a, b scored) int {
		if a.score != b.score {
			return b.score - a.score
		}
		ta, tb := recent[a.m.Trigger], recent[b.m.Trigger]
		if !ta.Equal(tb) {
			return tb.Compare(ta)
		}
		return strings.Compare(a.m.Trigger, b.m.Trigger)
	})
	p.results = p.results[:0]
	for _, f := range found {
		p.results = append(p.results, f.m)
	}
	p.choose(0)
}

func matchScore(q string, m model.Match) (int, bool) {
	if q == "" {
		return 0, true
	}
	best, any := 0, false
	if s, ok := engine.FuzzyScore(q, m.Trigger); ok {
		best, any = s, true
	}
	if m.Label != "" {
		if s, ok := engine.FuzzyScore(q, m.Label); ok && (!any || s+2 > best) {
			best, any = s+2, true
		}
	}
	if s, ok := engine.FuzzyScore(q, head(m.Replace, 200)); ok && (!any || max(0, s-2) > best) {
		best, any = max(0, s-2), true
	}
	return best, any
}

// head returns the first n characters of s, without copying it.
func head(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

func (p *palette) choose(i int) {
	p.chosen = max(0, min(i, len(p.results)-1))
	if len(p.results) > 0 {
		p.list.ScrollIntoView(p.chosen)
	}
}

func (p *palette) selected() (model.Match, bool) {
	if p.chosen < 0 || p.chosen >= len(p.results) {
		return model.Match{}, false
	}
	return p.results[p.chosen], true
}

// commit types the match into the app the user was in.
func (p *palette) commit(m model.Match) {
	p.hide()
	p.a.exp.InjectFromPalette(m)
	p.a.recency.Bump(m.Trigger)
}

func (p *palette) openEditor(m model.Match) {
	p.hide()
	p.a.editor.edit(m)
}

func (p *palette) openSettings() {
	p.hide()
	p.a.openSettings()
}

func (p *palette) newMatch() {
	trigger := strings.TrimSpace(p.query)
	if strings.ContainsAny(trigger, " \t") {
		trigger = ""
	}
	p.hide()
	p.a.editor.create(config.DefaultMatchFile, trigger)
}

func (p *palette) view(c *ui.Context) {
	t := c.Theme()
	root := c.Root()
	if c.Vibrancy() {
		root.Background(ui.Transparent)
	} else {
		root.Background(t.Background)
	}
	// Escape clears the search first, then closes.
	if p.query != "" && c.Shortcut(0, ui.KeyEscape) {
		p.query = ""
		p.filter()
	}
	if c.Shortcut(0, ui.KeyDown) {
		p.choose(p.chosen + 1)
	}
	if c.Shortcut(0, ui.KeyUp) {
		p.choose(p.chosen - 1)
	}
	if c.Shortcut(ui.Cmd, ui.KeyEnter) {
		if m, ok := p.selected(); ok {
			p.openEditor(m)
		}
	} else if c.Shortcut(0, ui.KeyEnter) {
		if m, ok := p.selected(); ok {
			p.commit(m)
		}
	}
	c.OnShortcut(ui.Cmd, ui.KeyComma, p.openSettings)
	c.OnShortcut(ui.Cmd, ui.KeyN, p.newMatch)

	ui.Column(c).Fill().Radius(14).Border(0.5, t.Border).Clip().Children(func() {
		ui.Row(c).Padding(14, 18).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconSearch).FontSize(19).TextColor(t.TextMuted)
			if ui.TextInputBase(c, &p.query).Bind(&p.input).Grow(1).FontSize(19).
				Placeholder("Search matches").Label("Search matches").Changed() {
				p.filter()
			}
			if p.query != "" {
				caption(c, plural(len(p.results), "result", "results")).FontFeatures("tnum").SingleLine()
			}
		})
		ui.Divider(c)
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			p.list.Key = func(i int) any { return p.results[i].ID }
			p.list.Label = func(i int) string { return p.results[i].Title() }
			ui.List(c, &p.list, len(p.results), func(i int) { p.row(c, i) }).Width(300).Padding(6).Gap(1).Children(func() {
				if len(p.results) == 0 {
					p.empty(c)
				}
			})
			ui.Divider(c).Vertical().Background(t.Border.Alpha(0.6))
			ui.Column(c).Grow(1).Shrink(1).Children(func() { p.preview(c) })
		})
		ui.Divider(c)
		ui.Row(c).Padding(8, 14).Gap(14).AlignItems(ui.Center).Background(t.Text.Alpha(0.025)).Children(func() {
			if icon := appIcon(); icon != nil {
				ui.Image(c, icon).Size(16, 16)
			}
			caption(c, "Sokki").FontWeight(600)
			ui.Spacer(c)
			if _, ok := p.selected(); ok {
				hint(c, "↵", "Insert")
				hint(c, "⌘↵", "Edit")
			} else {
				hint(c, "⌘N", "New Match")
			}
			hint(c, "esc", "Close")
		})
	})
}

func (p *palette) empty(c *ui.Context) {
	if len(p.a.store.Matches) == 0 {
		emptyState(c, iconMatches, "No Matches Yet", "Press ⌘N to make your first one.")
		return
	}
	emptyState(c, iconSearch, "No Results", "Press ⌘N to make a match for this.")
}

func (p *palette) row(c *ui.Context, i int) {
	m := p.results[i]
	t := c.Theme()
	chosen := i == p.chosen
	text, mutedColor := t.Text, t.TextMuted
	if chosen {
		text, mutedColor = t.AccentText, t.AccentText.Alpha(0.78)
	}
	row := ui.Row(c).Padding(7, 10).Gap(10).AlignItems(ui.Center).Radius(8)
	switch {
	case chosen:
		row.Background(t.Accent.Alpha(0.9))
	case row.Hovered():
		row.Background(t.Text.Alpha(0.05))
	}
	row.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			title := m.Label
			if title == "" {
				title = firstLine(m.Replace)
			}
			ui.Text(c, title).TextColor(text).SingleLine().Ellipsis("…")
			if m.Label != "" {
				ui.Text(c, firstLine(m.Replace)).FontSize(11).TextColor(mutedColor).SingleLine().Ellipsis("…")
			}
		})
		keycap(c, m.Trigger, chosen).Shrink(0).MaxWidth(110).Ellipsis("…")
	})
	if row.Clicked() {
		p.commit(m)
	}
}

func (p *palette) preview(c *ui.Context) {
	t := c.Theme()
	m, ok := p.selected()
	if !ok {
		return
	}
	if p.previewID != m.ID || p.previewOf != m.Replace {
		p.previewID, p.previewOf = m.ID, m.Replace
		p.previewText = engine.RenderPreview(m.Replace, p.clipboard, time.Now())
	}
	ui.Column(c).Fill().Children(func() {
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(16, 18).Gap(10).Children(func() {
				ui.Text(c, p.previewText).Font("monospace").FontSize(13).LineHeight(1.45).Wrap().Selectable()
			})
		})
		ui.Divider(c).Background(t.Border.Alpha(0.6))
		ui.Column(c).Padding(10, 18).Gap(5).Children(func() {
			meta := func(k, v string) {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					caption(c, k).Width(64)
					ui.Text(c, v).FontSize(11).SingleLine().Ellipsis("…").Shrink(1)
				})
			}
			meta("Trigger", m.Trigger)
			if m.SourceFile != "" {
				meta("File", m.SourceFile)
			}
			meta("Length", plural(len([]rune(p.previewText)), "character", "characters"))
		})
	})
}
