# Backspace-aware matching

Date: 2026-04-23
Status: proposed

## Problem

`RollingBuffer` appends every typed char. On backspace, `TapContext.feed` only
consults `ExpansionUndoManager` (to undo a *just-fired* expansion); it does not
pop the buffer or rewind `TrieMatcher` cursors. Arrow keys likewise leak
non-printable chars into the buffer.

Consequence: after `;fa` → ⌫ → more typing, the trie cursor is stuck at the
dead end under `;f → a` and the buffer is out of sync with what's visible to
the user. A subsequent trigger never fires even though the visible text ends
with a valid trigger.

Goal: if the user's visible text ends with a trigger — whether or not they
reached that text via corrections — the trigger fires.

## Scope

In scope:

- Plain `⌫`
- `⌥⌫` (delete word), `⌘⌫` (delete to line start)
- Caret-move keys: arrows `↑↓←→`, `Home`, `End`, `PageUp`, `PageDown`
- Modified caret moves `⌥←`/`⌥→`/`⌘←`/`⌘→` — covered for free, since
  `isCaretControlKey` matches on keycode and ignores modifiers

Out of scope:

- Mouse click / drag caret moves
- Forward-delete `⌦` (doesn't touch chars left of caret; deferring)
- Shift-arrow selections (the arrow handler's conservative clear covers this
  as a side effect — shift+arrow still produces the arrow keycode)
- Fuzzy matching
- IME multi-char commits (pre-existing — `KeystrokeMonitor` keeps only
  `str.first`; rewind inherits this limitation but doesn't worsen it)

## Behavior

| Input                           | Today                        | New                                             |
| ------------------------------- | ---------------------------- | ----------------------------------------------- |
| Plain ⌫ right after expansion  | Undo expansion               | Same — unchanged                                |
| Plain ⌫ otherwise              | No-op (buffer/matcher stale) | Pop 1 char from buffer; rebuild matcher cursors |
| ⇧⌫                             | Same as plain ⌫             | Same as plain ⌫ (shift is not semantic here)   |
| ⌥⌫ / ⌘⌫                     | No-op (falls into undo path) | Clear buffer + reset matcher (skip undo path)   |
| Caret-move (arrows, Home, etc.) | Appended as garbage char     | Clear buffer + reset matcher                    |
| Match fires                     | Matcher resets; buffer kept  | Matcher resets; **buffer also clears**          |

All "clear" and "rewind" branches also invalidate undo tracking by calling
`undoManager.handleNonBackspaceKey()` — the user has moved the caret or done
a bulk delete, so the next plain ⌫ must not restore a prior expansion.

## Mechanism

### Plain ⌫: re-feed

1. `RollingBuffer.popLast()` drops the last char.
2. `TrieMatcher.reset()`.
3. Loop the remaining buffer chars through `matcher.feed()`, **discarding** returned match results.

This yields exactly the `cursors` + `pendingMatch` state that would exist had
the popped char never been typed. `O(n)` where `n ≤ 128` — trivial.

Discarding return values is load-bearing: if the buffer ends at a full trigger
after rewind, re-feed would otherwise re-fire an already-expanded trigger. We
want cursor state only.

### Clear buffer on match fire (required)

This is a **change to existing behavior**. Today, when a match fires,
`TrieMatcher.feed` resets its cursors but `TapContext.feed` leaves
`state.buffer` untouched — the buffer keeps the consumed trigger chars
indefinitely. This is harmless today because no existing code path rebuilds
the matcher from buffer contents.

With rewind, it stops being harmless: a later ⌫ could rebuild the matcher
over chars that were already consumed by an expansion, resurrecting a
`pendingMatch` that fires a second time. Concrete regression:

- Triggers `;foo`, `;foobar`, and `a` all defined.
- Type `;foo` → pending `;foo` set.
- Type `a` → `a` is a root trigger with no descendants → immediate fire of
  `a` (length 1). The existing matcher silently drops pending `;foo`.
- Without clear-on-fire, buffer is `;fooa`. A later ⌫ rebuilds over `;foo`
  → pending `;foo` set again → next non-child keystroke fires `;foo` from a
  region that's been replaced. Corrupts text.

Fix: call `state.buffer.clear()` on every match fire in `TapContext.feed`.
Keeps buffer semantics aligned with matcher semantics (both empty after
fire). Rewind can never cross a match-fire boundary.

Cost: we lose the single-char-tail continuation in the pending-fire case
(`;foob` → `x` fires `;foo`, `x` is in the editor but not in our buffer).
This only matters if a trigger starts with that `x` AND the user types a
second trigger char without any intervening separator. Low value; KISS wins.

### Side benefit: latent `ExpansionUndoManager` bug fix

Today, `ExpansionUndoManager.handleBackspace` is called on *every* backspace
keycode regardless of modifier, incrementing `backspaceCount` by 1. One ⌥⌫
deletes a whole word but counts as 1 backspace; with a short replacement,
successive ⌥⌫s can spuriously trip `.undo(restoreTrigger:)`.

The new design routes ⌥⌫ and ⌘⌫ through `handleNonBackspaceKey()` instead,
which invalidates `lastExpansion`. This is a latent bug fix piggybacking on
the new behavior — the change is not purely additive.

### Alternative considered: cursor-snapshot stack

Push a `TrieCursor[]` snapshot per keystroke, pop per ⌫. `O(1)`.

Rejected: re-feed is ~10 lines, introduces no new invariants across expansion
fires / `updateMatches` rebuilds / buffer clears. No measurable perf win at
`n ≤ 128`.

### Alternative considered: trim `keepLast(count - triggerLength)` at fire

Retain a precise tail after each match fire to preserve the killing char in
pending-fire cases. Rejected: wrong in the shorter-match-drops-longer-pending
case (`;foo` pending + `a` fires → would retain `;foo` → re-fires after a
rewind). Full clear is simpler and closes every double-fire hole.

## Implementation

### `Sources/Sokki/Engine/RollingBuffer.swift`

Add:

```swift
@discardableResult
mutating func popLast() -> Bool {
    guard count > 0 else { return false }
    count -= 1
    return true
}
```

No `keepLast` needed.

### `Sources/Sokki/Engine/TrieMatcher.swift`

No API change.

### `Sources/Sokki/Engine/TapContext.swift`

Inside `feed(character:keyCode:modifierFlags:)`:

- **Caret-move keys** (see `isCaretControlKey` below): before any buffer
  append, call `undoManager.handleNonBackspaceKey()`, clear buffer, reset
  matcher, return `.none`.
- **Backspace** branch:
  - Modifier detection uses `contains(.maskAlternate)` or `contains(.maskCommand)`
    ONLY. Shift is explicitly ignored — `⇧⌫` behaves as plain `⌫`.
  - If `⌥⌫` or `⌘⌫`: call `undoManager.handleNonBackspaceKey()`, clear
    buffer, reset matcher, return `.none` (skip undo-restore path).
  - Otherwise (plain or shift+plain):
    - Run the existing `undoManager.handleBackspace()` path.
    - If it returns `.none`, invoke `rebuildAfterBackspace(&state)`.
- **Match fire** (the existing `if let result = state.matcher.feed(character)`
  branch that returns `.matched`): add `state.buffer.clear()` before
  returning. This is the behavior change called out above.
- **Logging**: each new path emits a `Logger.shared.debug(...)` one-liner,
  matching the existing `"Key: ..."` / `"Match found: ..."` style. Suggested
  lines: `"Rewind: popped, remaining=\(state.buffer.currentCount)"`,
  `"Caret-move clear: keyCode=\(keyCode)"`, `"Word/line delete clear: flags=\(flags.rawValue)"`.

Private helpers (inside `lock.withLock`):

```swift
private func isCaretControlKey(_ keyCode: UInt16) -> Bool {
    switch keyCode {
    case 0x7B, 0x7C, 0x7D, 0x7E: return true // ← → ↓ ↑
    case 0x73, 0x77, 0x74, 0x79: return true // Home, End, PageUp, PageDown
    default: return false
    }
}

private func invalidateMatchingState(_ state: inout State) {
    state.undoManager.handleNonBackspaceKey()
    state.buffer.clear()
    state.matcher.reset()
}

private func rebuildAfterBackspace(_ state: inout State) {
    guard state.buffer.popLast() else { return }
    state.matcher.reset()
    let remaining = state.buffer.toString()
    for ch in remaining {
        _ = state.matcher.feed(ch)
    }
}
```

Both the caret-move branch and the ⌥⌫/⌘⌫ branch call
`invalidateMatchingState(&state)` to keep the three-step invalidation in one
place. If `TrieMatcher` ever grows a convenience `rebuild(over: String)`,
`rebuildAfterBackspace` collapses to two lines — nice refactor, not required.

### `Sources/Sokki/Engine/KeystrokeMonitor.swift`

No change. Caret-move keys already flow through to `context.feed`.

## Edge cases

- **⌫ on empty buffer**: `popLast()` returns false; matcher already empty; no-op.
- **Forward-delete (⌦, keyCode `0x75`)**: deletes the char *right* of caret,
  so the buffer-left-of-caret view is unchanged. Current behavior (append as
  garbage) is still wrong, but different from caret-move. Deferring.
- **Shift + caret-move** (e.g., `⇧←`): produces the same arrow keycode, so
  the clear fires. Selection replacement on next keystroke gets a clean
  buffer. Correct.
- **Re-feed produces a match during rebuild**: silently discarded — no HUD,
  no injection.
- **Undo composition**: the undo-restore path wins on the first ⌫ after an
  expansion. The new rewind only runs when `undoManager` returns `.none`.
- **Consecutive backspaces**: each ⌫ is a separate `feed` call → separate
  `rebuildAfterBackspace`. `k` backspaces cost `O(k·n)`. Bounded, fast.
- **Synthesized ⌫ during expansion**: `TextInjector` posts backspaces to erase
  the trigger. These come back through the tap with `suppressMatching=true`
  and early-return before any buffer/matcher work. The rewind path never
  runs during our own injection. Confirmed pre-existing behavior.
- **Undo char-count heuristic**: `ExpansionUndoManager` uses
  `backspaceCount >= replacement.count` with a 3-second timeout. Any
  non-backspace keystroke in between calls `handleNonBackspaceKey()` and
  invalidates `lastExpansion`, so typing-then-⌫ doesn't wrongly progress
  undo. Pre-existing; neither fixed nor worsened by this design — listed so
  future readers don't attribute the heuristic to the rewind feature.
- **System undo (`⌘Z`)**: `KeystrokeMonitor` skips any key with `.maskCommand`
  except backspace, so ⌘Z never reaches `TapContext.feed`. The visible text
  changes out from under our buffer. Pre-existing desync, same family as
  mouse click and ⌘←; not addressed here.

## Testing

No test target exists (per `CLAUDE.md`). Manual plan — define `;foo` and
`;foobar` as static matches:

1. Type `;fa` → ⌫ → `oo` → `;foo` expansion fires.
2. Type `;foob` → ⌫ → type `x` → pending `;foo` fires, then `x` lands in the
   editor as a literal char (buffer is cleared by the fire, so `x` is not
   retained for cursor purposes — this is the documented tradeoff).
3. Type `abc`, press `⌥⌫`, type `;foo` → fires.
4. Type `abc`, press `←`, type `;foo` → fires.
5. Type `abc`, press `Home`, type `;foo` → fires.
5b. Type `abc`, press `⌥←`, type `;foo` → fires (modified caret-move variant).
6. Type `;foo` (expands) → ⌫ → undo restores trigger (regression guard).
7. Type `;foo` (expands) → type `abc` → ⌫⌫⌫ → type `;foo` again → fires.
8. **Double-fire guard (clear-on-fire)**: type `abc;foo` → expansion fires
   → ⌫⌫⌫ over the replacement → type `;foo` → fires exactly once.
9. **Shorter-match-drops-pending guard**: with `a` also defined as a trigger,
   type `;fooa` → `a` fires → ⌫ ... → `;foo` does NOT re-fire from the
   replaced region.
10. `⇧⌫` behaves as plain `⌫` (rewind, not clear).

## Non-goals / follow-ups

- Mouse click caret desync — separate design, same family of problems.
- `⌘Z` system undo desync — same family.
- Forward-delete `⌦` — defer; low-value and semantically distinct.
- IME multi-char commit handling — pre-existing; separate design.
- Fuzzy matching — explicit non-goal.

## Ship checklist

- Implement per the Implementation section.
- Add a `CHANGELOG.md` entry under Unreleased (standard per the `go` skill).
- Manual test plan from the Testing section.
