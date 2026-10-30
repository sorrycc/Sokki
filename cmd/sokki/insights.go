package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"sokki/internal/model"
	"sokki/internal/usage"
)

// charsPerMinute is how fast Insights takes typing to be: 40 words a
// minute, of 5 characters.
const charsPerMinute = 200

// The Insights page's motion: bars and numbers grow in one after
// another as the page comes in, each in growAnim, and hover fades.
var (
	hoverAnim = ui.Animation{Duration: 120 * time.Millisecond}
	growAnim  = ui.Animation{Duration: 240 * time.Millisecond}
	stagger   = 25 * time.Millisecond
	// unfoldTransition is how the list of unused matches opens.
	unfoldTransition = ui.ElementTransition{
		Duration: 180 * time.Millisecond, Colors: true,
		Enter: &ui.Motion{Y: -4},
	}
)

// maxUnused is how many unused matches show at most, which keeps the
// frame short with thousands of them.
const maxUnused = 48

// grow returns a value of e going to target, from 0 the first time e
// shows, after delay: bars and numbers grow as their page comes in.
func grow(e ui.Element, key any, target float32, delay time.Duration) float32 {
	if seen := ui.Local(e, key, func() bool { return false }); !*seen {
		*seen = true
		e.Jump(key, 0)
	}
	a := growAnim
	a.Delay = delay
	return e.AnimateTo(key, target, a)
}

// insightsCache is what the Insights page derives from the usage and the
// matches, as of their revisions: drawn on every frame, it is counted
// only after a change.
type insightsCache struct {
	rev, gen uint64
	ok       bool
	// matches are the matches of the most used keys, by key.
	matches map[string]model.Match
	// idle are the matches on that weren't expanded in the past 7 days.
	idle []model.Match
}

// insightsData returns the summary of the usage and what derives from it.
func (s *settingsWindow) insightsData() (usage.Summary, *insightsCache) {
	m := s.a.usage.Summary()
	ic := &s.insightsCache
	if ic.ok && ic.rev == m.Rev && ic.gen == s.a.store.Gen {
		return m, ic
	}
	ic.ok, ic.rev, ic.gen = true, m.Rev, s.a.store.Gen
	used := make(map[string]bool, len(m.Triggers))
	for _, tr := range m.Triggers {
		used[tr.Key] = true
	}
	top := m.Triggers[:min(10, len(m.Triggers))]
	ic.matches = make(map[string]model.Match, len(top))
	ic.idle = ic.idle[:0]
	for _, mt := range s.a.store.Matches {
		key := mt.Key()
		for _, tr := range top {
			if tr.Key == key {
				ic.matches[key] = mt
			}
		}
		if mt.Enabled && !used[key] {
			ic.idle = append(ic.idle, mt)
		}
	}
	return m, ic
}

// insights is the Insights page: what Sokki typed in the past 7 days,
// where, and the matches it didn't.
func (s *settingsWindow) insights(c *ui.Context) {
	m, ic := s.insightsData()
	if m.Total == 0 {
		s.insightsEmpty(c)
		return
	}
	t := c.Theme()
	now := c.Now()
	active := m.ActiveDays()
	subtitle := fmt.Sprintf("What Sokki typed for you, %s – %s.",
		now.AddDate(0, 0, -6).Format("Jan 2"), now.Format("Jan 2"))
	scrollPage(c, "Insights", subtitle, func() {
		ui.Row(c).Gap(12).Children(func() {
			statTile(c, iconZap, t.Warning, "Expansions", m.Total, "past 7 days", 0, 0)
			note, trend := todayNote(m.Daily, active)
			statTile(c, iconActivity, t.Accent, "Today", m.Daily[6], note, trend, 2*stagger)
			statTile(c, iconTimer, t.Success, "Characters saved", m.Saved, typingTime(m.Saved), 0, 4*stagger)
		})
		if active < 3 {
			earlyNote(c, active)
		}
		// The cards sit closer than the page's parts, as one report.
		ui.Column(c).Gap(14).Children(func() { s.insightCards(c, m, ic, active, now) })
		ui.Row(c).Justify(ui.End).Children(func() {
			if ui.Button(c, "Clear Usage Data…").TextColor(t.Danger).Clicked() {
				s.confirmClear = true
			}
		})
	})
	if ui.AlertDialog(c, &s.confirmClear, "Clear all usage data?",
		"The counts of the past 7 days are deleted. This cannot be undone.", "Cancel", "Clear") == 1 {
		s.a.usage.Clear()
	}
}

