//go:build darwin

// Package macos binds the parts of macOS that MyGo leaves out and Sokki
// needs: a keyboard event tap, synthesized key events, the Accessibility
// permission, secure input, the frontmost app, its selected text and the
// pasteboard. It calls the system frameworks with purego, without cgo, as
// MyGo does.
package macos

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	coreFoundationPath = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	appServicesPath    = "/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"
	carbonPath         = "/System/Library/Frameworks/Carbon.framework/Carbon"
	appKitPath         = "/System/Library/Frameworks/AppKit.framework/AppKit"
)

var (
	loadOnce sync.Once
	loadErr  error

	// CoreFoundation
	cfRelease                 func(ref uintptr)
	cfRunLoopGetCurrent       func() uintptr
	cfRunLoopRun              func()
	cfRunLoopStop             func(rl uintptr)
	cfRunLoopAddSource        func(rl, source, mode uintptr)
	cfMachPortCreateRLSource  func(alloc, port uintptr, order int) uintptr
	cfMachPortInvalidate      func(port uintptr)
	cfDictionaryCreate        func(alloc uintptr, keys, values unsafe.Pointer, n int, keyCB, valueCB uintptr) uintptr
	cfGetTypeID               func(ref uintptr) uintptr
	cfStringGetTypeID         func() uintptr
	kCFRunLoopCommonModes     uintptr
	kCFBooleanTrue            uintptr
	kCFTypeDictionaryKeyCBs   uintptr
	kCFTypeDictionaryValueCBs uintptr

	// CoreGraphics (through ApplicationServices)
	cgEventTapCreate              func(tap, place, options uint32, mask uint64, callback, refcon uintptr) uintptr
	cgEventTapEnable              func(port uintptr, enable bool)
	cgEventGetIntegerValueField   func(event uintptr, field uint32) int64
	cgEventGetFlags               func(event uintptr) uint64
	cgEventSetFlags               func(event uintptr, flags uint64)
	cgEventKeyboardGetUnicodeStr  func(event uintptr, max uint64, actual *uint64, buf *uint16)
	cgEventKeyboardSetUnicodeStr  func(event uintptr, n uint64, buf *uint16)
	cgEventSourceCreate           func(state int32) uintptr
	cgEventCreateKeyboardEvent    func(source uintptr, key uint16, down bool) uintptr
	cgEventPost                   func(tap uint32, event uintptr)
	axIsProcessTrusted            func() bool
	axIsProcessTrustedWithOptions func(options uintptr) bool
	kAXTrustedCheckOptionPrompt   uintptr
	axUIElementCreateApplication  func(pid int32) uintptr
	axUIElementCopyAttributeValue func(element, attribute uintptr, value *uintptr) int32
	axUIElementSetTimeout         func(element uintptr, seconds float32) int32

	// Carbon
	isSecureEventInputEnabled func() bool

	// Objective-C runtime
	autoreleasePoolPush func() uintptr
	autoreleasePoolPop  func(pool uintptr)
)

