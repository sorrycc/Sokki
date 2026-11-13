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
		counts := h.Counts()
		group(c, "Agents", func() {
			for i, src := range history.Sources {
				detail := src.Where
				if n := counts[src.ID]; n > 0 {
					detail = plural(n, "prompt", "prompts") + " · " + detail
				}
				setting(c, src.Name, detail, i == 0, func() {
					on := st.On(src.ID)
					if ui.Switch(c, &on).Label("Read " + src.Name).Changed() {
						s.a.setSource(src.ID, on)
					}
				})
			}
		})
		group(c, "", func() {
			read := "Not read yet."
			if at := h.Imported(); !at.IsZero() {
				read = "Read " + ago(at, time.Now()) + "."
			}
			setting(c, plural(len(h.Entries()), "prompt", "prompts"),
				read+" Sokki reads what is new every 2 minutes and as the palette opens.", true, func() {
					busy := s.a.scanning.Load()
					ui.Button(c, "Read Now").Disabled(busy).OnClick(s.a.scanHistory)
				})
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
