# Quit & Reopen after Accessibility grant

## 1. Background

Sokki depends on a `CGEvent` tap (`KeystrokeMonitor.runEventTap`) that is created
exactly once, at launch, on a dedicated tap thread. If Accessibility permission
isn't trusted at launch, `CGEvent.tapCreate` returns `nil`, the thread logs the
failure, and the app silently keeps running but cannot expand anything.

Today's UX: user sees "nothing happens", opens System Settings, grants
permission — and then discovers Sokki still doesn't work, because the tap was
never created. The only fix is a manual Quit + relaunch.

This doc covers a small UX improvement: detect the grant, prompt the user with
a native **Quit & Reopen** alert, and also add a permanent **Quit & Reopen**
status-menu item.

## 2. Requirements Summary

**Goal:** Remove the manual-restart friction after the user grants Accessibility.

**Scope**
- In: permission polling, modal alert on grant, relaunch helper, menu bar entry.
- Out: hot-restarting the event tap in-process; Screen Recording / other
  permissions; first-run onboarding UI.

**Key Decisions (from dual-question discovery)**
1. Trigger: poll only when trust check fails at launch.
2. UI: `NSAlert` modal with `[Quit & Reopen]` (default) / `[Later]`.
3. Menu item: always show `Quit & Reopen` in the status menu.

## 3. Acceptance Criteria

1. If `AXIsProcessTrusted` is `true` at launch → no polling, no alert, no
   behavior change.
2. If not trusted at launch → background polling detects the grant.
3. On grant → a native `NSAlert` appears with a clear "Sokki needs to restart"
   message and two buttons: **Quit & Reopen** (default) and **Later**. The
   alert reliably becomes key/frontmost even though Sokki is `LSUIElement` /
   `.accessory`.
4. **Quit & Reopen** launches a fresh `Sokki.app` instance and terminates the
   current one. After the new instance boots, Accessibility-backed features
   work without further action. The hand-off minimizes overlap between the two
   menu-bar icons via a short delay before termination, though perfect
   synchronization is best-effort (cold-launch time is variable).
5. **Later** dismisses the alert and stops polling for the remainder of this
   launch. The user can still use the menu item.
6. The status menu always has a **Quit & Reopen** item (above the existing
   **Quit** entry) that performs the same relaunch, regardless of permission
   state. This is also the escape hatch for cases the polling path can't
   detect: permission revoked while running, stale TCC entry, or other causes
   of tap-creation failure with `trusted == true`.
7. Dev builds (`swift run`, no `.app` bundle) degrade gracefully: menu-item
   click logs a single warning and does NOT terminate the running process; no
   crash. (Terminating a dev build on a misdirected click would be a foot-gun.)
8. Deny-then-grant path: if the user clicks "Deny" on the initial system
   prompt and later enables permission via System Settings, the alert still
   appears within ~2s of the grant without requiring a manual relaunch.

## 4. Problem Analysis

Approaches evaluated:

- **A — Hot-restart the tap in-process**: when permission flips, cancel the
  run loop, tear down the tap thread, and recreate. Rejected — `CFRunLoopRun`
  is a blocking call on a bespoke thread with retained C callbacks and
  `TapHandler` ownership; unwinding it safely is a nontrivial concurrency
  change for marginal UX benefit over a relaunch.
- **B — Always show a permission onboarding window at launch**: heavier surface
  area (a new SwiftUI view, copy, state machine); out of scope for this small
  feature and conflicts with the status-bar-only ethos.
- **C — Poll + alert + relaunch (chosen)**: small, localised change. Polling is
  cheap (`AXIsProcessTrusted` is a fast syscall). Relaunch is a well-worn
  pattern (`/usr/bin/open -n -a <bundle>` then `terminate`).

## 5. Decision Log

**1. Where does polling logic live?**
- Options: A) Inline in `AppDelegate` · B) New `Services/PermissionMonitor.swift`
- Decision: **B)** — matches the existing `Bootstrapper` / `UpdaterService`
  extraction pattern; keeps `AppDelegate` thin.

**2. Polling mechanism?**
- Options: A) Repeating `Timer` · B) `DispatchSourceTimer` · C) Workspace
  activation notification
- Decision: **A) Repeating `Timer` at 2s interval on main/`@MainActor`** —
  simplest, matches the debounce Timer already in `AppDelegate`. `AXIsProcessTrusted`
  is cheap.

