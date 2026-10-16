package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"

	"sokki/internal/ai"
	"sokki/internal/logx"
	"sokki/internal/model"
	"sokki/internal/prefs"
)

const settingsTitle = "Sokki Settings"

// settingsWindow is the Settings window: the matches, general settings,
// the AI's and privacy. It is made the first time it shows, and hides
// rather than closes.
type settingsWindow struct {
	a   *app
	win *mygo.Window
	tab int

	matches matchList
	aiTest  aiTest

	confirmReset bool
	// atLogin is whether Sokki opens at login, read as Settings shows.
	atLogin bool
}

func newSettingsWindow(a *app) *settingsWindow {
	s := &settingsWindow{a: a}
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
			Title:     settingsTitle,
			Width:     620,
			Height:    520,
			MinWidth:  500,
			MinHeight: 400,
			Hidden:    true,
			StateKey:  "settings",
			Content:   ui.View(s.view),
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
	s.aiTest.cancel()
	s.win.Hide()
	s.a.release()
}

func (s *settingsWindow) view(c *ui.Context) {
	c.OnShortcut(ui.Cmd, ui.Key1, func() { s.tab = 0 })
	c.OnShortcut(ui.Cmd, ui.Key2, func() { s.tab = 1 })
	c.OnShortcut(ui.Cmd, ui.Key3, func() { s.tab = 2 })
	c.OnShortcut(ui.Cmd, ui.Key4, func() { s.tab = 3 })
	c.OnShortcut(ui.Cmd, ui.KeyW, s.hide)
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(10, 12, 0, 12).Justify(ui.Center).Children(func() {
			ui.Tabs(c, &s.tab, "Matches", "General", "AI", "Privacy")
		})
		ui.Column(c).Grow(1).Children(func() {
			switch s.tab {
			case 0:
				s.matches.view(c)
			case 1:
				s.general(c)
			case 2:
				s.aiView(c)
			default:
				s.privacy(c)
			}
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
	if ui.Select(c, &label, labels).Changed() {
		for _, o := range options {
			if o.label == label {
				*value = o.value
				return true
			}
		}
	}
	return false
}

// number edits an int setting.
func number(c *ui.Context, v *int, lo, hi float64) bool {
	f := float64(*v)
	if ui.NumberInput(c, &f, lo, hi, 1).Width(120).Changed() {
		*v = int(f)
		return true
	}
	return false
}

func section(c *ui.Context, title string, fn func()) {
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, title).Bold().FontSize(13)
		ui.Column(c).Padding(12).Gap(10).Radius(8).Background(c.Theme().Surface).Children(func() {
			ui.Form(c, fn)
		})
	})
}

// switchRow is a switch with what it does beside it, so that the field's
// label stays level with the switch.
func switchRow(c *ui.Context, on *bool, description string) bool {
	changed := false
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		changed = ui.Switch(c, on).Changed()
		if description != "" {
			caption(c, description).Wrap().Shrink(1)
		}
	})
	return changed
}

func muted(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).TextColor(c.Theme().TextMuted)
}

func caption(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(11).TextColor(c.Theme().TextMuted)
}

var toggleOptions = []option{
	{"double_option", "Double Option"},
	{"double_shift", "Double Shift"},
	{"double_control", "Double Control"},
	{"none", "None"},
}

var paletteOptions = []option{
	{"opt+space", "⌥ Space"},
	{"ctrl+space", "⌃ Space"},
	{"ctrl+opt+space", "⌃⌥ Space"},
	{"ctrl+shift+space", "⌃⇧ Space"},
	{"none", "None"},
}

