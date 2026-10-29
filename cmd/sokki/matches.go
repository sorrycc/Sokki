package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sokki/internal/config"
	"sokki/internal/engine"
	"sokki/internal/macos"
	"sokki/internal/model"
)

// matchList is the Matches page: the matches, of every file or of one,
// filtered by a search, to add, edit, turn off and delete.
type matchList struct {
	a      *app
	file   string // "" is every file
	query  string
	chosen int
	search ui.Handle

	listState ui.ListState
	listWidth float32 // of the list, beside the detail

	// previewText is the preview of the match previewID, whose replacement
	// was previewOf, kept while the choice stays.
	previewID   int64
	previewOf   string
	previewText string

	shown []model.Match
	// key is what shown was filtered for: the store's generation, the
	// file and the query. Frames that change none of them keep it.
	key    filterKey
	lowers []string // the triggers and replacements in lower case, by match
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
	l.listState.ScrollIntoView(l.chosen)
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

func (l *matchList) duplicate(m model.Match) { l.a.editor.duplicate(m) }

// remove deletes m at once, with Undo in a toast to put it back where it
// was.
func (l *matchList) remove(c *ui.Context, m model.Match) {
	i := slices.IndexFunc(l.a.store.Matches, func(x model.Match) bool { return x.ID == m.ID })
	if i < 0 {
		return
	}
	l.a.store.Remove(m.ID)
	c.ToastAction(fmt.Sprintf("Deleted %s", m.Key()), "Undo", func() {
		l.a.store.Insert(i, m)
	})
}

// copyReplacement puts m's replacement on the clipboard, and says so.
func copyReplacement(c *ui.Context, m model.Match) {
	copyText(m.Replace)
	c.Toast("Copied to the clipboard")
}

func (l *matchList) toggle(m model.Match) {
	m.Enabled = !m.Enabled
	l.a.store.Update(m)
}

// shortcuts are the page's keys. Settings declares them at the top of
// the window, so that they work wherever the focus is, the sidebar too.
func (l *matchList) shortcuts(c *ui.Context) {
	c.OnShortcut(ui.Cmd, ui.KeyF, func() { l.search.Focus() })
	c.OnShortcut(ui.Cmd, ui.KeyN, func() { l.newMatch("") })
	c.OnShortcut(ui.Cmd, ui.KeyD, func() {
		if m, ok := l.selected(); ok {
			l.duplicate(m)
		}
	})
}

func (l *matchList) view(c *ui.Context) {
	l.filter()
	t := c.Theme()

	ui.Column(c).Fill().Children(func() {
		bar := c.TitleBar()
		ui.Row(c).Padding(max(bar.Height, 12), 16, 12, 20).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
			title, total := "All Matches", len(l.a.store.Matches)
			if l.file != "" {
				title, total = l.file, l.a.store.FileCounts()[l.file]
			}
			ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
				ui.Text(c, title).FontSize(17).Bold().SingleLine().Ellipsis("…")
				sub := plural(total, "match", "matches")
				if q := strings.TrimSpace(l.query); q != "" {
					sub = fmt.Sprintf("%d of %s", len(l.shown), sub)
				}
				caption(c, sub).FontFeatures("tnum")
			})
			search := ui.SearchField(c, &l.query).Bind(&l.search).Label("Search").Placeholder("Search").Tooltip("Search matches (⌘F)").Width(200)
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
		if !l.a.trusted {
			accessBanner(c, l.a)
		}

		if len(l.shown) == 0 {
			ui.Column(c).Grow(1).Children(func() {
				title, detail := l.emptyText()
				emptyState(c, iconMatches, title, detail)
			})
		} else {
			if l.listWidth == 0 {
				l.listWidth = 280
			}
			ui.Split(c, &l.listWidth, func() { l.list(c) }, func() { l.detail(c) }).Grow(1)
		}

		if err := l.a.store.LastError; err != "" {
			ui.Row(c).Padding(8, 16).Gap(8).AlignItems(ui.Center).Background(t.Warning.Alpha(0.12)).
				BorderWidth(0.5, 0, 0, 0).BorderColor(t.Warning.Alpha(0.25)).Children(func() {
				ui.Icon(c, iconWarning).FontSize(13).TextColor(t.Warning)
				ui.Text(c, err).FontSize(11).SingleLine().Ellipsis("…").Shrink(1).Grow(1).Tooltip(err)
				ui.Button(c, "Open Folder").Label("Open the config folder").OnClick(func() {
					mygo.Shell.OpenPath(string(l.a.dir))
				})
			})
		}
	})
}

