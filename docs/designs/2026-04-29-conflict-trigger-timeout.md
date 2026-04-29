# Conflict Trigger Timeout

## 1. Background

When two triggers form a prefix relationship (e.g. `;f` and `;foo` are both enabled), `TrieMatcher` holds the shorter match as `pendingMatch` while longer cursors remain live (`Sources/Sokki/Engine/TrieMatcher.swift:115–126`). The pending match only fires when a subsequent non-extending keystroke arrives, or via the explicit `flushPending()` call. There is no timeout. Result: typing `;f` and then stopping leaves the user staring at the literal text `;f` because the matcher is silently waiting for the next keystroke that may never come.

The prior design `docs/designs/2026-04-24-trigger-conflict-warnings.md` consciously declined to change runtime semantics and surfaced the conflict in the UI only. This work supersedes that runtime stance — the user wants both triggers to be usable: `;f` after a brief pause, `;foo` when typing continues.

## 2. Requirements Summary

**Goal:** Resolve trigger-prefix conflicts at runtime by adding a typing-inactivity timer. Pending shorter trigger fires after a brief pause; continued fast typing still resolves to the longer trigger.

**Scope in:**
- Typing-inactivity timer that flushes the current `pendingMatch` when no extending keystroke arrives within a configured delay.
- New `AppConfig.conflict_resolve_delay_ms` (default 400 ms), hot-reloaded via the existing `updateConfig` path.
- Timer cancel + pending drop (no fire) on context-changing events (mouse, focus, modifier shortcuts, toggle-off, secure input, AI/palette begin).
- Timer reset on every new pending match in a chain (debounce-by-inactivity, not original-pending age).
- Timer-fired match routes through the existing `onMatch` callback so HUD, undo, and injection paths are identical.

**Scope out:**
- Per-match override of the delay (delay is global).
- Settings UI for the new knob (config-file only — same staged approach as AI knobs).
- Re-arming pending after undo (passed on as surprising behavior).
- Fixing the unrelated trailing-char off-by-one in the existing keystroke-driven `flushPending` path (`;f<space>` consumes the space). Out of scope; not the user's reported pain.

## 3. Acceptance Criteria

1. With `;f` and `;foo` both enabled and `conflict_resolve_delay_ms = 400`:
   - Typing `;` then `f` then pausing ≥ 400 ms expands `;f`.
   - Typing `;foo` with each keystroke under 400 ms apart expands `;foo`, never `;f`.
   - Typing `;f<space>` immediately expands `;f` independent of the timer value (existing immediate-flush path; verifies Decision Log #8 is preserved).
2. Setting `conflict_resolve_delay_ms` in the YAML config and reloading takes effect without a restart.
3. While a match is pending, ALL of the following cancel the timer AND drop the pending match (no expansion fires):
   - Mouse click anywhere (left/right/other button down)
   - Modifier-held shortcut keystroke (e.g. Cmd+S, Ctrl+A)
   - Sokki toggle-off via double-modifier
   - Secure-input field activation
   - AI request begin (typed-trigger or palette)
   - Palette open
4. While a match is pending, pressing backspace cancels the in-flight timer; if `rebuildAfterBackspace` re-enters a pending state, a fresh timer is scheduled with the full delay.
5. Chained pending — with `;f`, `;ff`, `;ffoo` all enabled, typing `;`, `f`, `f` within < 400 ms apart leaves `;ff` pending with the timer reset on the second `f`; pausing ≥ 400 ms after the second `f` fires `;ff` (not `;f`).
6. A timer-fired expansion produces the same `HUDEvent` shape and the same `ExpansionUndoManager` recording as a keystroke-fired expansion.
7. Default behavior with no conflicting triggers is unchanged: matches fire on the keystroke that completes them; no pending state is ever set; timer code is never invoked.
8. `swift build` succeeds under Swift 6 strict concurrency. The timer storage and cancellation respect the existing `OSAllocatedUnfairLock` boundaries in `TapContext` and the `@MainActor` isolation of `KeystrokeMonitor`.

## 4. Problem Analysis

Approaches considered:

- **Approach A — fire pending eagerly on shorter match** — drop the descendant-tracking entirely; whenever a match is found, fire it immediately. Rejected: this breaks the longer-trigger case the user explicitly asked us to preserve. `;foo` would never fire because `;f` would always win first.
- **Approach B — Swift `Task { try await Task.sleep(...) }` per pending** — modern Swift concurrency, but `TapContext`'s mutable state lives behind `OSAllocatedUnfairLock` (struct), not an actor. Bridging `Task` cancellation across the lock boundary is more code than necessary, and we already have a precedent for `DispatchWorkItem`-based debounce.
- **Approach C — `DispatchWorkItem` on a dedicated serial queue with token-based stale-fire guard (chosen)** — direct precedent in `Sources/Sokki/Services/RecencyStore.swift:11,33–40`. Cancellation is sync and safe from any thread. A monotonic token in `State` makes stale work items self-discard when they win the lock race with a newer keystroke.

## 5. Decision Log

**1. Timer mechanism**
- Options: A) `DispatchSourceTimer` · B) `DispatchWorkItem` + `asyncAfter` · C) `Task` + `Task.sleep`
- Decision: **B)** — direct precedent at `Sources/Sokki/Services/RecencyStore.swift:33–40`; cancellation is synchronous; trivially Sendable. Same pattern as the existing debounce in this codebase.

