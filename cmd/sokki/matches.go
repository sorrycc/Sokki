package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"sokki/internal/config"
	"sokki/internal/model"
)

// matchList is the Matches page: the matches, of every file or of one,
// filtered by a search, to add, edit, turn off and delete.
type matchList struct {
	a      *app
	file   string // "" is every file
	query  string
	chosen int
	list   ui.ListState
	search ui.Handle

	shown []model.Match
	// key is what shown was filtered for: the store's generation, the
	// file and the query. Frames that change none of them keep it.
	key    filterKey
	lowers []string // the triggers and replacements in lower case, by match

	deleteOpen bool
	toDelete   model.Match
}

type filterKey struct {
	gen         uint64
	file, query string
	valid       bool
}

func (l *matchList) setFile(file string) {
	if l.file != file {
		l.file = file
		l.chosen = 0
	}
}

func (l *matchList) filter() {
	key := filterKey{l.a.store.Gen, l.file, l.query, true}
	if key == l.key {
		return
	}
	if key.gen != l.key.gen || !l.key.valid {
		l.lowers = l.lowers[:0]
		for _, m := range l.a.store.Matches {
			l.lowers = append(l.lowers, strings.ToLower(m.Trigger)+"\x00"+strings.ToLower(m.Label)+"\x00"+strings.ToLower(m.Replace))
		}
	}
	l.key = key
	q := strings.ToLower(strings.TrimSpace(l.query))
	l.shown = l.shown[:0]
	for i, m := range l.a.store.Matches {
		if l.file != "" && m.SourceFile != l.file {
			continue
		}
		if q != "" && !strings.Contains(l.lowers[i], q) {
			continue
		}
		l.shown = append(l.shown, m)
	}
	l.chosen = min(l.chosen, len(l.shown)-1)
}

func (l *matchList) selected() (model.Match, bool) {
	if l.chosen < 0 || l.chosen >= len(l.shown) {
		return model.Match{}, false
	}
	return l.shown[l.chosen], true
}

func (l *matchList) move(delta int) {
	if len(l.shown) == 0 {
		return
	}
	if l.chosen < 0 {
		if delta > 0 {
			l.chosen = 0
		} else {
			l.chosen = len(l.shown) - 1
		}
	} else {
		l.chosen = max(0, min(len(l.shown)-1, l.chosen+delta))
	}
	l.list.ScrollIntoView(l.chosen)
}

func (l *matchList) prefill() string {
	if l.file != "" {
		return l.file
	}
	return config.DefaultMatchFile
}

func (l *matchList) edit(m model.Match) { l.a.editor.edit(m) }

func (l *matchList) newMatch(trigger string) {
	l.a.editor.create(l.prefill(), trigger)
}

func (l *matchList) requestDelete(m model.Match) {
	l.toDelete, l.deleteOpen = m, true
}

func (l *matchList) toggle(m model.Match) {
	m.Enabled = !m.Enabled
	l.a.store.Update(m)
}