**3. Relaunch mechanism?**
- Options: A) `Process` → `/usr/bin/open -n -a <bundlePath>` · B)
  `NSWorkspace.openApplication(at:configuration:)` async API · C) fork/exec
  shell trick
- Decision: **A)** — synchronous, API-stable, one-liner. Gated on
  `bundleURL.pathExtension == "app"` so dev builds fall through.

**4. Where does the relaunch helper live?**
- Options: A) New `Services/Relauncher.swift` · B) Private static on
  `AppDelegate`
- Decision: **B)** — ~15 lines, single caller site pattern; YAGNI says no
  new file.

**5. How to stop polling after the alert fires?**
- Options: A) One-shot: invalidate timer on first trusted=true · B) Separate
  "don't ask again" preference
- Decision: **A)** — the alert fires exactly once per launch. Whether the
  user picks Quit & Reopen or Later, polling stops. No persisted state.

**6. Menu item placement?**
- Options: A) Directly above **Quit** · B) Elsewhere
- Decision: **A)** — restart-adjacent affordance belongs next to Quit.

**7. Alert copy?**
- Decision: Title "Accessibility granted — restart to start working", body
  "Sokki needs to restart before it can read keystrokes. Relaunch now?",
  default button "Quit & Reopen", alternate "Later".

## 6. Design

### PermissionMonitor (`Sources/Sokki/Services/PermissionMonitor.swift`)

```swift
@MainActor
final class PermissionMonitor {
    var onGranted: (() -> Void)?
    private var timer: Timer?

    func startIfNeeded() {
        guard !AXIsProcessTrusted() else { return }
        Logger.shared.warning("Accessibility not granted — polling for grant")
        // Timer.scheduledTimer takes a @Sendable closure under Swift 6.
        // Capture self INSIDE MainActor.assumeIsolated — capturing a
        // MainActor-isolated self in the outer @Sendable closure is a
        // strict-concurrency diagnostic. Mirrors `AppDelegate.debouncedReload`.
        timer = Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self else { return }
                if AXIsProcessTrusted() {
                    self.stop()
                    self.onGranted?()
                }
            }
        }
    }

    func stop() {
        timer?.invalidate()
        timer = nil
    }
}
```

Note: we intentionally do **not** pass the `AXTrustedCheckOptionPrompt: true`
option during polling — that would pop the system prompt every tick. The
initial prompt is still triggered by the existing `checkAccessibilityPermission`
call on launch.

### AppDelegate wiring

- Add `private let permissionMonitor = PermissionMonitor()`.
- In `applicationDidFinishLaunching`, after the existing
  `checkAccessibilityPermission()` call, set
  `permissionMonitor.onGranted = { [weak self] in self?.showAccessibilityRelaunchAlert() }`
  (explicit `[weak self]` to match the discipline used elsewhere in
  `AppDelegate`) and call `permissionMonitor.startIfNeeded()`.
- No explicit teardown needed: `Timer.scheduledTimer` is retained by the main
  runloop, not `AppDelegate`, but the closure uses `[weak self]` so there's no
  retain cycle, and `AppDelegate` is process-lifetime anyway. (Don't copy
  this "no teardown" stance to a non-process-lifetime owner.)
- New method `showAccessibilityRelaunchAlert()`: temporarily flips to
  `.regular` and activates so the `NSAlert` becomes key/frontmost on macOS 14+
  (this is the lesson `SokkiApp.swift:22-25` encoded — bare `NSApp.activate`
  is not sufficient under the tightened Tahoe activation rules). Uses a
  **save-and-restore** pattern rather than acquiring `activationCoordinator`,
  because `SokkiApp.init`'s Settings-open path already flips `.regular`
  directly without a lease — reusing the coordinator here would
  double-book-keep and could hide an open Settings window when `release()`
  drops lease back to 0. The self-contained pattern:

  ```swift
  let prior = NSApp.activationPolicy()
  if prior == .accessory {
      NSApp.setActivationPolicy(.regular)
  }
  NSApp.activate(ignoringOtherApps: true)

  let response = alert.runModal()

  if prior == .accessory {
      NSApp.setActivationPolicy(.accessory)
  }
  if response == .alertFirstButtonReturn {
      relaunchApp()
  }
  ```

  It ignores the coordinator's state and only touches policy when the app was
  `.accessory` before the alert — so an open Settings window (policy already
  `.regular`) is left alone, and the coordinator's lease count is unaffected.