// insightCards are the cards of the Insights page, under its numbers.
func (s *settingsWindow) insightCards(c *ui.Context, m usage.Summary, ic *insightsCache, active int, now time.Time) {
	t := c.Theme()
	card(c, "Last 7 Days", func() {
		if active >= 2 {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(12).Height(1).Background(t.Text.Alpha(0.35))
				caption(c, "Daily average "+perDay(float32(m.Total)/7)).FontSize(10).FontFeatures("tnum")
			})
		}
	}, func() { dailyChart(c, m.Daily, active >= 2, now) })
	card(c, "Most Used", func() {
		if n := len(m.Triggers); n > 10 {
			caption(c, fmt.Sprintf("Top 10 of %d", n)).FontFeatures("tnum")
		}
	}, func() { s.mostUsed(c, m.Triggers, m.Total, ic.matches) })
	card(c, "Typed vs Palette", nil, func() { sourceSplit(c, m.Total-m.Palette, m.Palette) })
	if len(m.Apps) > 0 {
		card(c, "By App", nil, func() { byApp(c, m.Apps, m.Total) })
	}
	s.unused(c, ic.idle)
}

// insightsEmpty is the Insights page before the first expansion: how to
// make one, with a trigger of the user's to try.
func (s *settingsWindow) insightsEmpty(c *ui.Context) {
	detail := "Type your triggers for a while: how often you use each, and where, shows up here."
	for _, m := range s.a.store.Matches {
		if m.Enabled && m.Trigger != "" {
			detail = "Type " + m.Trigger + " in any app to try it: how often you use each match, and where, shows up here."
			break
		}
	}
	ui.Column(c).Fill().Children(func() {
		ui.Box(c).Height(max(c.TitleBar().Height, 12)).DragWindow()
		emptyState(c, iconInsights, "No expansions yet", detail, func() {
			if ui.Button(c, "Show Matches").Clicked() {
				s.setPage(matchesPage)
			}
		})
	})
}

// earlyNote says, while there are few days to count, that the averages
// will come.
func earlyNote(c *ui.Context, active int) {
	t := c.Theme()
	text := "Early days: the daily average and trends fill in after a few days of use."
	if active == 2 {
		text = "Two days so far: trends get clearer as the week fills in."
	}
	ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(9, 12).Radius(8).
		Background(t.Accent.Alpha(0.07)).Border(0.5, t.Accent.Alpha(0.2)).Children(func() {
		ui.Icon(c, iconAbout).FontSize(14).TextColor(t.Accent).Shrink(0)
		ui.Text(c, text).FontSize(12).Wrap().Grow(1).Shrink(1)
	})
}

// card is a part of the Insights page on a card: its title, what end
// draws at the end of the title's row, and fn under it.
func card(c *ui.Context, title string, end func(), fn func()) {
	t := c.Theme()
	ui.Column(c).Gap(12).Padding(14, 16, 16, 16).Radius(10).Background(cardColor(t)).
		Border(0.5, t.Border.Alpha(0.6)).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(16).Children(func() {
			ui.Text(c, title).FontSize(12).FontWeight(600).Grow(1).SingleLine()
			if end != nil {
				end()
			}
		})
		fn()
	})
}

// usageCap shows a key of the usage log: a trigger as a keycap, a
// prompt's title after the prompt icon.
func usageCap(c *ui.Context, key string, trigger bool) ui.Element {
	if trigger {
		return keycap(c, key, false)
	}
	t := c.Theme()
	return ui.Row(c).Gap(5).AlignItems(ui.Center).Padding(2, 7).Radius(5).Border(0.5, t.Text.Alpha(0.14)).Children(func() {
		ui.Icon(c, iconPrompt).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		ui.Text(c, key).FontSize(12).SingleLine().Ellipsis("…").Shrink(1)
	})
}

// labelWidth is the width of the names of Most Used and By App, whose
// bars start at one line.
const labelWidth = 140

