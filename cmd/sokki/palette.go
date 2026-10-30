package main

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sokki/internal/engine"
	"sokki/internal/logx"
	"sokki/internal/macos"
	"sokki/internal/model"
)

const paletteWidth, paletteHeight = 760, 460

// flashFor is how long the footer says what was done. The palette's
// motion is style.go's; the choice and its preview change at once, as
// fast as the keys.
const flashFor = 2500 * time.Millisecond

// paletteMode is what the palette shows.
type paletteMode int

const (
	modeSearch paletteMode = iota // the matches, to find one and insert it
	modeAdd                       // the New Prompt form (palette_add.go)
	modeSaved                     // what the capture shortcut saved, briefly
)

// palette is the Spotlight-style finder of matches: a panel over every
// app that takes the keyboard without taking the front from the app the
// user is in, which keeps its focus for the injection and its selection
// for a new prompt.
type palette struct {
	a     *app
	win   *mygo.Window
	mode  paletteMode
	query string
	input ui.Handle
	list  ui.ListState

	results []model.Match
	found   []scored // what the last filter found, for the next to narrow
	spare   []scored // reused by filter
	chosen  int
	// folded is the store's matches folded for the search, as of the
	// store's generation foldedGen.
	folded    []folded
	foldedGen uint64
	q         []rune // the query, folded, as of the last filter
	// rowMarks are where q matches the rows shown, by match, made as
	// they show and dropped at each filter.
	rowMarks  map[int64]rowMarks
	clipboard string // a snapshot, for the preview and a new prompt

	// previewText is the preview of the match previewID, whose replacement
	// was previewOf, kept while the choice stays.
	previewID   int64
	previewOf   string
	previewText string

	// opened counts the showings, each of which comes in anew.
	opened int
	// flash is what the footer says was just done.
	flash flash

	add promptForm
	// captured is what the capture shortcut did, for modeSaved.
	captured capture
	// selectedText reads the selection in the app in front:
	// macos.SelectedText, another in tests.
	selectedText func() (text, app string)
}

// flash is a word, at the start of the palette's footer, of what was
// just done there, and the match it did it to, shown until until.
type flash struct {
	text  string
	id    int64
	until time.Time
}

// flashing returns the flash while it shows, and asks for the frame
// that takes it away.
func (p *palette) flashing(c *ui.Context) (flash, bool) {
	left := p.flash.until.Sub(c.Now())
	if left <= 0 {
		return flash{}, false
	}
	c.After(left)
	return p.flash, true
}

func newPalette(a *app) *palette { return &palette{a: a, selectedText: macos.SelectedText} }

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
	if p.win != nil && p.win.IsVisible() && p.mode != modeSaved {
		p.hide()
	} else {
		p.show()
	}
}

func (p *palette) show() {
	p.place(paletteHeight)
	p.a.exp.Ctx.CancelPending("palette-open")
	p.clipboard, _ = macos.PasteboardString()
	p.previewID = 0
	p.flash = flash{}
	p.opened++
	p.mode = modeSearch
	p.query = ""
	p.filter()
	p.input.Focus()
	p.win.Show()
	logx.Debugf("[Palette] show results=%d", len(p.results))
}

// place sizes the palette to height and puts it on the display with the
// pointer, its top a quarter of the way down.
func (p *palette) place(height int) {
	p.ensure()
	d := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	p.win.SetSize(paletteWidth, height)
	p.win.SetPosition(d.WorkArea.X+(d.WorkArea.Width-paletteWidth)/2, d.WorkArea.Y+d.WorkArea.Height/4)
}

func (p *palette) hide() {
	if p.win != nil && p.win.IsVisible() {
		p.win.Hide()
		logx.Debugf("[Palette] hide")
	}
}

// commit types the match into the app the user was in.
func (p *palette) commit(m model.Match) {
	p.hide()
	p.a.exp.InjectFromPalette(m)
	p.a.recency.Bump(m.Key())
}