func (s *settingsWindow) general(c *ui.Context) {
	cfg := &s.a.store.Config
	changed := false
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Padding(16, 20).Gap(18).Children(func() {
			section(c, "Behavior", func() {
				ui.Field(c, "Launch at Login", func() {
					if ui.Switch(c, &s.atLogin).Changed() {
						if err := mygo.App.SetOpenAtLogin(s.atLogin); err != nil {
							logx.Errorf("Failed to set launch at login: %v", err)
							s.atLogin = !s.atLogin
						}
					}
				})
				ui.Field(c, "Clipboard threshold (chars)", func() {
					changed = number(c, &cfg.ClipboardThreshold, 0, 100000) || changed
				}).Description("Text longer than this will use clipboard injection")
				ui.Field(c, "Preserve clipboard", func() {
					changed = switchRow(c, &cfg.PreserveClipboard, "Restore clipboard after clipboard-mode injection") || changed
				})
				ui.Field(c, "Inject delay (ms)", func() {
					changed = number(c, &cfg.InjectDelay, 0, 1000) || changed
				}).Description("Delay between simulated keypresses")
				ui.Field(c, "Restore clipboard delay (ms)", func() {
					changed = number(c, &cfg.RestoreClipboardDelay, 0, 10000) || changed
				})
			})
			section(c, "Logging", func() {
				ui.Field(c, "Log level", func() {
					changed = choose(c, &cfg.LogLevel, []option{{"error", "Error"}, {"debug", "Debug"}}) || changed
				})
			})
			section(c, "Toggle Shortcut", func() {
				ui.Field(c, "Quick toggle", func() {
					changed = choose(c, &cfg.ToggleShortcut, toggleOptions) || changed
				}).Description("Double-tap to toggle Sokki on/off")
			})
			section(c, "Palette Shortcut", func() {
				ui.Field(c, "Open palette", func() {
					changed = choose(c, &cfg.PaletteShortcut, paletteOptions) || changed
				}).Description("Spotlight-style fuzzy search across all your matches")
			})
			if mygo.Updater.Enabled() {
				section(c, "Updates", func() {
					ui.Field(c, "Current version", func() { muted(c, mygo.App.Version()) })
					ui.Field(c, "Automatically check for updates", func() {
						on := updater.AutomaticChecks()
						if ui.Switch(c, &on).Changed() {
							updater.SetAutomaticChecks(on)
						}
					})
					ui.Field(c, "", func() {
						if ui.Button(c, "Check for Updates…").AlignSelf(ui.Start).Clicked() {
							updater.CheckForUpdates()
						}
					})
				})
			}
		})
	})
	if changed {
		s.a.saveConfig()
	}
}

var placeholderOptions = []option{
	{string(model.PlaceholderStatic), "Static"},
	{string(model.PlaceholderBraille), "Braille"},
	{string(model.PlaceholderBlockSweep), "Block sweep"},
}

func (s *settingsWindow) aiView(c *ui.Context) {
	cfg := &s.a.store.Config.AI
	changed := false
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Padding(16, 20).Gap(18).Children(func() {
			muted(c, "AI features are powered by your locally-installed Claude Code CLI.")
			section(c, "Model", func() {
				ui.Field(c, "Model", func() {
					changed = ui.TextInput(c, &cfg.Model).Width(260).Changed() || changed
				})
				ui.Field(c, "Claude binary", func() {
					binary := deref(cfg.Binary)
					if ui.TextInput(c, &binary).Width(260).Placeholder("Auto-detect").Changed() {
						if binary == "" {
							cfg.Binary = nil
						} else {
							cfg.Binary = &binary
						}
						s.a.exp.Rewriter.Resolver.Invalidate()
						changed = true
					}
				}).Description("Path to the `claude` CLI. Leave blank to auto-detect: ~/.claude/local/claude, ~/.local/bin/claude, /opt/homebrew/bin/claude, /usr/local/bin/claude, then shell lookup.")
				ui.Field(c, "Timeout (seconds)", func() {
					changed = number(c, &cfg.TimeoutSeconds, 1, 600) || changed
				}).Description("Max seconds to wait for the AI response before failing.")
				ui.Field(c, "Selection capture timeout (ms)", func() {
					changed = number(c, &cfg.SelectionCaptureTimeoutMs, 50, 5000) || changed
				})
			})
			section(c, "Placeholder", func() {
				ui.Field(c, "Placeholder style", func() {
					style := string(cfg.PlaceholderStyle)
					if choose(c, &style, placeholderOptions) {
						cfg.PlaceholderStyle = model.PlaceholderStyle(style)
						changed = true
					}
				})
			})
			s.aiTest.view(c, s)
		})
	})
	if changed {
		s.a.saveConfig()
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// aiTest is the AI tab's "Test connection": a round trip through claude.
type aiTest struct {
	running bool
	gen     int
	rw      *ai.Rewriter
	ok      bool
	failed  bool
	binary  string
	model   string
	reply   string
	detail  string
}

func (t *aiTest) view(c *ui.Context, s *settingsWindow) {
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, "Test").Bold().FontSize(13)
		ui.Column(c).Padding(12).Gap(8).Radius(8).Background(c.Theme().Surface).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				label := "Test connection"
				if t.running {
					label = "Cancel"
				}
				if ui.Button(c, label).Clicked() {
					if t.running {
						t.cancel()
					} else {
						t.start(s)
					}
				}
				if t.running {
					ui.Spinner(c)
				}
			})
			th := c.Theme()
			switch {
			case t.running:
				muted(c, "Running…")
			case t.ok:
				ui.Text(c, "✓ Connection OK").TextColor(th.Success).Bold()
				caption(c, "Binary: "+t.binary).SingleLine()
				caption(c, "Model: "+t.model).SingleLine()
				caption(c, "Reply: "+t.reply).MaxLines(3)
			case t.failed:
				ui.Text(c, "✕ Test failed").TextColor(th.Danger).Bold()
				caption(c, t.detail).MaxLines(8)
			}
		})
	})
}

