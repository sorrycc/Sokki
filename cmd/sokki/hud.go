package main

import (
	"strings"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sokki/internal/config"
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
	// titles are the titles of the matches events expanded, by event, as
	// of when they did.
	titles map[int64]string
	list   ui.ListState
}

func newHUDPanel(a *app) *hudPanel {
	return &hudPanel{a: a, revealed: map[int64]bool{}, titles: map[int64]string{}}
}

func (h *hudPanel) record(ev model.HUDEvent) {
	h.events = append([]model.HUDEvent{ev}, h.events...)
	if len(h.events) > hudCapacity {
		for _, old := range h.events[hudCapacity:] {
			delete(h.titles, old.ID)
			delete(h.revealed, old.ID)
		}
		h.events = h.events[:hudCapacity]
	}
	if ev.Verdict == model.VerdictMatched && ev.MatchedTrigger != "" {
		for _, m := range h.a.store.Matches {
			if m.Trigger == ev.MatchedTrigger {
				h.titles[ev.ID] = matchTitle(m)
				break
			}
		}
	}
	if h.win != nil && h.win.IsVisible() {
		h.win.Invalidate()
	}
}

func (h *hudPanel) toggle() {
	if h.win == nil {
		h.win = mygo.NewWindow(mygo.WindowOptions{
			Title:         "Sokki Recent Activity",
			Panel:         true,
			Hidden:        true,
			Width:         380,
			Height:        460,
			MinWidth:      320,
			MinHeight:     240,
			TitleBarStyle: mygo.TitleBarHidden,
			Content:       ui.View(h.view),
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
	h.a.editor.create(config.DefaultMatchFile, strings.TrimSpace(buffer))
}

func copyText(s string) { macos.SetPasteboardString(s) }

func (h *hudPanel) view(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		bar := c.TitleBar()
		ui.Row(c).Height(max(bar.Height, 36)).Padding(0, 10, 0, max(bar.Left, 14)).Gap(8).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Icon(c, iconHistory).FontSize(14).TextColor(t.TextMuted)
			ui.Text(c, "Recent Activity").FontWeight(600).Grow(1).SingleLine()
			if len(h.events) > 0 {
				iconButton(c, iconTrash, "Clear").OnClick(func() {
					h.events, h.revealed, h.titles = nil, map[int64]bool{}, map[int64]string{}
				})
			}
		})
		ui.Divider(c)
		if len(h.events) == 0 {
			emptyState(c, iconHistory, "No Recent Activity", "Type in another app: expansions and what blocked them show here.", nil)
			return
		}
		h.list.Key = func(i int) any { return h.events[i].ID }
		ui.List(c, &h.list, len(h.events), func(i int) { h.row(c, i) }).Grow(1).Padding(6).Gap(2)
		ui.Divider(c)
		ui.Row(c).Padding(7, 14).Children(func() {
			caption(c, "Click to reveal what was typed · right-click for more").SingleLine().Ellipsis("…")
		})
	})
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
	row := ui.Row(c).Padding(8, 10).Gap(10).AlignItems(ui.Start).Radius(8)
	if row.Hovered() {
		row.Background(t.Text.Alpha(0.05))
	}
	row.Children(func() {
		verdictIcon(c, ev.Verdict)
		ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, verdictLabel(ev.Verdict)).FontWeight(600).SingleLine().Grow(1)
				caption(c, ev.Time.Format("15:04:05")).FontFeatures("tnum")
			})
			if !compact(ev.Verdict) {
				buf := ev.Buffer
				if !h.revealed[ev.ID] {
					buf = masked(buf)
				}
				ui.Text(c, buf).Font("monospace").FontSize(12).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
			}
			if ev.MatchedTrigger != "" {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if !compact(ev.Verdict) {
						caption(c, "Would have expanded")
					}
					keycap(c, ev.MatchedTrigger, false).Shrink(0).MaxWidth(140).Ellipsis("…")
					if title := h.titles[ev.ID]; title != "" && compact(ev.Verdict) {
						caption(c, "→").Shrink(0)
						caption(c, title).SingleLine().Ellipsis("…").Shrink(1)
					}
				})
			}
			if ev.Note != "" {
				caption(c, ev.Note).Wrap().MaxLines(2)
			}
		})
	})
	if row.Clicked() {
		h.revealed[ev.ID] = !h.revealed[ev.ID]
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("New Match from This…").Chosen() {
			h.createMatch(ev.Buffer)
		}
		if m.Item("Copy").Chosen() {
			copyText(ev.Buffer)
		}
	})
}

func verdictLabel(v model.Verdict) string {
	switch v {
	case model.VerdictTooSoon:
		return "Too soon after a paste"
	case model.VerdictDisabled:
		return "Sokki is paused"
	}
	return "Expanded"
}

func verdictIcon(c *ui.Context, v model.Verdict) {
	t := c.Theme()
	icon, tint := iconMatched, t.Success
	switch v {
	case model.VerdictTooSoon:
		icon, tint = iconWarning, t.Warning
	case model.VerdictDisabled:
		icon, tint = iconPaused, t.TextMuted
	}
	ui.Box(c).Size(26, 26).Radius(7).Background(tint.Alpha(0.14)).Center().Shrink(0).Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(tint)
	})
}
