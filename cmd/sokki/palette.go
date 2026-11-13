package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sokki/internal/engine"
	"sokki/internal/logx"
	"sokki/internal/macos"
	"sokki/internal/model"
	"sokki/internal/search"
)

const paletteWidth, paletteHeight = 760, 460

// listWidth is the width of the palette's list, beside the preview.
const listWidth = 300

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
	modeEdit                      // the chosen text, to edit before inserting (palette_recall.go)
)

// palette is the Spotlight-style finder of matches and of the prompts
// sent to agents (palette_recall.go): a panel over every
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

	// scope is what the palette searches: scopeAll, scopeMatches or
	// scopeHistory.
	scope   int
	results []result
	// days are the list's rows while the History scope lists without a
	// query, its prompts under the day they were last sent: the result
	// each row shows, or -1-day for the heading of day; nil otherwise,
	// when each row is its result. dayCounts are each day's results.
	days      []int
	dayCounts [3]int
	found     []scored // what the last filter found, for the next to narrow
	spare     []scored // reused by filter
	chosen    int
	// index is the matches and the history as the search compares them.
	index index
	// q is the query as of the last filter, in the scope qScope.
	q      search.Query
	qScope int
	// rowMarks are where q matches the rows shown, by match, made as
	// they show and dropped at each filter.
	rowMarks  map[int64]rowMarks
	clipboard string // a snapshot, for the preview and a new prompt

	// previewText is the preview of the match previewID, whose replacement
	// was previewOf, kept while the choice stays.
	previewID   int64
	previewOf   string
	previewText string
	// hits are where the query is found in the prompt previewed, and
	// previewScroll how far its text is scrolled, to the first of them.
	hits          previewHits
	previewScroll ui.ScrollState

	// opened counts the showings, each of which comes in anew.
	opened int
	// flash is what the footer says was just done.
	flash flash
	// news is how many prompts a reading just brought, which the footer
	// says at the next frame.
	news int

	add promptForm
	// edit is the text of modeEdit.
	edit editForm
	// hidden is the history entry ⌘⌫ hid last, which ⌘Z shows again
	// while the footer says so.
	hidden int64
	// arrived are the history's entries that a reading brought while
	// the palette showed, which fade in, until it shows again.
	arrived map[int64]bool
	// captured is what the capture shortcut did, for modeSaved.
	captured capture
	// selectedText reads the selection in the app in front:
	// macos.SelectedText, another in tests.
	selectedText func() (text, app string)
}

