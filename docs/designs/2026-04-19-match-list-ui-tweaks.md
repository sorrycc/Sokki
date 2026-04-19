# Match List UI Tweaks

## 1. Background

Two small UI papercuts in the Settings → Matches tab:

1. The add (+) and remove (−) buttons at the bottom of the list render at visibly different widths because the SF Symbol glyphs `plus` and `minus` have different intrinsic widths and the buttons use SwiftUI's default sizing.
2. Users can edit a match via the row's right-click context menu, but the more natural gesture — double-clicking the row — does nothing.

## 2. Requirements Summary

**Goal:** Polish the match list so the footer buttons are visually symmetric and the row's edit action is reachable via **both** double-click and Return on the selected row.

**Scope:**
- In scope: `Sources/Sokki/Views/MatchListView.swift` only.
- Out of scope: button restyling beyond size parity, context menu changes, selection model changes, `MatchEditorView`/`MatchStore` changes.

**Requirement evolution:** The original requirement phrased the edit trigger as "double-click the row." Implementation trials of `.simultaneousGesture`, `.onTapGesture(count: 2)`, `.contentShape(Rectangle())`, and layered count-1 + count-2 tap gestures each introduced a different defect — either breaking single-click selection outright, breaking selection only on opaque subviews, or imposing the system double-click window (~250 ms) as a visible latency on every text single-click. The latency is *inherent* to any design that relies on SwiftUI's `TapGesture(count: 2)` detection, because SwiftUI must wait out the possible second click before letting the first one resolve. The chosen design sidesteps this entirely: a single `count: 1` tap gesture fires immediately on every click, and double-click is detected manually by comparing the current click's `Date` against the previous click's `Date` + row id. Return on the selected row is added as a parallel, keyboard-idiomatic trigger.

**Key decisions** (with project-grounded evidence):
- No existing `.bordered`/`.borderless` convention in the Views folder; keep plain defaults.
- macOS 14+ target (`Package.swift`) makes `TapGesture(count: 2)` + `.simultaneousGesture` fully available.
- `List(selection: $selectedMatch)` + per-row `.contextMenu` must keep working after the gesture is added.

## 3. Acceptance Criteria

1. **AC1 — Button symmetry.** The + and − buttons render at identical width and height at runtime. Icons stay centered. Disabled state on − still reads visually (greyed icon). Tooltips "Add match" / "Remove match" still appear on hover.
2. **AC2a — Double-click opens editor.** Double-clicking any row (two clicks within 0.5 s on the same row) opens the edit sheet pre-populated with that match's values, with no perceptible single-click latency.
3. **AC2b — Return opens editor.** With a row selected, pressing Return opens the same edit sheet. Return with no selection is a no-op.
4. **AC3 — Existing interactions unbroken.** Single-click selects the row (enabling the − button) with zero added latency. Right-click still shows the existing context menu with its Edit / Delete / Enable-Disable entries.
5. **AC4 — Disabled rows editable via double-click and Return.** Parity with context-menu Edit, which currently works on disabled rows too.
6. **AC5 — Build passes.** `swift build` succeeds without new warnings.

## 4. Problem Analysis

Current state, `MatchListView.swift`:

- Bottom bar (lines 37–66) is an `HStack` with two `Button`s wrapping `Image(systemName: "plus")` and `Image(systemName: "minus")`. No explicit frame → the buttons size to their label's intrinsic width, which differs between the two glyphs.
- `MatchRow` (lines 87–117) is displayed inside `ForEach` and has a `.contextMenu` with Edit/Delete/Disable actions, but no double-click handler. Double-clicking a row today does nothing.

Approaches for Change 1 (button sizing):
- **A — `.bordered` + `.controlSize(.small)`** → gives rounded-rect chips; introduces a styling convention that doesn't exist elsewhere in the app. Rejected (scope creep).
- **B — `.borderless` + fixed frame** → explicit style but functionally same as current default. Rejected (noise).
- **C — Keep default style, add matching `.frame(width: 20, height: 20)`** → one-line fix, preserves current look, fixes asymmetry. **Chosen.**