// accessBanner asks for the Accessibility permission, without which no
// match expands.
func accessBanner(c *ui.Context, a *app) {
	t := c.Theme()
	ui.Row(c).Padding(10, 16, 10, 20).Gap(12).AlignItems(ui.Center).Background(t.Warning.Alpha(0.1)).
		BorderWidth(0, 0, 0.5, 0).BorderColor(t.Warning.Alpha(0.25)).Children(func() {
		ui.Icon(c, iconWarning).FontSize(16).TextColor(t.Warning)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, "Sokki needs Accessibility access").FontWeight(600)
			caption(c, "Until you allow it in System Settings, Sokki can't see what you type, and no trigger expands.").Wrap()
		})
		ui.Button(c, "Open System Settings").OnClick(a.grantAccess)
	})
}

// list is the column of matches, one row each.
func (l *matchList) list(c *ui.Context) {
	l.listState.Selected = &l.chosen
	l.listState.Key = func(i int) any { return l.shown[i].ID }
	l.listState.Label = func(i int) string { return l.shown[i].Title() }
	list := ui.List(c, &l.listState, len(l.shown), func(i int) { l.row(c, i) }).Fill().Padding(6, 8).Gap(1)
	if list.Submitted() {
		if m, ok := l.selected(); ok {
			l.edit(m)
		}
	}
	if list.Focused() && (list.Shortcut(0, ui.KeyBackspace) || list.Shortcut(0, ui.KeyDelete)) {
		if m, ok := l.selected(); ok {
			l.remove(c, m)
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
	row := ui.Row(c).Padding(7, 10).Gap(10).AlignItems(ui.Center).Radius(7).MinHeight(46)
	switch {
	case chosen:
		row.Background(t.Accent)
	case row.Hovered():
		row.Background(t.Text.Alpha(0.045))
	}
	row.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Opacity(enabledOpacity(m)).Children(func() {
			titleText(c, matchTitle(m), chosen).FontWeight(500).TextColor(text).SingleLine().Ellipsis("…")
			sub := firstLine(m.Replace)
			if m.Label == "" {
				sub = summary(m.Replace)
			}
			ui.Text(c, sub).FontSize(11).TextColor(mutedColor).SingleLine().Ellipsis("…")
		})
		if !m.Enabled {
			ui.Text(c, "Off").FontSize(10).FontWeight(600).TextColor(mutedColor).Padding(1, 6).Radius(4).Border(0.5, mutedColor)
		}
		triggerCap(c, m, chosen, nil).Shrink(0).MaxWidth(120).Ellipsis("…").Opacity(enabledOpacity(m))
	})
	row.ContextMenu(func(menu *ui.Menu) {
		if menu.Item("Edit…").Chosen() {
			l.edit(m)
		}
		if menu.Item("Duplicate…").Chosen() {
			l.duplicate(m)
		}
		if menu.Item("Copy Replacement").Chosen() {
			copyReplacement(c, m)
		}
		label := "Turn Off"
		if !m.Enabled {
			label = "Turn On"
		}
		if menu.Item(label).Chosen() {
			l.toggle(m)
		}
		menu.Separator()
		if menu.Item("Delete").Chosen() {
			l.remove(c, m)
		}
	})
}

// matchTitle is what a match is called in lists: its label, or the first
// line of its replacement, marked when more lines follow.
func matchTitle(m model.Match) string {
	title := m.Title()
	if m.Label == "" && title != m.Trigger && strings.Contains(strings.TrimSpace(m.Replace), "\n") {
		title += " …"
	}
	return title
}

// titleText shows a match's title, its variables in the accent's color
// but on a chosen row, which is the accent's color already.
func titleText(c *ui.Context, s string, chosen bool) ui.Element {
	if chosen || !strings.Contains(s, "{{") {
		return ui.Text(c, s)
	}
	spans := replacementSpans(c, s)
	for i := range spans {
		spans[i].Background, spans[i].Radius = ui.Color{}, 0
	}
	return ui.RichText(c, spans...)
}

func enabledOpacity(m model.Match) float32 {
	if m.Enabled {
		return 1
	}
	return 0.55
}

