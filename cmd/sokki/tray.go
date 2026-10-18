package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"syscall"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"

	"sokki/internal/logx"
)

func (a *app) setUpTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: trayIcon(1), IconIsTemplate: true, ToolTip: "Sokki"})
	if err != nil {
		logx.Errorf("tray: %v", err)
		return
	}
	a.tray = tray
	a.updateTray()
}

// updateTray shows the state in the menu bar: the icon dims while Sokki
// is off or secure input is on, the tooltip says the last config error,
// and the menu is built anew.
func (a *app) updateTray() {
	if a.tray == nil {
		return
	}
	alpha := 1.0
	if !a.store.Config.Enabled || a.secure || !a.trusted {
		alpha = 0.35
	}
	a.tray.SetIcon(trayIcon(alpha), true)
	tip := "Sokki"
	if !a.trusted {
		tip = "Sokki needs Accessibility access"
	}
	if a.store.LastError != "" {
		tip = "Sokki: " + a.store.LastError
	}
	a.tray.SetToolTip(tip)
	a.tray.SetMenu(a.trayMenu())
	a.settings.invalidate()
}

func (a *app) trayMenu() *mygo.Menu {
	click := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) { fn() }
	}
	cfg := a.store.Config
	items := []*mygo.MenuItem{
		{Label: a.status().text, Disabled: true},
	}
	if !a.trusted {
		items = append(items, &mygo.MenuItem{Label: "Grant Accessibility Access…", Click: click(a.grantAccess)})
	}
	if a.store.LastError != "" {
		items = append(items, &mygo.MenuItem{Label: "⚠ A match file has an error", Click: click(func() { a.settings.showPage(matchesPage) })})
	}
	enabled := "Pause Sokki"
	if !cfg.Enabled {
		enabled = "Resume Sokki"
	}
	if s := shortcutLabel(toggleOptions, cfg.ToggleShortcut); s != "" {
		enabled += "  (" + s + ")"
	}
	search := "Search Matches…"
	if s := shortcutLabel(paletteOptions, cfg.PaletteShortcut); s != "" {
		search += "  (" + s + ")"
	}
	items = append(items,
		&mygo.MenuItem{Label: enabled, Click: func(*mygo.MenuItem, *mygo.Window) {
			a.store.Config.Enabled = !a.store.Config.Enabled
			a.exp.SetConfig(a.store.Config)
			a.updateTray()
			a.settings.invalidate()
		}},
		mygo.Separator(),
		&mygo.MenuItem{Label: search, Click: click(a.palette.show)},
		&mygo.MenuItem{Label: "New Match…", Click: click(func() { a.settings.matches.newMatch("") })},
		&mygo.MenuItem{Label: "Recent Activity", Click: click(a.hud.toggle)},
		mygo.Separator(),
		&mygo.MenuItem{Label: "Settings…", Accelerator: "Cmd+,", Click: click(a.openSettings)},
	)
	if mygo.Updater.Enabled() {
		items = append(items, &mygo.MenuItem{Label: "Check for Updates…", Click: click(func() {
			mygo.App.Focus()
			updater.CheckForUpdates()
		})})
	}
	items = append(items,
		&mygo.MenuItem{Label: "Troubleshooting", Submenu: []*mygo.MenuItem{
			{Label: "Reload Matches", Click: click(a.reloadConfig)},
			{Label: "Open Config Folder", Click: click(func() { mygo.Shell.OpenPath(string(a.dir)) })},
			{Label: "Show Log", Click: click(func() { mygo.Shell.OpenPath(logx.File()) })},
			mygo.Separator(),
			{Label: "Restart Sokki", Click: click(a.relaunch)},
		}},
		mygo.Separator(),
		&mygo.MenuItem{Label: "Quit Sokki", Accelerator: "Cmd+Q", Click: click(mygo.App.Quit)},
	)
	return mygo.NewMenu(items)
}

// state is what Sokki is doing, as the menu bar and Settings say it.
type state struct {
	text string
	// on is whether triggers expand; attention whether the user is
	// needed for them to.
	on, attention bool
}

func (a *app) status() state {
	switch {
	case !a.trusted:
		return state{text: "Sokki needs Accessibility access", attention: true}
	case a.secure:
		return state{text: "Paused while you type a password"}
	case !a.store.Config.Enabled:
		return state{text: "Sokki is paused"}
	}
	return state{text: "Sokki is on", on: true}
}

// trayIcons are the menu bar icons drawn so far, by alpha.
var trayIcons = map[float64][]byte{}

// trayIcon draws the menu bar icon, a keycap with a tab arrow (⇥), black
// on transparent for macOS to tint, at 36×36 pixels for an 18-point
// status item, with alpha for its dimmed state.
func trayIcon(alpha float64) []byte {
	if b, ok := trayIcons[alpha]; ok {
		return b
	}
	const size, scale = 36, 2.0 // pixels, pixels per point
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 4 // supersamples per axis
	for py := range size {
		for px := range size {
			hits := 0
			for sy := range ss {
				for sx := range ss {
					x := (float64(px) + (float64(sx)+0.5)/ss) / scale
					y := (float64(py) + (float64(sy)+0.5)/ss) / scale
					if inIcon(x, y) {
						hits++
					}
				}
			}
			if hits > 0 {
				img.SetNRGBA(px, py, color.NRGBA{A: uint8(math.Round(255 * alpha * float64(hits) / ss / ss))})
			}
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	trayIcons[alpha] = b.Bytes()
	return b.Bytes()
}

// inIcon reports whether the point (in points, y down, on an 18×18 grid)
// is inked: the outline of the keycap, the shaft and head of the arrow,
// and the bar it ends at.
func inIcon(x, y float64) bool {
	const lw = 1.25
	// The keycap: a rounded rectangle 16×13 at (1, 2.5), radius 2.5.
	d := roundedRectDistance(x, y, 1, 2.5, 16, 13, 2.5)
	if math.Abs(d) <= lw/2 {
		return true
	}
	// The shaft, from (3.5, 9) to (10, 9), with round caps.
	if segmentDistance(x, y, 3.5, 9, 10, 9) <= lw/2 {
		return true
	}
	// The head, a triangle (10, 7), (12.5, 9), (10, 11).
	if inTriangle(x, y, 10, 7, 12.5, 9, 10, 11) {
		return true
	}
	// The bar, from (14, 6) to (14, 12).
	return segmentDistance(x, y, 14, 6, 14, 12) <= lw/2
}

func roundedRectDistance(x, y, rx, ry, w, h, r float64) float64 {
	cx, cy := rx+w/2, ry+h/2
	qx := math.Abs(x-cx) - (w/2 - r)
	qy := math.Abs(y-cy) - (h/2 - r)
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	inside := math.Min(math.Max(qx, qy), 0)
	return outside + inside - r
}

func segmentDistance(x, y, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	t := ((x-x1)*dx + (y-y1)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(x-(x1+t*dx), y-(y1+t*dy))
}

func inTriangle(x, y, x1, y1, x2, y2, x3, y3 float64) bool {
	sign := func(ax, ay, bx, by, cx, cy float64) float64 { return (ax-cx)*(by-cy) - (bx-cx)*(ay-cy) }
	d1 := sign(x, y, x1, y1, x2, y2)
	d2 := sign(x, y, x2, y2, x3, y3)
	d3 := sign(x, y, x3, y3, x1, y1)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

// macOSVersion returns the version of macOS, as 15.3.1.
func macOSVersion() string {
	v, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return "unknown"
	}
	return v
}