func (l *matchList) view(c *ui.Context) {
	l.filter()
	t := c.Theme()
	c.OnShortcut(ui.Cmd, ui.KeyF, func() { l.search.Focus() })
	c.OnShortcut(ui.Cmd, ui.KeyN, func() { l.newMatch("") })

	ui.Column(c).Fill().Children(func() {
		bar := c.TitleBar()
		ui.Row(c).Padding(max(bar.Height, 12), 16, 12, 20).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
			title, total := "All Matches", len(l.a.store.Matches)
			if l.file != "" {
				title, total = l.file, l.a.store.FileCounts()[l.file]
			}
			ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
				ui.Text(c, title).FontSize(17).Bold().SingleLine()
				sub := plural(total, "match", "matches")
				if q := strings.TrimSpace(l.query); q != "" {
					sub = fmt.Sprintf("%d of %s", len(l.shown), sub)
				}
				caption(c, sub)
			})
			search := ui.SearchField(c, &l.query).Bind(&l.search).Label("Search").Placeholder("Search").Width(220)
			if search.Changed() {
				l.filter()
			}
			if search.Focused() {
				if search.Shortcut(0, ui.KeyUp) {
					l.move(-1)
				}
				if search.Shortcut(0, ui.KeyDown) {
					l.move(1)
				}
				if search.Shortcut(0, ui.KeyEnter) {
					if m, ok := l.selected(); ok {
						l.edit(m)
					}
				}
			}
			ui.PrimaryButton(c, "").Label("Add match").Tooltip("New match (⌘N)").Children(func() {
				ui.Icon(c, iconPlus)
				ui.Text(c, "New").SingleLine()
			}).OnClick(func() { l.newMatch("") })
		})
		ui.Divider(c)

		l.list.Selected = &l.chosen
		l.list.Key = func(i int) any { return l.shown[i].ID }
		l.list.Label = func(i int) string { return l.shown[i].Trigger }
		list := ui.List(c, &l.list, len(l.shown), func(i int) { l.row(c, i) }).Grow(1).Padding(6, 8).Gap(2).Children(func() {
			if len(l.shown) == 0 {
				title, detail := l.emptyText()
				emptyState(c, iconMatches, title, detail)
			}
		})
		if list.Submitted() {
			if m, ok := l.selected(); ok {
				l.edit(m)
			}
		}
		if list.Focused() && (list.Shortcut(0, ui.KeyBackspace) || list.Shortcut(0, ui.KeyDelete)) {
			if m, ok := l.selected(); ok {
				l.requestDelete(m)
			}
		}

		if err := l.a.store.LastError; err != "" {
			ui.Row(c).Padding(8, 16).Gap(8).AlignItems(ui.Center).Background(t.Warning.Alpha(0.12)).Children(func() {
				ui.Icon(c, iconWarning).FontSize(13).TextColor(t.Warning)
				ui.Text(c, err).FontSize(11).SingleLine().Ellipsis("…").Shrink(1).Grow(1).Tooltip(err)
			})
		} else {
			ui.Divider(c)
			ui.Row(c).Padding(7, 16).Gap(16).AlignItems(ui.Center).Children(func() {
				hint(c, "↵", "to edit")
				hint(c, "⌫", "to delete")
				ui.Spacer(c)
				caption(c, "Right-click a match for more").SingleLine()
			})
		}
	})

	if ui.AlertDialog(c, &l.deleteOpen, "Delete Match?",
		fmt.Sprintf("The match %q will be permanently deleted. This action cannot be undone.", l.toDelete.Trigger),
		"Delete", "Cancel") == 0 {
		if _, ok := l.a.store.Match(l.toDelete.ID); ok {
			l.a.store.Remove(l.toDelete.ID)
		}
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func (l *matchList) emptyText() (title, detail string) {
	switch {
	case strings.TrimSpace(l.query) != "":
		return "No Results", fmt.Sprintf("No match has “%s”.", strings.TrimSpace(l.query))
	case l.file != "":
		return "No Matches Yet", "This file has no matches. Add one with New."
	}
	return "No Matches Yet", "A match types its replacement when you type its trigger. Add one with New."
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func (l *matchList) row(c *ui.Context, i int) {
	m := l.shown[i]
	t := c.Theme()
	chosen := i == l.chosen
	text, mutedColor := t.Text, t.TextMuted
	if chosen {
		text, mutedColor = t.AccentText, t.AccentText.Alpha(0.78)
	}
	row := ui.Row(c).Padding(7, 10).Gap(12).AlignItems(ui.Center).Radius(7)
	switch {
	case chosen:
		row.Background(t.Accent)
	case row.Hovered():
		row.Background(t.Text.Alpha(0.045))
	}
	if !m.Enabled {
		row.Opacity(0.55)
	}
	row.Children(func() {
		ui.Box(c).Width(120).Shrink(0).Children(func() {
			keycap(c, m.Trigger, chosen).AlignSelf(ui.Start).Shrink(1)
		})
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			if m.Label != "" {
				ui.Text(c, m.Label).FontWeight(500).TextColor(text).SingleLine().Ellipsis("…")
				ui.Text(c, firstLine(m.Replace)).FontSize(11).TextColor(mutedColor).SingleLine().Ellipsis("…")
			} else {
				ui.Text(c, firstLine(m.Replace)).TextColor(text).SingleLine().Ellipsis("…")
			}
		})
		if !m.Enabled {
			ui.Text(c, "Off").FontSize(10).FontWeight(600).TextColor(mutedColor).Padding(1, 6).Radius(4).Border(0.5, mutedColor)
		}
		if l.file == "" && m.SourceFile != "" {
			ui.Text(c, m.SourceFile).FontSize(11).TextColor(mutedColor).SingleLine().Ellipsis("…").MaxWidth(130)
		}
	})
	row.ContextMenu(func(menu *ui.Menu) {
		if menu.Item("Edit…").Chosen() {
			l.edit(m)
		}
		if menu.Item("Copy Replacement").Chosen() {
			copyText(m.Replace)
		}
		label := "Turn Off"
		if !m.Enabled {
			label = "Turn On"
		}
		if menu.Item(label).Chosen() {
			l.toggle(m)
		}
		menu.Separator()
		if menu.Item("Delete…").Chosen() {
			l.requestDelete(m)
		}
	})
}
