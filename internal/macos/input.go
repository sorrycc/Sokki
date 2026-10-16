//go:build darwin

package macos

import (
	"unicode/utf16"
	"unsafe"
)

const (
	sourceHIDSystemState = 1 // kCGEventSourceStateHIDSystemState
	tapHID               = 0 // kCGHIDEventTap
)

// PostKey posts a press or a release of the key with code, with flags.
func PostKey(code uint16, down bool, flags Flags) {
	if load() != nil {
		return
	}
	src := cgEventSourceCreate(sourceHIDSystemState)
	ev := cgEventCreateKeyboardEvent(src, code, down)
	if ev != 0 {
		if flags != 0 {
			cgEventSetFlags(ev, uint64(flags))
		}
		cgEventPost(tapHID, ev)
		cfRelease(ev)
	}
	if src != 0 {
		cfRelease(src)
	}
}

// TapKey presses and releases the key with code, with flags.
func TapKey(code uint16, flags Flags) {
	PostKey(code, true, flags)
	PostKey(code, false, flags)
}

// PostText types s as one key event carrying the text, at most 200 UTF-16
// units long, and a bare release. The release carries no text, since some
// apps (Electron, rich text fields) insert the text of releases too.
func PostText(s string) {
	if s == "" || load() != nil {
		return
	}
	units := utf16.Encode([]rune(s))
	src := cgEventSourceCreate(sourceHIDSystemState)
	down := cgEventCreateKeyboardEvent(src, 0, true)
	if down != 0 {
		cgEventKeyboardSetUnicodeStr(down, uint64(len(units)), (*uint16)(unsafe.Pointer(&units[0])))
		cgEventPost(tapHID, down)
		cfRelease(down)
	}
	up := cgEventCreateKeyboardEvent(src, 0, false)
	if up != 0 {
		cgEventPost(tapHID, up)
		cfRelease(up)
	}
	if src != 0 {
		cfRelease(src)
	}
}

// IsTrusted reports whether the app has the Accessibility permission.
func IsTrusted() bool {
	if load() != nil {
		return false
	}
	return axIsProcessTrusted()
}

// RequestTrust reports whether the app has the Accessibility permission,
// and when it does not, has macOS ask the user to grant it.
func RequestTrust() bool {
	if load() != nil {
		return false
	}
	if kAXTrustedCheckOptionPrompt == 0 || kCFBooleanTrue == 0 {
		return axIsProcessTrusted()
	}
	keys := [1]uintptr{kAXTrustedCheckOptionPrompt}
	values := [1]uintptr{kCFBooleanTrue}
	dict := cfDictionaryCreate(0, unsafe.Pointer(&keys[0]), unsafe.Pointer(&values[0]), 1,
		kCFTypeDictionaryKeyCBs, kCFTypeDictionaryValueCBs)
	if dict == 0 {
		return axIsProcessTrusted()
	}
	defer cfRelease(dict)
	return axIsProcessTrustedWithOptions(dict)
}

// IsSecureInput reports whether an app turned secure keyboard entry on,
// as password fields do, during which Sokki must not read keys.
func IsSecureInput() bool {
	if load() != nil {
		return false
	}
	return isSecureEventInputEnabled()
}
