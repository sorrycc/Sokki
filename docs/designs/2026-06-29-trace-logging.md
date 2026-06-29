# Engine Trace Logging

## 1. Background

When a user reports "I typed `;don` but nothing expanded", the log gives almost
nothing to work with. Today the engine logs every raw keystroke
(`KeystrokeMonitor.swift:399`) and a line only when a match actually *fires*
(`:407`). Every reason an expansion can fail — trigger not registered, cursor
died on a non-contiguous char, a held pending match silently cancelled, the app
disabled, secure input — leaves no trace. The single most useful signal, the
matcher's state after each keystroke, is recorded nowhere.

Separately, the raw `Key:` log at `KeystrokeMonitor.swift:399` runs *before*
`feed()`'s secure-input check (`TapContext.swift:79`), so password-field
characters currently land in `~/.config/sokki/logs/sokki.log` at the default
`debug` level. Adding buffer snapshots without fixing this widens the leak.

## 2. Requirements Summary

- Goal: always-on per-keystroke trace of the expansion engine's decision path so
  a non-expansion can be diagnosed from the log file alone.
- Scope: match-decision path only — `TapContext.feed`, `TrieMatcher`,
  pending-match cancellation, the conflict-resolve timeout fire, trie (re)load.
  Not injection, AI, or config load.
- Key decisions (locked with user): (1) always-on, bypasses `log_level`;
  (2) keep human-readable characters/buffer snapshots, but secure-field
  characters must never reach the log; (3) match-decision path only.

## 3. Acceptance Criteria

1. Typing a sequence such as `;don` produces one trace line per keystroke
   containing: the character, the recent buffer tail, the matcher state (active
   cursor depths + pending trigger), and the result (`none` or `matched`).
2. When a held pending match is dropped, the log records the reason
   (`mouse-click`, `app-switch`, `modifier-shortcut`, `palette-chord`,
   `toggle-shortcut`, `focus-change`, `suppress`, `matches-reload`) — and only
   when a pending match actually existed (no per-click spam).
3. On trie (re)load, the log records the enabled-match count and the list of
   enabled triggers, so "trigger never registered" is distinguishable from
   "registered but never fired".
4. The previously-silent guard branches — secure transitions, paste-too-soon,
   disabled — each emit a trace line. Per-key self-echo (our own injection) and
   `stillSecure` do not log (no spam, and no password length/cadence leak).
5. Trace lines are written regardless of `log_level`, including when it is
   `error`.
6. Secure-field characters never appear in the log: the pre-`feed` `Key:` log is
   removed, and the per-keystroke trace is emitted inside `feed` only after the
   secure-input check has passed.
7. A pending match that resolves later via the conflict-resolve timeout
   (`firePendingTimer`, not a keystroke) emits an always-on trace line naming the
   trigger — so the "`;don`, pause, it expands" path is explained end to end.
8. The buffer-clearing edits on the match-decision path — word/line delete,
   backspace rewind, caret-move — emit a trace line (today they are `debug`), so
   a buffer wiped mid-type is visible at any `log_level`. The rewind line also
   reports the post-re-feed matcher state, so the cursor-depth "jump" after a
   backspace is not surprising.

## 4. Problem Analysis

- **Approach A — log more from `TapHandler.handleEvent`** (the tap callback,
  `KeystrokeMonitor.swift`). Rejected: the handler does not hold the matcher
  state (that lives behind `TapContext`'s lock), and it sees keystrokes *before*
  the secure-input check — exactly the wrong side of the privacy boundary.
- **Approach B — emit the trace from inside `TapContext.feed`** and the other
  `TapContext` methods that own the match-decision state, the single serialized
  choke point that owns buffer + matcher under one lock and runs *after* the
  secure check. Chosen: all state is in hand, secure characters are already
  filtered out by the early returns above the trace point, and the existing code
  already calls `Logger.shared.debug(...)` from inside this lock
  (`TapContext.swift:121,132,141`) so there is precedent.
- **Persistent FileHandle for the Logger** — considered as a perf hedge for
  always-on writes, then rejected (YAGNI). The hot path adds one trace line per
  keystroke; at human typing speed the extra syscalls are far below the
  single-callback budget that trips `tapDisabledByTimeout`. Keep the Logger
  change to a new `trace()` method only.

## 5. Decision Log

**1. How does a trace line bypass `log_level`?**

- Options: A) add `case trace` to the `Level` enum at the verbose end ·
  B) add a dedicated `Logger.trace(_:)` that writes unconditionally, outside the
  level enum.