func (t *aiTest) start(s *settingsWindow) {
	cfg := s.a.store.Config.AI
	t.cancel()
	t.gen++
	gen := t.gen
	t.running, t.ok, t.failed = true, false, false
	resolver := &ai.Resolver{}
	t.rw = &ai.Rewriter{Resolver: resolver, ScratchDir: ai.ScratchDir()}
	rw := t.rw
	timeout := time.Duration(max(cfg.TimeoutSeconds, 1)) * time.Second
	go func() {
		path, found := resolver.Resolve(deref(cfg.Binary))
		if !found {
			path = "(not found)"
		}
		res := rw.Run(ai.Request{Prompt: "ping", Model: cfg.Model, Timeout: timeout, UserArgs: cfg.Args, Binary: deref(cfg.Binary)})
		s.win.Update(func() {
			if gen != t.gen || res.Outcome == ai.Canceled {
				return
			}
			t.running = false
			switch res.Outcome {
			case ai.Completed:
				t.ok, t.binary, t.model, t.reply = true, path, cfg.Model, truncate(strings.TrimSpace(res.Text), 200)
			case ai.Failed:
				t.failed, t.detail = true, testDetail(res.Reason, path, timeout)
			}
		})
	}()
}

func (t *aiTest) cancel() {
	if t.rw != nil {
		t.rw.Cancel()
		t.rw = nil
	}
	t.gen++
	t.running, t.ok, t.failed = false, false, false
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func testDetail(r ai.Reason, binary string, timeout time.Duration) string {
	var header string
	switch r.Kind {
	case ai.ReasonBinaryMissing:
		return "`claude` binary not found.\nSet a path in 'Claude binary' above, or install the Claude Code CLI."
	case ai.ReasonAuthRequired:
		header = "Not signed in to Claude Code CLI.\nRun `claude` in a terminal and sign in, then retry."
	case ai.ReasonTimeout:
		header = "Timed out after " + timeout.String() + ".\nIncrease the Timeout field above or check your network."
	case ai.ReasonNonZeroExit:
		tail := strings.TrimSpace(r.Tail)
		if tail == "" {
			tail = "(no stderr output)"
		}
		header = "CLI exited with code " + strconv.Itoa(r.Code) + ":\n" + truncate(tail, 500)
	default:
		header = "Protocol error: " + r.Detail
	}
	return header + "\nBinary: " + binary
}

func (s *settingsWindow) privacy(c *ui.Context) {
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Padding(16, 20).Gap(18).Children(func() {
			if s.a.telemetry == nil {
				muted(c, "Telemetry is only active in packaged release builds.")
				return
			}
			p := s.a.prefs
			section(c, "Telemetry", func() {
				ui.Field(c, "Send crash reports automatically", func() {
					on := p.Get().CrashReportsOn()
					if switchRow(c, &on, "When Sokki crashes it keeps a crash log. We upload those on next launch so we can fix the bug.") {
						p.Update(func(pr *prefs.Prefs) { pr.CrashReports = &on })
					}
				})
				ui.Field(c, "Send anonymous usage stats", func() {
					on := p.Get().DailyPingOn()
					if switchRow(c, &on, "Sends one anonymous ping per day with your app version, macOS version, and locale. Used to count active installs.") {
						p.Update(func(pr *prefs.Prefs) { pr.DailyPing = &on })
					}
				})
			})
			caption(c, "We never send personal data, keystroke contents, match text, or clipboard data. Data is stored on Cloudflare under our account and auto-deleted after one year.")
			section(c, "Identifier", func() {
				ui.Field(c, "Anonymous ID", func() {
					id := p.InstallID()
					muted(c, truncate(id, 8)).Font("monospace")
				})
				ui.Field(c, "", func() {
					if ui.Button(c, "Reset Anonymous ID").AlignSelf(ui.Start).Clicked() {
						s.confirmReset = true
					}
				})
			})
		})
	})
	if ui.AlertDialog(c, &s.confirmReset, "Reset Anonymous ID?",
		"Future pings and crashes will be attributed to a new UUID. This does not change the telemetry toggles above.",
		"Cancel", "Reset") == 1 {
		p := s.a.prefs
		p.ResetInstallID()
	}
}
