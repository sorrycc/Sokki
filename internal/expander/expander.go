//go:build darwin

// Package expander runs the expansion: the event tap feeding the engine,
// and the injection of replacements.
package expander

import (
	"sync"
	"time"
	"unicode/utf8"

	"sokki/internal/engine"
	"sokki/internal/logx"
	"sokki/internal/macos"
	"sokki/internal/model"
)

// Handlers are how the expander tells the app what happened. They are
// called on the expander's own goroutines: hand them to the UI thread.
type Handlers struct {
	SecureInputChanged func(secure bool)
	Toggled            func(enabled bool)
	HUDRequested       func()
	HUDEvent           func(ev model.HUDEvent)
	PaletteRequested   func()
	// CaptureRequested is called on the shortcut that saves the
	// selection as a prompt.
	CaptureRequested func()
	// Expanded is called after a match is typed in, by its trigger or
	// from the palette, with the characters it saved.
	Expanded func(m model.Match, saved int, fromPalette bool)
}

// Expander is the running expansion engine.
type Expander struct {
	Ctx      *engine.Context
	Injector *Injector
	h        Handlers

	mu  sync.Mutex
	cfg model.Config
	tap *macos.Tap

	// work runs the steps that follow keys, in their order, one at a
	// time, as the Swift app's main queue did.
	work chan func()
}

// New makes an expander, not started.
func New(h Handlers) *Expander {
	e := &Expander{
		Ctx:  engine.NewContext(macos.IsSecureInput),
		h:    h,
		cfg:  model.DefaultConfig(),
		work: make(chan func(), 256),
	}
	e.Injector = &Injector{cfg: e.Config}
	go func() {
		for fn := range e.work {
			fn()
		}
	}()
	e.Ctx.OnPendingFired(func(m model.Match, n int) {
		e.hud(model.VerdictMatched, m.Trigger, m.Trigger, "")
		e.Ctx.SetSuppress(true)
		e.onMatch(m, n)
	})
	return e
}

// Config returns the configuration in use.
func (e *Expander) Config() model.Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg
}

// SetConfig applies a configuration.
func (e *Expander) SetConfig(c model.Config) {
	e.mu.Lock()
	e.cfg = c
	e.mu.Unlock()
	e.Ctx.SetConfig(c)
}

// SetMatches replaces the matches.
func (e *Expander) SetMatches(ms []model.Match) { e.Ctx.SetMatches(ms) }

// Async runs fn after the steps already queued.
func (e *Expander) Async(fn func()) {
	select {
	case e.work <- fn:
	default:
		go func() { e.work <- fn }()
	}
}

// Start installs the event tap. It fails without the Accessibility
// permission.
func (e *Expander) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.tap != nil {
		return nil
	}
	tap, err := macos.StartTap(macos.TapOptions{
		OnKey: e.handleKey,
		OnMouseDown: func() {
			// A click may have moved the caret: no pending match.
			e.Ctx.CancelPending("mouse-click")
		},
		OnRearm: func(timeout bool) {
			reason := "userInput"
			if timeout {
				reason = "timeout"
			}
			logx.Warnf("Event tap disabled (%s) — re-enabling", reason)
		},
	})
	if err != nil {
		logx.Errorf("Failed to create CGEvent tap. Check Accessibility permissions. (%v)", err)
		return err
	}
	e.tap = tap
	logx.Debugf("KeystrokeMonitor started")
	return nil
}

// Stop removes the event tap.
func (e *Expander) Stop() {
	e.mu.Lock()
	tap := e.tap
	e.tap = nil
	e.mu.Unlock()
	if tap != nil {
		tap.Stop()
	}
	e.Ctx.CancelPending("monitor-stopped")
	logx.Debugf("KeystrokeMonitor stopped")
}

func (e *Expander) hud(v model.Verdict, buffer, trigger, note string) {
	if e.h.HUDEvent != nil {
		e.h.HUDEvent(model.HUDEvent{ID: model.NewID(), Time: time.Now(), Verdict: v, Buffer: buffer, MatchedTrigger: trigger, Note: note})
	}
}

func call(fn func()) {
	if fn != nil {
		fn()
	}
}