- Decision: **B)** — `error`/`warning` already write unconditionally (only
  `debug` is gated, `Logger.swift:45`). A `trace` *in* the enum would imply it is
  threshold-gated, which it is not. Refactor the file-write body of `log()` into
  `private func write(_ label:_ message:)` and have `error`/`warning`/`debug`/
  `trace` all call it (DRY); `trace` simply omits the level guard.

**2. Where is the per-keystroke trace emitted?**

- Options: A) `TapHandler.handleEvent` · B) `TapContext.feed`.
- Decision: **B)** — see Problem Analysis. Also makes AC6 fall out for free: the
  secure/suppress/paste/disabled early-returns sit above the trace point, so
  those characters are never formatted into a line.

**3. How is matcher state exposed for the trace?**

- Options: A) read `cursors`/`pendingMatch` ad hoc in `feed` (they are
  `private(set)`, same-module) · B) add a `var debugSummary: String` computed
  property on `TrieMatcher`.
- Decision: **B)** — keeps the formatting next to the data and DRY across the
  call sites (synchronous matched / none, plus the timeout-fire and rewind
  lines). Read-only, no behavioral change.

**4. How are pending-match drops attributed to a reason?**

- Options: A) log unconditionally inside `resetMatcherAndTimer` (the shared
  clear helper) · B) add a `reason:` parameter to `cancelPendingMatch` and a
  small `notePendingDrop(_:reason:)` helper that logs only when
  `pendingMatch != nil`, called from the public drop paths.
- Decision: **B)** — `resetMatcherAndTimer` runs on nearly every keystroke
  branch (match fire, secure transition, etc.); logging there would bury the
  signal. Gating on a live pending match and naming the caller's reason yields
  exactly AC2. The helper is `notePendingDrop(_ state: inout State, reason:)` —
  `inout` to match the existing `resetMatcherAndTimer(_:inout)` /
  `reschedulePendingTimer(_:inout)` convention and to avoid copying `State`
  (which holds a 128-element `RollingBuffer`). It only *reads*
  `state.matcher.pendingMatch`; it does not take the lock — every caller already
  runs inside `lock.withLock`, so it must never be invoked from outside one
  (no lock re-entry). It is called *before* `resetMatcherAndTimer` clears the
  pending.

**5. What does the buffer snapshot show?**

- Options: A) full rolling buffer (up to 128 chars) · B) last ~16 chars.
- Decision: **B)** — the diagnostic value is the recent suffix around the
  trigger; 16 chars keeps lines readable. `RollingBuffer.tail(_:)` returns `""`
  unless `length <= count`, so use `tail(min(16, currentCount))`.

**6. Should `stillSecure` and self-echo keystrokes be traced?**

- Options: A) trace them too · B) stay silent on both.
- Decision: **B)** — both fire once per keystroke during password entry / our own
  injection. Tracing `stillSecure` would leak password length and cadence
  (violates the spirit of AC6); tracing self-echo is our own output, not user
  intent. Only secure *transitions* (entered/exited) are traced, without the
  character.

**7. Is the conflict-resolve timeout fire (`firePendingTimer`) traced? [added in Phase 4 review]**

- Options: A) leave the existing `debug` line at `:429` · B) convert it to
  `trace`.
