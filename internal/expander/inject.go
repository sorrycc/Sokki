//go:build darwin

package expander

import (
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"sokki/internal/macos"
	"sokki/internal/model"
)

// Injector types text into the focused app with synthesized key events,
// or pastes it when it is long. One injection runs at a time.
type Injector struct {
	mu  sync.Mutex
	cfg func() model.Config
}

func sleepMs(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

// Inject erases the trigger and types the replacement.
func (in *Injector) Inject(replacement string, triggerLength int) {
	in.mu.Lock()
	defer in.mu.Unlock()
	cfg := in.cfg()
	in.erase(triggerLength, cfg)
	if utf8.RuneCountInString(replacement) <= cfg.ClipboardThreshold {
		for _, r := range replacement {
			macos.PostText(string(r))
			if cfg.InjectDelay > 0 {
				sleepMs(cfg.InjectDelay)
			}
		}
		return
	}
	in.paste(replacement, cfg)
}

// Erase posts count backspaces.
func (in *Injector) Erase(count int) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.erase(count, in.cfg())
}

func (in *Injector) erase(count int, cfg model.Config) {
	if count <= 0 {
		return
	}
	for range count {
		macos.TapKey(macos.KeyBackspace, 0)
		if cfg.InjectDelay > 0 {
			sleepMs(cfg.InjectDelay)
		}
	}
	// Let the app process the deletions.
	sleepMs(10)
}

// Type types text as key events of at most 200 UTF-16 units, without
// touching what is before the caret: a trigger restored by undo. It pauses between chunks, which apps that
// throttle input (Electron) need.
func (in *Injector) Type(text string) {
	if text == "" {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	gap := 1500 * time.Microsecond
	if d := in.cfg().InjectDelay; d > 0 {
		gap = time.Duration(d) * time.Millisecond
	}
	chunks := chunk(text, 200)
	for i, c := range chunks {
		macos.PostText(c)
		if i < len(chunks)-1 {
			time.Sleep(gap)
		}
	}
}

// chunk splits s into pieces of at most n UTF-16 units, not splitting a
// character.
func chunk(s string, n int) []string {
	var out []string
	start, units := 0, 0
	for i, r := range s {
		w := len(utf16.Encode([]rune{r}))
		if units+w > n && units > 0 {
			out = append(out, s[start:i])
			start, units = i, 0
		}
		units += w
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// paste puts text on the pasteboard, presses Cmd-V, and puts back what
// the pasteboard held, unless something else wrote to it meanwhile.
func (in *Injector) paste(text string, cfg model.Config) {
	var snap macos.PasteboardSnapshot
	if cfg.PreserveClipboard {
		snap = macos.SavePasteboard()
	}
	macos.SetPasteboardString(text)
	macos.TapKey(macos.KeyV, macos.FlagCommand)
	if cfg.PreserveClipboard {
		time.AfterFunc(time.Duration(cfg.RestoreClipboardDelay)*time.Millisecond, func() {
			RestorePasteboardIfOurs(snap)
		})
	}
}

// RestorePasteboardIfOurs puts a snapshot back, unless the pasteboard
// changed more than once since: Sokki's own write is the one change it
// expects, anything more is the user copying.
func RestorePasteboardIfOurs(snap macos.PasteboardSnapshot) {
	if macos.PasteboardChangeCount() != snap.ChangeCount+1 {
		return
	}
	macos.RestorePasteboard(snap)
}
