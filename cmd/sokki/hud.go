package main

import (
	"strings"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sokki/internal/macos"
	"sokki/internal/model"
)

// hudCapacity is how many events the HUD keeps.
const hudCapacity = 20

// hudPanel is Recent Activity: what Sokki did with the last things typed,
// for finding out why a trigger did not expand. It floats over every app
// without taking the front.
type hudPanel struct {
	a        *app
	win      *mygo.Window
	events   []model.HUDEvent // newest first
	revealed map[int64]bool
	list     ui.ListState
}

func newHUDPanel(a *app) *hudPanel { return &hudPanel{a: a, revealed: map[int64]bool{}} }

func (h *hudPanel) record(ev model.HUDEvent) {
	h.events = append([]model.HUDEvent{ev}, h.events...)
	if len(h.events) > hudCapacity {
		h.events = h.events[:hudCapacity]
	}
	if h.win != nil && h.win.IsVisible() {
		h.win.Invalidate()
	}
}

func (h *hudPanel) toggle() {
	if h.win == nil {
		h.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "Sokki Recent Activity",
			Panel:     true,
			Hidden:    true,
			Width:     380,
			Height:    420,
			MinWidth:  320,
			MinHeight: 240,
			Content:   ui.View(h.view),
		})
		h.a.hideOnClose(h.win, h.win.Hide)
		h.win.Center()
	}
	if h.win.IsVisible() {
		h.win.Hide()
		return
	}
	h.win.ShowInactive() // the app in front keeps the keyboard
}

// createMatch opens a new match with the typed text as its trigger.
func (h *hudPanel) createMatch(buffer string) {
	h.a.settings.show()
	h.a.settings.tab = 0
	h.a.settings.matches.newMatch(buffer)
}

func (h *hudPanel) view(c *ui.Context) {
	if len(h.events) == 0 {
		ui.Column(c).Fill().Center().Gap(6).Padding(24).Children(func() {
			ui.Text(c, "No recent activity").Bold().FontSize(15)
			muted(c, "Type in another app — matches and blocks appear here.").TextAlign(ui.Center)
		})
		return
	}
	h.list.Key = func(i int) any { return h.events[i].ID }
	ui.List(c, &h.list, len(h.events), func(i int) { h.row(c, i) }).Fill().Padding(6)
}

func compact(v model.Verdict) bool {
	return v != model.VerdictTooSoon && v != model.VerdictDisabled
}

// masked hides what was typed, but its spaces, until the row is clicked.
func masked(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return r
		}
		return '•'
	}, s)
}

func (h *hudPanel) row(c *ui.Context, i int) {
	ev := h.events[i]
	t := c.Theme()
	bg := ui.Transparent
	if i%2 == 1 {
		bg = t.Surface
	}
	row := ui.Row(c).Padding(6, 8).Gap(8).AlignItems(ui.Start).Radius(6).Background(bg).Children(func() {
		verdictChip(c, ev.Verdict)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			if !compact(ev.Verdict) {
				buf := ev.Buffer
				if !h.revealed[ev.ID] {
					buf = masked(buf)
				}
				ui.Text(c, buf).Font("monospace").SingleLine().Ellipsis("…")
			}
			if ev.MatchedTrigger != "" {
				if compact(ev.Verdict) {
					ui.Text(c, ev.MatchedTrigger).Font("monospace").SingleLine()
				} else {
					caption(c, "→ "+ev.MatchedTrigger).SingleLine()
				}
			}
			if ev.Note != "" {
				caption(c, ev.Note).MaxLines(2)
			}
		})
		caption(c, ev.Time.Format("15:04:05")).FontFeatures("tnum")
	})
	if row.Clicked() {
		h.revealed[ev.ID] = !h.revealed[ev.ID]
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("Create match from this").Chosen() {
			h.createMatch(ev.Buffer)
		}
		if m.Item("Copy").Chosen() {
			macos.SetPasteboardString(ev.Buffer)
		}
	})
}

func verdictChip(c *ui.Context, v model.Verdict) {
	t := c.Theme()
	label, tint := "Matched", t.Success
	switch v {
	case model.VerdictTooSoon:
		label, tint = "Too soon", t.Warning
	case model.VerdictDisabled:
		label, tint = "Disabled", t.TextMuted
	case model.VerdictAIThinking:
		label, tint = "AI…", ui.RGB(59, 130, 246)
	case model.VerdictAICompleted:
		label, tint = "AI done", t.Success
	case model.VerdictAIFailed:
		label, tint = "AI failed", t.Danger
	}
	ui.Text(c, label).FontSize(11).Bold().Padding(2, 6).Radius(8).Background(tint.Alpha(0.25)).SingleLine()
}
