package main

import (
	"embed"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"sokki/internal/engine"
	"sokki/internal/model"
)

// The icons are Lucide's (ISC license), drawn in the color of the text.
//
//go:embed icons/*.svg icons/app-icon.png
var iconFiles embed.FS

func svgIcon(name string) *ui.SVG {
	data, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

var (
	iconSearch   = svgIcon("search")
	iconPlus     = svgIcon("plus")
	iconMatches  = svgIcon("layers")
	iconFile     = svgIcon("folder")
	iconGeneral  = svgIcon("settings-2")
	iconKeyboard = svgIcon("keyboard")
	iconPrivacy  = svgIcon("shield-check")
	iconAbout    = svgIcon("info")
	iconCloud    = svgIcon("cloud")
	iconHistory  = svgIcon("history")
	iconMatched  = svgIcon("circle-check")
	iconPaused   = svgIcon("circle-pause")
	iconWarning  = svgIcon("triangle-alert")
	iconTrash    = svgIcon("trash-2")
	iconPencil   = svgIcon("pencil")
	iconCopy     = svgIcon("copy")
	iconDupe     = svgIcon("copy-plus")
	iconInsights = svgIcon("chart-column")
	iconZap      = svgIcon("zap")
	iconActivity = svgIcon("activity")
	iconTimer    = svgIcon("timer")
	iconPrompt   = svgIcon("message-square-text")
	iconCursor   = svgIcon("text-cursor")
	iconClip     = svgIcon("clipboard")
)

// appIcon returns Sokki's icon, decoded the first time it shows.
var appIcon = sync.OnceValue(func() *ui.Bitmap {
	data, err := iconFiles.ReadFile("icons/app-icon.png")
	if err != nil {
		return nil
	}
	b, _ := ui.DecodeBitmap(data)
	return b
})

// keycap shows a trigger as a key would: monospaced, on a light plate,
// in the accent's colors on a chosen row.
func keycap(c *ui.Context, s string, chosen bool) ui.Element {
	return keycapMarked(c, s, chosen, nil)
}

// keycapMarked is a keycap with the runes at marks in bold, the palette's
// search matched there.
func keycapMarked(c *ui.Context, s string, chosen bool, marks []int) ui.Element {
	t := c.Theme()
	fg, bg, border := t.Text, t.Text.Alpha(0.06), t.Text.Alpha(0.1)
	if chosen {
		fg, bg, border = t.AccentText, t.AccentText.Alpha(0.18), t.AccentText.Alpha(0.25)
	}
	var e ui.Element
	if marks == nil {
		e = ui.Text(c, s)
	} else {
		mark := ui.Span{Weight: 700}
		if !chosen {
			mark.Color = t.Accent
		}
		e = ui.RichText(c, marked(s, marks, mark)...)
	}
	return e.Font("monospace").FontSize(12).TextColor(fg).SingleLine().
		Padding(2, 7).Radius(5).Background(bg).Border(0.5, border)
}

// triggerCap is a match's trigger as a keycap, or, for a match without
// one, a plate saying it is inserted from the palette.
func triggerCap(c *ui.Context, m model.Match, chosen bool, marks []int) ui.Element {
	if m.Trigger != "" {
		return keycapMarked(c, m.Trigger, chosen, marks)
	}
	t := c.Theme()
	fg, border := t.TextMuted, t.Text.Alpha(0.14)
	if chosen {
		fg, border = t.AccentText.Alpha(0.85), t.AccentText.Alpha(0.3)
	}
	return ui.Row(c).Gap(4).AlignItems(ui.Center).Padding(2, 7).Radius(5).Border(0.5, border).
		Tooltip("No trigger: inserted from the palette").Children(func() {
		ui.Icon(c, iconPrompt).FontSize(11).TextColor(fg)
		ui.Text(c, "Palette").FontSize(11).TextColor(fg).SingleLine()
	})
}

// marked splits s into spans, the runes at marks (ascending rune indexes)
// in the style of mark, the rest plain.
func marked(s string, marks []int, mark ui.Span) []ui.Span {
	var spans []ui.Span
	start, i, m := 0, 0, 0
	inMark := false
	for b := range s {
		on := m < len(marks) && marks[m] == i
		if on {
			m++
		}
		if b > start && on != inMark {
			spans = append(spans, styled(s[start:b], inMark, mark))
			start = b
		}
		inMark = on
		i++
	}
	if start < len(s) {
		spans = append(spans, styled(s[start:], inMark, mark))
	}
	return spans
}

func styled(text string, on bool, mark ui.Span) ui.Span {
	if !on {
		return ui.Span{Text: text}
	}
	mark.Text = text
	return mark
}

// replacementSpans shows a replacement with its variables apart, in the
// accent's color on a tint of it, as code within text.
func replacementSpans(c *ui.Context, s string) []ui.Span {
	vars := engine.Variables(s)
	if vars == nil {
		return []ui.Span{{Text: s}}
	}
	t := c.Theme()
	spans := make([]ui.Span, 0, 2*len(vars)+1)
	last := 0
	for _, v := range vars {
		if v[0] > last {
			spans = append(spans, ui.Span{Text: s[last:v[0]]})
		}
		spans = append(spans, ui.Span{Text: s[v[0]:v[1]], Color: t.Accent, Background: t.Accent.Alpha(0.12), Radius: 3})
		last = v[1]
	}
	if last < len(s) {
		spans = append(spans, ui.Span{Text: s[last:]})
	}
	return spans
}

// variableRanges are the variables of a replacement being edited, for its
// text area: in the accent's color. Their offsets are in runes.
func variableRanges(c *ui.Context, s string) []ui.TextRange {
	vars := engine.Variables(s)
	if vars == nil {
		return nil
	}
	accent := c.Theme().Accent
	out := make([]ui.TextRange, len(vars))
	runes, last := 0, 0
	for i, v := range vars {
		runes += utf8.RuneCountInString(s[last:v[0]])
		start := runes
		runes += utf8.RuneCountInString(s[v[0]:v[1]])
		out[i] = ui.TextRange{Start: start, End: runes, Color: accent}
		last = v[1]
	}
	return out
}

// ago says how long ago t was, as "5 minutes ago", or the day it was
// once that is more than a week.
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute", "minutes") + " ago"
	case d < 24*time.Hour:
		return plural(int(d/time.Hour), "hour", "hours") + " ago"
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d/(24*time.Hour)))
	case t.Year() == now.Year():
		return t.Local().Format("January 2")
	}
	return t.Local().Format("January 2, 2006")
}

