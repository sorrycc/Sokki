//go:build darwin

package macos

import (
	"os"

	"github.com/ebitengine/purego/objc"
)

// axTimeout is how long, in seconds, an app has to answer an
// Accessibility query before SelectedText gives up on it.
const axTimeout = 0.3

// SelectedText returns the text selected in the app in front, and the
// app's name, as Accessibility reports them. It returns "" when nothing
// is selected, when the app does not say (some Electron apps don't), when
// Sokki is in front, and while a password field has secure input on.
// What it reads is never logged.
func SelectedText() (text, appName string) {
	if load() != nil || isSecureEventInputEnabled() || !axIsProcessTrusted() {
		return "", ""
	}
	var pid int32
	withPool(func() {
		app := class("NSWorkspace").Send(sel("sharedWorkspace")).Send(sel("frontmostApplication"))
		if app != 0 {
			pid = objc.Send[int32](app, sel("processIdentifier"))
			appName = goString(app.Send(sel("localizedName")))
		}
	})
	if pid <= 0 || int(pid) == os.Getpid() {
		return "", ""
	}
	el := axUIElementCreateApplication(pid)
	if el == 0 {
		return "", ""
	}
	defer cfRelease(el)
	axUIElementSetTimeout(el, axTimeout)
	focused := axAttribute(el, "AXFocusedUIElement")
	if focused == 0 {
		return "", ""
	}
	defer cfRelease(focused)
	value := axAttribute(focused, "AXSelectedText")
	if value == 0 {
		return "", ""
	}
	defer cfRelease(value)
	if cfGetTypeID(value) != cfStringGetTypeID() {
		return "", ""
	}
	withPool(func() { text = goString(objc.ID(value)) })
	return text, appName
}

// axAttribute returns the value of an Accessibility attribute of el,
// which the caller releases, or 0.
func axAttribute(el uintptr, name string) uintptr {
	var value uintptr
	var err int32
	withPool(func() {
		err = axUIElementCopyAttributeValue(el, uintptr(nsString(name)), &value)
	})
	if err != 0 {
		return 0
	}
	return value
}
