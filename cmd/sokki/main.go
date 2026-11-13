// Sokki is a menu bar text expander for macOS: type a trigger anywhere
// and it is replaced with its text. Its windows are MyGo's native UI.
package main

import (
	"context"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater/native"

	"sokki/internal/cli"
	"sokki/internal/config"
	"sokki/internal/expander"
	"sokki/internal/history"
	"sokki/internal/logx"
	"sokki/internal/macos"
	"sokki/internal/model"
	"sokki/internal/prefs"
	"sokki/internal/telemetry"
	"sokki/internal/usage"
)

// bundleID is the identifier of the Swift app's UserDefaults, which the
// preferences are migrated from.
const bundleID = "com.sokki.Sokki"

// app is Sokki's state. Its fields are read and changed on the main
// thread; the expander's handlers hand their work to it with post.
type app struct {
	dir     config.Dir
	store   *config.Store
	recency *config.Recency
	usage   *usage.Store
	// history is the prompts sent to agents, and the uses of matches.
	history *history.Store
	// scanning is set while the history's sources are read.
	scanning atomic.Bool
	// historyHome is where the agents keep their history, "" for the
	// home folder; tests have their own.
	historyHome string
	prefs       *prefs.Store
	exp         *expander.Expander
	telemetry   *telemetry.Config

	tray   *mygo.Tray
	secure bool
	// trusted is whether Sokki has the Accessibility permission, without
	// which it cannot see what is typed.
	trusted bool

	settings *settingsWindow
	editor   *editorWindow
	palette  *palette
	hud      *hudPanel
	// cloud syncs the configuration and matches with the person's
	// account.
	cloud *cloud

	// leases counts the windows that need Sokki to be a regular app, with
	// a Dock icon, so that macOS lets them come to the front and take the
	// keyboard; it is an accessory app otherwise.
	leases int

	mainQueue chan func()
	// commandPath is where the sokki command is installed, "" for
	// nowhere.
	commandPath string

	// quitting is set once the app quits, which closes the windows that
	// otherwise hide when closed.
	quitting bool
}

// hideOnClose has closing win (Cmd-W, its close button, Escape in a panel)
// hide it instead, so that it shows again at once, as it was; quitting
// still closes it.
func (a *app) hideOnClose(win *mygo.Window, hide func()) {
	win.OnClose(func(e *mygo.CloseEvent) {
		if a.quitting {
			return
		}
		e.PreventDefault()
		hide()
	})
}

// post runs fn on the main thread, after the functions posted before it.
func (a *app) post(fn func()) {
	select {
	case a.mainQueue <- fn:
	default:
		go func() { a.mainQueue <- fn }()
	}
}

func (a *app) runMainQueue() {
	for fn := range a.mainQueue {
		mygo.RunOnMain(fn)
	}
}

// acquire makes Sokki a regular app while a window that needs the front
// shows; release undoes it once none does.
func (a *app) acquire() {
	a.leases++
	if a.leases == 1 {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	}
	mygo.App.Focus()
}

