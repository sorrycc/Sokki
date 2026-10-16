package main

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sokki/internal/engine"
	"sokki/internal/logx"
	"sokki/internal/macos"
	"sokki/internal/model"
)

const paletteWidth, paletteHeight = 720, 420

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
	targetPID int    // the app in front when it opened
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
	p.targetPID = macos.FrontmostPID()
	p.clipboard, _ = macos.PasteboardString()
	// On the display with the pointer, its top a quarter of the way down.
	d := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	w, _ := p.win.Size()
	p.win.SetPosition(d.WorkArea.X+(d.WorkArea.Width-w)/2, d.WorkArea.Y+d.WorkArea.Height/4)
	p.query = ""
	p.filter()
	p.input.Focus()
	p.win.Show()
	logx.Debugf("[Palette] show target=%d results=%d", p.targetPID, len(p.results))
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
	head := []rune(m.Replace)
	if len(head) > 200 {
		head = head[:200]
	}
	if s, ok := engine.FuzzyScore(q, string(head)); ok && (!any || max(0, s-2) > best) {
		best, any = max(0, s-2), true
	}
	return best, any
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
	p.a.exp.InjectFromPalette(m, p.targetPID)
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

func (p *palette) view(c *ui.Context) {
	t := c.Theme()
	root := c.Root()
	if c.Vibrancy() {
		root.Background(ui.Transparent)
	} else {
		root.Background(t.Background)
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

	ui.Column(c).Fill().Radius(12).Border(0.5, t.Border).Clip().Children(func() {
		ui.Row(c).Padding(12, 14).AlignItems(ui.Center).Children(func() {
			if ui.TextInputBase(c, &p.query).Bind(&p.input).Grow(1).FontSize(16).
				Placeholder("Search…").Label("Search matches").Changed() {
				p.filter()
			}
		})
		ui.Divider(c)
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			p.list.Key = func(i int) any { return p.results[i].ID }
			p.list.Label = func(i int) string { return p.results[i].Title() }
			ui.List(c, &p.list, len(p.results), func(i int) { p.row(c, i) }).Width(280).Padding(6)
			ui.Divider(c).Vertical()
			ui.Column(c).Grow(1).Children(func() { p.preview(c) })
		})
	})
}

func (p *palette) row(c *ui.Context, i int) {
	m := p.results[i]
	t := c.Theme()
	chosen := i == p.chosen
	text, mutedColor, bg := t.Text, t.TextMuted, ui.Transparent
	if chosen {
		text, mutedColor, bg = t.AccentText, t.AccentText.Alpha(0.75), t.Accent.Alpha(0.85)
	}
	row := ui.Column(c).Padding(5, 10).Gap(2).Radius(7).Background(bg).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			title := ui.Text(c, m.Title()).TextColor(text).SingleLine().Shrink(1)
			if m.Label == "" {
				title.Font("monospace")
			} else {
				ui.Text(c, m.Trigger).Font("monospace").FontSize(11).TextColor(mutedColor).SingleLine().Shrink(1)
			}
			if m.IsAI() {
				aiBadge(c)
			}
		})
		ui.Text(c, previewLine(m)).FontSize(11).TextColor(mutedColor).SingleLine()
	})
	if row.Clicked() {
		p.commit(m)
	}
}

func previewLine(m model.Match) string {
	if m.Prompt != nil {
		return firstLine(model.Glyphs(*m.Prompt))
	}
	return firstLine(m.Replace)
}

func (p *palette) preview(c *ui.Context) {
	m, ok := p.selected()
	if !ok {
		msg := "No results"
		if len(p.a.store.Matches) == 0 {
			msg = "No matches"
		}
		ui.Column(c).Fill().Center().Children(func() { muted(c, msg) })
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Padding(14).Gap(12).Children(func() {
			if m.Prompt != nil {
				ui.Column(c).Gap(3).Children(func() {
					ui.Text(c, "PROMPT").FontSize(11).Bold().TextColor(c.Theme().TextMuted)
					ui.Text(c, *m.Prompt).Font("monospace").Wrap().Selectable()
				})
				caption(c, "Runs Claude ("+p.a.store.Config.AI.Model+") via the local `claude` CLI. `{{selection}}` / `{{clipboard}}` placeholders are filled at run time. Result replaces the placeholder “⟨generating…⟩”.").Wrap()
				return
			}
			ui.Text(c, engine.RenderPreview(m.Replace, p.clipboard, time.Now())).Font("monospace").Wrap().Selectable()
		})
	})
}
