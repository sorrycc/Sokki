//go:build darwin

package macos

import (
	"errors"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"unicode/utf16"

	"github.com/ebitengine/purego"
)

// EventType is a CGEventType.
type EventType uint32

// The event types Sokki taps.
const (
	EventLeftMouseDown          EventType = 1
	EventRightMouseDown         EventType = 3
	EventKeyDown                EventType = 10
	EventKeyUp                  EventType = 11
	EventFlagsChanged           EventType = 12
	EventOtherMouseDown         EventType = 25
	EventTapDisabledByTimeout   EventType = 0xFFFFFFFE
	EventTapDisabledByUserInput EventType = 0xFFFFFFFF
)

// Flags are CGEventFlags.
type Flags uint64

// The modifier flags.
const (
	FlagShift     Flags = 0x00020000
	FlagControl   Flags = 0x00040000
	FlagAlternate Flags = 0x00080000
	FlagCommand   Flags = 0x00100000

	// ModifierMask is the four modifiers that shortcuts compare.
	ModifierMask = FlagShift | FlagControl | FlagAlternate | FlagCommand
)

// Has reports whether every flag of m is set.
func (f Flags) Has(m Flags) bool { return f&m == m }

// Virtual key codes Sokki reads or sends (ANSI layout).
const (
	KeyC         uint16 = 0x08
	KeyV         uint16 = 0x09
	KeySpace     uint16 = 0x31
	KeyBackspace uint16 = 0x33
	KeyEscape    uint16 = 0x35
	KeyHome      uint16 = 0x73
	KeyPageUp    uint16 = 0x74
	KeyEnd       uint16 = 0x77
	KeyPageDown  uint16 = 0x79
	KeyLeft      uint16 = 0x7B
	KeyRight     uint16 = 0x7C
	KeyDown      uint16 = 0x7D
	KeyUp        uint16 = 0x7E
)

const (
	tapSession          = 1 // kCGSessionEventTap
	tapHeadInsert       = 0 // kCGHeadInsertEventTap
	tapOptionDefault    = 0 // kCGEventTapOptionDefault
	tapOptionListenOnly = 1 // kCGEventTapOptionListenOnly

	fieldKeycode   = 9  // kCGKeyboardEventKeycode
	fieldTargetPID = 40 // kCGEventTargetUnixProcessID
)

// KeyEvent is a key press or a change of the modifiers, as the tap sees it.
type KeyEvent struct {
	Type    EventType
	KeyCode uint16
	Flags   Flags
	// Text is what the key types, as UTF-16 decoded, or "".
	Text string
}

// Tap is a running keyboard event tap, with a listen-only tap of mouse
// presses beside it, on a thread of its own.
type Tap struct {
	mu       sync.Mutex
	keys     uintptr // CFMachPortRef
	mouse    uintptr
	runLoop  uintptr
	stopped  bool
	onKey    func(ev KeyEvent) (pass bool)
	onMouse  func()
	onRearm  func(timeout bool)
	keysCB   uintptr
	mouseCB  uintptr
	pid      int64
	started  chan error
	finished chan struct{}
}

// TapOptions are the handlers of a tap. They run on the tap's thread and
// must return quickly: macOS disables a tap that keeps events waiting.
type TapOptions struct {
	// OnKey is called for every key down and modifier change; returning
	// false swallows the event, so the focused app never sees it.
	OnKey func(ev KeyEvent) (pass bool)
	// OnMouseDown is called for every mouse button pressed in another
	// app.
	OnMouseDown func()
	// OnRearm is called when macOS disabled the tap, which Tap enables
	// again at once.
	OnRearm func(timeout bool)
}

// ErrTapCreate is returned when macOS refuses the tap, which it does
// without the Accessibility permission.
var ErrTapCreate = errors.New("CGEventTapCreate failed (Accessibility permission missing?)")

// StartTap creates the taps on a new locked OS thread running a CFRunLoop
// and returns once they are installed.
func StartTap(o TapOptions) (*Tap, error) {
	if err := load(); err != nil {
		return nil, err
	}
	t := &Tap{
		onKey: o.OnKey, onMouse: o.OnMouseDown, onRearm: o.OnRearm,
		pid: int64(os.Getpid()), started: make(chan error, 1), finished: make(chan struct{}),
	}
	t.keysCB, t.mouseCB = callbacks()
	current.Store(t)
	go t.run()
	if err := <-t.started; err != nil {
		current.CompareAndSwap(t, nil)
		return nil, err
	}
	return t, nil
}

// Callbacks are a limited resource in purego (about 2000 for the life of
// the process), so the two the taps call are made once and dispatch to
// the tap that runs: one at a time.
var (
	cbOnce     sync.Once
	cbKeysPtr  uintptr
	cbMousePtr uintptr
	current    atomic.Pointer[Tap]
)