// copy puts the match on the clipboard, its variables filled in, to
// paste it elsewhere, and says so briefly without the keyboard.
func (p *palette) copy(m model.Match) {
	p.hide()
	copyText(engine.Render(m.Replace, func() string { return p.clipboard }, time.Now()))
	p.a.recency.Bump(m.Key())
	p.captured = capture{gen: p.captured.gen + 1, copied: matchTitle(m)}
	p.notify(copiedFor)
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
	// The keys go first, at the root, where the window's shortcuts are.
	switch p.mode {
	case modeAdd:
		p.addKeys(c)
	case modeSaved:
		if c.Shortcut(0, ui.KeyEscape) {
			p.hide()
		}
	default:
		p.searchKeys(c)
	}
	// Each showing is a new frame, which comes in; each mode a new
	// content, which slides into it.
	c.Key(p.opened)
	ui.Column(c).Fill().Radius(14).Border(0.5, t.Border).Clip().Transition(openTransition).Children(func() {
		c.Key(p.mode)
		content := ui.Column(c).Fill().Transition(modeTransition)
		switch p.mode {
		case modeAdd:
			content.Children(func() { p.addView(c) })
		case modeSaved:
			p.keep(content.Hovered())
			content.Children(func() { p.savedView(c) })
		default:
			content.Children(func() { p.searchView(c) })
		}
	})
}

func (p *palette) searchKeys(c *ui.Context) {
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
	if c.Shortcut(ui.Cmd, ui.KeyC) {
		if m, ok := p.selected(); ok {
			p.copy(m)
		}
	}
	c.OnShortcut(ui.Cmd, ui.KeyComma, p.openSettings)
	c.OnShortcut(ui.Cmd, ui.KeyN, p.quickAdd)
}

func (p *palette) searchView(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Padding(14, 18).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconSearch).FontSize(19).TextColor(t.TextMuted)
		if ui.TextInputBase(c, &p.query).Bind(&p.input).Grow(1).FontSize(19).
			Placeholder("Search matches and prompts").Label("Search matches and prompts").Changed() {
			p.filter()
		}
		if p.query != "" {
			caption(c, plural(len(p.results), "result", "results")).FontFeatures("tnum").SingleLine()
		}
	})
	ui.Divider(c)
	if len(p.results) == 0 {
		ui.Column(c).Grow(1).Children(func() { p.empty(c) })
	} else {
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			p.list.Key = func(i int) any { return p.results[i].ID }
			p.list.Label = func(i int) string { return p.results[i].Title() }
			ui.List(c, &p.list, len(p.results), func(i int) { p.row(c, i) }).Width(300).Padding(6).Gap(1)
			ui.Divider(c).Vertical().Background(t.Border.Alpha(0.6))
			ui.Column(c).Grow(1).Shrink(1).Children(func() { p.preview(c) })
		})
	}
	ui.Divider(c)
	p.footer(c, func() {
		if _, ok := p.selected(); ok {
			hint(c, "↵", "Insert")
			hint(c, "⌘C", "Copy")
			hint(c, "⌘↵", "Edit")
		}
		hint(c, "⌘N", "New Prompt")
		hint(c, "esc", "Close")
	})
}

// footer is the palette's bottom bar: Sokki's name, or for a moment what
// was just done, and the keys that hints shows at its end.
func (p *palette) footer(c *ui.Context, hints func()) {
	t := c.Theme()
	ui.Row(c).Padding(8, 14).Gap(14).AlignItems(ui.Center).Background(t.Text.Alpha(0.025)).Children(func() {
		if f, ok := p.flashing(c); ok {
			c.Key(f.until)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(1).Transition(flashTransition).Children(func() {
				ui.Icon(c, iconMatched).FontSize(14).TextColor(t.Success)
				caption(c, f.text).FontWeight(600).TextColor(t.Text).SingleLine().Ellipsis("…")
			})
		} else {
			if icon := appIcon(); icon != nil {
				ui.Image(c, icon).Size(16, 16)
			}
			caption(c, "Sokki").FontWeight(600)
		}
		ui.Spacer(c)
		hints()
	})
}