**2. Where the timer lives**
- Options: A) Inside `TapContext` · B) Inside `KeystrokeMonitor` · C) New `PendingMatchTimer` type
- Decision: **A) `TapContext`** — the matcher state and the cancel-on-event sites already live there under one lock. Pulling the timer out adds a second source of truth and a race surface. Single new file is unwarranted (KISS).

**3. Stale-fire protection**
- Options: A) Rely solely on `DispatchWorkItem.cancel()` · B) Token check inside lock · C) Both
- Decision: **C) Both** — `cancel()` short-circuits work that hasn't been dequeued; the token check covers the narrow window where the work item already started waiting on the lock when `cancel()` was called. Token is a `UInt64` incremented on every schedule-or-clear.

**4. Default delay value**
- Options: A) 200 ms · B) 400 ms · C) 600 ms
- Decision: **B) 400 ms** — comfortably above existing 200 ms paste-debounce (`TapContext.swift:167`) and 300 ms double-modifier window (`TapContext.swift:204`). Long enough to cover normal inter-keystroke typing, short enough to feel snappy on pause.

**5. Configurability surface**
- Options: A) Hardcoded constant · B) `AppConfig` YAML knob, no UI · C) `AppConfig` knob + Settings UI
- Decision: **B)** — staged path matches the AI/keystroke knobs (`inject_delay`, `restore_clipboard_delay` at `AppConfig.swift:6,9`). Settings UI is YAGNI until a real user requests it.

**6. Knob naming**
- Options: A) `conflict_resolve_delay_ms` · B) `pending_match_timeout_ms` · C) `trigger_debounce_ms`
- Decision: **A)** — describes the user-visible behavior ("how long to wait when triggers conflict") in the same snake_case style as existing keys.

**7. Knob clamping**
- Options: A) Trust the value · B) Clamp to `[50, 5000]`
- Decision: **B)** — values < 50 ms make the matcher state machine racy with the keystroke-feed loop; values > 5 s mean the user effectively never gets the shorter match. Clamp silently (no error) on read inside `TapContext`. KISS.

**8. Non-extending-keystroke behavior**
- Options: A) Keep existing immediate flush · B) Force timer wait
- Decision: **A)** — preserves the snappy `;f<space>` UX. Timer is a pure-pause fallback. (Existing trailing-char off-by-one in this path is a separate latent issue; out of scope.)

**9. Cancel-on-mouse policy**
- Options: A) Cancel only when pending exists · B) Cancel unconditionally on every click
- Decision: **B)** — `cancelPendingMatch()` is an idempotent no-op when nothing is pending. Branchless and avoids leaking pending-state checks across the actor boundary. Lock acquisition is the only cost (~ns).

**10. Cancel-on-app-switch policy**
- Options: A) Subscribe to `NSWorkspace.didDeactivateApplicationNotification` · B) Rely on the mouse-click cancel · C) Both
- Decision: **A)** — Cmd+Tab away has no mouse click; relying on the click would leave a hole. Two-line subscription, low cost.