func callbacks() (keys, mouse uintptr) {
	cbOnce.Do(func() {
		cbKeysPtr = purego.NewCallback(func(_ uintptr, typ uint32, event uintptr, _ uintptr) uintptr {
			if t := current.Load(); t != nil {
				return t.handleKey(EventType(typ), event)
			}
			return event
		})
		cbMousePtr = purego.NewCallback(func(_ uintptr, typ uint32, event uintptr, _ uintptr) uintptr {
			if t := current.Load(); t != nil {
				return t.handleMouse(EventType(typ), event)
			}
			return event
		})
	})
	return cbKeysPtr, cbMousePtr
}

func (t *Tap) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(t.finished)

	keyMask := uint64(1)<<EventKeyDown | uint64(1)<<EventFlagsChanged
	keys := cgEventTapCreate(tapSession, tapHeadInsert, tapOptionDefault, keyMask, t.keysCB, 0)
	if keys == 0 {
		t.started <- ErrTapCreate
		return
	}
	mouseMask := uint64(1)<<EventLeftMouseDown | uint64(1)<<EventRightMouseDown | uint64(1)<<EventOtherMouseDown
	mouse := cgEventTapCreate(tapSession, tapHeadInsert, tapOptionListenOnly, mouseMask, t.mouseCB, 0)

	rl := cfRunLoopGetCurrent()
	keysSrc := cfMachPortCreateRLSource(0, keys, 0)
	cfRunLoopAddSource(rl, keysSrc, kCFRunLoopCommonModes)
	var mouseSrc uintptr
	if mouse != 0 {
		mouseSrc = cfMachPortCreateRLSource(0, mouse, 0)
		cfRunLoopAddSource(rl, mouseSrc, kCFRunLoopCommonModes)
	}
	cgEventTapEnable(keys, true)
	if mouse != 0 {
		cgEventTapEnable(mouse, true)
	}

	t.mu.Lock()
	t.keys, t.mouse, t.runLoop = keys, mouse, rl
	t.mu.Unlock()
	t.started <- nil

	cfRunLoopRun()

	cfMachPortInvalidate(keys)
	cfRelease(keysSrc)
	cfRelease(keys)
	if mouse != 0 {
		cfMachPortInvalidate(mouse)
		cfRelease(mouseSrc)
		cfRelease(mouse)
	}
}

// Stop disables the taps and ends their thread.
func (t *Tap) Stop() {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.stopped = true
	keys, mouse, rl := t.keys, t.mouse, t.runLoop
	t.mu.Unlock()
	if keys != 0 {
		cgEventTapEnable(keys, false)
	}
	if mouse != 0 {
		cgEventTapEnable(mouse, false)
	}
	if rl != 0 {
		cfRunLoopStop(rl)
	}
	<-t.finished
	current.CompareAndSwap(t, nil)
}

func (t *Tap) handleKey(typ EventType, event uintptr) uintptr {
	if typ == EventTapDisabledByTimeout || typ == EventTapDisabledByUserInput {
		t.mu.Lock()
		keys, stopped := t.keys, t.stopped
		t.mu.Unlock()
		if !stopped && keys != 0 {
			cgEventTapEnable(keys, true)
		}
		if t.onRearm != nil {
			t.onRearm(typ == EventTapDisabledByTimeout)
		}
		return event
	}
	if t.onKey == nil || (typ != EventKeyDown && typ != EventFlagsChanged) {
		return event
	}
	ev := KeyEvent{Type: typ, Flags: Flags(cgEventGetFlags(event))}
	if typ == EventKeyDown {
		ev.KeyCode = uint16(cgEventGetIntegerValueField(event, fieldKeycode))
		var buf [4]uint16
		var n uint64
		cgEventKeyboardGetUnicodeStr(event, uint64(len(buf)), &n, &buf[0])
		if n > 0 {
			ev.Text = string(utf16.Decode(buf[:min(n, uint64(len(buf)))]))
		}
	}
	if !t.onKey(ev) {
		return 0
	}
	return event
}

func (t *Tap) handleMouse(typ EventType, event uintptr) uintptr {
	if typ == EventTapDisabledByTimeout || typ == EventTapDisabledByUserInput {
		t.mu.Lock()
		mouse, stopped := t.mouse, t.stopped
		t.mu.Unlock()
		if !stopped && mouse != 0 {
			cgEventTapEnable(mouse, true)
		}
		return event
	}
	// Clicks in Sokki's own windows (the HUD, the palette) are not the
	// user moving the caret in another app.
	if cgEventGetIntegerValueField(event, fieldTargetPID) == t.pid {
		return event
	}
	if t.onMouse != nil {
		t.onMouse()
	}
	return event
}