func (a *app) release() {
	a.leases = max(0, a.leases-1)
	if a.leases == 0 {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "cli" {
		// The sokki command, which asks the running app: see control.go.
		os.Exit(cli.Main(os.Args[2:], cli.Env{Dev: mygo.IsDev()}))
	}
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	a := &app{dir: config.UserDir(), mainQueue: make(chan func(), 256)}
	mygo.App.SetName("Sokki")
	if mygo.IsDev() {
		// Development keeps its prefs, crashes and window state in its
		// configuration directory, apart from the installed Sokki's.
		data := filepath.Join(string(a.dir), "data")
		os.MkdirAll(data, 0o755)
		mygo.App.SetPath(mygo.PathUserData, data)
	}
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {}) // a menu bar app runs without windows
	mygo.App.OnSecondInstance(func([]string, string) { a.openSettings() })
	mygo.Use(native.Plugin)
	mygo.App.WhenReady(a.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *app) ready() {
	go a.runMainQueue()
	data, _ := mygo.App.Path(mygo.PathUserData)
	crashes := telemetry.KeepCrashes(data)
	a.prefs = prefs.Open(filepath.Join(data, "prefs.json"))

	created, err := a.dir.Bootstrap()
	if err != nil {
		logx.Errorf("Bootstrap failed: %v", err)
	}

	a.store = config.NewStore(a.dir)
	a.recency = config.NewRecency(a.dir)
	a.usage = usage.Open(string(a.dir))
	a.history = history.Open(data)
	a.exp = expander.New(expander.Handlers{
		SecureInputChanged: func(s bool) { a.post(func() { a.secure = s; a.updateTray() }) },
		Toggled:            func(on bool) { a.post(func() { a.store.Config.Enabled = on; a.updateTray() }) },
		HUDRequested:       func() { a.post(a.hud.toggle) },
		HUDEvent:           func(ev model.HUDEvent) { a.post(func() { a.recordEvent(ev) }) },
		PaletteRequested:   func() { a.post(a.palette.toggle) },
		CaptureRequested:   func() { a.post(a.palette.capture) },
		Expanded: func(m model.Match, saved int, fromPalette bool) {
			a.post(func() { a.logUse(m, saved, fromPalette) })
		},
	})
	a.store.OnMatchesChanged = func() {
		a.exp.SetMatches(a.store.Matches)
		a.settings.invalidate()
		a.palette.invalidate()
	}
	a.settings = newSettingsWindow(a)
	a.editor = &editorWindow{a: a}
	a.palette = newPalette(a)
	a.hud = newHUDPanel(a)

	a.cloud = newCloud(a)

	a.setUpTray()
	a.store.Load()
	a.applyConfig()
	a.watchConfig()
	a.cloud.start()
	a.startControl()
	a.loadCommand()
	a.scanHistory()
	go func() {
		for range time.Tick(historyEvery) {
			a.scanHistory()
		}
	}()

	// Typing in another app starts afresh; leaving one drops a match held
	// for a longer trigger.
	macos.OnAppActivated(func() { a.exp.Ctx.ClearBuffer() })
	macos.OnAppDeactivated(func() { a.exp.Ctx.CancelPending("app-switch") })

	a.trusted = macos.RequestTrust()
	a.updateTray()
	if !a.trusted {
		logx.Warnf("Accessibility permission not granted")
		go a.waitForTrust()
	}
	a.exp.Start()

	if a.telemetry = telemetry.Load(resourcesDir()); a.telemetry != nil {
		a.telemetry.AppVersion = mygo.App.Version()
		a.telemetry.OSVersion = macOSVersion()
		a.telemetry.Locale = strings.ReplaceAll(mygo.App.Locale(), "-", "_")
	}
	// The installed Sokki's install ID and switches come over from the
	// Swift app's UserDefaults before telemetry reads them.
	go func() {
		if !mygo.IsDev() {
			a.prefs.MigrateUserDefaults(bundleID)
		}
		if a.telemetry != nil {
			a.telemetry.UploadCrashes(a.prefs, crashes)
			a.telemetry.Ping(a.prefs)
		}
	}()

	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.quitting = true })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		a.exp.Stop()
		a.cloud.flush()
		a.recency.Flush()
		a.usage.Flush()
		a.history.Flush()
		if a.tray != nil {
			a.tray.Destroy()
		}
		logx.Flush()
	})
	if created {
		go a.welcome()
	}
	logx.Debugf("Sokki started")
}

func resourcesDir() string {
	dir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return ""
	}
	return dir
}

// applyConfig gives the configuration to the engine and the tray.
func (a *app) applyConfig() {
	logx.SetLevel(a.store.Config.LogLevel)
	a.exp.SetConfig(a.store.Config)
	a.updateTray()
}

func (a *app) reloadConfig() {
	a.store.Load()
	a.applyConfig()
	a.settings.invalidate()
	a.cloud.changed()
	logx.Debugf("Config reloaded")
}

// saveConfig writes the configuration after an edit in Settings.
func (a *app) saveConfig() {
	a.store.SaveConfig()
	a.applyConfig()
}

// watchConfig reloads the configuration as its files change, apart from
// the log's, which Sokki writes itself: reloading on them wrote more log,
// in a loop. Settings sync's own files are left out too.
func (a *app) watchConfig() {
	w, err := mygo.WatchFiles(context.Background(), string(a.dir), mygo.FileWatchOptions{
		Recursive: true,
		Debounce:  300 * time.Millisecond,
		Ignore: func(p string, isDir bool) bool {
			return isDir && path.Base(p) == "logs"
		},
	})
	if err != nil {
		logx.Errorf("Failed to watch %s: %v", a.dir, err)
		return
	}
	logx.Debugf("Config watcher started for %s", a.dir)
	go func() {
		defer w.Close()
		for {
			events, err := w.Next(context.Background())
			if err != nil {
				return
			}
			relevant := false
			for _, e := range events {
				base := path.Base(e.Path)
				if !strings.HasPrefix(e.Path, "logs/") && e.Path != "logs" && !strings.HasPrefix(base, ".sokki-") &&
					!strings.HasPrefix(base, syncFile) && base != tokenKey && !strings.HasPrefix(base, usage.File) &&
					!strings.HasPrefix(base, history.File) {
					relevant = true
				}
			}
			if relevant {
				a.post(a.reloadConfig)
			}
		}
	}()
}

