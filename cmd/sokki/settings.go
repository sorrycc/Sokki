package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"

	"sokki/internal/logx"
	"sokki/internal/prefs"
)

const settingsTitle = "Sokki Settings"

// The pages of Settings, by the IDs of their sidebar items. A match file's
// page is filePage followed by its name.
const (
	matchesPage   = "matches"
	filePage      = "file:"
	generalPage   = "general"
	shortcutsPage = "shortcuts"
	privacyPage   = "privacy"
	aboutPage     = "about"
)

// sidebarWidth is the width of Settings' sidebar.
const sidebarWidth = 210

// settingsWindow is the Settings window: the matches, general settings,
// shortcuts, privacy and about. It is made the first time it shows, and
// hides rather than closes.
type settingsWindow struct {
	a    *app
	win  *mygo.Window
	page string

	matches matchList

	filesShown   bool
	confirmReset bool
	// atLogin is whether Sokki opens at login, read as Settings shows.
	atLogin bool
}

func newSettingsWindow(a *app) *settingsWindow {
	s := &settingsWindow{a: a, page: matchesPage, filesShown: true}
	s.matches.a = a
	return s
}

func (s *settingsWindow) invalidate() {
	if s != nil && s.win != nil {
		s.win.Invalidate()
	}
}

func (s *settingsWindow) show() {
	if s.win == nil {
		s.win = mygo.NewWindow(mygo.WindowOptions{
			Title:         settingsTitle,
			Width:         780,
			Height:        560,
			MinWidth:      640,
			MinHeight:     440,
			Hidden:        true,
			StateKey:      "settings",
			TitleBarStyle: mygo.TitleBarHidden,
			Vibrancy:      mygo.VibrancySidebar,
			Content:       ui.View(s.view),
		})
		s.a.hideOnClose(s.win, s.hide)
	}
	if !s.win.IsVisible() {
		s.a.acquire()
	}
	s.atLogin = mygo.App.OpenAtLogin()
	s.win.Show()
	s.win.Focus()
}

func (s *settingsWindow) hide() {
	if s.win == nil || !s.win.IsVisible() {
		return
	}
	s.win.Hide()
	s.a.release()
}

// showPage shows Settings on a page.
func (s *settingsWindow) showPage(page string) {
	s.setPage(page)
	s.show()
}

func (s *settingsWindow) setPage(page string) {
	s.page = page
	switch {
	case page == matchesPage:
		s.matches.setFile("")
	case strings.HasPrefix(page, filePage):
		s.matches.setFile(strings.TrimPrefix(page, filePage))
	}
}

func (s *settingsWindow) view(c *ui.Context) {
	t := c.Theme()
	pages := []string{matchesPage, generalPage, shortcutsPage, privacyPage, aboutPage}
	for i, p := range pages {
		c.OnShortcut(ui.Cmd, ui.Key1+ui.Key(i), func() { s.setPage(p) })
	}
	c.OnShortcut(ui.Cmd, ui.KeyW, s.hide)

	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		side := ui.Column(c).Width(sidebarWidth).Shrink(0)
		if !c.Vibrancy() {
			side.Background(t.Surface)
		}
		side.Children(func() { s.sidebar(c) })
		ui.Divider(c).Vertical()
		ui.Column(c).Grow(1).Shrink(1).Background(t.Background).Children(func() {
			switch {
			case s.page == matchesPage || strings.HasPrefix(s.page, filePage):
				s.matches.view(c)
			case s.page == generalPage:
				s.general(c)
			case s.page == shortcutsPage:
				s.shortcuts(c)
			case s.page == privacyPage:
				s.privacy(c)
			default:
				s.about(c)
			}
		})
	})
}