// flash is a word, at the start of the palette's footer, of what was
// just done there, and the match it did it to, shown until until, after
// its icon: a check when none is given.
type flash struct {
	text  string
	icon  *ui.SVG
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

// arrivedNote has the footer say that a reading brought n prompts, as
// they fade into the list, unless it says something else.
func (p *palette) arrivedNote(n int) {
	if p.win != nil && p.win.IsVisible() && p.mode == modeSearch {
		p.news = n
		p.win.Invalidate()
	}
}

func newPalette(a *app) *palette { return &palette{a: a, selectedText: macos.SelectedText} }

func (p *palette) invalidate() {
	if p != nil && p.win != nil && p.win.IsVisible() {
		p.refilter()
		p.win.Invalidate()
	}
}

// refilter filters again after the matches or the history changed under
// the palette: the choice stays where it was, and the prompts that came
// in fade in.
func (p *palette) refilter() {
	r, ok := p.selected()
	was := make(map[int64]bool, len(p.results))
	for _, r := range p.results {
		was[r.id()] = true
	}
	p.filter()
	for _, r := range p.results {
		if r.entry && !was[r.id()] {
			if p.arrived == nil {
				p.arrived = map[int64]bool{}
			}
			p.arrived[r.id()] = true
		}
	}
	if ok {
		p.chooseID(r.id())
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
	p.flash, p.news, p.hits = flash{}, 0, previewHits{}
	p.opened++
	clear(p.arrived)
	p.mode = modeSearch
	p.query = ""
	p.filter()
	p.input.Focus()
	p.win.Show()
	p.a.scanHistory() // what was sent since, which comes in as it is read
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

// commit puts the match or the prompt into the app the user was in,
// and with send presses Return after it, which sends it to an agent.
func (p *palette) commit(r result, send bool) {
	p.hide()
	if r.entry {
		p.a.recall(r.e, r.e.Text, send)
		return
	}
	p.a.exp.InjectFromPalette(r.m, send)
	p.a.used(r.m)
}

// copy puts the match on the clipboard, its variables filled in, or the
// prompt, to paste it elsewhere, and says so briefly without the
// keyboard.
func (p *palette) copy(r result) {
	p.hide()
	if r.entry {
		copyText(r.e.Text)
		p.a.history.Recall(r.e.ID, "")
	} else {
		copyText(engine.Render(r.m.Replace, func() string { return p.clipboard }, time.Now()))
		p.a.used(r.m)
	}
	p.captured = capture{gen: p.captured.gen + 1, copied: r.title()}
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
	case modeEdit:
		p.editKeys(c)
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
		case modeEdit:
			content.Children(func() { p.editView(c) })
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
	r, ok := p.selected()
	switch {
	case c.Shortcut(ui.Cmd, ui.KeyEnter):
		if ok && !r.entry {
			p.openEditor(r.m)
		} else if ok {
			p.saveEntry(r.e)
		}
	case c.Shortcut(ui.Alt, ui.KeyEnter):
		if ok {
			p.commit(r, true)
		}
	case c.Shortcut(0, ui.KeyEnter):
		if ok {
			p.commit(r, false)
		}
	}
	if ok && c.Shortcut(0, ui.KeyTab) {
		p.startEdit(r)
	}
	if c.Shortcut(ui.Cmd, ui.KeyC) && ok {
		p.copy(r)
	}
	if ok && r.entry {
		if c.Shortcut(ui.Cmd, ui.KeyS) {
			p.saveEntry(r.e)
		}
		if c.Shortcut(ui.Cmd, ui.KeyBackspace) {
			p.hideEntry(c, r.e)
		}
	}
	if f, ok := p.flashing(c); ok && f.id == p.hidden && p.hidden != 0 && c.Shortcut(ui.Cmd, ui.KeyZ) {
		p.unhideEntry()
	}
	for i := range scopeLabels {
		c.OnShortcut(ui.Cmd, ui.Key1+ui.Key(i), func() { p.setScope(i) })
	}
	c.OnShortcut(ui.Cmd, ui.KeyComma, p.openSettings)
	c.OnShortcut(ui.Cmd, ui.KeyN, p.quickAdd)
}

// setScope searches matches, history or both.
func (p *palette) setScope(scope int) {
	p.scope = scope
	p.list = ui.ListState{}
	p.filter()
	p.input.Focus()
}

func (p *palette) searchView(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Padding(14, 18).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconSearch).FontSize(19).TextColor(t.TextMuted)
		if ui.TextInputBase(c, &p.query).Bind(&p.input).Grow(1).FontSize(19).
			Placeholder(searchHelp(p.scope)).Label("Search").Changed() {
			p.filter()
		}
		if p.query != "" {
			caption(c, plural(len(p.results), "result", "results")).FontFeatures("tnum").SingleLine()
		}
		scope := p.scope
		if ui.Segmented(c, &scope, scopeLabels...).Label("Search in").Changed() {
			p.setScope(scope)
		}
	})
	ui.Divider(c)
	if len(p.results) == 0 {
		ui.Column(c).Grow(1).Children(func() { p.empty(c) })
	} else {
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			rows, row := len(p.results), func(i int) { p.row(c, i) }
			p.list.Key = func(i int) any { return p.results[i].id() }
			p.list.Label = func(i int) string { return p.results[i].title() }
			if p.days != nil {
				rows = len(p.days)
				row = func(i int) {
					if r := p.days[i]; r >= 0 {
						p.row(c, r)
					} else {
						p.dayHeading(c, -1-r, i == 0)
					}
				}
				p.list.Key = func(i int) any {
					if r := p.days[i]; r >= 0 {
						return p.results[r].id()
					}
					return dayKey(-1 - p.days[i])
				}
				p.list.Label = func(i int) string {
					if r := p.days[i]; r >= 0 {
						return p.results[r].title()
					}
					return dayNames[-1-p.days[i]]
				}
			}
			ui.List(c, &p.list, rows, row).Width(listWidth).Padding(6).Gap(1)
			ui.Divider(c).Vertical().Background(t.Border.Alpha(0.6))
			ui.Column(c).Grow(1).Shrink(1).Children(func() { p.preview(c) })
		})
	}
	ui.Divider(c)
	p.footer(c, func() {
		if r, ok := p.selected(); ok {
			hint(c, "↵", "Insert")
			hint(c, "⌥↵", "Send")
			hint(c, "⇥", "Edit")
			if r.entry {
				hint(c, "⌘S", "Save as Prompt")
				hint(c, "⌘⌫", "Hide")
			} else {
				hint(c, "⌘C", "Copy")
				hint(c, "⌘↵", "Open")
			}
		} else {
			hint(c, "⌘N", "New Prompt")
		}
		hint(c, "esc", "Close")
	})
}