// hoverRow is a row of a list of the Insights page, which tints as the
// pointer is over it; a row with onClick set is a button.
func hoverRow(c *ui.Context, label string, onClick func(), fn func()) {
	t := c.Theme()
	row := ui.ButtonBase(c).Padding(4, 8).Margin(-4, -8).Radius(6)
	if onClick != nil {
		row.Label(label).OnClick(onClick)
	}
	hover := ui.Transparent
	if row.Hovered() {
		hover = t.Text.Alpha(0.045)
	}
	row.Background(row.AnimateColor("hover", hover, hoverAnim)).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Grow(1).Children(fn)
	})
}

// countCells are the count of a row and its share of all, at its end.
func countCells(c *ui.Context, n, total int) {
	ui.Text(c, strconv.Itoa(n)).FontSize(11).FontWeight(600).FontFeatures("tnum").Width(36).TextAlign(ui.End)
	ui.Textf(c, "%.0f%%", float32(n)*100/float32(total)).FontSize(11).FontFeatures("tnum").
		TextColor(c.Theme().TextMuted).Width(34).TextAlign(ui.End)
}

// mostUsed lists the 10 triggers expanded most, with bars; clicking one
// edits its match.
func (s *settingsWindow) mostUsed(c *ui.Context, triggers []usage.Count, total int, matches map[string]model.Match) {
	t := c.Theme()
	triggers = triggers[:min(10, len(triggers))]
	most := float32(triggers[0].Count)
	ui.Column(c).Gap(8).Children(func() {
		for i, tr := range triggers {
			m, ok := matches[tr.Key]
			var edit func()
			if ok {
				edit = func() { s.a.editor.edit(m) }
			}
			c.Key(tr.Key)
			hoverRow(c, "Edit "+tr.Key, edit, func() {
				rank := ui.Text(c, strconv.Itoa(i+1)).FontSize(11).FontWeight(600).FontFeatures("tnum").
					Width(16).TextAlign(ui.End).TextColor(t.TextMuted).SingleLine()
				if i == 0 {
					rank.TextColor(t.Warning)
				}
				ui.Row(c).Width(labelWidth - 26).Shrink(0).Children(func() {
					capEl := usageCap(c, tr.Key, !ok || m.Trigger != "").Shrink(1)
					if ok {
						capEl.Tooltip(summary(m.Replace))
					}
				})
				bar(c, float32(tr.Count)/most, t.Accent, time.Duration(i)*stagger)
				countCells(c, tr.Count, total)
			})
		}
	})
}

// bar is a track that a bar of fraction of it fills, in color.
func bar(c *ui.Context, fraction float32, color ui.Color, delay time.Duration) {
	ui.Row(c).Grow(1).Height(8).Radius(4).Background(c.Theme().Text.Alpha(0.06)).Children(func() {
		fill := ui.Box(c).Height(8).Radius(4).MinWidth(4)
		fill.WidthPercent(grow(fill, "w", fraction*100, delay))
		fill.LinearGradient(ui.LinearGradient{From: color.Alpha(0.55), To: color, Angle: 90})
	})
}

// sourceSplit shows how many expansions were typed and how many came
// from the palette, as one bar of two parts.
func sourceSplit(c *ui.Context, typed, palette int) {
	t := c.Theme()
	total := float32(typed + palette)
	ui.Column(c).Gap(10).Children(func() {
		ui.Row(c).Height(10).Radius(5).Clip().Background(t.Text.Alpha(0.06)).Children(func() {
			if typed > 0 {
				part := ui.Box(c.Key("typed")).Height(10).Background(t.Accent)
				part.WidthPercent(grow(part, "w", float32(typed)*100/total, 0))
			}
			if palette > 0 {
				// A hairline of the card's color keeps the parts apart.
				part := ui.Box(c.Key("palette")).Height(10).Background(t.Success)
				if typed > 0 {
					part.BorderWidth(0, 0, 0, 2).BorderColor(cardColorOpaque(t))
				}
				part.WidthPercent(grow(part, "w", float32(palette)*100/total, 2*stagger))
			}
		})
		ui.Row(c).Gap(18).Children(func() {
			legend(c, t.Accent, "Typed", typed, total)
			legend(c, t.Success, "Palette", palette, total)
		})
	})
}

// cardColorOpaque is the color a card shows, its tint over the window's.
func cardColorOpaque(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Background.Mix(t.Text, 0.04)
	}
	return t.Background
}

func legend(c *ui.Context, color ui.Color, label string, n int, total float32) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(8, 8).Radius(2).Background(color)
		ui.Text(c, label).FontSize(12)
		caption(c, fmt.Sprintf("%d · %.0f%%", n, float32(n)*100/total)).FontFeatures("tnum")
	})
}

