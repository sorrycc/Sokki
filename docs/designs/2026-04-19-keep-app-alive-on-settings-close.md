# Keep App Alive When Settings Window Closes

## 1. Background
Sokki is a macOS menu-bar app (`LSUIElement=true`). Today, clicking the red close button on the Settings window terminates the whole process, which is wrong for a menu-bar app — it should keep running in the status bar until the user explicitly picks Quit.

Root cause: `SokkiApp` declares a SwiftUI `Window` scene (`Sources/Sokki/SokkiApp.swift:9`). SwiftUI's default termination policy quits the app when the last window closes, and `AppDelegate` does not override `applicationShouldTerminateAfterLastWindowClosed`.

## 2. Requirements Summary
**Goal:** Closing the Settings window must not quit Sokki; the status-bar app keeps running until the user explicitly chooses Quit.

**Scope:**
- In scope: prevent app termination when the last window closes; preserve existing reopen + quit paths.
- Out of scope: close-button hide-vs-destroy semantics, persisting SettingsView UI state, adding new windows.

## 3. Acceptance Criteria
1. Open Settings via menu bar "Settings..." (or Cmd+,), click the red close button → process stays alive (status-bar icon remains, `pgrep Sokki` returns a PID).
2. After closing, click menu bar "Settings..." again → Settings window reappears with the same MatchStore data.
3. With Settings window open, click menu bar "Quit" → app terminates; `applicationWillTerminate` runs (keystroke monitor + FSEvents stopped).
4. With Settings window closed, click menu bar "Quit" → app terminates normally.
5. No regression to keystroke matching while Settings window is closed.

## 4. Problem Analysis
- **Approach A — SwiftUI-only workaround** (e.g. attach `.onDisappear` to hide a window via NSWindow reference): fragile, SwiftUI gives no first-class API for "don't quit on last close."
- **Approach B — NSWindowDelegate intercept `windowShouldClose` to hide**: works, but more code and changes close semantics unnecessarily.
- **Chosen: Approach C — `applicationShouldTerminateAfterLastWindowClosed → false` in AppDelegate**: one-line, idiomatic AppKit, exactly what LSUIElement apps want. SwiftUI's default close behavior is fine once termination is blocked.

## 5. Decision Log

**1. How to prevent app termination when last window closes?**
- Options: A) Override `applicationShouldTerminateAfterLastWindowClosed` · B) SwiftUI window-style hacks · C) `NSWindowDelegate.windowShouldClose` intercept
- Decision: **A)** — Minimal, idiomatic for LSUIElement apps. AppDelegate is already wired via `@NSApplicationDelegateAdaptor` (`Sources/Sokki/SokkiApp.swift:5`).

**2. Scope — blanket or settings-only?**
- Options: A) Blanket (all windows) · B) Settings window only
- Decision: **A)** — Only one `Window` scene exists today and menu-bar apps should never quit on last-window-close. YAGNI on per-window gating.

**3. Close-button semantics (hide vs destroy)?**
- Options: A) Leave SwiftUI default · B) Force-hide via NSWindowDelegate
- Decision: **A)** — Existing reopen path (`SokkiApp.swift:24-28`) iterates `NSApp.windows` by title and calls `makeKeyAndOrderFront`, which tolerates both hidden and recreated windows. No change needed.

**4. Quit path safety?**
- Decision: Unchanged. `StatusItemController.swift:131` calls `NSApplication.shared.terminate(nil)`. `applicationShouldTerminateAfterLastWindowClosed` only affects the auto-quit-on-last-close pathway; explicit `terminate(_:)` still flows through `applicationShouldTerminate` (default `.terminateNow`) and `applicationWillTerminate` cleanup (`AppDelegate.swift:80`).

## 6. Design
Add one method to `AppDelegate`:

```swift
nonisolated func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
    return false
}
```

Notes:
- `nonisolated` avoids a Swift 6 strict-concurrency mismatch against the `NSApplicationDelegate` protocol, which requires this method to be non-isolated. The body returns a constant and touches no actor state, so isolation is unnecessary.
- AppKit calls this delegate method for any scene type (`Window`, `WindowGroup`), so SwiftUI's `Window` scene is covered — no SwiftUI-specific shim needed.
- Reopen path (`SokkiApp.swift:24-28`) stays as-is: SwiftUI's singleton `Window` scene retains its NSWindow after close (unlike `WindowGroup`, which can tear windows down), so `NSApp.windows` iteration continues to find it by title. If empirical testing shows otherwise, fall back to a `@Environment(\.openWindow)` based ViewModifier — out of scope unless AC2 fails.
- Sparkle interaction: `AppDelegate.updater` can present its own update sheets. With the new policy, closing those while Settings is also closed correctly leaves the menu-bar app running; no new interaction to manage.

No other files change. The SwiftUI `Window` scene, the notification-driven reopen, the menu-bar Quit item, and the termination cleanup all keep working as-is.

## 7. Files Changed
- `Sources/Sokki/AppDelegate.swift` — add `applicationShouldTerminateAfterLastWindowClosed` returning `false`

## 8. Verification
1. [AC1] `swift run` (or launch packaged app). Open Settings via menu bar. Click red close. Confirm status-bar icon remains and `pgrep -l Sokki` still lists the process.
2. [AC2] Re-open Settings from menu bar. Confirm matches list renders and reflects current MatchStore state. Then close → reopen a second time to catch any SwiftUI scene-teardown regression.
3. [AC3] With Settings open, pick Quit from menu bar. Confirm process exits. Tail `~/.config/sokki/logs/sokki.log` to confirm clean shutdown (keystroke monitor stopped).
4. [AC4] With Settings closed, pick Quit from menu bar. Confirm clean exit.
5. [AC5] Close Settings, type a known abbreviation in another app, confirm expansion still happens.
