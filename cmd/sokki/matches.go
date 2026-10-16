package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"sokki/internal/config"
	"sokki/internal/model"
)

const allFiles = "All Files"

// matchList is the Matches tab: the matches, filtered by file and by a
// search, to add, edit, turn off and delete.
type matchList struct {
	a      *app
	file   string // "" is every file
	query  string
	chosen int
	list   ui.ListState
	search ui.Handle

	shown []model.Match

	deleteOpen bool
	toDelete   model.Match
}

func (l *matchList) filter() {
	q := strings.ToLower(strings.TrimSpace(l.query))
	l.shown = l.shown[:0]
	for _, m := range l.a.store.Matches {
		if l.file != "" && m.SourceFile != l.file {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(m.Trigger), q) && !strings.Contains(strings.ToLower(m.Replace), q) {
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

func (l *matchList) view(c *ui.Context) {
	l.filter()
	t := c.Theme()
	c.OnShortcut(ui.Cmd, ui.KeyF, func() { l.search.Focus() })
	c.OnShortcut(ui.Cmd, ui.KeyN, func() { l.newMatch("") })

	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(10, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			files := append([]string{allFiles}, l.a.store.SourceFiles()...)
			file := l.file
			if file == "" {
				file = allFiles
			}
			if ui.Select(c, &file, files).Label("Source file").Changed() {
				if file == allFiles {
					file = ""
				}
				l.file = file
				l.filter()
			}
			search := ui.SearchField(c, &l.query).Bind(&l.search).Label("Search").Grow(1)
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
		})
		ui.Divider(c)

		l.list.Selected = &l.chosen
		l.list.Key = func(i int) any { return l.shown[i].ID }
		l.list.Label = func(i int) string { return l.shown[i].Trigger }
		list := ui.List(c, &l.list, len(l.shown), func(i int) { l.row(c, i) }).Grow(1).Padding(4).Children(func() {
			if len(l.shown) == 0 {
				ui.Column(c).Fill().Center().Gap(6).Padding(24).Children(func() {
					ui.Text(c, "No matches").Bold().FontSize(15)
					muted(c, l.emptyText())
				})
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
		ui.Divider(c)

		ui.Row(c).Padding(8).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Button(c, "+").Tooltip("Add match (⌘N)").Label("Add match").OnClick(func() { l.newMatch("") })
			_, has := l.selected()
			ui.Button(c, "−").Tooltip("Remove match").Label("Remove match").Disabled(!has).OnClick(func() {
				if m, ok := l.selected(); ok {
					l.requestDelete(m)
				}
			})
			ui.Spacer(c)
			if err := l.a.store.LastError; err != "" {
				ui.Text(c, "⚠ "+err).FontSize(11).TextColor(t.Warning).SingleLine().Shrink(1)
			}
		})
	})

	if ui.AlertDialog(c, &l.deleteOpen, "Delete Match?",
		fmt.Sprintf("The match %q will be permanently deleted. This action cannot be undone.", l.toDelete.Trigger),
		"Delete", "Cancel") == 0 {
		if _, ok := l.a.store.Match(l.toDelete.ID); ok {
			l.a.store.Remove(l.toDelete.ID)
		}
	}
}

func (l *matchList) emptyText() string {
	switch {
	case strings.TrimSpace(l.query) != "":
		return "Try a different search or clear the filter."
	case l.file != "":
		return "No matches in this file yet."
	}
	return "Add a match with the + button below."
}

// subtitle is the second line of a match's row: its replacement, or its
// prompt with the placeholders as ⟨…⟩.
func subtitle(m model.Match) string {
	if m.Prompt != nil {
		return firstLine(model.Glyphs(*m.Prompt))
	}
	return firstLine(m.Replace)
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func (l *matchList) row(c *ui.Context, i int) {
	m := l.shown[i]
	t := c.Theme()
	chosen := i == l.chosen
	text, mutedColor := t.Text, t.TextMuted
	if chosen {
		text, mutedColor = t.AccentText, t.AccentText.Alpha(0.75)
	}
	if !m.Enabled && !chosen {
		text = t.TextMuted
	}
	bg := ui.Transparent
	if chosen {
		bg = t.Accent
	}
	row := ui.Row(c).Padding(6, 10).Gap(8).AlignItems(ui.Center).Radius(6).Background(bg).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, m.Trigger).Font("monospace").TextColor(text).SingleLine()
			ui.Text(c, subtitle(m)).FontSize(11).TextColor(mutedColor).SingleLine()
		})
		if m.SourceFile != "" {
			ui.Text(c, m.SourceFile).FontSize(11).TextColor(mutedColor).SingleLine().MaxWidth(140)
		}
		if m.IsAI() {
			aiBadge(c)
		}
		if !m.Enabled {
			ui.Badge(c, "Disabled")
		}
	})
	row.ContextMenu(func(menu *ui.Menu) {
		if menu.Item("Edit").Chosen() {
			l.edit(m)
		}
		if menu.Item("Delete").Chosen() {
			l.requestDelete(m)
		}
		menu.Separator()
		label := "Disable"
		if !m.Enabled {
			label = "Enable"
		}
		if menu.Item(label).Chosen() {
			m.Enabled = !m.Enabled
			l.a.store.Update(m)
		}
	})
}

// aiBadge marks an AI match, in blue, as the Swift app did.
func aiBadge(c *ui.Context) {
	ui.Text(c, "AI").FontSize(10).Bold().TextColor(ui.RGB(255, 255, 255)).
		Padding(1, 6).Radius(8).Background(ui.RGB(59, 130, 246).Alpha(0.85))
}