**11. Timer queue topology**
- Options: A) Reuse `KeystrokeMonitor`'s tap thread · B) Reuse `RecencyStore`'s queue · C) Dedicated `sokki.matcher.timer` serial queue
- Decision: **C)** — separation of concerns; the matcher timer must not be blocked by recency disk I/O or compete with the high-priority tap thread. Single new `DispatchQueue` instance is cheap.

**12. Callback signature**
- Options: A) `(Match, Int) -> Void` · B) `(TrieMatchResult) -> Void` · C) `(FeedAction) -> Void`
- Decision: **A)** — matches the existing `onMatch` signature in `KeystrokeMonitor.start()` exactly (`KeystrokeMonitor.swift:38`); the timeout-fire closure can simply mirror the keystroke-driven matched branch.

## 6. Design

### 6.1 `TrieMatcher` — unchanged

`TrieMatcher.swift` stays as-is. The existing `pendingMatch` accessor and `flushPending()` method already expose what we need. No new method required; no behavioral change.

### 6.2 `TapContext` — new state, scheduling, cancellation

**State additions** (inside `struct State`):

```swift
struct State {
    // ... existing fields ...
    var conflictResolveDelayMs: Int = 400
    var pendingTimer: DispatchWorkItem?
    var pendingTimerToken: UInt64 = 0
    var onPendingMatchFired: (@Sendable (Match, Int) -> Void)?
}
```

**New private serial queue** (instance field on `TapContext`, alongside `lock`; one instance per `TapContext`, lifetime = `TapContext` lifetime):

```swift
private let timerQueue = DispatchQueue(label: "sokki.matcher.timer", qos: .userInitiated)
```

`.userInitiated` reflects that the timer drives a user-perceptible expansion.

**Scheduling helper** (called inside `lock.withLock` from `feed`, after the `matcher.feed(character)` line):

```swift
private func reschedulePendingTimer(_ state: inout State) {
    state.pendingTimer?.cancel()
    state.pendingTimerToken &+= 1
    guard state.matcher.pendingMatch != nil else {
        state.pendingTimer = nil
        return
    }
    let token = state.pendingTimerToken
    let delayMs = max(50, min(5000, state.conflictResolveDelayMs))
    let work = DispatchWorkItem { [weak self] in
        self?.firePendingTimer(token: token)
    }
    state.pendingTimer = work
    timerQueue.asyncAfter(deadline: .now() + .milliseconds(delayMs), execute: work)
}
```

**Where it is called from `feed`:** ONLY on the single fall-through path that runs after `state.matcher.feed(character)` (currently `TapContext.swift:146–155`). That path covers both the `.matched` return and the `.none` return — both end at the bottom of `feed`. Insert one call to `reschedulePendingTimer(&state)` immediately before the existing `return FeedResult(action: .matched(…))` and immediately before the trailing `return FeedResult(action: .none)`.

**Early-return paths do NOT call `reschedulePendingTimer`.** They use `resetMatcherAndTimer` (defined below), which both clears the matcher and cancels the timer. The early-return sites are: secure-input transitions (`:74–86`), paste-too-soon window (`:89–96`), disabled-app branch (`:100–107`), word/line backspace (`:111–115`), undo backspace (`:117–122`), caret-control keys (`:131–135`).

The `rebuildAfterBackspace` helper is special: it is followed by a `return .none` at `:126` that does NOT fall through the post-`matcher.feed` site, so the helper itself ends with `reschedulePendingTimer(&state)` (after the inner re-feed loop). The backspace branch does not also call `reschedulePendingTimer` — it returns directly.

**Fire helper** (top-level method, called from the work item on `timerQueue`):

```swift
private func firePendingTimer(token: UInt64) {
    let result: (Match, Int, (@Sendable (Match, Int) -> Void))? = lock.withLock { state in
        guard token == state.pendingTimerToken,
              let pending = state.matcher.flushPending(),
              let cb = state.onPendingMatchFired
        else { return nil }
        state.pendingTimer = nil
        state.buffer.clear()
        return (pending.match, pending.triggerLength, cb)
    }
    if let (match, length, cb) = result {
        cb(match, length)
    }
}
```

The token check inside the lock is the stale-fire guard: any keystroke that ran between schedule and fire has bumped the token; the work item discards itself.

