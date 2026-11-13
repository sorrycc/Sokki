package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sokki/internal/history"
)

// historyForm is what the Agent History page edits before it applies it:
// the texts to leave out, applied at once, would drop prompts at every
// key typed.
type historyForm struct {
	loaded       bool
	ignore       string
	minChars     int
	confirmClear bool
	confirmRead  bool
}

// load reads the settings into the form, as the page first shows.
func (f *historyForm) load(st history.Settings) {
	f.loaded = true
	f.ignore = strings.Join(st.Ignore, "\n")
	f.minChars = st.MinChars
	if f.minChars <= 0 {
		f.minChars = history.DefaultMinChars
	}
}

// settings returns st with the form's changes.
func (f *historyForm) settings(st history.Settings) history.Settings {
	st.Ignore = nil
	for line := range strings.Lines(f.ignore) {
		if line = strings.TrimSpace(line); line != "" && !slices.Contains(st.Ignore, line) {
			st.Ignore = append(st.Ignore, line)
		}
	}
	st.MinChars = f.minChars
	if st.MinChars == history.DefaultMinChars {
		st.MinChars = 0
	}
	return st
}

// agentHistory is the Agent History page: the agents read, what is left
// out, and clearing it.
func (s *settingsWindow) agentHistory(c *ui.Context) {
	h := s.a.history
	f := &s.historyForm
	st := h.Settings()
	if !f.loaded {
		f.load(st)
	}
	t := c.Theme()
	scrollPage(c, "Agent History", "What you sent to coding agents, to find in the palette and send again in any of them.", func() {
		s.historySummary(c)
		counts := h.Counts()
		group(c, "Agents", func() {
			for i, src := range history.Sources {
				on := st.On(src.ID)
				if agentSetting(c, src, counts[src.ID], h.Found(src.ID), h.Where(src.ID), &on, i == 0) {
					s.a.setSource(src.ID, on)
				}
			}
		})
		group(c, "Leave Out", func() {
			setting(c, "Shorter than", "Prompts as short as “continue” or “commit” aren’t worth finding.", true, func() {
				number(c, &f.minChars, 1, 200, "chars")
			})
			ui.Column(c).Padding(10, 14).Gap(6).BorderWidth(0.5, 0, 0, 0).BorderColor(t.Border.Alpha(0.6)).Children(func() {
				ui.Text(c, "Prompts containing")
				caption(c, "One text a line, in any case. Slash and shell commands, text sent by tools, and prompts with keys or tokens are always left out.").Wrap()
				ui.TextArea(c, &f.ignore).Label("Prompts containing").Placeholder("One text a line").Font("monospace").MinHeight(64)
			})
		})
		dirty := !slices.Equal(f.settings(st).Ignore, st.Ignore) || f.settings(st).MinChars != st.MinChars
		ui.Row(c).Gap(8).Children(func() {
			ui.PrimaryButton(c, "Apply").Disabled(!dirty).OnClick(func() {
				h.SetSettings(f.settings(h.Settings()))
				s.a.palette.invalidate()
			})
			ui.Spacer(c)
			if ui.Button(c, "Read Everything Again…").Clicked() {
				f.confirmRead = true
			}
			if ui.Button(c, "Clear History…").TextColor(t.Danger).Clicked() {
				f.confirmClear = true
			}
		})
		ui.Row(c).Gap(10).Padding(12, 14).Radius(10).Background(t.Success.Alpha(0.1)).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, iconPrivacy).FontSize(16).TextColor(t.Success)
			ui.Text(c, "The history stays on this Mac: Sokki reads the agents’ own files, and never syncs or sends what it finds there.").
				FontSize(12).Wrap().Shrink(1)
		})
	})
	if ui.AlertDialog(c, &f.confirmClear, "Clear agent history?",
		fmt.Sprintf("Sokki forgets the %s it has. Only prompts sent from now on come in again; the agents keep theirs.", plural(len(h.Entries()), "prompt", "prompts")),
		"Cancel", "Clear") == 1 {
		h.Clear(false)
		s.a.palette.invalidate()
	}
	if ui.AlertDialog(c, &f.confirmRead, "Read every agent again?",
		"Sokki forgets its history, and the prompts it hid, then reads all the agents keep, from the start.",
		"Cancel", "Read Again") == 1 {
		h.Clear(true)
		s.a.scanHistory()
	}
}