- Decision: **B)** — the held-pending-then-timeout path (`;don`, pause, it
  expands) does not pass through `feed`, so without this the headline use-case
  ends in an unexplained injection, and the existing `debug` line vanishes under
  `log_level=error` (AC5's own test). Convert `:429` to
  `Logger.shared.trace("pending fired (timeout): \(trigger) len=\(length)")`.
  In scope (match-decision path) and the natural counterpart to the synchronous
  `-> matched` line. (AC7.)

**8. Do the backspace/caret/word-line-delete clears trace? [added in Phase 4 review]**

- Options: A) leave them `debug` · B) upgrade the three to `trace`.
- Decision: **B)** — these are prime "my buffer got wiped mid-type and the
  trigger never matched" causes, more central than the paste/disabled guards the
  design already upgrades; leaving them `debug` is an inconsistent altitude with
  no rationale. Upgrade `:121` (word/line delete), `:132` (rewind), `:141`
  (caret-move) to `trace`. The rewind line additionally appends
  `state.matcher.debugSummary` (computed after `rebuildAfterBackspace` re-feeds
  the buffer at `:388`), so the post-rewind cursor state is visible and the
  re-fed chars need no per-char trace. (AC8.)

## 6. Design

### Logger (`Sources/Sokki/Services/Logger.swift`)

Extract the file-append body of `log(level:message:)` into
`private func write(_ label: String, _ message: String)` (timestamp + lock +
`rotateIfNeeded` + append — unchanged). Then:

- `error` / `warning` → `write("ERROR"/"WARN", message)` (always on, as today).
- `debug` → `guard Self.level >= .debug` then `write("DEBUG", message)`.
- `trace` → `write("TRACE", message)` with **no** level guard.

The `Level` enum is untouched; `trace` lives outside it deliberately.

### TrieMatcher (`Sources/Sokki/Engine/TrieMatcher.swift`)

Add a read-only computed property:

```swift
var debugSummary: String {
    let depths = cursors.map(\.depth).sorted()
    return "cursors=\(depths) pending=\(pendingMatch?.match.trigger ?? "-")"
}
```

### TapContext (`Sources/Sokki/Engine/TapContext.swift`)

Per-keystroke trace in `feed`, capturing the tail after `append` and the matcher
summary after `feed` (so the "pending held" case shows `-> none` with a non-`-`
pending, which is exactly the `;don` vs `;done` situation):

```swift
state.buffer.append(character)
let tail = state.buffer.tail(min(16, state.buffer.currentCount))
let mResult = state.matcher.feed(character)
let summary = state.matcher.debugSummary
if let result = mResult {
    Logger.shared.trace("feed '\(character)' buf=\"\(tail)\" \(summary) -> matched \(result.match.trigger) len=\(result.triggerLength)")
    state.buffer.clear()
    reschedulePendingTimer(&state)
    return FeedResult(action: .matched(match: result.match, triggerLength: result.triggerLength))
}
Logger.shared.trace("feed '\(character)' buf=\"\(tail)\" \(summary) -> none")
reschedulePendingTimer(&state)
return FeedResult(action: .none)
```

Guard-branch traces (all already-non-secure where they log a character):

- `enteredSecure` / `exitedSecure`: `trace("secure: entered/exited — buffer cleared")` — no character. `stillSecure`: silent.
- paste-too-soon (inside the `!hasEmittedTooSoonForCurrentPaste` block): `trace("blocked: paste-too-soon buf=\"…\"")`.
- disabled (inside the `!hasEmittedDisabledSinceLastEnable` block): `trace("blocked: disabled buf=\"…\"")`.
- self-echo (`suppressMatching`) early return: silent.

Clear-path traces (AC8) — upgrade the three existing `debug` lines to `trace`:

- `:121` word/line delete → `trace("clear: word/line-delete flags=…")`.
- `:141` caret-move → `trace("clear: caret-move keyCode=…")`.
- `:132` rewind → `trace("rewind: popped, remaining=\(currentCount) \(state.matcher.debugSummary)")`.

Pending-drop attribution:

```swift
private func notePendingDrop(_ state: inout State, reason: String) {
    if let p = state.matcher.pendingMatch {
        Logger.shared.trace("pending dropped (\(reason)): \(p.match.trigger)")
    }
}
```

Called *before* the clear in each public drop path (each already inside
`lock.withLock`):

- `cancelPendingMatch(reason:)` gains a `reason: String` parameter →
  `notePendingDrop(&state, reason: reason)`.
