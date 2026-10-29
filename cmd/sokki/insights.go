package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"sokki/internal/model"
	"sokki/internal/usage"
)

// charsPerMinute is how fast Insights takes typing to be: 40 words a
// minute, of 5 characters.
const charsPerMinute = 200

var (
	hoverAnim = ui.Animation{Duration: 120 * time.Millisecond}
	growAnim  = ui.Animation{Spring: ui.SpringOptions{Duration: 600 * time.Millisecond}}
)

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

// insights is the Insights page: what Sokki typed in the past 7 days,
// where, and the matches it didn't.
func (s *settingsWindow) insights(c *ui.Context) {
	u := s.a.usage
	total := u.Total()
	if total == 0 {
		ui.Column(c).Fill().Children(func() {
			ui.Box(c).Height(max(c.TitleBar().Height, 12)).DragWindow()
			emptyState(c, iconInsights, "No expansions yet",
				"Type your triggers for a while: how often you use each, and where, shows up here.")
		})
		return
	}
	t := c.Theme()
	days := u.Daily()
	saved := u.Saved()
	scrollPage(c, "Insights", "What Sokki typed for you in the past 7 days.", func() {
		ui.Row(c).Gap(12).Children(func() {
			statTile(c, iconZap, t.Warning, "Expansions", total, "past 7 days", 0)
			statTile(c, iconActivity, t.Accent, "Today", days[len(days)-1],
				againstAverage(float32(days[len(days)-1]), float32(total)/float32(len(days))), 60*time.Millisecond)
			statTile(c, iconTimer, t.Success, "Characters saved", saved, typingTime(saved), 120*time.Millisecond)
		})
		section(c, "Last 7 Days", func() { dailyChart(c, days) })
		section(c, "Most Used", func() { s.mostUsed(c, u.Triggers(), total) })
		section(c, "Typed vs Palette", func() { sourceSplit(c, total-u.Palette(), u.Palette()) })
		if apps := u.Apps(); len(apps) > 0 {
			section(c, "By App", func() { byApp(c, apps) })
		}
		s.unused(c, u.Triggers())
		ui.Row(c).Justify(ui.End).Children(func() {
			if ui.Button(c, "Clear Usage Data…").TextColor(t.Danger).Clicked() {
				s.confirmClear = true
			}
		})
	})
	if ui.AlertDialog(c, &s.confirmClear, "Clear all usage data?",
		"The counts of the past 7 days are deleted. This cannot be undone.", "Cancel", "Clear") == 1 {
		u.Clear()
	}
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

// matchOf returns the match of a key of the usage log: a trigger, or a
// prompt's title.
func (s *settingsWindow) matchOf(key string) (model.Match, bool) {
	return s.a.store.WithKey(key)
}

// mostUsed lists the 10 triggers expanded most, with bars; clicking one
// edits its match.
func (s *settingsWindow) mostUsed(c *ui.Context, triggers []usage.Count, total int) {
	t := c.Theme()
	triggers = triggers[:min(10, len(triggers))]
	most := float32(triggers[0].Count)
	for i, tr := range triggers {
		m, ok := s.matchOf(tr.Key)
		row := ui.ButtonBase(c.Key(tr.Key)).Padding(4, 8).Margin(-4, -8).Radius(6)
		if ok {
			row.Label("Edit " + tr.Key).Tooltip(summary(m.Replace)).OnClick(func() { s.a.editor.edit(m) })
		}
		hover := ui.Transparent
		if ok && row.Hovered() {
			hover = t.Text.Alpha(0.05)
		}
		row.Background(row.AnimateColor("hover", hover, hoverAnim)).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Grow(1).Children(func() {
				rank := ui.Textf(c, "%d", i+1).FontSize(11).FontWeight(600).Width(14).TextAlign(ui.End).TextColor(t.TextMuted)
				if i == 0 {
					rank.TextColor(t.Warning)
				}
				ui.Row(c).Width(130).Children(func() { usageCap(c, tr.Key, !ok || m.Trigger != "").Shrink(1).Ellipsis("…") })
				bar(c, float32(tr.Count)/most, t.Accent, time.Duration(i)*35*time.Millisecond)
				ui.Textf(c, "%d", tr.Count).FontSize(11).FontWeight(600).FontFeatures("tnum").Width(32).TextAlign(ui.End)
				ui.Textf(c, "%.0f%%", float32(tr.Count)*100/float32(total)).FontSize(11).FontFeatures("tnum").
					TextColor(t.TextMuted).Width(34).TextAlign(ui.End)
			})
		})
	}
}

