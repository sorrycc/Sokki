package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

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
				if agentSetting(c, src, counts[src.ID], &on, i == 0) {
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

// historySummary heads the Agent History page: how many prompts Sokki
// has, from how many agents, and when it last read them, or that it reads
// them now.
func (s *settingsWindow) historySummary(c *ui.Context) {
	h := s.a.history
	t := c.Theme()
	busy := s.a.scanning.Load()
	agents := 0
	for _, n := range h.Counts() {
		if n > 0 {
			agents++
		}
	}
	group(c, "", func() {
		ui.Row(c).Padding(12, 14).Gap(12).AlignItems(ui.Center).Children(func() {
			iconPlate(c, iconHistory, t.Accent, 34)
			ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
				title := plural(len(h.Entries()), "prompt", "prompts")
				if agents > 1 {
					title += " from " + plural(agents, "agent", "agents")
				}
				ui.Text(c, title).FontSize(14).FontWeight(600).FontFeatures("tnum").SingleLine()
				// What it is doing, fading in as it changes.
				c.Key(busy)
				ui.Row(c).Gap(6).AlignItems(ui.Center).Transition(fadeTransition).Children(func() {
					if busy {
						ui.Spinner(c).Size(11, 11)
						caption(c, "Reading the agents’ history…").SingleLine()
						return
					}
					read := "Not read yet."
					if at := h.Imported(); !at.IsZero() {
						read = "Read " + ago(at, time.Now()) + "."
					}
					caption(c, read+" New prompts come in every 2 minutes and as the palette opens.").SingleLine().Ellipsis("…").Shrink(1)
				})
			})
			ui.Button(c, "Read Now").Disabled(busy).Shrink(0).OnClick(s.a.scanHistory)
		})
	})
}

// agentSetting is the row of an agent on the Agent History page: its
// dot, dimmed when it is not read, its name and where it reads, the
// prompts it gave, and its switch, which agentSetting reports changed.
func agentSetting(c *ui.Context, src history.Source, n int, on *bool, first bool) bool {
	t := c.Theme()
	row := ui.Row(c).Padding(9, 14).Gap(12).AlignItems(ui.Center).MinHeight(44)
	if !first {
		row.BorderWidth(0.5, 0, 0, 0).BorderColor(t.Border.Alpha(0.6))
	}
	changed := false
	row.Children(func() {
		tint := agentTint(t, src.ID)
		if !*on {
			tint = t.Text.Alpha(0.2)
		}
		ui.Box(c).Size(8, 8).Radius(4).Background(row.AnimateColor("dot", tint, hoverAnim)).Shrink(0)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			label := ui.Text(c, src.Name)
			if !*on {
				label.TextColor(t.TextMuted)
			}
			caption(c, src.Where).Font("monospace").SingleLine().Ellipsis("…")
		})
		if n > 0 {
			caption(c, plural(n, "prompt", "prompts")).FontFeatures("tnum").SingleLine().Shrink(0)
		}
		changed = ui.Switch(c, on).Label("Read " + src.Name).Changed()
	})
	return changed
}
