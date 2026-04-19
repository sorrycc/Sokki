# Match List UI Tweaks

## 1. Background

Two small UI papercuts in the Settings → Matches tab:

1. The add (+) and remove (−) buttons at the bottom of the list render at visibly different widths because the SF Symbol glyphs `plus` and `minus` have different intrinsic widths and the buttons use SwiftUI's default sizing.
2. Users can edit a match via the row's right-click context menu, but the more natural gesture — double-clicking the row — does nothing.

## 2. Requirements Summary

**Goal:** Polish the match list so the footer buttons are visually symmetric and double-clicking a row opens the match editor.

**Scope:**
- In scope: `Sources/Sokki/Views/MatchListView.swift` only.
- Out of scope: button restyling beyond size parity, context menu changes, selection model changes, `MatchEditorView`/`MatchStore` changes.

**Key decisions** (with project-grounded evidence):
- No existing `.bordered`/`.borderless` convention in the Views folder; keep plain defaults.
- macOS 14+ target (`Package.swift`) makes `TapGesture(count: 2)` + `.simultaneousGesture` fully available.
- `List(selection: $selectedMatch)` + per-row `.contextMenu` must keep working after the gesture is added.

## 3. Acceptance Criteria

1. **AC1 — Button symmetry.** The + and − buttons render at identical width and height at runtime. Icons stay centered. Disabled state on − still reads visually (greyed icon). Tooltips "Add match" / "Remove match" still appear on hover.
2. **AC2 — Double-click opens editor.** Double-clicking any `MatchRow` opens the edit sheet pre-populated with that match's values.
3. **AC3 — Existing interactions unbroken.** Single-click still selects the row (enabling the − button). Right-click still shows the existing context menu with its Edit / Delete / Enable-Disable entries.
4. **AC4 — Disabled rows editable via double-click.** Parity with context-menu Edit, which currently works on disabled rows too.
5. **AC5 — Build passes.** `swift build` succeeds without new warnings.

## 4. Problem Analysis

Current state, `MatchListView.swift`:

- Bottom bar (lines 37–66) is an `HStack` with two `Button`s wrapping `Image(systemName: "plus")` and `Image(systemName: "minus")`. No explicit frame → the buttons size to their label's intrinsic width, which differs between the two glyphs.
- `MatchRow` (lines 87–117) is displayed inside `ForEach` and has a `.contextMenu` with Edit/Delete/Disable actions, but no double-click handler. Double-clicking a row today does nothing.

Approaches for Change 1 (button sizing):
- **A — `.bordered` + `.controlSize(.small)`** → gives rounded-rect chips; introduces a styling convention that doesn't exist elsewhere in the app. Rejected (scope creep).
- **B — `.borderless` + fixed frame** → explicit style but functionally same as current default. Rejected (noise).
- **C — Keep default style, add matching `.frame(width: 20, height: 20)`** → one-line fix, preserves current look, fixes asymmetry. **Chosen.**

Approaches for Change 2 (double-click edit):
- **A — `.onTapGesture(count: 2)` on the row** → swallows single-clicks in the List's selection handling. Rejected (breaks AC3).
- **B — `.simultaneousGesture(TapGesture(count: 2))` on the row** → runs alongside List's built-in recognizer; selection and context menu keep working. **Chosen.**
- **C — Wrap row in `Button(.plain)` + gesture** → requires reworking List's selection binding. Rejected (over-engineered; violates YAGNI).

## 5. Decision Log

**1. Button style for equalizing sizing?**
- Options: A) `.bordered` + `controlSize` · B) `.borderless` + frame · C) Keep defaults + matching frame
- Decision: **C)** — Preserves existing plain-default look used across the Views folder; minimal diff.

**2. Frame size?**
- Options: A) 20×20 · B) 22×22 · C) 24×24
- Decision: **B) 22×22** — Matches the approximate current rendered size of the default `Button` + SF Symbol pairing, so fixing asymmetry does not also shrink the controls.

**3. `.contentShape(Rectangle())` on the buttons?**
- Options: A) Add it · B) Skip
- Decision: **B) Skip** — `Button` with an `Image` label already has a full-frame hit area. YAGNI.

**4. Double-click gesture attachment?**
- Options: A) `.onTapGesture(count: 2)` · B) `.simultaneousGesture(TapGesture(count: 2))` · C) Wrap row in `Button(.plain)`
- Decision: **A) `.onTapGesture(count: 2)`** — Initial pick was B on the theory that `.simultaneousGesture` protects List selection, but real-world testing on macOS 14 showed B intermittently suppresses single-click selection because SwiftUI's gesture recognizer observes the first mouse-down to detect a possible double-click, racing the `NSTableView`-backed selection. `.onTapGesture(count: 2)` only fires on the second click, so single clicks fall through cleanly. C is still rejected (fights List selection binding).

**5. Handler: inline vs. extracted helper?**
- Options: A) Inline closure (duplicated with context-menu Edit) · B) Extract `openEditor(for:)` helper
- Decision: **A)** — Two call sites, two lines each. Extraction is premature; can refactor later if a third call site appears.

