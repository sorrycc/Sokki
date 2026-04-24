# AI Test Cancellation

## 1. Background

`AITestController` (added earlier today) runs a one-shot `claude` CLI call
from the AI settings tab. The button was disabled during the run — a
deliberate guard against double-clicks that left no way to abort. Separately,
`cancel()` aborts the Task but does not reset observable state. Because
SwiftUI's `Window` scene preserves view state across window close/reopen on
macOS, closing the Settings window mid-test leaves the controller pinned at
`.running`; reopening shows a stuck spinner.

## 2. Requirements Summary

- Cancellable test from the same button that starts it.
- Closing the Settings window kills the CLI process AND clears UI state.

## 3. Acceptance Criteria

1. `.idle`/`.ok`/`.fail` state → button label "Test connection", click starts.
2. `.running` state → button label "Cancel", click aborts.
3. Button is never disabled.
4. Cancellation (via button or window close) transitions `state` to `.idle`.
5. Reopen after mid-test close → AI tab shows `.idle`, no spinner, no stale
   success/failure panel.

## 4. Problem Analysis

SwiftUI `Window("Sokki Settings", id: "settings")` on macOS preserves the
view hierarchy while the window is hidden — `@State` instances survive. The
controller instance therefore outlives close/reopen. The existing
`cancel()` implementation stops the in-flight Task but never writes to
`state`; the Task itself early-returns on `.cancelled`. Result: after a
mid-test close, `state == .running` at reopen.

Chosen approach: move the `.idle` write into `cancel()` so there is exactly
one writer per transition:

- `start()` → writes `.running`
- Task continuation on natural completion → writes `.ok` / `.fail`
- `cancel()` → writes `.idle`
- `.cancelled` branch inside the Task → early-return (no write)

This removes races between concurrent `start()`/`cancel()` paths without
extra guards.

## 5. Decision Log

**1. Button design: morph vs. two buttons?**
- Options: A) single morphing button · B) separate Test + Cancel
- Decision: **A)** — user preference; cleaner for a two-state interaction.

**2. Cancel button style?**
- Options: A) plain · B) destructive (red)
- Decision: **A)** — cancel is not destructive; user preference.

**3. `.cancelled` branch writes `.idle`?**
- Options: A) yes · B) no, early-return
- Decision: **B)** — avoids racing the next `start()`'s `.running` write.

**4. Guard against double-start in `start()`?**
- Options: A) early-return if running · B) cancel existing and start fresh
- Decision: **B)** — already implemented (`task?.cancel()` at top of
  `start()`), behaviour is what a user re-click expects.

## 6. Design

### `AITestController.cancel()`

```swift
func cancel() {
    task?.cancel()
    task = nil
    state = .idle
}
```

### `AISettingsView` button

```swift
Button(controller.state.isRunning ? "Cancel" : "Test connection") {
    if controller.state.isRunning {
        controller.cancel()
    } else {
        controller.start(config: store.config.ai)
    }
}
```

No `.disabled(...)`. ProgressView continues to show during `.running`.

## 7. Files Changed

- `Sources/Sokki/Views/SettingsView.swift` — button action/label toggle,
  remove `.disabled`; `AITestController.cancel()` writes `state = .idle`.

## 8. Verification

1. [AC1-3] Start test → button flips to "Cancel" → click it → state resets
   to `.idle` (no spinner, no result panel), button reads "Test connection".
2. [AC4] Same result via window close: click Test, close Settings window,
   reopen — AI tab is idle.
3. Happy path unchanged: click Test → wait → success panel appears.
4. Click Test during a running test (if not cancelled) → cancels prior,
   starts new (already worked, should keep working).