// readNoteFor is how long the summary says what Read Now found.
const readNoteFor = 4 * time.Second

// showInFinder shows a file or folder in the Finder; tests have their own.
var showInFinder = mygo.Shell.ShowItemInFolder

// historySummary heads the Agent History page: how many prompts Sokki
// has, from how many agents, how many came in the newest day, and when it
// last read them, or that it reads them now.
func (s *settingsWindow) historySummary(c *ui.Context) {
	h := s.a.history
	t := c.Theme()
	busy := s.a.scanning.Load()
	var from []string
	counts := h.Counts()
	for _, src := range history.Sources {
		if counts[src.ID] > 0 {
			from = append(from, src.Name)
		}
	}
	total := len(h.Entries())
	group(c, "", func() {
		ui.Row(c).Padding(12, 14).Gap(12).AlignItems(ui.Center).Children(func() {
			iconPlate(c, iconHistory, t.Accent, 34)
			ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					title := "No prompts yet"
					switch {
					case total == 0:
					case len(from) == 1:
						title = plural(total, "prompt", "prompts") + " from " + from[0]
					case len(from) > 1:
						title = plural(total, "prompt", "prompts") + " from " + plural(len(from), "agent", "agents")
					default:
						title = plural(total, "prompt", "prompts")
					}
					ui.Text(c, title).FontSize(14).FontWeight(600).FontFeatures("tnum").SingleLine().Ellipsis("…").Shrink(1)
					if n, day := s.newestDay(c.Now()); n > 0 {
						c.Key(day)
						ui.Text(c, strconv.Itoa(n)+" "+day).FontSize(11).FontWeight(600).FontFeatures("tnum").
							TextColor(t.Accent).Padding(1, 7).Radius(8).Background(t.Accent.Alpha(0.12)).SingleLine().Shrink(0).
							Tooltip("Prompts last sent " + day).Transition(fadeTransition)
					}
				})
				// What it is doing, or for a moment what Read Now found,
				// fading in as it changes.
				r := &s.a.lastRead
				if r.fresh {
					r.fresh = false
					if time.Since(r.at) < readNoteFor {
						r.until = c.Now().Add(readNoteFor)
					}
				}
				state := "read"
				if busy {
					state = "busy"
				} else if left := r.until.Sub(c.Now()); left > 0 {
					state = "found"
					c.After(left)
				}
				c.Key(state)
				ui.Row(c).Gap(6).AlignItems(ui.Center).Transition(fadeTransition).Children(func() {
					switch state {
					case "busy":
						ui.Spinner(c).Size(11, 11)
						caption(c, "Reading the agents’ history…").SingleLine()
						return
					case "found":
						ui.Icon(c, iconMatched).FontSize(12).TextColor(t.Success)
						if r.found == 0 {
							caption(c, "Up to date: nothing new since the last reading.").SingleLine().Ellipsis("…").Shrink(1)
						} else {
							caption(c, plural(r.found, "new prompt", "new prompts")+" read just now.").
								TextColor(t.Text).FontWeight(600).FontFeatures("tnum").SingleLine().Ellipsis("…").Shrink(1)
						}
						return
					}
					read := "Not read yet."
					if at := h.Imported(); !at.IsZero() {
						read = "Read " + ago(at, time.Now()) + "."
						c.After(30 * time.Second) // which ages
					}
					caption(c, read+" Checked again every 2 minutes.").Tooltip("Sokki reads what is new every 2 minutes, and as the palette opens.").SingleLine().Ellipsis("…").Shrink(1)
				})
			})
			ui.Button(c, "Read Now").Disabled(busy).Shrink(0).OnClick(s.a.readHistoryNow)
		})
	})
}

