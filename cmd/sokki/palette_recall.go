package main

import (
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"sokki/internal/config"
	"sokki/internal/engine"
	"sokki/internal/history"
)

// editForm is the palette's text to edit before inserting it, from ⇥:
// a match's with its variables filled in, or a prompt of the history's.
// What is edited is inserted once; the match or the prompt stays as it
// was.
type editForm struct {
	of   result
	text string
	// orig is the text as the edit began, which Revert puts back.
	orig string
	in   ui.Handle
}

// startEdit turns the palette into the text of r, to edit before
// inserting it.
func (p *palette) startEdit(r result) {
	text := r.text()
	if !r.entry {
		text = engine.Render(r.m.Replace, func() string { return p.clipboard }, time.Now())
	}
	p.edit = editForm{of: r, text: text, orig: text}
	p.mode = modeEdit
	p.edit.in.Focus()
}

// insertEdited puts the edited text into the app the user was in, and
// with send presses Return after it.
func (p *palette) insertEdited(send bool) {
	f := p.edit
	p.hide()
	if f.of.entry {
		p.a.recall(f.of.e, f.text, send)
		return
	}
	m := f.of.m
	p.a.used(m)
	p.a.exp.InsertFromPalette(f.text, send, func() {
		p.a.post(func() { p.a.logUse(m, utf8.RuneCountInString(f.text), true) })
	})
}

func (p *palette) editKeys(c *ui.Context) {
	if c.Shortcut(0, ui.KeyEscape) {
		p.mode = modeSearch
		p.input.Focus()
	}
	switch {
	case c.Shortcut(ui.Cmd|ui.Alt, ui.KeyEnter):
		p.insertEdited(true)
	case c.Shortcut(ui.Cmd, ui.KeyEnter):
		p.insertEdited(false)
	}
}

func (p *palette) editView(c *ui.Context) {
	t := c.Theme()
	f := &p.edit
	note := "The match stays as it is; this is for this time only."
	if f.of.entry {
		note = "The history keeps the prompt as it was sent."
	}
	ui.Row(c).Padding(14, 18).Gap(10).AlignItems(ui.Center).Children(func() {
		iconPlate(c, iconPencil, t.Accent, 28)
		ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
			heading(c, "Edit, Then Insert").SingleLine()
			caption(c, note).SingleLine().Ellipsis("…")
		})
		ui.Button(c, "Insert and Send").OnClick(func() { p.insertEdited(true) })
		ui.PrimaryButton(c, "Insert").OnClick(func() { p.insertEdited(false) })
	})
	ui.Divider(c)
	ui.Column(c).Grow(1).Shrink(1).Padding(14, 18).Gap(6).Children(func() {
		ui.TextArea(c, &f.text).Bind(&f.in).Label("Text to insert").Font("monospace").Grow(1).MinHeight(120)
		ui.Row(c).Gap(6).AlignItems(ui.Center).MinHeight(22).Children(func() {
			// Once changed, the title gives way to saying so, with the
			// way back.
			edited := f.text != f.orig
			c.Key(edited)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(1).Transition(fadeTransition).Children(func() {
				if !edited {
					caption(c, f.of.title()).SingleLine().Ellipsis("…").Shrink(1)
					return
				}
				ui.Text(c, "Edited").FontSize(10).FontWeight(700).TextColor(t.Accent).SingleLine().
					Padding(1, 6).Radius(8).Background(t.Accent.Alpha(0.12)).Shrink(0)
				revert := ui.ButtonBase(c).Label("Revert").Padding(1, 4).Radius(4).Children(func() {
					ui.Icon(c, iconUndo).FontSize(11).TextColor(t.Accent)
					ui.Text(c, "Revert").FontSize(11).TextColor(t.Accent).SingleLine()
				}).Gap(3).AlignItems(ui.Center)
				if revert.Hovered() {
					revert.Background(t.Accent.Alpha(0.08))
				}
				if revert.Clicked() {
					f.text = f.orig
					f.in.Focus()
				}
			})
			ui.Spacer(c)
			caption(c, plural(utf8.RuneCountInString(f.text), "character", "characters")).FontFeatures("tnum").Shrink(0)
		})
	})
	ui.Divider(c)
	p.footer(c, func() {
		hint(c, "⌘↵", "Insert")
		hint(c, "⌥⌘↵", "Insert and Send")
		hint(c, "esc", "Back")
	})
}