**`buffer.clear()` rationale:** mirrors the existing `state.buffer.clear()` after a successful `matcher.feed` returns a match (`TapContext.swift:147`). Once the trigger has fired, the buffer no longer reflects in-progress typing.

**Cancellation helpers**:

```swift
/// Drop pending match + cancel timer. Called from mouse-click / focus-change
/// monitors and from the modifier-shortcut early-return path.
func cancelPendingMatch() {
    lock.withLock { state in
        state.pendingTimer?.cancel()
        state.pendingTimer = nil
        state.pendingTimerToken &+= 1
        state.matcher.reset()
    }
}

/// Public setter so KeystrokeMonitor can attach the fire callback at start.
func setPendingMatchFiredCallback(_ cb: @escaping @Sendable (Match, Int) -> Void) {
    lock.withLock { state in state.onPendingMatchFired = cb }
}
```

**`setSuppressMatching(true)` augmentation** — when matching is suppressed AND a pending match is live, drop it:

```swift
func setSuppressMatching(_ suppress: Bool) {
    lock.withLock { state in
        state.suppressMatching = suppress
        if suppress, state.matcher.pendingMatch != nil {
            state.pendingTimer?.cancel()
            state.pendingTimer = nil
            state.pendingTimerToken &+= 1
            state.matcher.reset()
        }
    }
}
```

The `pendingMatch != nil` guard is **load-bearing**: `setSuppressMatching(true)` is called once per AI animation frame (`KeystrokeMonitor.swift:402`, ~120 ms cadence). Without the guard, every animation frame would churn the matcher and bump the token. With the guard, frame toggles are a free no-op when nothing is pending, preserving the long-standing "suppressMatching is a cheap self-echo gate" invariant.

This single edit covers the keystroke-driven `.matched` path (`KeystrokeMonitor.swift:757`, where `pendingMatch` was already cleared by `feed` itself, so the guard short-circuits — confirmed no-op, no regression) and pre-injection fences in the AI typed-trigger flow.

**`updateConfig` augmentation** — read the new knob:

```swift
state.conflictResolveDelayMs = config.conflictResolveDelayMs
```

A delay change does not cancel an in-flight timer; the next reschedule picks up the new value. KISS.

**`updateMatches` augmentation** — when matches reload, the in-flight pending may reference a now-stale `Match`. Cancel and drop:

```swift
state.pendingTimer?.cancel()
state.pendingTimer = nil
state.pendingTimerToken &+= 1
// (state.matcher = TrieMatcher(matches:) already replaces matcher state below)
```

`onPendingMatchFired` and `suppressMatching` are deliberately preserved across `updateMatches` — the callback is per-monitor lifetime (a reload mid-session keeps the same `KeystrokeMonitor`), and a reload mid-injection must not flip the self-echo gate.

**`clearBuffer` becomes**:

```swift
func clearBuffer() {
    lock.withLock { state in
        state.buffer.clear()
        resetMatcherAndTimer(&state)
    }
}
```

(Single source of truth for the matcher+timer pair; replaces the prior bare `state.matcher.reset()` at `:239`.)

**Existing `state.matcher.reset()` callsites** — they all need timer cancellation paired. The cleanest way is a private helper:

```swift
private func resetMatcherAndTimer(_ state: inout State) {
    state.matcher.reset()
    state.pendingTimer?.cancel()
    state.pendingTimer = nil
    state.pendingTimerToken &+= 1
}
```

Replace the bare `state.matcher.reset()` calls at:
- `TapContext.swift:76` (entered secure)
- `TapContext.swift:80` (exited secure)
- `TapContext.swift:121` (undo path)
- `TapContext.swift:213` (toggle off)
- `TapContext.swift:239` (clearBuffer — though this is also covered above; consolidate)
- `TapContext.swift:339` (invalidateMatchingState — caret-control keys)
- `TapContext.swift:344` (`rebuildAfterBackspace` — `state.matcher.reset()` + re-feed loop)

For `rebuildAfterBackspace`, the helper itself ends with `reschedulePendingTimer(&state)` after the re-feed loop, so the freshly-re-established pending state (if any) gets a new timer:

```swift
private func rebuildAfterBackspace(_ state: inout State) {
    guard state.buffer.popLast() else { return }
    resetMatcherAndTimer(&state)  // was: state.matcher.reset()
    let remaining = state.buffer.toString()
    for ch in remaining {
        _ = state.matcher.feed(ch)
    }
    reschedulePendingTimer(&state)
}
```