func (s *settingsWindow) sidebar(c *ui.Context) {
	bar := c.TitleBar()
	// The room of the window's controls, which drags the window.
	ui.Box(c).Height(max(bar.Height, 12) + 8).DragWindow()
	page := s.page
	counts := s.a.store.FileCounts()
	sb := ui.Sidebar(c, &page, func() {
		ui.SidebarItem(c, matchesPage, iconMatches, "All Matches").Children(func() {
			sidebarCount(c, len(s.a.store.Matches))
		})
		ui.SidebarSection(c, "Match Files", &s.filesShown, func() {
			for _, f := range s.a.store.SourceFiles() {
				ui.SidebarItem(c, filePage+f, iconFile, f).Children(func() {
					sidebarCount(c, counts[f])
				})
			}
		})
		ui.SidebarSection(c, "Settings", nil, func() {
			ui.SidebarItem(c, generalPage, iconGeneral, "General")
			ui.SidebarItem(c, shortcutsPage, iconKeyboard, "Shortcuts")
			ui.SidebarItem(c, privacyPage, iconPrivacy, "Privacy")
			ui.SidebarItem(c, aboutPage, iconAbout, "About")
		})
	}).Grow(1).Label("Settings")
	if c.Vibrancy() {
		sb.Background(ui.Transparent)
	}
	if sb.Changed() {
		s.setPage(page)
	}
	s.statusCard(c)
}

// statusCard, at the foot of the sidebar, says whether Sokki is expanding
// triggers, with a switch to pause it, or what it needs to.
func (s *settingsWindow) statusCard(c *ui.Context) {
	t := c.Theme()
	st := s.a.status()
	ui.Column(c).Padding(8, 10, 12, 10).Children(func() {
		ui.Column(c).Padding(10, 12).Gap(10).Radius(10).Background(t.Text.Alpha(0.05)).Border(0.5, t.Text.Alpha(0.06)).Children(func() {
			ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
				switch {
				case st.attention:
					ui.Icon(c, iconWarning).FontSize(14).TextColor(t.Warning)
				case st.on:
					dot(c, t.Success)
				case s.a.secure:
					dot(c, t.Warning)
				default:
					dot(c, t.TextMuted.Alpha(0.6))
				}
				ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
					title, detail := "Sokki is on", "Active in all apps"
					switch {
					case st.attention:
						title, detail = "Needs access", "Sokki can't see what you type"
					case s.a.secure:
						title, detail = "Paused", "A password field is active"
					case !st.on:
						title, detail = "Paused", "Triggers don't expand"
					}
					ui.Text(c, title).FontSize(12).FontWeight(600).SingleLine()
					caption(c, detail).Wrap()
				})
				if !st.attention {
					on := s.a.store.Config.Enabled
					if ui.Switch(c, &on).Label("Expand triggers").Tooltip("Pause or resume Sokki").Changed() {
						s.a.setEnabled(on)
					}
				}
			})
			if st.attention {
				ui.PrimaryButton(c, "Grant Access…").Label("Grant Accessibility access").OnClick(s.a.grantAccess)
			}
		})
	})
}

func sidebarCount(c *ui.Context, n int) {
	caption(c, fmt.Sprint(n)).FontFeatures("tnum")
}

// scrollPage lays out a page of settings: its header, which drags the
// window, over its groups, which scroll.
func scrollPage(c *ui.Context, title, subtitle string, fn func()) {
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Padding(0, 28, 28, 28).Gap(22).MaxWidth(680).Children(func() {
			ui.Box(c).Height(max(c.TitleBar().Height, 12)).DragWindow()
			ui.Column(c).DragWindow().Children(func() { pageHeader(c, title, subtitle, nil) })
			fn()
		})
	})
}

// option is a value of a setting and its label.
type option struct{ value, label string }

// choose shows a drop-down of options for *value, and reports a change.
func choose(c *ui.Context, value *string, options []option) bool {
	labels := make([]string, len(options))
	label := ""
	for i, o := range options {
		labels[i] = o.label
		if o.value == *value {
			label = o.label
		}
	}
	if label == "" && len(options) > 0 {
		label = options[len(options)-1].label
	}
	if ui.Select(c, &label, labels).MinWidth(170).Changed() {
		for _, o := range options {
			if o.label == label {
				*value = o.value
				return true
			}
		}
	}
	return false
}

// number edits an int setting, in unit, which shows after it.
func number(c *ui.Context, v *int, lo, hi float64, unit string) bool {
	f := float64(*v)
	changed := false
	if ui.NumberInput(c, &f, lo, hi, 1).Changed() {
		*v = int(f)
		changed = true
	}
	caption(c, unit).Width(36).SingleLine()
	return changed
}

var toggleOptions = []option{
	{"double_option", "Double-tap ⌥ Option"},
	{"double_shift", "Double-tap ⇧ Shift"},
	{"double_control", "Double-tap ⌃ Control"},
	{"none", "None"},
}