- `clearBuffer` → `reason: "focus-change"`.
- `setSuppressMatching(true)` reset branch → `reason: "suppress"`.
- `updateMatches` → `reason: "matches-reload"`, plus
  `trace("updateMatches: \(enabled.count) enabled; triggers=[\(triggers)]")`
  where `enabled = matches.filter(\.isEnabled)`.
- `handlePaletteChord` matched branch → `reason: "palette-chord"`.
- `handleModifierTap` toggle branch (`:222`) → `reason: "toggle-shortcut"`.

(Secure transitions also clear a pending, but they already emit their own
`secure: entered/exited` line, so no separate `notePendingDrop` there — and the
pending trigger predates the secure field, so suppressing it avoids an
unnecessary trigger string next to a password event.)

### `firePendingTimer` (AC7)

Convert the existing `Logger.shared.debug` at `:429` to:

```swift
Logger.shared.trace("pending fired (timeout): \(match.trigger) len=\(length)")
```

This runs outside `lock.withLock` (as today, after the result is unpacked), so
no isolation change.

### KeystrokeMonitor (`Sources/Sokki/Engine/KeystrokeMonitor.swift`)

- Remove `Logger.shared.debug("Key: \(character) (code: \(keyCode))")` at
  `:399` (the secure-input leak; the `feed` trace replaces and improves it).
- Pass reasons to the `cancelPendingMatch` call sites: mouse monitor
  (`:139` → `"mouse-click"`), app-switch observer (`:150` → `"app-switch"`),
  Cmd/Ctrl shortcut path (`:373` → `"modifier-shortcut"`), and `stop()`
  (`:167` → `"monitor-stopped"`).

## 7. Files Changed

- `Sources/Sokki/Services/Logger.swift` — extract `write(_:_:)`; add always-on
  `trace(_:)`.
- `Sources/Sokki/Engine/TrieMatcher.swift` — add `debugSummary` computed property.
- `Sources/Sokki/Engine/TapContext.swift` — per-keystroke `feed` trace;
  guard-branch traces (secure transitions, paste-too-soon, disabled);
  clear-path traces (word/line-delete, rewind+state, caret-move);
  pending-drop attribution via `notePendingDrop` across all in-scope drop paths
  (cancel/focus/suppress/reload/palette-chord/toggle); `reason:` param on
  `cancelPendingMatch`; trie-load trace in `updateMatches`; `firePendingTimer`
  timeout line upgraded to `trace`.
- `Sources/Sokki/Engine/KeystrokeMonitor.swift` — remove leaking `Key:` log; pass
  drop reasons to `cancelPendingMatch` call sites.

## 8. Verification

1. [AC1] `swift build` succeeds; type `;don` into a text field and confirm
   `~/.config/sokki/logs/sokki.log` shows four `feed '…' … -> none` lines with
   ascending cursor depths.
2. [AC1/AC2] With both `;don` and `;done` configured, type `;don`, then click
   the mouse: log shows `-> none pending=;don` then
   `pending dropped (mouse-click): ;don`.
3. [AC2] With a palette shortcut configured, type `;don` then fire the palette
   chord: log shows `pending dropped (palette-chord): ;don`.
4. [AC3] Edit matches in Settings; log shows
   `updateMatches: N enabled; triggers=[…]` including `;don`.
5. [AC4] Trigger a disabled state / paste-too-soon and confirm a single
   `blocked: …` trace line each.
6. [AC5] Set `log_level` to `error` in config; confirm `feed …` trace lines
   still appear.
7. [AC6] Focus a password field, type, and confirm **no** `feed`/`Key:` line and
   no characters appear in the log — only `secure: entered — buffer cleared`.
8. [AC7] With `;don` and `;done` configured, type `;don` and pause past the
   conflict-resolve delay; confirm `pending fired (timeout): ;don len=4`.
9. [AC8] Mid-word, press Option-Delete (word delete) and the arrow keys; confirm
   `clear: word/line-delete …` and `clear: caret-move …` lines, and that a
   backspace produces `rewind: popped, remaining=N cursors=[…] pending=…`.