Approaches for Change 2 (edit trigger):
- **A — `.onTapGesture(count: 2)` on the row** → observed in practice: gesture recognizer on opaque subviews (`Text`, `Capsule`) consumes single clicks during the double-click detection window, breaking selection on the rows' visible content. Rejected (breaks AC3).
- **B — `.simultaneousGesture(TapGesture(count: 2))`** → initial pick; observed to intermittently suppress single-click selection because SwiftUI's recognizer observes mouse-down to detect a possible double-click and races `NSTableView`'s selection dispatch. Rejected after testing.
- **C — `.contentShape(Rectangle())` + `.onTapGesture(count: 2)`** → widens `MatchRow`'s hit region into zones List does not own; empty-area clicks bypass List entirely. Rejected after testing.
- **D — Layered `.onTapGesture(count: 2)` + `.onTapGesture(count: 1)` manually driving `selectedMatch`** → restores selection on text, but forces SwiftUI to wait the system double-click window before the count-1 gesture can fire, giving every text single-click a ~250 ms perceived lag. Rejected after testing.
- **E — Manual double-click detection via `Date` timestamps** (single count-1 gesture on each row, compare elapsed against the previous click on the same row id) → count-1 gesture fires immediately (no double-click window wait), single-click selection drives `selectedMatch` directly, second click on same row within 0.5 s opens editor. **Chosen** for the mouse path.
- **F — `.onKeyPress(.return)` on the List** → Return on the selected row opens the editor. Zero added click latency, zero interference, matches Finder/Mail/Xcode. **Chosen** for the keyboard path, layered alongside E.

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

**4. Edit trigger?**
- Options: A) `.onTapGesture(count: 2)` on row · B) `.simultaneousGesture(TapGesture(count: 2))` · C) Wrap row in `Button(.plain)` · D) Layered `count: 2` + `count: 1` taps · E) Manual timestamp-based double-click · F) `.onKeyPress(.return)` on the List
- Decision: **E + F together.** A/B/C were rejected in testing for breaking single-click selection; D worked but imposed a ~250 ms lag on every single-click (inherent to SwiftUI's `count: 2` arbitration window). E fires each `count: 1` gesture immediately and checks elapsed time against the previous click to decide "was this a double?" — no gesture-arbitration delay, reliable across opaque and empty row regions. F adds Return as a keyboard-idiomatic alternative. The two triggers are independent and coexist without conflict.

**5. Handler: inline vs. extracted helper?**
- Options: A) Inline closure at every call site · B) Extract `openEditor(for:)` helper
- Decision: **B)** — Three call sites now (double-click `handleTap`, Return `onKeyPress`, context-menu Edit) all need the same id-lookup + state mutation. A shared `openEditor(for match: Match)` helper removes the duplication and keeps each call site a single line. Previously picked A because there were only two call sites and the logic was one line; adding E pushed the count to three and the logic to two lines (id-lookup + sheet toggle), which is past the DRY threshold.

**6. `.contentShape(Rectangle())` on the row?**
- Options: A) Add it · B) Skip
- Decision: **A) Add it** — Without it, the `count: 1` `.onTapGesture` only fires on opaque subviews (`Text`, `Capsule`), so double-clicking the empty padding / `Spacer()` area of a row silently does nothing. Earlier iterations of this doc rejected `contentShape` on the theory that it broke selection, but that flakiness was driven by the paired `count: 2` gesture's arbitration window (since removed), not by `contentShape` itself. With the current count-1 tap + manual timestamp detection, `contentShape` simply widens the gesture's hit region — single-click still sets `selectedMatch` instantly via `handleTap`, double-click works everywhere on the row.

**7. Allow Return on disabled rows?**
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

### Change 2 — Double-click and Return to edit

Two parallel edit triggers share a single `openEditor(for:)` helper:

1. **Mouse — manual double-click detection** on each `MatchRow`. A single `count: 1` `.onTapGesture` fires on every click and dispatches to `handleTap(_:)`, which decides "select" vs. "open editor" by comparing the current click against the previous click's time + row id.
2. **Keyboard — Return on selected row** via `.onKeyPress(.return)` on the `List`.

```swift
struct MatchListView: View {
    @Bindable var store: MatchStore
    @State private var selectedMatch: Match?
    @State private var showingEditor = false
    @State private var editingMatch: Match?
    @State private var lastClickTime: Date?
    @State private var lastClickedId: UUID?

    private static let doubleClickInterval: TimeInterval = 0.5

    var body: some View {
        List(selection: $selectedMatch) {
            ForEach(store.matches) { match in
                MatchRow(match: match)
                    .tag(match)
                    .contentShape(Rectangle())
                    .onTapGesture { handleTap(match) }
                    .contextMenu {
                        Button("Edit") { openEditor(for: match) }
                        // Delete / Enable-Disable — unchanged
                    }
            }
        }
        .onKeyPress(.return) {
            guard let selected = selectedMatch else { return .ignored }
            openEditor(for: selected)
            return .handled
        }
        .onChange(of: showingEditor) { _, isShowing in
            if !isShowing { lastClickTime = nil; lastClickedId = nil }
        }
    }

    private func handleTap(_ match: Match) {
        let now = Date()
        if lastClickedId == match.id,
           let last = lastClickTime,
           now.timeIntervalSince(last) < Self.doubleClickInterval {
            openEditor(for: match)
            lastClickTime = nil
            lastClickedId = nil
        } else {
            selectedMatch = match
            lastClickTime = now
            lastClickedId = match.id
        }
    }

    private func openEditor(for match: Match) {
        editingMatch = store.matches.first(where: { $0.id == match.id }) ?? match
        showingEditor = true
    }
}
```

