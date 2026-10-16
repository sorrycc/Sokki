package engine

import (
	"strings"
	"sync"
	"time"

	"sokki/internal/logx"
	"sokki/internal/model"
)

// Flags are the modifier flags of a key event (CGEventFlags).
type Flags uint64

// The modifiers.
const (
	FlagShift     Flags = 0x00020000
	FlagControl   Flags = 0x00040000
	FlagAlternate Flags = 0x00080000
	FlagCommand   Flags = 0x00100000

	modifierMask = FlagShift | FlagControl | FlagAlternate | FlagCommand
)

// Key codes the state machine looks at.
const (
	KeySpace     uint16 = 0x31
	KeyBackspace uint16 = 0x33
)

func isCaretKey(code uint16) bool {
	switch code {
	case 0x7B, 0x7C, 0x7D, 0x7E, // arrows
		0x73, 0x77, 0x74, 0x79: // Home, End, PageUp, PageDown
		return true
	}
	return false
}

// Action is what feeding a key decided.
type Action int

const (
	ActionNone Action = iota
	ActionMatched
	ActionUndo
	ActionSecureInputChanged
	ActionToggled
	ActionHUDRequested
	ActionBlockedTooSoon
	ActionBlockedDisabled
)

// Result is an Action and what goes with it.
type Result struct {
	Action        Action
	Match         model.Match
	TriggerLength int
	// Trigger is the trigger to restore, for ActionUndo.
	Trigger string
	// Secure and Enabled are the new states of ActionSecureInputChanged
	// and ActionToggled.
	Secure, Enabled bool
	// Snapshot is what was typed, for the blocked actions.
	Snapshot string
}

// PaletteChord is the key and the modifiers that open the palette.
type PaletteChord struct {
	KeyCode   uint16
	Modifiers Flags
}

// ParsePaletteChord parses palette_shortcut, nil for "none".
func ParsePaletteChord(s string) *PaletteChord {
	switch s {
	case "opt+space":
		return &PaletteChord{KeySpace, FlagAlternate}
	case "ctrl+space":
		return &PaletteChord{KeySpace, FlagControl}
	case "ctrl+opt+space":
		return &PaletteChord{KeySpace, FlagControl | FlagAlternate}
	case "ctrl+shift+space":
		return &PaletteChord{KeySpace, FlagControl | FlagShift}
	}
	return nil
}

// Context is the state of the expansion engine, which the event tap's
// thread feeds and the rest of the app reads and changes: every method
// takes its lock.
type Context struct {
	mu sync.Mutex

	buffer        *Buffer
	matcher       *Matcher
	wasSecure     bool
	undo          Undo
	suppress      bool
	pasteUntil    time.Time
	enabled       bool
	toggleShort   string
	hudShort      string
	lastTapAt     time.Time
	lastTapFlag   Flags
	tooSoonShown  bool
	disabledShown bool
	chord         *PaletteChord
	conflictDelay int
	timer         *time.Timer
	timerToken    uint64
	onPending     func(model.Match, int)

	// IsSecure reports whether secure input is on; macOS's in the app,
	// another in tests.
	IsSecure func() bool
	// Now is the clock, for tests.
	Now func() time.Time
}

// NewContext makes an engine without matches.
func NewContext(isSecure func() bool) *Context {
	return &Context{
		buffer: NewBuffer(128), matcher: NewMatcher(nil), enabled: true,
		toggleShort: "none", hudShort: "none", conflictDelay: 400,
		IsSecure: isSecure, Now: time.Now,
	}
}

