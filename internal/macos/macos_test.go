//go:build darwin

package macos

import (
	"testing"
	"time"
)

func TestPasteboardRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("touches the user's pasteboard")
	}
	snap := SavePasteboard()
	defer RestorePasteboard(snap)
	before := PasteboardChangeCount()
	SetPasteboardString("sokki ✓ test")
	if PasteboardChangeCount() == before {
		t.Fatal("change count did not move")
	}
	if s, ok := PasteboardString(); !ok || s != "sokki ✓ test" {
		t.Fatalf("got %q %v", s, ok)
	}
}

func TestSystemQueries(t *testing.T) {
	_ = IsTrusted()
	_ = IsSecureInput()
	_, _ = FrontmostApp()
	_, _ = SelectedText()
}

func TestTapStartStop(t *testing.T) {
	tap, err := StartTap(TapOptions{OnKey: func(KeyEvent) bool { return true }})
	if err == ErrTapCreate {
		t.Skip("no Accessibility permission here")
	}
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	tap.Stop()
}

func TestTapSeesPostedModifier(t *testing.T) {
	got := make(chan KeyEvent, 8)
	tap, err := StartTap(TapOptions{OnKey: func(ev KeyEvent) bool {
		if ev.Type == EventFlagsChanged {
			select {
			case got <- ev:
			default:
			}
		}
		return true
	}})
	if err == ErrTapCreate {
		t.Skip("no Accessibility permission here")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer tap.Stop()
	start := time.Now()
	PostKey(0x38, true, FlagShift) // left Shift: changes no text anywhere
	PostKey(0x38, false, 0)
	select {
	case ev := <-got:
		t.Logf("flags %#x after %v", ev.Flags, time.Since(start))
	case <-time.After(2 * time.Second):
		t.Skip("tap saw no posted events (the tap is not trusted here)")
	}
}
