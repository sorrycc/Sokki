package main

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
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
	// It comes back as a reading's prompts come in.
	if p.arrived == nil {
		p.arrived = map[int64]time.Time{}
	}
	p.arrived[id] = time.Time{}
}

// scopeNote says where a search found nothing.
func scopeNote(scope int) string {
	switch scope {
	case scopeMatches:
		return " in your matches"
	}
	return ""
}

// agentTints tell the agents apart at a glance, in rows, the preview
// and Settings: one hue each, deeper on light, lighter on dark, where
// either reads as well.
var agentTints = map[string][2]ui.Color{
	"claude":      {ui.Hex("#d2693f"), ui.Hex("#e8906f")},
	"codex":       {ui.Hex("#6b7a90"), ui.Hex("#9aa7bb")},
	"gemini":      {ui.Hex("#3b78e7"), ui.Hex("#6ea1ff")},
	"antigravity": {ui.Hex("#7c4dea"), ui.Hex("#a78bfa")},
	"qoder":       {ui.Hex("#0f9f8f"), ui.Hex("#2dd4bf")},
	"grok":        {ui.Hex("#db2777"), ui.Hex("#f472b6")},
	"opencode":    {ui.Hex("#d97706"), ui.Hex("#fbbf24")},
	"aider":       {ui.Hex("#16a34a"), ui.Hex("#4ade80")},
}

// agentTint is the hue of the agent of id in the theme t, the muted
// text's for one unknown.
func agentTint(t *ui.Theme, id string) ui.Color {
	tints, ok := agentTints[id]
	if !ok {
		return t.TextMuted
	}
	if t.Dark {
		return tints[1]
	}
	return tints[0]
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
	rowFill(c, row, chosen)
	p.arriving(c, row, e.ID, chosen)
	row.Children(func() {
		title := e.Title()
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			titleMarked(c, title, p.rowMarksOf(r, title).title, chosen).TextColor(text).SingleLine().Ellipsis("…")
			ui.Text(c, entryDetail(e, time.Now())).FontSize(11).TextColor(mutedColor).SingleLine().Ellipsis("…")
		})
		agentCap(c, e, chosen).Shrink(0).MaxWidth(120).Ellipsis("…")
	})
	p.rowClicks(row, i)
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

// arriving fades in the row of an entry a reading just brought, with a
// wash of the accent under it that fades out after: by hand, frame by
// frame, as MyGo's List leaves a row's Enter transition out.
func (p *palette) arriving(c *ui.Context, row ui.Element, id int64, chosen bool) {
	at, ok := p.arrived[id]
	if !ok {
		return
	}
	now := c.Now()
	if at.IsZero() {
		at = now
		p.arrived[id] = at
	}
	k := float32(now.Sub(at)) / float32(arriveFor)
	if k >= 1 {
		delete(p.arrived, id)
		return
	}
	if c.Preferences().ReduceMotion {
		k = max(k, 1-float32(motionFast)/float32(arriveFor))
	}
	// The row is whole by a third of the way; the wash goes on after.
	in := min(1, 3*k)
	row.Opacity(1 - (1-in)*(1-in))
	if !chosen && !row.Hovered() {
		row.Background(c.Theme().Accent.Alpha(0.14 * (1 - k) * (1 - k)))
	}
	c.After(time.Second / 60)
}

func (p *palette) entryPreview(c *ui.Context, e history.Entry) {
	t := c.Theme()
	now := time.Now()
	ui.Column(c).Fill().Children(func() {
		// A hairline under the head once the text scrolls under it.
		head := ui.Column(c).Padding(16, 18, 12, 18).Gap(8).BorderWidth(0, 0, 0.5, 0)
		edge := t.Border.Alpha(0)
		if p.previewScroll.Y > 1 {
			edge = t.Border.Alpha(0.6)
		}
		head.BorderColor(head.AnimateColor("edge", edge, hoverAnim))
		head.Children(func() {
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
		hits := p.entryHits(e)
		ui.Scroll(c).TrackScroll(&p.previewScroll).Grow(1).Children(func() {
			ui.Column(c).Padding(0, 18, 16, 18).Children(func() {
				if hits == nil {
					textPlate(c, e.Text)
					return
				}
				mark := ui.Span{Background: t.Accent.Alpha(0.22), Radius: 3}
				if t.Dark {
					mark.Background = t.Accent.Alpha(0.4)
				}
				plate(c, ui.RichText(c, hitSpans(e.Text, hits, mark)...))
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

// previewHits are where the query is found in the text of the entry
// previewed, worked out once for the entry and the query.
type previewHits struct {
	id     int64
	query  string
	ranges [][2]int
}

// maxHits bounds the places marked in a preview.
const maxHits = 200

// previewTextWidth is the width the preview's text wraps at: the
// palette's, less the list's and the plate's margins.
const previewTextWidth = paletteWidth - listWidth - 1 - 2*18 - 2*12 - 1

// entryHits returns where the query is found in the text of e, and as
// the entry or the query changes scrolls the preview to the first place,
// a little above it: at once for another entry, as the choice changes,
// gliding for a query typed on the same.
func (p *palette) entryHits(e history.Entry) [][2]int {
	h := &p.hits
	if h.id == e.ID && h.query == p.query {
		return h.ranges
	}
	same := h.id == e.ID
	*h = previewHits{id: e.ID, query: p.query, ranges: p.q.Ranges(e.Text, maxHits)}
	y := float32(0)
	if len(h.ranges) > 0 {
		y = textY(e.Text[:h.ranges[0][0]]) - 3*previewLine
	}
	if y < previewLine*4 {
		y = 0
	}
	if same {
		p.previewScroll.ScrollTo(0, y, ui.Animation{Duration: motionBase})
	} else {
		p.previewScroll.Y = y
	}
	return h.ranges
}

// previewLine is the height of a line of the preview's text.
const previewLine = 12 * 1.45

// textY is how far down the preview's text the end of prefix, its start,
// shows: by the lines it wraps into, as shaped at the preview's width.
func textY(prefix string) float32 {
	if !strings.Contains(prefix, "\n") && utf8.RuneCountInString(prefix) < 40 {
		return 0
	}
	l := ui.ShapeText(prefix, ui.Font{Family: "monospace", Size: 12}, previewTextWidth)
	at := l.Caret(utf16Len(prefix))
	if at.H <= 0 {
		return 0
	}
	return float32(math.Round(float64(at.Y/at.H))) * previewLine
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// hitSpans is text with the ranges marked.
func hitSpans(text string, ranges [][2]int, mark ui.Span) []ui.Span {
	spans := make([]ui.Span, 0, 2*len(ranges)+1)
	at := 0
	for _, r := range ranges {
		if r[0] > at {
			spans = append(spans, ui.Span{Text: text[at:r[0]]})
		}
		spans = append(spans, styled(text[r[0]:r[1]], true, mark))
		at = r[1]
	}
	if at < len(text) {
		spans = append(spans, ui.Span{Text: text[at:]})
	}
	return spans
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