func (p *palette) empty(c *ui.Context) {
	if len(p.a.store.Matches) == 0 {
		emptyState(c, iconPrompt, "No Prompts Yet",
			"Save text you reuse as a prompt, then find it here and insert it into any app.", func() {
				ui.PrimaryButton(c, "New Prompt").OnClick(p.quickAdd)
				kbd(c, "⌘N")
			})
		return
	}
	emptyState(c, iconSearch, "No Results", fmt.Sprintf("Nothing matches “%s”.", strings.TrimSpace(p.query)), func() {
		ui.Button(c, "Save as a New Prompt").OnClick(p.quickAdd)
		kbd(c, "⌘N")
	})
}

func (p *palette) row(c *ui.Context, i int) {
	m := p.results[i]
	t := c.Theme()
	chosen := i == p.chosen
	text, mutedColor := rowColors(t, chosen)
	row := ui.Row(c).Padding(7, 10).Gap(10).AlignItems(ui.Center).Radius(8)
	rowFill(c, row, chosen)
	row.Children(func() {
		title := matchTitle(m)
		marks := p.rowMarksOf(m, title)
		titleMarks, triggerMarks := marks.title, marks.trigger
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			var e ui.Element
			if titleMarks != nil {
				mark := ui.Span{Weight: 700}
				if !chosen {
					mark.Color = t.Accent
				}
				e = ui.RichText(c, marked(title, titleMarks, mark)...)
			} else {
				e = titleText(c, title, chosen)
			}
			e.TextColor(text).SingleLine().Ellipsis("…")
			if m.Label != "" {
				ui.Text(c, firstLine(m.Replace)).FontSize(11).TextColor(mutedColor).SingleLine().Ellipsis("…")
			}
		})
		if f, ok := p.flashing(c); ok && f.id == m.ID {
			newBadge(c, chosen)
		}
		triggerCap(c, m, chosen, triggerMarks).Shrink(0).MaxWidth(110).Ellipsis("…")
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
		ui.Column(c).Padding(16, 18, 12, 18).Gap(8).Children(func() {
			heading(c, matchTitle(m)).SingleLine().Ellipsis("…")
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				triggerCap(c, m, false, nil).Shrink(1).Ellipsis("…")
				if m.SourceFile != "" {
					ui.Icon(c, iconFile).FontSize(12).TextColor(t.TextMuted)
					caption(c, m.SourceFile).SingleLine().Ellipsis("…").Shrink(1)
				}
			})
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(0, 18, 16, 18).Children(func() {
				textPlate(c, p.previewText)
			})
		})
		ui.Divider(c).Background(t.Border.Alpha(0.6))
		ui.Row(c).Padding(8, 18).Gap(6).AlignItems(ui.Center).Children(func() {
			caption(c, plural(utf8.RuneCountInString(p.previewText), "character", "characters")).FontFeatures("tnum")
			if strings.Contains(m.Replace, "{{") {
				caption(c, "·")
				caption(c, "Variables filled in as of now").SingleLine().Ellipsis("…").Shrink(1)
			}
			ui.Spacer(c)
			if at, ok := p.a.recency.Last(m.Key()); ok {
				caption(c, "Used "+ago(at, time.Now())).SingleLine()
			}
		})
	})
}

// newBadge marks the row of a prompt just saved.
func newBadge(c *ui.Context, chosen bool) {
	t := c.Theme()
	fg, bg := t.Success, t.Success.Alpha(0.14)
	if chosen {
		fg, bg = t.AccentText, t.AccentText.Alpha(0.2)
	}
	ui.Text(c, "New").FontSize(10).FontWeight(700).TextColor(fg).SingleLine().
		Padding(1, 6).Radius(8).Background(bg).Shrink(0)
}
