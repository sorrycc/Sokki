# Conflict Trigger Follow-ups

## 1. Background

Two follow-ups to `2026-04-29-conflict-trigger-timeout.md`:

1. **Pending must drop when the user types past it.** With `;f` and `;foo` both enabled, typing `;fo` and pausing currently still fires `;f` after the configured 400 ms. The user's expectation: typing `o` past `;f` is an explicit commitment to a longer-prefix path; the `;f` proposal should be retracted, not held in reserve.
2. **Remove the trigger-conflict UI.** With the runtime now resolving prefix conflicts gracefully via the timeout, the warning badges in `MatchListView` and the inline caption in `MatchEditorView` (added in `2026-04-24-trigger-conflict-warnings.md`) are noise. Delete them outright — KISS.

## 2. Requirements Summary

**Goal:** (1) Drop `pendingMatch` the moment the matcher advances to a depth deeper than the pending's trigger length. (2) Remove all conflict-warning UI and its supporting model code.

**Scope in:**
- `TrieMatcher.feed` — invalidate `pendingMatch` when a cursor advances past `pendingMatch.triggerLength`.
- Delete `Sources/Sokki/Models/TriggerConflict.swift`.
- Strip `conflictsByMatchId` and the `didSet` recompute from `MatchStore`.
- Strip the badge column, `conflictTooltip`, and `allMatches:` plumbing from `MatchListView` (`MatchRow`).
- Strip `triggerConflict`, `allMatches`, `editorConflictMessages`, `formatPrefixLine`, and the `.onChange(of: trigger)` handler from `MatchEditorView`.

**Scope out:**
- Any other change to the timeout mechanism (delay value, cancel-events, callback wiring).
- Any change to `TrieMatcher`'s match-found / immediate-flush semantics.
- Telemetry, logging additions.

## 3. Acceptance Criteria

1. **AC1** With `;f` and `;foo` both enabled: typing `;f`, then `o` (any speed), then pausing for ≥ 400 ms — does NOT fire `;f`. Field literal stays `;fo`.
2. **AC2** Continuing to type `;foo` after the drop fires `;foo` normally on the second `o`.
3. **AC3** Typing `;f` alone and pausing ≥ 400 ms still fires `;f` (no regression).
4. **AC4** Typing `;f<space>` immediately fires `;f` (no regression to the immediate-flush path).
5. **AC5** Chained `;f`/`;ff`/`;ffoo`: typing `;ff` then pausing fires `;ff`. Required ordering: the new depth-drop runs BEFORE the match-found branch, so the old `;f` pending is cleared first and THEN the match-found branch installs the new `;ff` pending. Reordering breaks this AC.
6. **AC6** `Sources/Sokki/Models/TriggerConflict.swift` is deleted.
7. **AC7** `MatchListView` shows no warning badge anywhere; `MatchRow` has no `conflict` parameter.
8. **AC8** `MatchEditorView` shows no inline conflict caption; the editor no longer takes `allMatches`.
9. **AC9** `MatchStore` no longer exposes `conflictsByMatchId`; the `didSet` on `matches` is removed (the property is a plain `var`).
10. **AC10** `swift build -c release` clean under Swift 6 strict concurrency.

## 4. Problem Analysis

For #1 — three approaches considered:

- **A) Drop pending when extended in `TapContext.feed`** — would require exposing `TrieMatcher.pendingMatch` mutability outside the type. Crosses a clean encapsulation boundary that's there for a reason.
- **B) Drop pending in `TrieMatcher.feed` based on cursor depth** (chosen) — single 4-line edit inside the matcher, runs after `cursors = newCursors` and BEFORE the match-found branch so the chained-pending replacement still works correctly.
- **C) Drop pending only when `pendingMatch` is no longer reachable as a "next match" from any active cursor** — would require walking the trie. Over-engineered. The depth check is sufficient: if any cursor is deeper than `pending.triggerLength`, the user has already typed past the pending's terminal node.

For #2 — straight deletion. No alternatives worth considering.

## 5. Decision Log

**1. Where does the depth check live?**
- Options: A) In `TrieMatcher.feed` · B) In `TapContext.feed` after the matcher returns
- Decision: **A)** — keeps `pendingMatch` lifecycle wholly inside `TrieMatcher`. `TapContext` already trusts the matcher to manage its own pending state.