// Feed takes a character typed (backspace as '\b') with its key code and
// modifiers, and decides what it does.
func (c *Context) Feed(ch rune, keyCode uint16, flags Flags) Result {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Our own injected keys echo back through the tap: ignore them.
	if c.suppress {
		return Result{}
	}

	// Secure input first: nothing typed in a secure field goes further,
	// not even into the HUD.
	secure := c.IsSecure != nil && c.IsSecure()
	switch {
	case secure && !c.wasSecure:
		c.wasSecure = true
		logx.Tracef("secure: entered — buffer cleared")
		c.buffer.Clear()
		c.resetMatcher()
		return Result{Action: ActionSecureInputChanged, Secure: true}
	case !secure && c.wasSecure:
		c.wasSecure = false
		logx.Tracef("secure: exited — buffer cleared")
		c.buffer.Clear()
		c.resetMatcher()
		return Result{Action: ActionSecureInputChanged, Secure: false}
	case secure:
		return Result{}
	}

	now := c.Now()
	if !c.pasteUntil.IsZero() && now.Before(c.pasteUntil) {
		if !c.tooSoonShown {
			c.tooSoonShown = true
			snap := c.buffer.String() + string(ch)
			logx.Tracef("blocked: paste-too-soon buf=%q", snap)
			return Result{Action: ActionBlockedTooSoon, Snapshot: snap}
		}
		return Result{}
	}
	c.pasteUntil = time.Time{}

	if !c.enabled {
		if !c.disabledShown {
			c.disabledShown = true
			snap := c.buffer.String() + string(ch)
			logx.Tracef("blocked: disabled buf=%q", snap)
			return Result{Action: ActionBlockedDisabled, Snapshot: snap}
		}
		return Result{}
	}

	if keyCode == KeyBackspace {
		if flags&FlagAlternate != 0 || flags&FlagCommand != 0 {
			c.invalidate()
			logx.Tracef("clear: word/line-delete flags=%d", flags)
			return Result{}
		}
		if trigger, ok := c.undo.Backspace(); ok {
			c.buffer.Clear()
			c.resetMatcher()
			return Result{Action: ActionUndo, Trigger: trigger}
		}
		c.rebuildAfterBackspace()
		logx.Tracef("rewind: popped, remaining=%d %s", c.buffer.Len(), c.matcher.DebugSummary())
		return Result{}
	}

	if isCaretKey(keyCode) {
		c.invalidate()
		logx.Tracef("clear: caret-move keyCode=%d", keyCode)
		return Result{}
	}

	c.undo.Clear()
	c.buffer.Append(ch)
	tail := c.buffer.Tail(min(16, c.buffer.Len()))
	res := c.matcher.Feed(ch)
	summary := c.matcher.DebugSummary()
	if res != nil {
		logx.Tracef("feed '%c' buf=%q %s -> matched %s len=%d", ch, tail, summary, res.Match.Trigger, res.TriggerLength)
		c.buffer.Clear()
		c.reschedule()
		return Result{Action: ActionMatched, Match: res.Match, TriggerLength: res.TriggerLength}
	}
	logx.Tracef("feed '%c' buf=%q %s -> none", ch, tail, summary)
	c.reschedule()
	return Result{}
}

// PasteDetected starts the short window after Cmd-V in which typing does
// not expand.
func (c *Context) PasteDetected() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now()
	if c.pasteUntil.IsZero() || !now.Before(c.pasteUntil) {
		c.tooSoonShown = false
	}
	c.pasteUntil = now.Add(200 * time.Millisecond)
}

func shortcutFlag(s string) Flags {
	switch s {
	case "double_option":
		return FlagAlternate
	case "double_shift":
		return FlagShift
	case "double_control":
		return FlagControl
	}
	return 0
}

// ModifierChanged takes the modifiers after a flags-changed event, and
// fires the toggle or the HUD on a double tap of their modifier.
func (c *Context) ModifierChanged(flags Flags) Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	active := flags & modifierMask
	hud, toggle := shortcutFlag(c.hudShort), shortcutFlag(c.toggleShort)

	// The HUD wins when both are the same modifier.
	var kind string
	var flag Flags
	switch {
	case hud != 0 && active == hud:
		kind, flag = "hud", hud
	case toggle != 0 && active == toggle:
		kind, flag = "toggle", toggle
	default:
		c.lastTapAt = time.Time{}
		return Result{}
	}

	now := c.Now()
	if !c.lastTapAt.IsZero() && c.lastTapFlag == flag && now.Sub(c.lastTapAt) < 300*time.Millisecond {
		c.lastTapAt, c.lastTapFlag = time.Time{}, 0
		if kind == "hud" {
			return Result{Action: ActionHUDRequested}
		}
		c.enabled = !c.enabled
		c.notePendingDrop("toggle-shortcut")
		c.buffer.Clear()
		c.resetMatcher()
		if c.enabled {
			c.disabledShown = false
		}
		return Result{Action: ActionToggled, Enabled: c.enabled}
	}
	c.lastTapAt, c.lastTapFlag = now, flag
	return Result{}
}