**6. `.contentShape(Rectangle())` on the row?**
- Options: A) Add it · B) Skip
- Decision: **B) Skip** — Initial pick was A, but in practice `.contentShape(Rectangle())` widens `MatchRow`'s hit region into zones the underlying `NSTableView`-backed List does not own, so clicks in the expanded area bypass List's selection dispatch and selection fails intermittently. Removing it restores native selection behavior; the cost is that double-click only fires on opaque subviews (`Text`, `Capsule`) — acceptable trade-off because those cover the visually meaningful content of the row.

**7. Allow double-click on disabled rows?**
- Options: A) Yes (parity with context menu) · B) Skip disabled rows
- Decision: **A)** — Context menu's Edit already works on disabled rows; keep behavior consistent.

## 6. Design

### Change 1 — Footer button sizing

In the bottom `HStack` (around lines 38–55), add `.frame(width: 22, height: 22)` to each `Image(systemName: …)` *inside* the two `Button`s. The frame MUST sit on the `Image` (the button's label), not on the `Button` itself: the default `Button` chrome sizes itself around its label plus symmetric padding, so equal label sizes → equal chrome sizes. Putting the frame on the `Button` instead would leave the narrower glyph centered in a fixed box, reintroducing visual asymmetry once the chrome's inner padding is computed.

```swift
Button(action: { editingMatch = nil; showingEditor = true }) {
    Image(systemName: "plus")
        .frame(width: 22, height: 22)
}
.help("Add match")

Button(action: {
    if let match = selectedMatch { store.remove(match); selectedMatch = nil }
}) {
    Image(systemName: "minus")
        .frame(width: 22, height: 22)
}
.disabled(selectedMatch == nil)
.help("Remove match")
```

### Change 2 — Double-click row to edit

Inside the `ForEach` in `MatchListView.body` (around lines 12–30), attach `.contentShape(Rectangle())` (for a reliable full-row hit area) and `.simultaneousGesture(TapGesture(count: 2).onEnded { ... })` to `MatchRow(match:)`:

```swift
ForEach(store.matches) { match in
    MatchRow(match: match)
        .tag(match)
        .onTapGesture(count: 2) {
            let current = store.matches.first(where: { $0.id == match.id }) ?? match
            editingMatch = current
            showingEditor = true
        }
        .onTapGesture {
            selectedMatch = match
        }
        .contextMenu {
            Button("Edit") {
                editingMatch = match
                showingEditor = true
            }
            // ...unchanged
        }
}
```

The layered taps are deliberate. `.onTapGesture(count: 2)` installs a recognizer on `MatchRow`'s opaque subviews (`Text`, `Capsule`); left on its own, this recognizer consumes single clicks on those pixels, so List's native selection never fires and the row appears unclickable in exactly the visible regions users care about. Adding a second `.onTapGesture` (count defaults to 1) that manually sets `selectedMatch = match` restores selection for those pixels; SwiftUI prefers the higher-count gesture and falls back to count 1 when no second click arrives within the system's double-click window. Clicks in the transparent right-side padding continue to reach List directly (we intentionally do not add `.contentShape(Rectangle())` back — see Decision 6).

The handler resolves the match the user clicked — specifically `match` captured from the `ForEach` closure, **not** `selectedMatch`. This guarantees double-clicking a non-selected row opens the editor for that row, not for whatever happens to be highlighted. Looking `current` up by `match.id` keeps the behavior robust if `store.matches` is mutated (e.g., by a disk-reload) between the first and second click of the double-click: the editor opens with the latest value, not a stale snapshot.

The handler mutates the same two pieces of state the context-menu Edit action mutates, so the existing sheet plumbing (lines 68–83) picks it up without further changes.

### Error handling / edge cases

- Double-click fires only on `.onEnded`, so rapid triple-clicks cannot open two sheets back-to-back — once `showingEditor` is true, the sheet is already modal and the gesture is consumed.
- `selectedMatch` binding is unaffected; the `simultaneousGesture` does not claim the single-click path.
- Disabled rows (`match.isEnabled == false`) still receive the gesture — intentional, mirrors context-menu behavior.
- Stale capture: if `store.matches` mutates between clicks, the id-lookup above picks up the current value; fallback to the captured `match` keeps behavior sane if the row was deleted mid-click.
- Swift 6 isolation: the project builds under Swift 6 language mode (`Package.swift`). The closure captures `store` (`@Bindable` → `@Observable`) and view state on the main actor; this is expected to compile cleanly, but verify with `swift build` in Phase 5 and add `@MainActor` annotations only if a warning actually appears.

### Testing strategy

No unit tests — project has no test target. Verification is manual against the acceptance criteria.

## 7. Files Changed

- `Sources/Sokki/Views/MatchListView.swift` — add matching `.frame(width: 20, height: 20)` to `plus`/`minus` `Image`s; add `.contentShape(Rectangle())` + `.simultaneousGesture` to `MatchRow` in the `ForEach`.

## 8. Verification

1. **[AC1]** Build and run; open Settings → Matches. Confirm + and − buttons are the same visible size. Hover each to confirm tooltips still work. Deselect any row and confirm − is still visibly disabled.
2. **[AC2]** Double-click any row; the match editor sheet opens with the row's trigger/replace populated.
3. **[AC3]** Single-click a row → row highlights; − button enables. Right-click a row → context menu shows Edit/Delete/Enable-Disable as before.
4. **[AC4]** Create a disabled match via the context menu's "Disable"; double-click it → editor opens.
5. **[AC5]** `swift build` reports no new warnings and succeeds.