var paletteOptions = []option{
	{"opt+space", "⌥ Space"},
	{"ctrl+space", "⌃ Space"},
	{"ctrl+opt+space", "⌃⌥ Space"},
	{"ctrl+shift+space", "⌃⇧ Space"},
	{"none", "None"},
}

// shortcutLabel returns how an option of a shortcut reads, "" for none.
func shortcutLabel(options []option, value string) string {
	for _, o := range options {
		if o.value == value && value != "none" {
			return o.label
		}
	}
	return ""
}

func (s *settingsWindow) general(c *ui.Context) {
	cfg := &s.a.store.Config
	changed := false
	scrollPage(c, "General", "How Sokki starts and types for you.", func() {
		group(c, "", func() {
			setting(c, "Expand triggers", "Turn off to pause Sokki everywhere.", true, func() {
				changed = ui.Switch(c, &cfg.Enabled).Label("Expand triggers").Changed() || changed
			})
			setting(c, "Launch at login", "Start Sokki when you log in to your Mac.", false, func() {
				if ui.Switch(c, &s.atLogin).Label("Launch at login").Changed() {
					if err := mygo.App.SetOpenAtLogin(s.atLogin); err != nil {
						logx.Errorf("Failed to set launch at login: %v", err)
						s.atLogin = !s.atLogin
					}
				}
			})
		})
		group(c, "Typing", func() {
			setting(c, "Key delay", "The pause between typed keys. Raise it for apps that drop keys.", true, func() {
				changed = number(c, &cfg.InjectDelay, 0, 1000, "ms") || changed
			})
			setting(c, "Overlap delay", "When a trigger starts a longer one, how long to wait for the rest.", false, func() {
				changed = number(c, &cfg.ConflictResolveDelayMs, 50, 5000, "ms") || changed
			})
		})
		group(c, "Clipboard", func() {
			setting(c, "Paste longer than", "Longer replacements are pasted instead of typed, which is faster.", true, func() {
				changed = number(c, &cfg.ClipboardThreshold, 0, 100000, "chars") || changed
			})
			setting(c, "Restore clipboard", "Put back what was on the clipboard after pasting.", false, func() {
				changed = ui.Switch(c, &cfg.PreserveClipboard).Label("Restore clipboard").Changed() || changed
			})
			setting(c, "Restore after", "How long to wait before putting the clipboard back.", false, func() {
				changed = number(c, &cfg.RestoreClipboardDelay, 0, 10000, "ms") || changed
			})
		})
		group(c, "Troubleshooting", func() {
			setting(c, "Log level", "Debug also logs every expansion.", true, func() {
				changed = choose(c, &cfg.LogLevel, []option{{"error", "Errors only"}, {"debug", "Debug"}}) || changed
			})
			setting(c, "Files", "Matches and settings live in ~/.config/sokki.", false, func() {
				if ui.Button(c, "Show Log").Clicked() {
					mygo.Shell.OpenPath(logx.File())
				}
				if ui.Button(c, "Open Folder").Clicked() {
					mygo.Shell.OpenPath(string(s.a.dir))
				}
			})
		})
	})
	if changed {
		s.a.saveConfig()
	}
}