// load opens the frameworks and binds their functions, once.
func load() error {
	loadOnce.Do(func() {
		cf, err := purego.Dlopen(coreFoundationPath, purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			loadErr = err
			return
		}
		as, err := purego.Dlopen(appServicesPath, purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			loadErr = err
			return
		}
		carbon, err := purego.Dlopen(carbonPath, purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			loadErr = err
			return
		}
		if _, err := purego.Dlopen(appKitPath, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			loadErr = err
			return
		}
		lobjc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			loadErr = err
			return
		}

		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&cfRunLoopGetCurrent, cf, "CFRunLoopGetCurrent")
		purego.RegisterLibFunc(&cfRunLoopRun, cf, "CFRunLoopRun")
		purego.RegisterLibFunc(&cfRunLoopStop, cf, "CFRunLoopStop")
		purego.RegisterLibFunc(&cfRunLoopAddSource, cf, "CFRunLoopAddSource")
		purego.RegisterLibFunc(&cfMachPortCreateRLSource, cf, "CFMachPortCreateRunLoopSource")
		purego.RegisterLibFunc(&cfMachPortInvalidate, cf, "CFMachPortInvalidate")
		purego.RegisterLibFunc(&cfDictionaryCreate, cf, "CFDictionaryCreate")
		purego.RegisterLibFunc(&cfGetTypeID, cf, "CFGetTypeID")
		purego.RegisterLibFunc(&cfStringGetTypeID, cf, "CFStringGetTypeID")
		kCFRunLoopCommonModes = deref(cf, "kCFRunLoopCommonModes")
		kCFBooleanTrue = deref(cf, "kCFBooleanTrue")
		kCFTypeDictionaryKeyCBs = sym(cf, "kCFTypeDictionaryKeyCallBacks")
		kCFTypeDictionaryValueCBs = sym(cf, "kCFTypeDictionaryValueCallBacks")

		purego.RegisterLibFunc(&cgEventTapCreate, as, "CGEventTapCreate")
		purego.RegisterLibFunc(&cgEventTapEnable, as, "CGEventTapEnable")
		purego.RegisterLibFunc(&cgEventGetIntegerValueField, as, "CGEventGetIntegerValueField")
		purego.RegisterLibFunc(&cgEventGetFlags, as, "CGEventGetFlags")
		purego.RegisterLibFunc(&cgEventSetFlags, as, "CGEventSetFlags")
		purego.RegisterLibFunc(&cgEventKeyboardGetUnicodeStr, as, "CGEventKeyboardGetUnicodeString")
		purego.RegisterLibFunc(&cgEventKeyboardSetUnicodeStr, as, "CGEventKeyboardSetUnicodeString")
		purego.RegisterLibFunc(&cgEventSourceCreate, as, "CGEventSourceCreate")
		purego.RegisterLibFunc(&cgEventCreateKeyboardEvent, as, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&cgEventPost, as, "CGEventPost")
		purego.RegisterLibFunc(&axIsProcessTrusted, as, "AXIsProcessTrusted")
		purego.RegisterLibFunc(&axIsProcessTrustedWithOptions, as, "AXIsProcessTrustedWithOptions")
		kAXTrustedCheckOptionPrompt = deref(as, "kAXTrustedCheckOptionPrompt")
		purego.RegisterLibFunc(&axUIElementCreateApplication, as, "AXUIElementCreateApplication")
		purego.RegisterLibFunc(&axUIElementCopyAttributeValue, as, "AXUIElementCopyAttributeValue")
		purego.RegisterLibFunc(&axUIElementSetTimeout, as, "AXUIElementSetMessagingTimeout")

		purego.RegisterLibFunc(&isSecureEventInputEnabled, carbon, "IsSecureEventInputEnabled")

		purego.RegisterLibFunc(&autoreleasePoolPush, lobjc, "objc_autoreleasePoolPush")
		purego.RegisterLibFunc(&autoreleasePoolPop, lobjc, "objc_autoreleasePoolPop")
	})
	return loadErr
}

// sym returns the address of a symbol, or 0.
func sym(lib uintptr, name string) uintptr {
	p, err := purego.Dlsym(lib, name)
	if err != nil {
		return 0
	}
	return p
}

// deref returns the value of a global pointer variable, such as a
// CFStringRef constant, or 0.
func deref(lib uintptr, name string) uintptr {
	p := sym(lib, name)
	if p == 0 {
		return 0
	}
	return **(**uintptr)(unsafe.Pointer(&p))
}

// withPool runs fn inside an autorelease pool, which Objective-C calls made
// off the main thread's run loop need so that their objects are freed.
func withPool(fn func()) {
	if load() != nil {
		return
	}
	pool := autoreleasePoolPush()
	defer autoreleasePoolPop(pool)
	fn()
}

var (
	selMu   sync.Mutex
	selByID = map[string]objc.SEL{}
)

// sel returns the selector named name, registered once.
func sel(name string) objc.SEL {
	selMu.Lock()
	defer selMu.Unlock()
	s, ok := selByID[name]
	if !ok {
		s = objc.RegisterName(name)
		selByID[name] = s
	}
	return s
}

// class returns the Objective-C class named name as an ID to send to.
func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

// nsString makes an autoreleased NSString of s.
func nsString(s string) objc.ID {
	return class("NSString").Send(sel("stringWithUTF8String:"), s)
}

// goString copies an NSString into Go; nil is "".
func goString(s objc.ID) string {
	if s == 0 {
		return ""
	}
	return objc.Send[string](s, sel("UTF8String"))
}