// summary describes a replacement under a match without a label, whose
// first line is its title already: how long it is.
func summary(s string) string {
	lines := strings.Count(s, "\n") + 1
	n := utf8.RuneCountInString(s)
	if lines > 1 {
		return plural(lines, "line", "lines") + " · " + plural(n, "character", "characters")
	}
	return plural(n, "character", "characters")
}

// detail shows the chosen match whole: its title, trigger and file, its
// replacement, a preview of its variables, and what can be done with it.
func (l *matchList) detail(c *ui.Context) {
	t := c.Theme()
	m, ok := l.selected()
	if !ok {
		emptyState(c, iconMatches, "No Match Chosen", "Choose a match to see it whole.")
		return
	}
	ui.Column(c).Fill().Children(func() {
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(18, 22).Gap(18).Children(func() {
				ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
					ui.Column(c).Grow(1).Shrink(1).Gap(8).Children(func() {
						ui.Text(c, matchTitle(m)).FontSize(16).Bold().Wrap().MaxLines(2).Ellipsis("…")
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							triggerCap(c, m, false, nil).Shrink(1).Ellipsis("…")
							if m.SourceFile != "" {
								ui.Icon(c, iconFile).FontSize(12).TextColor(t.TextMuted)
								caption(c, m.SourceFile).SingleLine().Ellipsis("…").Shrink(1)
							}
						})
					})
					on := m.Enabled
					if ui.Switch(c, &on).Label("Enabled").Tooltip("Turn this match on or off").Changed() {
						l.toggle(m)
					}
				})
				section(c, "Replacement", func() {
					replacementPlate(c, m.Replace)
				})
				if strings.Contains(m.Replace, "{{") {
					section(c, "Preview", func() {
						textPlate(c, l.preview(m))
					})
				}
				section(c, "Details", func() {
					used := "Never"
					if at, ok := l.a.recency.Last(m.Key()); ok {
						used = ago(at, time.Now())
					}
					facts(c, [][2]string{
						{"Length", summary(m.Replace)},
						{"Inserted", l.delivery(m)},
						{"Last used", used},
					})
				})
			})
		})
		ui.Divider(c).Background(t.Border.Alpha(0.6))
		ui.Row(c).Padding(10, 16).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Button(c, "Edit…").Label("Edit match").OnClick(func() { l.edit(m) })
			ui.Spacer(c)
			iconButton(c, iconCopy, "Copy replacement").OnClick(func() { copyReplacement(c, m) })
			iconButton(c, iconDupe, "Duplicate match (⌘D)").OnClick(func() { l.duplicate(m) })
			iconButton(c, iconTrash, "Delete match (⌫)").OnClick(func() { l.remove(c, m) })
		})
	})
}

// facts shows pairs of a name and a value, one a row, on a card.
func facts(c *ui.Context, rows [][2]string) {
	t := c.Theme()
	ui.Column(c).Radius(8).Border(0.5, t.Border.Alpha(0.7)).Clip().Children(func() {
		for i, r := range rows {
			row := ui.Row(c).Padding(7, 12).Gap(12).AlignItems(ui.Center)
			if i > 0 {
				row.BorderWidth(0.5, 0, 0, 0).BorderColor(t.Border.Alpha(0.6))
			}
			row.Children(func() {
				muted(c, r[0]).FontSize(12).Width(76).Shrink(0).SingleLine()
				ui.Text(c, r[1]).FontSize(12).FontFeatures("tnum").SingleLine().Ellipsis("…").Grow(1).Shrink(1).Selectable()
			})
		}
	})
}

// delivery says how a match's replacement reaches the app: typed, or
// pasted when it is longer than the clipboard threshold, and only from
// the palette for a match without a trigger.
func (l *matchList) delivery(m model.Match) string {
	pasted := utf8.RuneCountInString(m.Replace) > l.a.store.Config.ClipboardThreshold
	switch {
	case m.Trigger == "" && pasted:
		return "From the palette, pasted"
	case m.Trigger == "":
		return "From the palette, typed"
	case pasted:
		return "Pasted through the clipboard"
	}
	return "Typed key by key"
}

// preview renders m's variables, again only when the match or its
// replacement changed: it reads the clipboard.
func (l *matchList) preview(m model.Match) string {
	if l.previewID != m.ID || l.previewOf != m.Replace {
		clip, _ := macos.PasteboardString()
		l.previewID, l.previewOf = m.ID, m.Replace
		l.previewText = engine.RenderPreview(m.Replace, clip, time.Now())
	}
	return l.previewText
}