// recall puts text, the prompt e as sent or as edited, into the app in
// front, and counts it as a use of e.
func (a *app) recall(e history.Entry, text string, send bool) {
	a.history.Recall(e.ID, "")
	agent := ""
	if len(e.Sources) > 0 {
		agent = e.Sources[0]
	}
	a.exp.InsertFromPalette(text, send, func() {
		a.post(func() { a.logRecall(agent, text) })
	})
}

// saveEntry turns the palette into the New Prompt form, filled with the
// prompt e, to keep it as a prompt of its own.
func (p *palette) saveEntry(e history.Entry) {
	f := &p.add
	f.label, f.trigger, f.file = p.uniqueLabel(promptLabel(e.Text)), "", config.PromptFile
	f.body, f.source, f.sourceIcon = e.Text, "From "+agentNames(e), iconHistory
	p.mode = modeAdd
	f.labelIn.Focus()
}

// hideEntry hides e from the history, saying so with ⌘Z to undo it.
func (p *palette) hideEntry(c *ui.Context, e history.Entry) {
	at := p.chosen
	p.a.history.Hide(e.ID)
	p.hidden = e.ID
	p.filter()
	p.choose(at)
	p.flash = flash{text: "Hidden from history · ⌘Z to undo", id: e.ID, until: c.Now().Add(flashFor)}
}

func (p *palette) unhideEntry() {
	id := p.hidden
	p.a.history.Unhide(id)
	p.hidden, p.flash = 0, flash{}
	p.filter()
	p.chooseID(id)
}

// scopeNote says where a search found nothing.
func scopeNote(scope int) string {
	switch scope {
	case scopeMatches:
		return " in your matches"
	case scopeHistory:
		return " in the agents’ history"
	}
	return ""
}

// agentTints tell the agents apart at a glance, in rows, the preview
// and Settings: one hue each, read on light and dark alike.
var agentTints = map[string]ui.Color{
	"claude":      ui.Hex("#d97757"),
	"codex":       ui.Hex("#7c8aa0"),
	"gemini":      ui.Hex("#4285f4"),
	"antigravity": ui.Hex("#8b5cf6"),
	"qoder":       ui.Hex("#14b8a6"),
	"grok":        ui.Hex("#ec4899"),
	"opencode":    ui.Hex("#f59e0b"),
	"aider":       ui.Hex("#22c55e"),
}

// agentTint is the hue of the agent of id, the muted text's for one
// unknown.
func agentTint(t *ui.Theme, id string) ui.Color {
	if tint, ok := agentTints[id]; ok {
		return tint
	}
	return t.TextMuted
}

// agentDot is the small round mark of an agent: in the accent's text
// color on a chosen row, where a hue near the accent's would not read.
func agentDot(c *ui.Context, id string, chosen bool) ui.Element {
	tint := agentTint(c.Theme(), id)
	if chosen {
		tint = c.Theme().AccentText.Alpha(0.85)
	}
	return ui.Box(c).Size(7, 7).Radius(3.5).Background(tint).Shrink(0)
}

// agentChip is an agent by its name after its dot, on a hairline plate.
func agentChip(c *ui.Context, id, name string, chosen bool) ui.Element {
	t := c.Theme()
	fg, border := t.TextMuted, t.Text.Alpha(0.14)
	if chosen {
		fg, border = t.AccentText.Alpha(0.85), t.AccentText.Alpha(0.3)
	}
	return ui.Row(c).Gap(5).AlignItems(ui.Center).Padding(2, 7).Radius(5).Border(0.5, border).Children(func() {
		agentDot(c, id, chosen)
		ui.Text(c, name).FontSize(11).TextColor(fg).SingleLine()
	})
}