func (s *settingsWindow) shortcuts(c *ui.Context) {
	cfg := &s.a.store.Config
	changed := false
	scrollPage(c, "Shortcuts", "Reach Sokki from any app.", func() {
		group(c, "", func() {
			setting(c, "Search matches", "Opens the palette to find a match and type it.", true, func() {
				changed = choose(c, &cfg.PaletteShortcut, paletteOptions) || changed
			})
			setting(c, "Pause or resume", "Turns expansion off and on again.", false, func() {
				changed = choose(c, &cfg.ToggleShortcut, toggleOptions) || changed
			})
			setting(c, "Recent Activity", "Shows what Sokki did with what you typed.", false, func() {
				changed = choose(c, &cfg.HUDShortcut, toggleOptions) || changed
			})
		})
		if cfg.HUDShortcut != "none" && cfg.HUDShortcut == cfg.ToggleShortcut {
			ui.Row(c).Gap(6).AlignItems(ui.Center).PaddingX(4).Children(func() {
				ui.Icon(c, iconWarning).FontSize(12).TextColor(c.Theme().Warning)
				caption(c, "Both use the same key: it shows Recent Activity.").Wrap().Shrink(1)
			})
		}
		group(c, "In the palette", func() {
			for i, h := range [][2]string{
				{"↵", "Type the match into the app you were in"},
				{"⌘↵", "Edit the match"},
				{"⌘N", "New match"},
				{"⌘,", "Open Settings"},
				{"esc", "Clear the search, then close"},
			} {
				setting(c, h[1], "", i == 0, func() { kbd(c, h[0]) })
			}
		})
	})
	if changed {
		s.a.saveConfig()
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (s *settingsWindow) privacy(c *ui.Context) {
	scrollPage(c, "Privacy", "What Sokki sends, and what it never does.", func() {
		if s.a.telemetry == nil {
			group(c, "", func() {
				setting(c, "Telemetry is off in this build", "Only release builds can send crash reports and usage stats.", true, func() {})
			})
			s.never(c)
			return
		}
		p := s.a.prefs
		group(c, "", func() {
			setting(c, "Send crash reports", "When Sokki crashes it keeps a crash log, sent at the next launch so the bug can be fixed.", true, func() {
				on := p.Get().CrashReportsOn()
				if ui.Switch(c, &on).Label("Send crash reports").Changed() {
					p.Update(func(pr *prefs.Prefs) { pr.CrashReports = &on })
				}
			})
			setting(c, "Send anonymous usage stats", "One ping a day with the versions of Sokki and macOS and your locale, to count active installs.", false, func() {
				on := p.Get().DailyPingOn()
				if ui.Switch(c, &on).Label("Send anonymous usage stats").Changed() {
					p.Update(func(pr *prefs.Prefs) { pr.DailyPing = &on })
				}
			})
		})
		group(c, "Identifier", func() {
			setting(c, "Anonymous ID", "Reports and pings carry it, and nothing else about you.", true, func() {
				muted(c, truncate(p.InstallID(), 8)).Font("monospace").Selectable()
				if ui.Button(c, "Reset…").Clicked() {
					s.confirmReset = true
				}
			})
		})
		s.never(c)
	})
	if ui.AlertDialog(c, &s.confirmReset, "Reset Anonymous ID?",
		"Future pings and crash reports will carry a new ID. The switches above stay as they are.",
		"Cancel", "Reset") == 1 {
		s.a.prefs.ResetInstallID()
	}
}

func (s *settingsWindow) never(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Gap(10).Padding(12, 14).Radius(10).Background(t.Success.Alpha(0.1)).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, iconPrivacy).FontSize(16).TextColor(t.Success)
		ui.Text(c, "Sokki never sends what you type, your matches or your clipboard. What it sends is kept on Cloudflare under our account and deleted after a year.").
			FontSize(12).Wrap().Shrink(1)
	})
}

func (s *settingsWindow) about(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		ui.Box(c).Height(max(c.TitleBar().Height, 12) + 40).DragWindow()
		ui.Column(c).Grow(1).AlignItems(ui.Center).Gap(6).Padding(0, 28, 28, 28).Children(func() {
			if icon := appIcon(); icon != nil {
				ui.Image(c, icon).Size(112, 112).Margin(0, 0, 8, 0)
			}
			ui.Text(c, "Sokki").FontSize(26).Bold()
			if v := mygo.App.Version(); v != "" {
				muted(c, "Version "+v).Selectable()
			}
			caption(c, "Type a trigger anywhere, and Sokki types the rest.").Margin(4, 0, 0, 0)
			ui.Row(c).Gap(8).Margin(18, 0, 0, 0).Children(func() {
				if mygo.Updater.Enabled() && ui.PrimaryButton(c, "Check for Updates…").Clicked() {
					updater.CheckForUpdates()
				}
				if ui.Button(c, "Release Notes").Clicked() {
					mygo.Shell.OpenExternal("https://github.com/sorrycc/Sokki/blob/main/CHANGELOG.md")
				}
				if ui.Button(c, "GitHub").Clicked() {
					mygo.Shell.OpenExternal("https://github.com/sorrycc/Sokki")
				}
			})
			if mygo.Updater.Enabled() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(14, 0, 0, 0).Children(func() {
					on := updater.AutomaticChecks()
					if ui.Checkbox(c, &on, "Check for updates automatically").Changed() {
						updater.SetAutomaticChecks(on)
					}
				})
			}
			ui.Spacer(c)
			ui.Text(c, "© 2026 sorrycc").FontSize(11).TextColor(t.TextMuted.Alpha(0.8))
		})
	})
}