The backspace branch in `feed` does NOT also call `reschedulePendingTimer` — it returns directly after `rebuildAfterBackspace` (or after `resetMatcherAndTimer` for the word/line/undo variants).

### 6.3 `KeystrokeMonitor` — wiring

**Attach the fire callback** at `start()`, after the existing `onMatch` closure is built. Mirror the keystroke-driven matched branch (HUD event → suppressMatching flag → onMatch dispatch). To respect Swift 6 strict concurrency, capture the local `let context = tapContext` (already in scope at `KeystrokeMonitor.swift:37`) and the local `let onHUDEv = …` (already in scope at `:80`). Do NOT capture `self.tapContext` or `self.onHUDEvent` from a `@Sendable` closure — `KeystrokeMonitor` is `@MainActor` and a `@Sendable` closure on the timer queue cannot reach into MainActor-isolated stored properties synchronously. This mirrors the existing pattern at `:37` and the `onMatch`/`onUndo` closures at `:38–64`.

```swift
// `context`, `onMatch`, and `onHUDEv` are already-@Sendable locals in start()
// at :37, :38, :80. Capture them directly — no rebinding, no `[weak self]`.
let timeoutFire: @Sendable (Match, Int) -> Void = { [context, onMatch, onHUDEv] match, triggerLength in
    // Mirror keystroke path ordering: HUD event first (matches TapHandler:750–756),
    // then suppressMatching=true (matches :757), then dispatch onMatch.
    onHUDEv(HUDEvent(
        id: UUID(),
        timestamp: Date(),
        verdict: .matched,
        capturedBuffer: match.trigger,
        matchedTrigger: match.trigger
    ))
    context.setSuppressMatching(true)
    onMatch(match, triggerLength)
}
context.setPendingMatchFiredCallback(timeoutFire)
```

Ordering rationale: the keystroke path is `onHUDEvent → setSuppressMatching(true) → onMatch` (`KeystrokeMonitor.swift:750,757,758`). The timeout path now uses the same order. AC6 holds.

`onHUDEv` is the existing `@Sendable` closure that already wraps a `DispatchQueue.main.async` to deliver the HUD event on main (`:80–84`). Calling it from the timer queue is identical to calling it from the tap thread.

**Palette chord cancel hook** — `handlePaletteChord` (`TapContext.swift:262–269`) currently just predicate-tests the chord and returns true; it does not mutate state. We add a cancel inside the matching branch, since this is the canonical "palette is about to open" point in the keystroke pipeline:

```swift
func handlePaletteChord(keyCode: UInt16, flags: CGEventFlags) -> Bool {
    lock.withLock { state in
        guard let chord = state.paletteChord else { return false }
        let modifierMask: CGEventFlags = [.maskShift, .maskControl, .maskAlternate, .maskCommand]
        let activeModifiers = flags.intersection(modifierMask)
        let matched = keyCode == chord.keyCode && activeModifiers == chord.modifiers
        if matched {
            state.pendingTimer?.cancel()
            state.pendingTimer = nil
            state.pendingTimerToken &+= 1
            state.matcher.reset()
        }
        return matched
    }
}
```

This mutation runs inside the existing `lock.withLock`, so no nested locking. AC3's "Palette open cancels pending" is now backed by a concrete keystroke-path hook.

**Mouse-click cancellation** — add a global mouse monitor that lives for the lifetime of `start()`:

```swift
private var pendingCancelMouseMonitor: Any?

private func beginPendingCancelMouseWatch() {
    guard pendingCancelMouseMonitor == nil else { return }
    pendingCancelMouseMonitor = NSEvent.addGlobalMonitorForEvents(
        matching: [.leftMouseDown, .rightMouseDown, .otherMouseDown]
    ) { [weak self] _ in
        self?.tapContext.cancelPendingMatch()
    }
}

private func endPendingCancelMouseWatch() {
    if let m = pendingCancelMouseMonitor {
        NSEvent.removeMonitor(m)
        pendingCancelMouseMonitor = nil
    }
}
```

Called from `start()` and `stop()` respectively. Distinct from `aiMouseMonitor` which is scoped to the AI request lifetime.