// agentCap is the plate of a prompt of the history where a match has
// its trigger: the agent it was sent in, and how many more.
func agentCap(c *ui.Context, e history.Entry, chosen bool) ui.Element {
	id, name := "", "Agent"
	if len(e.Sources) > 0 {
		id, name = e.Sources[0], history.SourceName(e.Sources[0])
	}
	if len(e.Sources) > 1 {
		name += " +" + strconv.Itoa(len(e.Sources)-1)
	}
	return agentChip(c, id, name, chosen).Tooltip("Sent in " + agentNames(e))
}

func (p *palette) entryRow(c *ui.Context, i int) {
	r := p.results[i]
	e := r.e
	t := c.Theme()
	chosen := i == p.chosen
	text, mutedColor := rowColors(t, chosen)
	row := ui.Row(c).Padding(7, 10).Gap(10).AlignItems(ui.Center).Radius(8)
	if p.arrived[e.ID] {
		row.Transition(arriveTransition)
	}
	rowFill(c, row, chosen)
	row.Children(func() {
		title := e.Title()
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			titleMarked(c, title, p.rowMarksOf(r, title).title, chosen).TextColor(text).SingleLine().Ellipsis("…")
			ui.Text(c, entryDetail(e, time.Now())).FontSize(11).TextColor(mutedColor).SingleLine().Ellipsis("…")
		})
		agentCap(c, e, chosen).Shrink(0).MaxWidth(120).Ellipsis("…")
	})
	if row.Clicked() {
		p.commit(r, false)
	}
}

// entryDetail is the line under a prompt of the history: its project,
// how often and when it was last sent.
func entryDetail(e history.Entry, now time.Time) string {
	s := ""
	if pr := e.Project(); pr != "" {
		s = pr + " · "
	}
	if e.Count > 1 {
		s += strconv.Itoa(e.Count) + "× · "
	}
	return s + ago(time.Unix(e.Last, 0), now)
}

func (p *palette) entryPreview(c *ui.Context, e history.Entry) {
	t := c.Theme()
	now := time.Now()
	ui.Column(c).Fill().Children(func() {
		ui.Column(c).Padding(16, 18, 12, 18).Gap(8).Children(func() {
			heading(c, e.Title()).SingleLine().Ellipsis("…")
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				for _, src := range e.Sources {
					agentChip(c, src, history.SourceName(src), false).Shrink(0)
				}
				if pr := e.Project(); pr != "" {
					ui.Box(c).Width(2).Shrink(0)
					ui.Icon(c, iconFile).FontSize(12).TextColor(t.TextMuted)
					caption(c, pr).SingleLine().Ellipsis("…").Shrink(1).Tooltip(e.Projects[0])
				}
			})
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(0, 18, 16, 18).Children(func() {
				textPlate(c, e.Text)
			})
		})
		ui.Divider(c).Background(t.Border.Alpha(0.6))
		ui.Row(c).Padding(8, 18).Gap(6).AlignItems(ui.Center).Children(func() {
			caption(c, plural(utf8.RuneCountInString(e.Text), "character", "characters")).FontFeatures("tnum")
			caption(c, "·")
			caption(c, "Sent "+times(e.Count)).FontFeatures("tnum").SingleLine()
			if e.Recalls > 0 {
				caption(c, "· "+strconv.Itoa(e.Recalls)+" from Sokki").FontFeatures("tnum").SingleLine()
			}
			ui.Spacer(c)
			last := "Last sent " + ago(time.Unix(e.Last, 0), now)
			if e.Count > 1 && e.Last-e.First >= 24*60*60 {
				last += ", first " + ago(time.Unix(e.First, 0), now)
			}
			caption(c, last).SingleLine()
		})
	})
}

// times says how many times: once, twice, 3 times.
func times(n int) string {
	switch n {
	case 1:
		return "once"
	case 2:
		return "twice"
	}
	return strconv.Itoa(n) + " times"
}