// dot is a small round mark of a state's color.
func dot(c *ui.Context, color ui.Color) ui.Element {
	return ui.Box(c).Size(8, 8).Radius(4).Background(color).Shrink(0)
}

// kbd shows a key of a hint, as ↵ in "↵ Insert".
func kbd(c *ui.Context, key string) ui.Element {
	t := c.Theme()
	return ui.Text(c, key).FontSize(11).TextColor(t.TextMuted).SingleLine().
		MinWidth(18).TextAlign(ui.Center).Padding(1, 5).Radius(4).
		Background(t.Text.Alpha(0.06)).Border(0.5, t.Text.Alpha(0.12))
}

// hint shows a key and what it does.
func hint(c *ui.Context, key, what string) {
	ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
		kbd(c, key)
		caption(c, what).SingleLine()
	})
}

// group is a card of settings under a title, as System Settings draws
// them: rows with their label at the start and their control at the end,
// a hairline between them.
func group(c *ui.Context, title string, fn func()) {
	t := c.Theme()
	ui.Column(c).Gap(6).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(t.TextMuted).PaddingX(4)
		}
		ui.Column(c).Radius(10).Background(cardColor(t)).Border(0.5, t.Border.Alpha(0.6)).Clip().Children(fn)
	})
}

func cardColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Text.Alpha(0.04)
	}
	return t.Background
}

// setting is a row of a group: a label, what it does under it, and its
// control at the end. first leaves out the hairline above it.
func setting(c *ui.Context, label, description string, first bool, control func()) {
	t := c.Theme()
	row := ui.Row(c).Padding(10, 14).Gap(16).AlignItems(ui.Center).MinHeight(44)
	if !first {
		row.BorderWidth(0.5, 0, 0, 0).BorderColor(t.Border.Alpha(0.6))
	}
	row.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, label)
			if description != "" {
				caption(c, description).Wrap()
			}
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(control)
	})
}

// pageHeader is the title of a page of Settings, with what is at its end.
func pageHeader(c *ui.Context, title, subtitle string, end func()) {
	ui.Row(c).Padding(0, 0, 4, 0).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(20).Bold().SingleLine()
			if subtitle != "" {
				muted(c, subtitle).SingleLine()
			}
		})
		if end != nil {
			end()
		}
	})
}

// emptyState fills its parent with an icon on a tinted disc, a title, a
// line under it and, when actions is set, the buttons it draws.
func emptyState(c *ui.Context, icon *ui.SVG, title, detail string, actions func()) {
	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(8).Padding(24).Children(func() {
		ui.Box(c).Size(52, 52).Radius(26).Background(t.Text.Alpha(0.05)).Center().Margin(0, 0, 4, 0).Children(func() {
			ui.Icon(c, icon).FontSize(24).TextColor(t.TextMuted)
		})
		ui.Text(c, title).FontSize(14).FontWeight(600).TextAlign(ui.Center)
		if detail != "" {
			muted(c, detail).Wrap().TextAlign(ui.Center).MaxWidth(300)
		}
		if actions != nil {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(8, 0, 0, 0).Children(actions)
		}
	})
}

// iconButton is a borderless button showing only an icon, named by label
// for assistive technology and in its tooltip.
func iconButton(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Padding(5).Radius(6).Children(func() {
		ui.Icon(c, icon).FontSize(15).TextColor(t.TextMuted)
	})
	if b.Pressed() {
		b.Background(t.Text.Alpha(0.12))
	} else if b.Hovered() {
		b.Background(t.Text.Alpha(0.07))
	}
	return b
}

func muted(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).TextColor(c.Theme().TextMuted)
}

func caption(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(11).TextColor(c.Theme().TextMuted)
}

// section is a part of a pane under a small title.
func section(c *ui.Context, title string, fn func()) {
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, title).FontSize(11).FontWeight(600).TextColor(c.Theme().TextMuted)
		fn()
	})
}

// textPlate shows a text as it will be typed: monospaced, wrapped, on a
// light plate, selectable.
func textPlate(c *ui.Context, s string) ui.Element {
	return plate(c, ui.Text(c, s))
}

// replacementPlate is a textPlate of a replacement, its variables apart.
func replacementPlate(c *ui.Context, s string) ui.Element {
	return plate(c, ui.RichText(c, replacementSpans(c, s)...))
}

func plate(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	return e.Font("monospace").FontSize(12).LineHeight(1.45).Wrap().Selectable().
		Padding(10, 12).Radius(8).Background(t.Text.Alpha(0.035)).Border(0.5, t.Border.Alpha(0.7))
}