// waitForTrust polls for the Accessibility permission, and once the user
// grants it offers to relaunch, which the event tap needs.
func (a *app) waitForTrust() {
	logx.Warnf("Accessibility not granted — polling for grant")
	for !macos.IsTrusted() {
		time.Sleep(2 * time.Second)
	}
	logx.Warnf("Accessibility granted — prompting to relaunch")
	a.post(func() {
		a.trusted = true
		a.updateTray()
		a.acquire()
	})
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Type:    mygo.MessageInfo,
		Message: "Accessibility granted — restart to start working",
		Detail:  "Sokki needs to restart before it can read keystrokes. Relaunch now?",
		Buttons: []string{"Quit & Reopen", "Later"},
	})
	a.post(a.release)
	if err == nil && res.Button == 0 {
		a.post(a.relaunch)
	}
}

func (a *app) relaunch() {
	logx.Debugf("Quit & Reopen")
	mygo.App.Relaunch()
}

// welcome tells a first launch how to try Sokki and where its matches
// are.
func (a *app) welcome() {
	a.post(a.acquire)
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Type:    mygo.MessageInfo,
		Message: "Welcome to Sokki",
		Detail: "Type :hello in any app to try it.\n\n" +
			"Your matches are in Settings, under the keyboard icon in the menu bar, " +
			"and kept as text in ~/.config/sokki.",
		Buttons: []string{"Open Settings", "OK"},
	})
	a.post(a.release)
	if err == nil && res.Button == 0 {
		a.post(a.openSettings)
	}
}

// recordEvent takes what the expander did with a trigger: Recent Activity
// shows it, and an expansion counts as a use of its match.
func (a *app) recordEvent(ev model.HUDEvent) {
	if ev.Verdict == model.VerdictMatched && ev.MatchedTrigger != "" {
		a.recency.Bump(ev.MatchedTrigger)
		a.history.BumpMatch(ev.MatchedTrigger)
		a.settings.invalidate()
	}
	a.hud.record(ev)
}

// logUse counts an expansion for Insights, in the app in front, which
// it went to.
func (a *app) logUse(m model.Match, saved int, fromPalette bool) {
	id, name := macos.FrontmostApp()
	a.usage.Log(usage.Event{Trigger: m.Key(), Palette: fromPalette, App: id, AppName: name, Saved: saved})
	a.settings.invalidate()
}

// used counts a use of m from the palette or the sokki command, which
// the palette ranks by.
func (a *app) used(m model.Match) {
	a.recency.Bump(m.Key())
	a.history.BumpMatch(m.Key())
}

// logRecall counts a prompt of the agents' history that the palette
// inserted for Insights, in the app in front, which it went to.
func (a *app) logRecall(agent, text string) {
	id, name := macos.FrontmostApp()
	a.usage.Log(usage.Event{Agent: agent, Palette: true, App: id, AppName: name, Saved: utf8.RuneCountInString(text)})
	a.settings.invalidate()
}

// historyEvery is how often the agents' history is read, besides as the
// palette shows.
const historyEvery = 2 * time.Minute

// historyEnv is where the history's sources look.
func (a *app) historyEnv() history.Env {
	return history.Env{Home: a.historyHome, StateHome: os.Getenv("XDG_STATE_HOME")}
}

// scanHistory reads what the agents added to their history since the
// last reading, away from the main thread, unless a reading runs.
func (a *app) scanHistory() {
	if !a.scanning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.scanning.Store(false)
		gen := a.history.Gen()
		a.history.Scan(a.historyEnv())
		changed := a.history.Gen() != gen
		a.post(func() {
			if changed {
				a.palette.invalidate()
			}
			a.settings.invalidate()
		})
	}()
}

// accessibilitySettings is the pane of System Settings that grants Sokki
// the Accessibility permission.
const accessibilitySettings = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"

// grantAccess opens System Settings where Sokki gets the Accessibility
// permission.
func (a *app) grantAccess() { mygo.Shell.OpenExternal(accessibilitySettings) }

// setEnabled turns expansion on or off, as the Settings switch does.
func (a *app) setEnabled(on bool) {
	a.store.Config.Enabled = on
	a.saveConfig()
	a.settings.invalidate()
}

// openSettings shows Settings, on the Matches tab unless told otherwise.
func (a *app) openSettings() { a.settings.show() }