**2. What counts as "past pending"?**
- Options: A) Any cursor with `depth > pendingMatch.triggerLength` · B) The cursor that originally set pending must still exist · C) Active cursors must include `pending.triggerLength` exactly
- Decision: **A)** — simplest, correct in every case I traced. The cursor that originally set pending may have died or advanced; only the depth max matters. If max depth ≤ pending depth, the user is still at-or-before the pending boundary.
- Invariant: a NEW root cursor lands at depth 1, and `pending.triggerLength ≥ 1` (matches forbid empty triggers). So a fresh root cursor cannot trip `> triggerLength` unless `pending.triggerLength == 0`, which never occurs. If `pending.triggerLength == 1` and the user retypes the same single-char trigger, depth 1 is not `> 1`; the match-found branch immediately reinstates an equivalent pending. Either way, no false drop.

**3. Order vs. match-found branch?**
- Options: A) Run the drop check BEFORE the match-found branch · B) After
- Decision: **A) Before** — chained pending (`;f` → `;ff`) needs the old `;f` pending dropped first, then the match-found branch installs the new `;ff` pending. Running after would fight the chained-pending overwrite.

**4. Remove `TriggerConflict.swift` entirely or keep the data model dormant?**
- Options: A) Delete the file · B) Keep model, drop UI only
- Decision: **A)** — YAGNI; nothing else uses it. If a future feature needs conflict detection, it can be re-introduced from the prior design doc.

**5. Keep the `didSet` on `MatchStore.matches` as a placeholder?**
- Options: A) Remove the property's `didSet` entirely · B) Replace with `didSet { }` empty body
- Decision: **A)** — empty `didSet` is dead code. Remove it; the property becomes a plain `var`.

## 6. Design

### 6.1 `TrieMatcher.feed` — depth-based pending drop

Insert the drop check immediately after `cursors = newCursors`, before the match-found branch:

```swift
cursors = newCursors

// Drop pending if the matcher has advanced past it. The user has typed
// further than the pending's trigger boundary, which means they've
// committed to a longer-prefix path; the shorter pending proposal is
// no longer their intent. Runs BEFORE the match-found branch so a
// chained-pending replacement (e.g. `;f` → `;ff`) still installs cleanly.
if let pending = pendingMatch,
   cursors.contains(where: { $0.depth > pending.triggerLength }) {
    pendingMatch = nil
}

// Handle match found
if let match = longestMatch { ... }
```

(Use `cursors`, not `newCursors`, since the assignment on the prior line makes them identical and `cursors` is the canonical name.)

**Behavior trace:**
- `;f` typed (with `;f`/`;foo` enabled): cursor at depth 2; longestMatch=`;f`; descendants → pendingMatch=`;f` set in match-found branch. ✓
- `;fo` typed (continuation): cursor advances to depth 3; max depth 3 > pending.triggerLength 2 → drop pending. longestMatch=nil; hasActiveCursorsWithPotential=true → return nil. Buffer stays empty of pending; timer next reschedule sees `pendingMatch==nil` and cancels. ✓
- Pause at `;fo`: nothing fires. Field literal stays `;fo`. ✓
- Continue with another `o` to `;foo`: cursor advances to depth 4; matches `;foo`; no descendants → fires immediately. ✓
- Backspace from `;fo`: `rebuildAfterBackspace` re-feeds `;` then `f` from buffer → re-establishes pending=`;f`; reschedule arms timer. ✓
- Chained `;ff` (with `;f`/`;ff`/`;ffoo`): on second `f`, cursor advances to depth 3; max depth 3 > pending.triggerLength 2 → drop old `;f` pending. THEN match-found branch sets new pending=`;ff` (depth 3). ✓
- `;f` alone, pause: cursor at depth 2; pending=`;f` set; no further keystrokes; timer fires `;f`. ✓
- `;f<space>`: space doesn't extend any cursor; new root cursor fails too; cursors=[]; max depth=0 ≤ pending.triggerLength=2 → don't drop pending. The existing bottom-of-`feed` `!hasActiveCursorsWithPotential && pending` branch fires the pending immediately and returns the match. ✓

### 6.2 Delete `TriggerConflict.swift`

`rm Sources/Sokki/Models/TriggerConflict.swift`.

### 6.3 `MatchStore` — strip conflict state

```swift
@Observable
@MainActor
final class MatchStore {
    var matches: [Match] = []   // remove the `didSet` block entirely
    var config: AppConfig = AppConfig()
    var lastError: String?
    // remove: private(set) var conflictsByMatchId: [UUID: ConflictReport] = [:]
    ...
}
```

### 6.4 `MatchListView` — strip badge