// footer is the palette's bottom bar: Sokki's name, or for a moment what
// was just done, and the keys that hints shows at its end.
func (p *palette) footer(c *ui.Context, hints func()) {
	t := c.Theme()
	if p.news > 0 {
		if _, busy := p.flashing(c); !busy {
			p.flash = flash{text: plural(p.news, "new prompt", "new prompts") + " from your agents", icon: iconHistory, until: c.Now().Add(flashFor)}
		}
		p.news = 0
	}
	ui.Row(c).Padding(8, 14).Gap(14).AlignItems(ui.Center).Background(t.Text.Alpha(0.025)).Children(func() {
		if f, ok := p.flashing(c); ok {
			c.Key(f.until)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(1).Transition(flashTransition).Children(func() {
				if f.icon != nil {
					ui.Icon(c, f.icon).FontSize(13).TextColor(t.Accent)
				} else {
					ui.Icon(c, iconMatched).FontSize(14).TextColor(t.Success)
				}
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
	if p.scope == scopeHistory && p.q.Empty() {
		emptyState(c, iconHistory, "No Agent History",
			"Prompts you send in Claude Code, Codex, Gemini and other agents show here, to find and send again anywhere.", func() {
				ui.Button(c, "History Settings…").OnClick(func() { p.hide(); p.a.settings.showPage(historyPage) })
			})
		return
	}
	if len(p.a.store.Matches) == 0 && p.scope != scopeHistory && p.q.Empty() {
		emptyState(c, iconPrompt, "No Prompts Yet",
			"Save text you reuse as a prompt, then find it here and insert it into any app.", func() {
				ui.PrimaryButton(c, "New Prompt").OnClick(p.quickAdd)
				kbd(c, "⌘N")
			})
		return
	}
	emptyState(c, iconSearch, "No Results", fmt.Sprintf("Nothing matches “%s”%s.", strings.TrimSpace(p.query), scopeNote(p.scope)), func() {
		ui.Button(c, "Save as a New Prompt").OnClick(p.quickAdd)
		kbd(c, "⌘N")
	})
}

func (p *palette) row(c *ui.Context, i int) {
	r := p.results[i]
	if r.entry {
		p.entryRow(c, i)
		return
	}
	m := r.m
	t := c.Theme()
	chosen := i == p.chosen
	text, mutedColor := rowColors(t, chosen)
	row := ui.Row(c).Padding(7, 10).Gap(10).AlignItems(ui.Center).Radius(8)
	rowFill(c, row, chosen)
	row.Children(func() {
		title := matchTitle(m)
		marks := p.rowMarksOf(r, title)
		titleMarks, triggerMarks := marks.title, marks.trigger
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			titleMarked(c, title, titleMarks, chosen).TextColor(text).SingleLine().Ellipsis("…")
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
		p.commit(r, false)
	}
}

// dayHeading heads the prompts of a day in the History scope: its name
// and how many, in small type, a little apart from the day above.
func (p *palette) dayHeading(c *ui.Context, day int, first bool) {
	t := c.Theme()
	top := float32(12)
	if first {
		top = 4
	}
	ui.Row(c).Padding(top, 10, 4, 10).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, dayNames[day]).FontSize(11).FontWeight(600).TextColor(t.TextMuted).SingleLine()
		ui.Spacer(c)
		ui.Text(c, strconv.Itoa(p.dayCounts[day])).FontSize(11).TextColor(t.TextMuted.Alpha(0.7)).FontFeatures("tnum").SingleLine()
	})
}

// titleMarked is a row's title, where the query matched it in bold.
func titleMarked(c *ui.Context, title string, marks []int, chosen bool) ui.Element {
	if marks == nil {
		return titleText(c, title, chosen)
	}
	mark := ui.Span{Weight: 700}
	if !chosen {
		mark.Color = c.Theme().Accent
	}
	return ui.RichText(c, marked(title, marks, mark)...)
}

func (p *palette) preview(c *ui.Context) {
	t := c.Theme()
	r, ok := p.selected()
	if !ok {
		return
	}
	if r.entry {
		p.entryPreview(c, r.e)
		return
	}
	m := r.m
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
			if n := p.a.history.MatchUse(m.Key()).Count; n > 1 {
				caption(c, plural(n, "use", "uses")).FontFeatures("tnum").SingleLine()
				caption(c, "·")
			}
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
