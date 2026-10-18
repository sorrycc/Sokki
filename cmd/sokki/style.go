package main

import (
	"embed"
	"sync"

	"github.com/egoist/mygo/ui"
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
	iconHistory  = svgIcon("history")
	iconMatched  = svgIcon("circle-check")
	iconPaused   = svgIcon("circle-pause")
	iconWarning  = svgIcon("triangle-alert")
	iconTrash    = svgIcon("trash-2")
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
	t := c.Theme()
	fg, bg, border := t.Text, t.Text.Alpha(0.06), t.Text.Alpha(0.1)
	if chosen {
		fg, bg, border = t.AccentText, t.AccentText.Alpha(0.18), t.AccentText.Alpha(0.25)
	}
	return ui.Text(c, s).Font("monospace").FontSize(12).TextColor(fg).SingleLine().
		Padding(2, 7).Radius(5).Background(bg).Border(0.5, border)
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

// emptyState fills its parent with an icon, a title and a line under it.
func emptyState(c *ui.Context, icon *ui.SVG, title, detail string) {
	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(8).Padding(24).Children(func() {
		ui.Icon(c, icon).FontSize(28).TextColor(t.TextMuted.Alpha(0.7))
		ui.Text(c, title).FontSize(14).FontWeight(600).TextAlign(ui.Center)
		if detail != "" {
			muted(c, detail).Wrap().TextAlign(ui.Center).MaxWidth(280)
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
	t := c.Theme()
	return ui.Text(c, s).Font("monospace").FontSize(12).LineHeight(1.45).Wrap().Selectable().
		Padding(10, 12).Radius(8).Background(t.Text.Alpha(0.035)).Border(0.5, t.Border.Alpha(0.7))
}