// SetSuppress turns on and off the gate that ignores the keys Sokki
// injects itself.
func (c *Context) SetSuppress(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.suppress = on
	if on && c.matcher.Pending() != nil {
		c.notePendingDrop("suppress")
		c.resetMatcher()
	}
}

// ClearBuffer forgets what was typed, as when another app takes focus.
func (c *Context) ClearBuffer() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notePendingDrop("focus-change")
	c.buffer.Clear()
	c.resetMatcher()
}

// SetMatches replaces the triggers.
func (c *Context) SetMatches(matches []model.Match) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notePendingDrop("matches-reload")
	c.stopTimer()
	c.matcher = NewMatcher(matches)
	c.buffer.Clear()
	var triggers []string
	for _, m := range matches {
		if m.Enabled {
			triggers = append(triggers, m.Trigger)
		}
	}
	logx.Tracef("updateMatches: %d enabled; triggers=[%s]", len(triggers), strings.Join(triggers, ", "))
}

// SetConfig applies the configuration's settings of the engine.
func (c *Context) SetConfig(cfg model.Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	wasDisabled := !c.enabled
	c.enabled = cfg.Enabled
	c.toggleShort = cfg.ToggleShortcut
	c.hudShort = cfg.HUDShortcut
	c.chord = ParsePaletteChord(cfg.PaletteShortcut)
	c.conflictDelay = cfg.ConflictResolveDelayMs
	if wasDisabled && c.enabled {
		c.disabledShown = false
	}
}

// PaletteChord reports whether a key down is the palette's shortcut, and
// drops a pending match when it is.
func (c *Context) PaletteChord(keyCode uint16, flags Flags) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.chord == nil {
		return false
	}
	if keyCode == c.chord.KeyCode && flags&modifierMask == c.chord.Modifiers {
		c.notePendingDrop("palette-chord")
		c.resetMatcher()
		return true
	}
	return false
}

// CancelPending drops a pending match, as a click or an app switch does.
func (c *Context) CancelPending(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notePendingDrop(reason)
	c.resetMatcher()
}

// OnPendingFired sets what runs when a pending match fires after the
// conflict delay, on a timer's goroutine.
func (c *Context) OnPendingFired(fn func(model.Match, int)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onPending = fn
}

// RecordExpansion notes an expansion, for undo by backspace.
func (c *Context) RecordExpansion(trigger, replacement string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.undo.now = c.Now
	c.undo.Record(trigger, replacement)
}

// Enabled reports whether expansion is on.
func (c *Context) Enabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled
}

// The helpers below run with the lock held.

func (c *Context) notePendingDrop(reason string) {
	if p := c.matcher.Pending(); p != nil {
		logx.Tracef("pending dropped (%s): %s", reason, p.Match.Trigger)
	}
}

func (c *Context) invalidate() {
	c.undo.Clear()
	c.buffer.Clear()
	c.resetMatcher()
}

func (c *Context) rebuildAfterBackspace() {
	if !c.buffer.PopLast() {
		return
	}
	c.resetMatcher()
	for _, r := range c.buffer.String() {
		c.matcher.Feed(r)
	}
	c.reschedule()
}

func (c *Context) stopTimer() {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.timerToken++
}

func (c *Context) resetMatcher() {
	c.matcher.Reset()
	c.stopTimer()
}

func (c *Context) reschedule() {
	c.stopTimer()
	if c.matcher.Pending() == nil {
		return
	}
	token := c.timerToken
	delay := time.Duration(max(50, min(5000, c.conflictDelay))) * time.Millisecond
	c.timer = time.AfterFunc(delay, func() { c.firePending(token) })
}

func (c *Context) firePending(token uint64) {
	c.mu.Lock()
	if token != c.timerToken || c.onPending == nil {
		c.mu.Unlock()
		return
	}
	p := c.matcher.FlushPending()
	if p == nil {
		c.mu.Unlock()
		return
	}
	c.timer = nil
	c.buffer.Clear()
	fn := c.onPending
	c.mu.Unlock()
	logx.Tracef("pending fired (timeout): %s len=%d", p.Match.Trigger, p.TriggerLength)
	fn(p.Match, p.TriggerLength)
}