// byApp lists the 8 apps expanded in most.
func byApp(c *ui.Context, apps []usage.Count, total int) {
	t := c.Theme()
	apps = apps[:min(8, len(apps))]
	most := float32(apps[0].Count)
	ui.Column(c).Gap(8).Children(func() {
		for i, app := range apps {
			c.Key(app.Key)
			hoverRow(c, app.Name, nil, func() {
				ui.Row(c).Width(labelWidth).Shrink(0).Gap(8).AlignItems(ui.Center).Tooltip(app.Key).Children(func() {
					ui.Avatar(c, app.Name, nil).Size(18, 18).Shrink(0)
					ui.Text(c, app.Name).FontSize(12).SingleLine().Ellipsis("…").Shrink(1)
				})
				bar(c, float32(app.Count)/most, t.Accent, time.Duration(i)*stagger)
				countCells(c, app.Count, total)
			})
		}
	})
}

// unused lists the matches on that weren't expanded in the past 7 days,
// folded, so that they can be cleaned up; clicking one edits it.
func (s *settingsWindow) unused(c *ui.Context, idle []model.Match) {
	if len(idle) == 0 {
		return
	}
	t := c.Theme()
	toggle := "Show"
	if s.unusedShown {
		toggle = "Hide"
	}
	card(c, "Unused", func() {
		caption(c, plural(len(idle), "match", "matches")+" not used in 7 days").FontFeatures("tnum")
		b := ui.ButtonBase(c).Label("Unused matches").Padding(2, 6).Radius(5).
			OnClick(func() { s.unusedShown = !s.unusedShown })
		hover := ui.Transparent
		if b.Hovered() {
			hover = t.Accent.Alpha(0.1)
		}
		b.Background(b.AnimateColor("hover", hover, hoverAnim)).Children(func() {
			ui.Text(c, toggle).FontSize(11).FontWeight(500).TextColor(t.Accent)
		})
	}, func() {
		if !s.unusedShown {
			return
		}
		ui.Column(c).Gap(8).Transition(unfoldTransition).Children(func() {
			ui.Row(c).Wrap().Gap(6).Children(func() {
				for _, m := range idle[:min(maxUnused, len(idle))] {
					b := ui.ButtonBase(c.Key(m.Key())).Label("Edit " + m.Key()).Tooltip(summary(m.Replace)).
						OnClick(func() { s.a.editor.edit(m) })
					if b.Hovered() {
						b.Opacity(0.75)
					}
					b.Children(func() { usageCap(c, m.Key(), m.Trigger != "") })
				}
			})
			if more := len(idle) - maxUnused; more > 0 {
				caption(c, fmt.Sprintf("and %d more", more)).FontFeatures("tnum")
			}
		})
	})
}

// typingTime says how long typing n characters takes.
func typingTime(n int) string {
	switch m := float32(n) / charsPerMinute; {
	case m < 1:
		return "under a minute of typing"
	case m < 60:
		return fmt.Sprintf("about %.0f min of typing", m)
	default:
		return "about " + perDay(m/60) + " h of typing"
	}
}