- Remove `conflict: ConflictReport?` from `MatchRow`.
- Remove the warning-icon `if let c = conflict, !c.isEmpty { ... }` block in `MatchRow.body`.
- Remove `conflictTooltip(_:)` method.
- At call site: `MatchRow(match: match)` — drop the `conflict:` argument.
- `MatchEditorView` invocations: drop `allMatches: store.matches` from both call sites at `:90` and `:102`.

**Also `PaletteController.swift:194`:** the palette's editor sheet also constructs `MatchEditorView(...)` and passes `allMatches: matchStore.matches`. Drop the `allMatches:` argument here too. AC10 (clean build) will fail without this edit — `MatchEditorView`'s init signature loses the parameter.

### 6.5 `MatchEditorView` — strip caption

- Remove `@State private var triggerConflict`.
- Remove `private let allMatches: [Match]` and the corresponding init parameter + assignment.
- Remove the `if !triggerConflict.isEmpty { ... }` block in the Form body.
- Remove `editorConflictMessages(_:)` and `formatPrefixLine(_:singular:plural:)`.
- Remove `.onChange(of: trigger, initial: true) { ... }` modifier.

### 6.6 Edge cases

- **Live-edit of a trigger inside `MatchEditorView`** — the existing `.onChange(of: prompt)` and `.onChange(of: isAI)` modifiers (which clear `promptError`) remain unchanged.
- **`MatchListView` and `PaletteController` call sites** — the consumers of `allMatches` and `conflictsByMatchId` are the editor sheet (both `MatchListView` and `PaletteController`) and `MatchRow`. Removing them does not orphan any other data dependency.
- **Build hygiene** — deleting `TriggerConflict.swift` requires removing every `import`-free reference (`ConflictReport`, `TriggerConflict`, `TriggerConflictKind`, `TriggerConflictDetector`); a project-wide grep after the edits will flag any leftover.
- **Existing pending-fire paths are intentionally retained.** `flushPending()` (timer path) and the `!hasActiveCursorsWithPotential && pending` branch in `feed` are unchanged and still required. The new depth check ONLY handles the case where a cursor advanced THROUGH the pending boundary — not the case where typing kills all cursors at-or-before that boundary (handled by the existing branch) and not the case where the timer fires (handled by `flushPending`).

## 7. Files Changed

- `Sources/Sokki/Engine/TrieMatcher.swift` — 4-line `pendingMatch` drop based on cursor depth, immediately after `cursors = newCursors`.
- `Sources/Sokki/Models/TriggerConflict.swift` — **deleted**.
- `Sources/Sokki/Models/MatchStore.swift` — remove `conflictsByMatchId` + the `didSet` on `matches`.
- `Sources/Sokki/Views/MatchListView.swift` — strip `MatchRow.conflict`, the badge view, `conflictTooltip`, and `allMatches:` arguments to `MatchEditorView`.
- `Sources/Sokki/Views/MatchEditorView.swift` — strip `triggerConflict` state, `allMatches` property + init param, conflict caption view, `editorConflictMessages`, `formatPrefixLine`, `.onChange(of: trigger)` recompute.
- `Sources/Sokki/Services/PaletteController.swift` — drop the `allMatches:` argument from the `MatchEditorView(...)` call at `:194`.

## 8. Verification

Manual checklist mapped to acceptance criteria:

1. **[AC1]** Define `;f → foo` and `;foo → fooooobar`. Type `;fo`. Pause 1 s. Confirm field stays literally `;fo`; no expansion. Confirm no HUD `matched` event for `;f`.
2. **[AC2]** Continuing from above, type one more `o` (so the field reads `;foo`). Confirm `;foo` expands to `fooooobar` immediately on the second `o`.
3. **[AC3]** Type `;f` only. Pause 1 s. Confirm `;f` expands to `foo`.
4. **[AC4]** Type `;f<space>`. Confirm `;f` expands to `foo` immediately on the space (existing behavior).
5. **[AC5]** Define `;f → A`, `;ff → B`, `;ffoo → C`. Type `;ff`. Pause 1 s. Confirm `B` is injected (not `A`).
6. **[AC6]** `git status` shows `Sources/Sokki/Models/TriggerConflict.swift` deleted. `grep -r ConflictReport Sources/` returns no matches.
7. **[AC7]** Open Settings → Matches. Define two conflicting triggers (`;f`, `;foo`). Confirm no warning icon appears on either row.
8. **[AC8]** Open the editor for either match. Confirm no orange/red caption below the trigger field.
9. **[AC9]** `grep conflictsByMatchId Sources/` returns no matches.
10. **[AC10]** `swift build -c release` exits 0 with no errors or warnings.