// bar is a track that a bar of fraction of it fills, in color.
func bar(c *ui.Context, fraction float32, color ui.Color, delay time.Duration) {
	ui.Row(c).Grow(1).Height(8).Radius(4).Background(c.Theme().Text.Alpha(0.06)).Children(func() {
		fill := ui.Box(c).Height(8).Radius(4)
		fill.WidthPercent(grow(fill, "w", fraction*100, delay))
		fill.LinearGradient(ui.LinearGradient{From: color.Alpha(0.55), To: color, Angle: 90})
	})
}

// sourceSplit shows how many expansions were typed and how many came
// from the palette, as one bar of two parts.
func sourceSplit(c *ui.Context, typed, palette int) {
	t := c.Theme()
	total := float32(typed + palette)
	ui.Column(c).Gap(8).Children(func() {
		ui.Row(c).Height(10).Radius(5).Clip().Background(t.Text.Alpha(0.06)).Children(func() {
			if typed > 0 {
				part := ui.Box(c.Key("typed")).Height(10).Background(t.Accent)
				part.WidthPercent(grow(part, "w", float32(typed)*100/total, 0))
			}
			if palette > 0 {
				part := ui.Box(c.Key("palette")).Height(10).Background(t.Success)
				part.WidthPercent(grow(part, "w", float32(palette)*100/total, 60*time.Millisecond))
			}
		})
		ui.Row(c).Gap(16).Children(func() {
			legend(c, t.Accent, "Typed", typed, total)
			legend(c, t.Success, "Palette", palette, total)
		})
	})
}

func legend(c *ui.Context, color ui.Color, label string, n int, total float32) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(8, 8).Radius(2).Background(color)
		ui.Text(c, label).FontSize(12)
		caption(c, fmt.Sprintf("%d · %.0f%%", n, float32(n)*100/total)).FontFeatures("tnum")
	})
}

// byApp lists the 8 apps expanded in most.
func byApp(c *ui.Context, apps []usage.Count) {
	t := c.Theme()
	apps = apps[:min(8, len(apps))]
	most := float32(apps[0].Count)
	for i, app := range apps {
		ui.Row(c.Key(app.Key)).Gap(10).AlignItems(ui.Center).Tooltip(app.Key).Children(func() {
			ui.Avatar(c, app.Name, nil).Size(20, 20).Shrink(0)
			ui.Text(c, app.Name).Width(120).SingleLine().Ellipsis("…")
			bar(c, float32(app.Count)/most, t.Accent, time.Duration(i)*35*time.Millisecond)
			ui.Textf(c, "%d", app.Count).FontSize(11).FontWeight(600).FontFeatures("tnum").Width(32).TextAlign(ui.End)
		})
	}
}

// unused lists the matches on that weren't expanded in the past 7 days,
// folded, so that they can be cleaned up; clicking one edits it.
func (s *settingsWindow) unused(c *ui.Context, triggers []usage.Count) {
	used := make(map[string]bool, len(triggers))
	for _, tr := range triggers {
		used[tr.Key] = true
	}
	var idle []model.Match
	for _, m := range s.a.store.Matches {
		if m.Enabled && !used[m.Key()] {
			idle = append(idle, m)
		}
	}
	if len(idle) == 0 {
		return
	}
	t := c.Theme()
	ui.Column(c).Gap(8).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Unused").FontSize(11).FontWeight(600).TextColor(t.TextMuted)
			caption(c, plural(len(idle), "match", "matches")+" not used in 7 days").Grow(1)
			toggle := "Show"
			if s.unusedShown {
				toggle = "Hide"
			}
			ui.ButtonBase(c).Label("Unused matches").OnClick(func() { s.unusedShown = !s.unusedShown }).Children(func() {
				ui.Text(c, toggle).FontSize(11).TextColor(t.Accent)
			})
		})
		if !s.unusedShown {
			return
		}
		ui.Row(c).Wrap().Gap(6).Children(func() {
			for _, m := range idle {
				b := ui.ButtonBase(c.Key(m.Key())).Label("Edit " + m.Key()).Tooltip(summary(m.Replace)).
					OnClick(func() { s.a.editor.edit(m) })
				b.Children(func() { usageCap(c, m.Key(), m.Trigger != "") })
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

// againstAverage says how today compares with the daily average.
func againstAverage(today, avg float32) string {
	switch d := today - avg; {
	case d >= 0.05:
		return perDay(d) + " above average"
	case d <= -0.05:
		return perDay(-d) + " below average"
	}
	return "on average"
}

// statTile is a number of the Insights page, which counts up as it shows.
func statTile(c *ui.Context, icon *ui.SVG, tint ui.Color, label string, n int, unit string, delay time.Duration) {
	t := c.Theme()
	tile := ui.Column(c).Grow(1).Basis(0).Gap(6).Padding(12, 14).Radius(10).Background(cardColor(t)).
		Border(0.5, t.Border.Alpha(0.6))
	v := grow(tile, "n", float32(n), delay)
	tile.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).FontSize(13).TextColor(tint)
			ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(t.TextMuted).SingleLine()
		})
		ui.Textf(c, "%d", int(v+0.5)).FontSize(24).FontWeight(700).FontFeatures("tnum")
		caption(c, unit).SingleLine().Ellipsis("…")
	})
}

