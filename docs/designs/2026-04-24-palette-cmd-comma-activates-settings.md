# Palette Cmd+, Should Activate the Settings Window

## 1. Background
Pressing `⌘,` while the palette is open is supposed to open the Settings window.
Today it does open it, but the Settings window comes up behind whatever app the
user was in before they invoked the palette — not as the frontmost, focused
window. The user has to Cmd+Tab to find it.

## 2. Requirements Summary
**Goal:** Pressing `⌘,` in the palette opens Settings and makes it the active,
frontmost, key window.

**Scope:**
- In scope: the Cmd+, code path inside the palette
- Out of scope: status-bar Settings path (already works), Settings window
  content, the activation-policy coordinator's broader design

## 3. Acceptance Criteria
1. Pressing `⌘,` while the palette is open dismisses the palette AND the
   Settings window becomes the frontmost, key, focused window.
2. The user's previous app does NOT receive focus (even momentarily) during
   the transition.
3. The existing status-bar "Open Settings" path continues to work unchanged.

## 4. Problem Analysis
Current state — `PaletteView.swift:52-55`:
```swift
Button("") {
    onClose()
    NotificationCenter.default.post(name: .openSettings, object: nil)
}
.keyboardShortcut(",", modifiers: .command)
```

`onClose` is wired to `PaletteController.close()` which defaults
`reactivateTarget: true`, so it does:
1. `activation.release()` → policy back to `.accessory`
2. `targetApp.activate(options: [])` → hands focus to the user's previous app

Then the `.openSettings` observer (`SokkiApp.swift:21-43`) flips policy back to
`.regular` and calls `NSApp.activate(ignoringOtherApps: true)` +
`makeKeyAndOrderFront` + `orderFrontRegardless`. macOS Tahoe gates this second
activation because step 2 just handed activation to another app — so Settings
opens but stays behind.

Approaches evaluated:
- **A) Reorder calls in PaletteView (post first, close second)** — doesn't help;
  `close()` will still call `targetApp.activate` and steal focus right back.
- **B) Defer the observer's activate via DispatchQueue.main.async / re-call
  `orderFrontRegardless` aggressively** — papers over the race; brittle and
  changes a path that other callers (status-bar menu) also use.
- **C) Skip `targetApp.activate` when the palette is closing because Sokki
  itself is taking focus next** — the project already has this exact pattern:
  `commitOpenEditor` calls `close(reactivateTarget: false)` to avoid bouncing
  focus to another app when opening the Match Editor window
  (`PaletteController.swift:181`). Mirror it for Settings. **Chosen.**

## 5. Decision Log

**1. How to expose the "skip reactivation" behavior to the palette view?**
- Options: A) New `onOpenSettings` callback prop · B) Change `onClose` to take a
  flag · C) View calls a "close-without-reactivate" callback then posts itself
- Decision: **A) New `onOpenSettings` callback** — Mirrors existing
  `onCommit` / `onEdit` / `onClose` convention; PaletteView stays free of
  NSApp / NotificationCenter details; controller owns the close-policy + post
  sequence as one atomic step.

**2. Who posts `.openSettings`?**
- Options: A) Controller · B) View
- Decision: **A) Controller** — The view should not know about
  `NotificationCenter` plumbing or activation policy; the controller already
  owns activation lifecycle.

**3. Should SokkiApp's `.openSettings` observer route through
`ActivationPolicyCoordinator`?**
- Options: A) Yes (unify lease management) · B) No (leave as-is)
- Decision: **B) No** — YAGNI. The observer manages its own `.accessory`
  reset on `willClose`; mixing it into the coordinator broadens scope and
  risks regressing the menu-bar path, which works.

## 6. Design

Three small changes:

**1. `PaletteController` — new method (named to match `commitOpenEditor` / `commitInjection`):**
```swift
private func commitOpenSettings() {
    // Don't bounce focus back to the user's prior app — focus is going to
    // the Settings window, which the .openSettings observer activates.
    close(reactivateTarget: false)
    NotificationCenter.default.post(name: .openSettings, object: nil)
}
```

**2. `PaletteController.show()` — pass a new callback into `PaletteView`:**
```swift
let view = PaletteView(
    matchStore: matchStore,
    recency: recency,
    renderer: renderer,
    clipboardSnapshot: clipboardSnapshot,
    onCommit: { [weak self] match in self?.commitInjection(match) },
    onEdit:   { [weak self] match in self?.commitOpenEditor(match) },
    onClose:  { [weak self] in self?.close() },
    onOpenSettings: { [weak self] in self?.commitOpenSettings() }
)
```

**3. `PaletteView` — add prop, swap the hidden button body:**
```swift
let onOpenSettings: () -> Void
…
Button("") { onOpenSettings() }
    .keyboardShortcut(",", modifiers: .command)
```

### Why this fixes the race
With `reactivateTarget: false`, `close()` only calls `activation.release()`
(policy → `.accessory`); it does NOT activate another app. The observer
immediately runs `setActivationPolicy(.regular)` + `NSApp.activate` +
`makeKeyAndOrderFront` + `orderFrontRegardless`. There is no competing
cross-app activation for macOS to gate against, so the window comes up
frontmost — same as the status-bar menu path which already works.

### Accepted trade-offs
- **Brief `.accessory` → `.regular` flip within one runloop turn.** The
  coordinator's `release()` and the observer's `setActivationPolicy(.regular)`
  fire on the same runloop turn before macOS gets a chance to surface another
  app. `orderFrontRegardless` (already in the observer) is the backstop.
- **Coordinator vs. observer not unified.** The `.openSettings` observer in
  `SokkiApp.swift` manages its own policy lifecycle separately from
  `ActivationPolicyCoordinator`. Unifying them is real arch debt but
  out of scope for this fix — that change would touch the working status-bar
  path and risk regression.
- **Rapid `⌘,` double-press while palette is reopening.** The existing
  `isShowing` guard in `close()` short-circuits the second call; the second
  notification still posts and the observer harmlessly re-keys the existing
  Settings window. No action needed.

## 7. Files Changed
- `Sources/Sokki/Services/PaletteController.swift` — add
  `commitOpenSettings()`; pass `onOpenSettings` callback into PaletteView.
- `Sources/Sokki/Views/PaletteView.swift` — add `onOpenSettings: () -> Void`
  prop; replace the inline close+post in the hidden Cmd+, button with a single
  `onOpenSettings()` call.

## 8. Verification
1. **[AC1]** `swift build` succeeds. Launch app, open palette (`⌥Space` or
   configured hotkey), press `⌘,` — Settings window appears as the frontmost
   key window with focus on Sokki (Sokki name in menu bar; Settings window
   has standard title-bar focus styling).
2. **[AC2]** While palette is open over another app (e.g., Safari), press
   `⌘,`. Safari's menu must NOT briefly appear in the menu bar during the
   transition. To verify objectively (polling `frontmostApplication` can
   miss sub-frame flickers), subscribe to
   `NSWorkspace.didActivateApplicationNotification` during testing and
   assert the activation sequence contains only Sokki — never Safari.
3. **[AC3]** Status-bar menu → "Open Settings" still brings the Settings
   window forward (unchanged path).