**App-switch cancellation** — subscribe to `NSWorkspace` notifications in `start()`. Observer registered AFTER `tapThread.start()` so failure to install the tap doesn't leak observers; removed in `stop()` BEFORE `tapThread = nil`. `queue: nil` posts on the posting thread, which is acceptable because `cancelPendingMatch` is thread-safe (acquires `TapContext.lock`).

The observer closure reuses the outer `context` already declared at `KeystrokeMonitor.swift:37` — no shadowing rebind.

```swift
private var appSwitchObserver: NSObjectProtocol?

// in start(), after tapThread.start() (note: `context` is the outer let from :37):
self.appSwitchObserver = NSWorkspace.shared.notificationCenter.addObserver(
    forName: NSWorkspace.didDeactivateApplicationNotification,
    object: nil,
    queue: nil
) { [context] _ in
    context.cancelPendingMatch()
}

// in stop(), before tapThread = nil:
if let obs = appSwitchObserver {
    NSWorkspace.shared.notificationCenter.removeObserver(obs)
    appSwitchObserver = nil
}
tapContext.cancelPendingMatch()  // drop any in-flight timer
```

The closure captures `context` (a `Sendable` reference type) explicitly via the capture list, mirroring the `onMatch`/`onUndo` capture pattern.

**Modifier-shortcut early-return** — inside `TapHandler.handleEvent` at the existing skip block (`KeystrokeMonitor.swift:712–715`):

```swift
if keyCode != backspaceKeyCode && (flags.contains(.maskCommand) || flags.contains(.maskControl)) {
    context.cancelPendingMatch()
    return Unmanaged.passRetained(event)
}
```

### 6.4 `AppConfig` — new key

Add the field with snake_case decoding mirroring the surrounding tunables:

```swift
var conflictResolveDelayMs: Int = 400

enum CodingKeys: String, CodingKey {
    // ... existing cases ...
    case conflictResolveDelayMs = "conflict_resolve_delay_ms"
}

// inside init(from:):
self.conflictResolveDelayMs =
    (try? container.decode(Int.self, forKey: .conflictResolveDelayMs)) ?? 400
```

### 6.5 Threading & Sendability

- `State` already contains non-Sendable types (e.g. `Date?`); the existing `@unchecked Sendable` declaration on `TapContext` covers it. Adding `DispatchWorkItem?` and `(@Sendable (Match, Int) -> Void)?` does not change that.
- The fire callback is `@Sendable` and explicitly captures `context: TapContext` (a `Sendable` reference type) and `onHUDEv` / `onMatch` (already-`@Sendable` closures from `start()` at `:38,:80`). It does NOT capture `KeystrokeMonitor`'s `self` — MainActor isolation is reached only inside `onHUDEv`/`onMatch`, which already wrap their bodies in `DispatchQueue.main.async`.
- The `timerQueue` is a serial queue: at most one `firePendingTimer` runs at a time. Combined with token gating, no double-fire is possible.

### 6.6 Edge cases

- **Disabled state.** When `state.isEnabled` is false, `feed` returns early at `TapContext.swift:100`. No pending state can be set; no timer is ever scheduled. ✓
- **Suppressed feed.** `feed` returns at `TapContext.swift:66` when `suppressMatching` is true. The matcher isn't fed, so `pendingMatch` is unchanged. The timer set by the prior keystroke can still fire — but `setSuppressMatching(true)` (with the new `pendingMatch != nil` guard) already cancelled it. ✓
- **AI begin (typed-trigger invocation).** Pending was already drained at `KeystrokeMonitor.swift:757` via the augmented `setSuppressMatching(true)` triggered by the matched keystroke. The redundant `setSuppressMatching(true)` at `:225` ("Idempotent: typed path already has it") is a no-op via the guard.
- **AI begin (palette invocation).** Palette opens via `handlePaletteChord` which now cancels pending in its match branch (§6.3). Subsequent `setSuppressMatching(true)` at `:225` runs against an already-cleared matcher.
- **Configuration race during reschedule.** `state.conflictResolveDelayMs` is captured into a local under the lock; even if `updateConfig` runs between schedule and fire, the work item runs at the originally-captured deadline.
- **Reload while pending.** `updateMatches` cancels the timer and replaces the matcher; the pending `Match` value would have been from the old trie. ✓
- **Hot-reload of the delay.** Next pending match uses the new delay; an in-flight timer keeps its original deadline. Matches user expectation that "config takes effect for new actions."
- **`stop()` while pending.** `stop()` removes the observer, ends the mouse watch, and calls `context.cancelPendingMatch()` so any in-flight work item self-discards via the bumped token even if it has already been dequeued. Safe across a hypothetical `stop()` → `start()` cycle.