// dailyChart shows the expansions of each of the last 7 days as bars,
// today at the right in the accent color, over the daily average.
func dailyChart(c *ui.Context, days [7]int) {
	t := c.Theme()
	most, total := 1, 0
	for _, n := range days {
		most = max(most, n)
		total += n
	}
	const height = 96
	avg := float32(total) / float32(len(days)) * height / float32(most)
	today := time.Now()
	ui.Row(c).Gap(6).AlignItems(ui.Center).Justify(ui.End).Children(func() {
		ui.Box(c).Width(12).Height(1).Background(t.Text.Alpha(0.3))
		caption(c, "Daily average "+perDay(float32(total)/float32(len(days)))).FontSize(10)
		ui.Box(c).Size(8, 8).Radius(2).Margin(0, 0, 0, 6).Background(t.Accent)
		caption(c, "Today").FontSize(10)
	})
	// The columns touch, which keeps the average line through them whole.
	ui.Row(c).AlignItems(ui.End).Children(func() {
		for i, n := range days {
			day := today.AddDate(0, 0, i-len(days)+1)
			isToday := i == len(days)-1
			col := ui.Column(c.Key(i)).Grow(1).Basis(0).Gap(6).AlignItems(ui.Center).
				Tooltip(fmt.Sprintf("%s: %d", day.Format("Monday, Jan 2"), n))
			hovered := col.Hovered()
			col.Children(func() {
				countColor := t.TextMuted
				if hovered {
					countColor = t.Text
				}
				count := ui.Text(c, fmt.Sprint(n)).FontSize(10).FontWeight(600).FontFeatures("tnum").
					TextColor(col.AnimateColor("count", countColor, hoverAnim))
				if n == 0 {
					count.Invisible()
				}
				ui.Column(c).Height(height).FillWidth().Justify(ui.End).AlignItems(ui.Center).Children(func() {
					line := ui.Box(c.Key("avg")).Absolute().Left(0).Right(0).Height(1).PassThrough().
						Background(t.Text.Alpha(0.14))
					line.Bottom(grow(line, "y", avg, 0))
					// Rounded at the top, as it rises from the axis.
					b := ui.Box(c).Radius(5, 5, 2, 2).Width(22)
					h := grow(b, "h", float32(n)*height/float32(most), time.Duration(i)*40*time.Millisecond)
					b.Height(max(h, 3))
					switch {
					case n == 0:
						b.Background(t.Text.Alpha(0.06))
					case isToday:
						b.LinearGradient(ui.LinearGradient{From: t.Accent.Mix(ui.RGB(255, 255, 255), 0.2), To: t.Accent, Angle: 180})
					case hovered:
						b.Background(b.AnimateColor("bg", t.Accent.Alpha(0.6), hoverAnim))
					default:
						b.Background(b.AnimateColor("bg", t.Accent.Alpha(0.35), hoverAnim))
					}
				})
				label := caption(c, day.Format("Mon")).FontSize(10)
				if isToday {
					label.FontWeight(700).TextColor(t.Text)
				}
			})
		}
	})
}