// newestDay is how many prompts were last sent on the newest day any was,
// and that day's name: today, yesterday or its date. It is worked out
// again only as the history or the day changes.
func (s *settingsWindow) newestDay(now time.Time) (int, string) {
	d := &s.historyDay
	y, m, dd := now.Date()
	today := time.Date(y, m, dd, 0, 0, 0, 0, now.Location())
	if gen := s.a.history.Gen(); d.gen != gen || !d.today.Equal(today) {
		*d = dayCount{gen: gen, today: today}
		var newest time.Time
		for _, e := range s.a.history.Entries() {
			y, m, dd := time.Unix(e.Last, 0).In(now.Location()).Date()
			switch day := time.Date(y, m, dd, 0, 0, 0, 0, now.Location()); {
			case day.After(newest):
				newest, d.n = day, 1
			case day.Equal(newest):
				d.n++
			}
		}
		switch {
		case d.n == 0:
		case newest.Equal(today):
			d.name = "today"
		case newest.Equal(today.AddDate(0, 0, -1)):
			d.name = "yesterday"
		case newest.Year() == today.Year():
			d.name = "on " + newest.Format("Jan 2")
		default:
			d.name = "on " + newest.Format("Jan 2, 2006")
		}
	}
	return d.n, d.name
}

// dayCount is what newestDay worked out, as of the history's gen and the
// day.
type dayCount struct {
	gen   uint64
	today time.Time
	n     int
	name  string
}

// agentSetting is the row of an agent on the Agent History page: its
// dot, dimmed when it is not read or not on this Mac, its name and where
// it reads, the prompts it gave or that its files were not found, a way
// to where, in the Finder, and its switch, which agentSetting reports
// changed.
func agentSetting(c *ui.Context, src history.Source, n int, found bool, where string, on *bool, first bool) bool {
	t := c.Theme()
	row := ui.Row(c).Padding(9, 14).Gap(12).AlignItems(ui.Center).MinHeight(44)
	if !first {
		row.BorderWidth(0.5, 0, 0, 0).BorderColor(t.Border.Alpha(0.6))
	}
	changed := false
	row.Children(func() {
		// A ring for an agent not on this Mac, a dim dot for one not read.
		tint, ring := agentTint(t, src.ID), t.Text.Alpha(0)
		switch {
		case !found:
			tint, ring = t.Text.Alpha(0), t.Text.Alpha(0.35)
		case !*on:
			tint = t.Text.Alpha(0.2)
		}
		ui.Box(c).Size(8, 8).Radius(4).Background(row.AnimateColor("dot", tint, hoverAnim)).
			Border(1, row.AnimateColor("ring", ring, hoverAnim)).Shrink(0)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			label := ui.Text(c, src.Name)
			if !*on || !found {
				label.TextColor(t.TextMuted)
			}
			where := caption(c, src.Where).Font("monospace").SingleLine().Ellipsis("…")
			if !found {
				where.TextColor(t.TextMuted.Alpha(0.7))
			}
		})
		switch {
		case n > 0:
			caption(c, plural(n, "prompt", "prompts")).FontFeatures("tnum").SingleLine().Shrink(0)
		case !found:
			caption(c, "Not found").SingleLine().Shrink(0).Padding(1, 6).Radius(5).Border(0.5, t.Text.Alpha(0.14)).
				Tooltip("Nothing at " + src.Where + ": this agent isn’t used on this Mac, or keeps no history there.")
		}
		// The Finder's way, in a place of its own so that the switches
		// and counts line up, and only where there is something to show.
		ui.Box(c).Size(26, 26).Center().Shrink(0).Children(func() {
			if where == "" {
				return
			}
			label := "Show " + src.Name + "’s History in Finder"
			if !found {
				label = "Show Where " + src.Name + " Keeps Its History in Finder"
			}
			if iconButton(c, iconFile, label).Clicked() {
				showInFinder(where)
			}
		})
		changed = ui.Switch(c, on).Label("Read " + src.Name).Changed()
	})
	return changed
}