Design notes:
- Why a single `count: 1` tap instead of `count: 2`: SwiftUI's `TapGesture(count: 2)` must wait out the system double-click window before allowing a count-1 gesture to resolve, which imposes a visible ~250 ms lag on every single-click. A single count-1 tap fires immediately; comparing `Date`s against the stored `lastClickTime`/`lastClickedId` reproduces double-click detection without the arbitration delay.
- `0.5 s` is the classic macOS double-click threshold. Not reading `NSEvent.doubleClickInterval` (would require `AppKit` import in a pure SwiftUI view); 0.5 s is close enough for this affordance and avoids a hardcoded AppKit dependency.
- On editor dismiss, we clear `lastClickTime`/`lastClickedId` via `.onChange(of: showingEditor)` — otherwise a slow second click landing after the editor closes could be mistaken for "first click of a new double-click" and cause a surprise re-open on a different row clicked within 0.5 s of the close.
- `.contentShape(Rectangle())` broadens the row's hit region so clicks on the `Spacer()` padding also reach `handleTap`. Safe to use here (unlike earlier iterations) because we're no longer paired with a `count: 2` gesture that would starve List of single clicks — the count-1 tap fires immediately and sets `selectedMatch` itself.
- `.onKeyPress(.return)` is macOS 14+; project targets `.macOS(.v14)` so no availability guard is needed.
- `openEditor(for:)` looks the match up by id so the sheet always sees the freshest value even if `store.matches` mutated between click and handler.

The handler resolves the match the user clicked — specifically `match` captured from the `ForEach` closure, **not** `selectedMatch`. This guarantees double-clicking a non-selected row opens the editor for that row, not for whatever happens to be highlighted. Looking `current` up by `match.id` keeps the behavior robust if `store.matches` is mutated (e.g., by a disk-reload) between the first and second click of the double-click: the editor opens with the latest value, not a stale snapshot.

The handler mutates the same two pieces of state the context-menu Edit action mutates, so the existing sheet plumbing (lines 68–83) picks it up without further changes.

### Error handling / edge cases

- Return with no row selected → `.ignored` returned; no editor opens, no state mutation.
- Double-click on row A then single-click row B within 0.5 s → `lastClickedId != B.id`, so the `else` branch runs: row B selects, timestamp + id update, no editor. Correct.
- Second click on row A after > 0.5 s → elapsed check fails, `else` branch runs, new timestamp recorded, no editor opens. Correct.
- Disabled rows (`match.isEnabled == false`) behave identically to enabled rows under both triggers — matches context-menu Edit behavior.
- Stale selection / row: `openEditor(for:)` looks up by id, so the sheet always reflects the latest `Match` value; fallback to the captured value if the row was deleted mid-flow.
- Editor-close state reset: `.onChange(of: showingEditor)` clears `lastClickTime`/`lastClickedId` on dismiss so the next click after closing always starts fresh (no accidental double-click carry-over).
- Text-field focus: when the editor sheet is open, Return inside its text fields is handled by the sheet's own form controls, not by `.onKeyPress(.return)` on the background List (the sheet sits above it in the responder chain).

### Testing strategy

No unit tests — project has no test target. Verification is manual against the acceptance criteria.

## 7. Files Changed

- `Sources/Sokki/Views/MatchListView.swift` — add matching `.frame(width: 22, height: 22)` to `plus`/`minus` `Image`s; add `lastClickTime`/`lastClickedId` state + `handleTap(_:)` + `openEditor(for:)` helpers; attach `.contentShape(Rectangle())` + a single `count: 1` `.onTapGesture` to each `MatchRow`; add `.onKeyPress(.return)` to the List; add `.onChange(of: showingEditor)` to reset double-click state on sheet dismiss; route the context-menu Edit button through `openEditor(for:)`.

## 8. Verification

1. **[AC1]** Build and run; open Settings → Matches. Confirm + and − buttons are the same visible size. Hover each to confirm tooltips still work. Deselect any row and confirm − is still visibly disabled.
2. **[AC2a]** Double-click any row (two clicks within ~0.5 s on the same row, including on text or the "Disabled" capsule) → the match editor sheet opens pre-populated. Single-clicks between those two clicks that land on *different* rows must not open the editor.
3. **[AC2b]** Click a row to select it, then press Return → the same match editor sheet opens.
4. **[AC3]** Single-click anywhere on a row (text, padding, capsule) → row selects instantly with no perceptible delay; − button enables. Right-click a row → context menu shows Edit/Delete/Enable-Disable as before.
5. **[AC4]** Create a disabled match via the context menu's "Disable"; double-click or Return on it → editor opens.
6. **[AC5]** `swift build` reports no new warnings and succeeds.