// handleKey runs on the tap's thread for every key down and modifier
// change, and returns whether the event goes on to the focused app.
func (e *Expander) handleKey(ev macos.KeyEvent) bool {
	flags := engine.Flags(ev.Flags)
	if ev.Type == macos.EventFlagsChanged {
		switch r := e.Ctx.ModifierChanged(flags); r.Action {
		case engine.ActionToggled:
			e.Async(func() { call(func() { e.h.Toggled(r.Enabled) }) })
		case engine.ActionHUDRequested:
			e.Async(func() { call(e.h.HUDRequested) })
		}
		return true
	}

	// The palette's chord is swallowed, so that ⌥Space, say, does not
	// also switch the input source.
	if e.Ctx.PaletteChord(ev.KeyCode, flags) {
		e.Async(func() { call(e.h.PaletteRequested) })
		return false
	}
	if e.Ctx.CaptureChord(ev.KeyCode, flags) {
		e.Async(func() { call(e.h.CaptureRequested) })
		return false
	}

	if flags&engine.FlagCommand != 0 && ev.KeyCode == macos.KeyV {
		e.Ctx.PasteDetected()
		return true
	}

	// A shortcut (Cmd or Ctrl, but backspace) is no typing.
	if ev.KeyCode != macos.KeyBackspace && (flags&engine.FlagCommand != 0 || flags&engine.FlagControl != 0) {
		e.Ctx.CancelPending("modifier-shortcut")
		return true
	}

	var ch rune
	switch {
	case ev.KeyCode == macos.KeyBackspace:
		ch = '\b'
	case ev.Text != "":
		ch = []rune(ev.Text)[0]
	default:
		return true
	}

	// The engine traces each key only after its secure input check, so
	// that no password reaches the log.
	r := e.Ctx.Feed(ch, ev.KeyCode, flags)
	switch r.Action {
	case engine.ActionMatched:
		logx.Debugf("Match found: %s -> %s", r.Match.Trigger, r.Match.Replace)
		e.hud(model.VerdictMatched, r.Match.Trigger, r.Match.Trigger, "")
		e.Ctx.SetSuppress(true)
		e.onMatch(r.Match, r.TriggerLength)
	case engine.ActionUndo:
		logx.Debugf("Undo: restoring trigger %s", r.Trigger)
		e.Ctx.SetSuppress(true)
		e.Async(func() {
			e.Injector.Type(r.Trigger)
			e.Ctx.SetSuppress(false)
		})
	case engine.ActionSecureInputChanged:
		e.Async(func() { call(func() { e.h.SecureInputChanged(r.Secure) }) })
	case engine.ActionToggled:
		e.Async(func() { call(func() { e.h.Toggled(r.Enabled) }) })
	case engine.ActionHUDRequested:
		e.Async(func() { call(e.h.HUDRequested) })
	case engine.ActionBlockedTooSoon:
		e.hud(model.VerdictTooSoon, r.Snapshot, "", "")
	case engine.ActionBlockedDisabled:
		e.hud(model.VerdictDisabled, r.Snapshot, "", "")
	}
	return true
}

// onMatch expands a typed trigger, after the steps already queued. The
// engine is suppressed when it is called.
func (e *Expander) onMatch(m model.Match, triggerLength int) {
	e.Async(func() {
		rendered := engine.Render(m.Replace, clipboardText, time.Now())
		e.Injector.Inject(rendered, triggerLength)
		e.Ctx.RecordExpansion(m.Trigger, rendered)
		e.Ctx.SetSuppress(false)
		e.expanded(m, rendered, false)
	})
}

// expanded reports an expansion, which saved the replacement's
// characters less the trigger's.
func (e *Expander) expanded(m model.Match, rendered string, fromPalette bool) {
	if e.h.Expanded != nil {
		saved := utf8.RuneCountInString(rendered) - utf8.RuneCountInString(m.Trigger)
		e.h.Expanded(m, max(saved, 0), fromPalette)
	}
}

func clipboardText() string {
	s, _ := macos.PasteboardString()
	return s
}

// InjectFromPalette types a match chosen in the palette, once the app
// that was in front has the keyboard again.
func (e *Expander) InjectFromPalette(m model.Match) {
	rendered := engine.Render(m.Replace, clipboardText, time.Now())
	time.AfterFunc(30*time.Millisecond, func() {
		e.Async(func() {
			e.Injector.Inject(rendered, 0)
			e.expanded(m, rendered, true)
		})
	})
}