// perDay says an average, with a decimal while it is small.
func perDay(n float32) string {
	if n >= 10 {
		return fmt.Sprintf("%.0f", n)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", n), ".0")
}

// againstAverage says how today compares with the daily average, and
// whether it is above (1) or below (-1) it.
func againstAverage(today, avg float32) (string, int) {
	switch d := today - avg; {
	case d >= 0.05:
		return perDay(d) + " above average", 1
	case d <= -0.05:
		return perDay(-d) + " below average", -1
	}
	return "on average", 0
}

// todayNote says how today went, against the average once there are
// two days to make one.
func todayNote(days [7]int, active int) (string, int) {
	switch {
	case active >= 2:
		total := 0
		for _, n := range days {
			total += n
		}
		return againstAverage(float32(days[6]), float32(total)/float32(len(days)))
	case days[6] > 0:
		return "your first day", 0
	}
	return "none yet today", 0
}

// thousands writes n with a comma between each 3 digits.
func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// statTile is a number of the Insights page, which counts up as it shows,
// under its label and over a note; trend colors the note, up or down.
func statTile(c *ui.Context, icon *ui.SVG, tint ui.Color, label string, n int, note string, trend int, delay time.Duration) {
	t := c.Theme()
	tile := ui.Column(c).Grow(1).Basis(0).Gap(8).Padding(12, 14).Radius(10).Background(cardColor(t))
	border := t.Border.Alpha(0.6)
	if tile.Hovered() {
		border = tint.Alpha(0.45)
	}
	tile.Border(0.5, tile.AnimateColor("border", border, hoverAnim))
	v := grow(tile, "n", float32(n), delay)
	tile.Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(20, 20).Radius(5).Background(tint.Alpha(0.14)).Center().Shrink(0).Children(func() {
				ui.Icon(c, icon).FontSize(12).TextColor(tint)
			})
			ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
		})
		ui.Text(c, thousands(int(v+0.5))).FontSize(26).FontWeight(700).FontFeatures("tnum").SingleLine()
		ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
			color := t.TextMuted
			switch trend {
			case 1:
				color = t.Success
				ui.Text(c, "↑").FontSize(11).FontWeight(700).TextColor(color)
			case -1:
				ui.Text(c, "↓").FontSize(11).FontWeight(700).TextColor(color)
			}
			caption(c, note).TextColor(color).SingleLine().Ellipsis("…").Shrink(1)
		})
	})
}

// dailyChart shows the expansions of each of the last 7 days as bars,
// today at the right in the accent color, over the daily average when
// there is one.
func dailyChart(c *ui.Context, days [7]int, average bool, now time.Time) {
	t := c.Theme()
	most, total := 1, 0
	for _, n := range days {
		most = max(most, n)
		total += n
	}
	const height = 104
	avg := float32(total) / float32(len(days)) * height / float32(most)
	// The columns touch, which keeps the average line and the axis
	// through them whole.
	ui.Row(c).AlignItems(ui.End).Children(func() {
		for i, n := range days {
			day := now.AddDate(0, 0, i-len(days)+1)
			isToday := i == len(days)-1
			col := ui.Column(c.Key(i)).Grow(1).Basis(0).Gap(6).AlignItems(ui.Center).Padding(4, 0, 0, 0).
				Radius(6).Tooltip(fmt.Sprintf("%s: %s", day.Format("Monday, Jan 2"), plural(n, "expansion", "expansions")))
			hovered := col.Hovered()
			band := ui.Transparent
			if hovered {
				band = t.Text.Alpha(0.04)
			}
			col.Background(col.AnimateColor("band", band, hoverAnim))
			col.Children(func() {
				countColor := t.TextMuted
				if hovered || isToday {
					countColor = t.Text
				}
				count := ui.Text(c, strconv.Itoa(n)).FontSize(10).FontWeight(600).FontFeatures("tnum").
					TextColor(col.AnimateColor("count", countColor, hoverAnim))
				if n == 0 {
					count.Invisible()
				}
				ui.Column(c).Height(height).FillWidth().Justify(ui.End).AlignItems(ui.Center).
					BorderWidth(0, 0, 1, 0).BorderColor(t.Text.Alpha(0.1)).Children(func() {
					if average {
						line := ui.Box(c.Key("avg")).Absolute().Left(0).Right(0).Height(1).PassThrough().
							Background(t.Text.Alpha(0.2))
						line.Bottom(grow(line, "y", avg, 0))
					}
					// Rounded at the top, as it rises from the axis.
					b := ui.Box(c).Radius(5, 5, 1, 1).Width(24)
					h := grow(b, "h", float32(n)*height/float32(most), time.Duration(i)*stagger)
					b.Height(max(h, 2))
					switch {
					case n == 0:
						b.Background(t.Text.Alpha(0.08))
					case isToday:
						b.LinearGradient(ui.LinearGradient{From: t.Accent.Mix(ui.RGB(255, 255, 255), 0.2), To: t.Accent, Angle: 180})
					case hovered:
						b.Background(b.AnimateColor("bg", t.Accent.Alpha(0.65), hoverAnim))
					default:
						b.Background(b.AnimateColor("bg", t.Accent.Alpha(0.35), hoverAnim))
					}
				})
				if isToday {
					caption(c, "Today").FontSize(10).Margin(0, 0, 4, 0).FontWeight(700).TextColor(t.Text)
				} else {
					caption(c, day.Format("Mon")).FontSize(10).Margin(0, 0, 4, 0)
				}
			})
		}
	})
}