- New `relaunchApp()`: guarded by a `private var isRelaunching = false` flag
  on `AppDelegate` (re-entrancy guard so a quick second click on the menu
  item doesn't spawn a second `/usr/bin/open` or stack two `terminate` calls).
  Resolves `Bundle.main.bundleURL`. If the path extension is not `"app"`
  (dev `swift run`), log a warning and return WITHOUT terminating. Otherwise:

  ```swift
  guard !isRelaunching else { return }
  isRelaunching = true

  let proc = Process()
  proc.launchPath = "/usr/bin/open"
  proc.arguments = ["-n", "-a", bundleURL.path]
  do {
      try proc.run()
      proc.waitUntilExit()   // `open` returns quickly once the launch is initiated
  } catch {
      Logger.shared.error("Quit & Reopen: failed to spawn open: \(error)")
      isRelaunching = false
      return
  }
  guard proc.terminationStatus == 0 else {
      Logger.shared.error("Quit & Reopen: open exited \(proc.terminationStatus) — aborting terminate")
      isRelaunching = false
      return
  }

  DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) {
      NSApp.terminate(nil)
  }
  ```

  Checking `/usr/bin/open`'s exit status before scheduling termination catches
  silent launch failures (bad bundle path, quarantine, etc) so we don't leave
  the user with zero running instances. `waitUntilExit()` is safe on main —
  `open` exits in a few ms once the spawn is initiated; it doesn't block on
  the launched app's lifecycle. `NSApp.terminate(nil)` (NOT `exit(0)`) so
  `applicationWillTerminate` still fires, stopping the tap + FSEvents cleanly.
  The 250ms is a heuristic — cold-launch time varies, so AC4 is best-effort,
  not a guarantee.

### StatusItemController

- Add `var onRelaunch: (() -> Void)?`.
- In `menuNeedsUpdate`, insert a `Quit & Reopen` item immediately above the
  `Quit` item, wired to `onRelaunch`. No `keyEquivalent` (⌘Q stays bound to
  plain Quit).
- `AppDelegate.setupStatusItemActions()` assigns `onRelaunch = { [weak self] in self?.relaunchApp() }`.

### Error handling

- Polling continues until permission flips `true`; we don't bound the duration.
  When the user revokes permission again later (rare), we don't react — the
  next launch will catch it.
- `NSAlert` "Later" path simply returns; polling stays stopped.
- If `Process.run()` throws (should not happen on well-formed paths), log at
  `.error` and don't terminate — user retains the menu-item option.

### Concurrency

All new code runs on `@MainActor`. Timer closure uses
`MainActor.assumeIsolated` (mirroring the existing `debouncedReload` pattern
in `AppDelegate`).

## 7. Files Changed

- `Sources/Sokki/Services/PermissionMonitor.swift` — new; ~30 lines.
- `Sources/Sokki/AppDelegate.swift` — wire monitor, add alert + relaunch helper,
  hand `onRelaunch` to the status controller.
- `Sources/Sokki/StatusItemController.swift` — new `onRelaunch` callback,
  "Quit & Reopen" menu item.

## 8. Verification

Manual checks against Acceptance Criteria (no test target exists — CLAUDE.md
forbids inventing one):

1. **[AC1]** `swift build -c release && ./Scripts/package_app.sh` → open
   `Sokki.app` with Accessibility already granted → no alert, no log spam.
2. **[AC2,3,8]** Revoke Sokki in System Settings → Privacy & Security →
   Accessibility. Relaunch bundle. System prompt appears → click Deny. Later,
   toggle Sokki on in System Settings. Within ~2s, the NSAlert appears and is
   visibly frontmost over whatever app was focused.
3. **[AC4]** Click **Quit & Reopen**: original process exits, new process
   starts, typing a trigger expands (confirming tap works without manual
   restart). No double menu-bar icon visible during the swap.
4. **[AC5]** Repeat step 2; click **Later**: alert dismisses, polling stops
   (verify via log: no further "granted" entries), menu item remains.
5. **[AC6]** Click **Quit & Reopen** from menu at any time → clean relaunch.
   Revoke permission while running → expansions stop silently → clicking
   Quit & Reopen from the menu triggers the alert on the next launch (or just
   relaunches directly if already re-granted).
6. **[AC7]** `swift run`: click the menu item → single
   "Quit & Reopen: not an .app bundle" warning in log, process **remains
   running**; no crash.