### 6.7 Logging

Add a single debug log inside the `if let (match, length, cb) = result { … }` branch (after `firePendingTimer` releases the lock), mirroring the verbosity of existing matcher logs:

```swift
if let (match, length, cb) = result {
    Logger.shared.debug("Pending match timeout fired: trigger=\(match.trigger) len=\(length)")
    cb(match, length)
}
```

No new log levels; debug is sufficient for diagnosis and disabled by default in production builds (`AppConfig.logLevel`).

## 7. Files Changed

- `Sources/Sokki/Models/AppConfig.swift` — add `conflictResolveDelayMs` field, `CodingKey` case, lenient decode default 400.
- `Sources/Sokki/Engine/TapContext.swift` — add timer state to `State`, `timerQueue` field, `reschedulePendingTimer`, `firePendingTimer`, `cancelPendingMatch`, `setPendingMatchFiredCallback`; augment `setSuppressMatching`, `updateConfig`, `updateMatches`, `clearBuffer`; replace bare `state.matcher.reset()` callsites with `resetMatcherAndTimer`; call `reschedulePendingTimer` at the end of `feed` and at the end of `rebuildAfterBackspace`.
- `Sources/Sokki/Engine/KeystrokeMonitor.swift` — wire `timeoutFire` callback in `start()` (using `[context]`/local-`@Sendable` capture pattern, not `[weak self]`); add `pendingCancelMouseMonitor` + `appSwitchObserver` lifecycle (registered after tap-thread start; both removed plus a final `cancelPendingMatch()` in `stop()`); cancel pending in the modifier-shortcut early-return inside `TapHandler.handleEvent`.

No new files; no changes to `TrieMatcher.swift`, `Match`, `MatchStore`, views, or any UI surface.

## 8. Verification

Manual checklist mapped to acceptance criteria:

1. **[AC1]** Define `;f → foo` and `;foo → fooooobar`. Type `;f`, wait 1 s. Confirm `;f` expands. Type `;foo` rapidly. Confirm `;foo` expands. Type `;f<space>`. Confirm `;f` expands immediately with no perceived 400 ms delay (and that the existing trailing-char behavior is unchanged from today).
2. **[AC2]** Edit `~/.config/sokki/config.yml`, set `conflict_resolve_delay_ms: 1000`. Reload (no restart). Repeat AC1; the pause must now be ≥ 1 s.
3. **[AC3]** Type `;f`, then click somewhere with the mouse. Wait 1 s. Confirm no `;f` expansion fires. Type `;f`, then press Cmd+S. Confirm no expansion. Repeat for: app switch via Cmd+Tab (no mouse), Sokki toggle-off via the configured double-modifier, focus a password field, open the palette (`Opt+Space` or configured chord), type an AI trigger that opens the AI flow.
4. **[AC4]** Type `;f`, press backspace. The buffer drops back to `;`. Now type `foo` rapidly within 400 ms intervals. Confirm `;foo` expands (the re-fed pending was cancelled then re-armed when `;foo` matched).
5. **[AC5]** With `;f`, `;ff`, `;ffoo` all enabled, type `;ff` then pause. Confirm `;ff` fires (not `;f`).
6. **[AC6]** Type `;f`, pause, observe expansion. Open the HUD (`Cmd+,` or configured shortcut) — confirm a `matched` row with trigger `;f`. Press backspace within the undo window: confirm `;f` is restored (standard undo).
7. **[AC7]** With only one match `;hello → world` (no conflict), type `;hello`. Confirm immediate expansion with no measurable delay. Add a debug log to verify `firePendingTimer` is never invoked in this run.
8. **[AC8]** `swift build -c release` exits 0 with no Swift 6 strict-concurrency errors. `./Scripts/package_app.sh` builds a signed bundle.
