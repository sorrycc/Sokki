//go:build darwin

package macos

import (
	"github.com/ebitengine/purego/objc"
)

// ActivateApp brings the app with process ID pid to the front.
func ActivateApp(pid int) {
	if pid <= 0 {
		return
	}
	withPool(func() {
		app := class("NSRunningApplication").Send(sel("runningApplicationWithProcessIdentifier:"), int32(pid))
		if app != 0 {
			objc.Send[bool](app, sel("activateWithOptions:"), uint(0))
		}
	})
}

// FrontmostApp returns the bundle identifier and name of the app in front,
// "" for both when there is none.
func FrontmostApp() (bundleID, name string) {
	withPool(func() {
		app := class("NSWorkspace").Send(sel("sharedWorkspace")).Send(sel("frontmostApplication"))
		if app != 0 {
			bundleID = goString(app.Send(sel("bundleIdentifier")))
			name = goString(app.Send(sel("localizedName")))
		}
	})
	return bundleID, name
}

// observers keeps the blocks of the notifications observed, which live as
// long as the app.
var observers []objc.Block

// OnAppActivated calls fn whenever another app becomes active, and
// OnAppDeactivated whenever the active app resigns, as with Cmd-Tab. fn
// runs on the thread that posted the notification, the main thread.
func OnAppActivated(fn func()) { observe("NSWorkspaceDidActivateApplicationNotification", fn) }

// OnAppDeactivated: see OnAppActivated.
func OnAppDeactivated(fn func()) { observe("NSWorkspaceDidDeactivateApplicationNotification", fn) }

func observe(name string, fn func()) {
	withPool(func() {
		ws := class("NSWorkspace").Send(sel("sharedWorkspace"))
		center := ws.Send(sel("notificationCenter"))
		block := objc.NewBlock(func(_ objc.Block, _ objc.ID) { fn() })
		observers = append(observers, block)
		center.Send(sel("addObserverForName:object:queue:usingBlock:"), nsString(name), objc.ID(0), objc.ID(0), block)
	})
}
