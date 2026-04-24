# Settings Search Keyboard UX

## 1. Background

The Matches tab of the Settings window has a search input (focusable via Cmd+F) and a selectable list of matches. Keyboard navigation between them is currently incomplete:

- Esc does nothing while the search is focused — user must click elsewhere or Tab out.
- Up/Down arrows do not move list selection while typing — user must move focus to the list (no dedicated shortcut) or use the mouse.

The Spotlight-style `PaletteView` already implements exactly the desired UX (Esc to dismiss, Up/Down to navigate while search stays focused, Enter to commit). We port that pattern into `MatchListView`.

## 2. Requirements Summary

**Goal:** Make the Settings matches pane's search input fully keyboard-navigable.

**Scope:**
- In scope: `MatchListView` search + list interaction.
- Out of scope: Other Settings tabs, Palette window, global shortcuts.

**Key decisions (from Phase 1 discovery):**
1. Pattern — mirror `PaletteView` (`.onKeyPress` + `@FocusState`). No `NSEvent` monitors.
2. Esc — two-step: non-empty clears query; empty unfocuses to list.
3. Up/Down — move selection while keeping search focus.
4. Enter — open editor for selected match.
5. Selection type — keep `selectedMatch: Match?` (no refactor to UUID).
6. Clamp selection at list bounds (no wraparound), matching Palette.
7. Auto-scroll selection into view with `ScrollViewReader`.

## 3. Acceptance Criteria

1. Cmd+F focuses the search input (existing behavior; verify unchanged).
2. Esc in search with non-empty query clears the query; focus stays in search.
3. Esc in search with empty query moves focus to the list.
4. Down in focused search moves selection to next match; stops at last.
5. Up in focused search moves selection to previous match; stops at first.
6. With no selection, Down selects first; Up selects last.
7. Enter in focused search opens the match editor for the selected match.
8. Selected row scrolls into view while navigating.
9. Existing shortcuts (list Return/Delete, Cmd+N add, Cmd+F focus search) keep working.

## 4. Problem Analysis

Existing state (`Sources/Sokki/Views/MatchListView.swift`):
- Line 5: `@State private var selectedMatch: Match?`
- Line 9: `@State private var searchQuery: String = ""`
- Line 12: `@FocusState private var focus: Focus?` with `enum Focus { case list, search }`
- Lines 32–34: `pruneSelection()` already fires on `fileFilter`, `searchQuery`, and `store.matches` changes — stale-selection cleanup is already in place.
- Lines 74–80: hidden Button with `.keyboardShortcut("f", modifiers: .command)` setting `focus = .search`.
- Line 153: `TextField("Search", text: $searchQuery).focused($focus, equals: .search)` — no key handlers.
- Lines 162–190: `List(selection: $selectedMatch) { ForEach(filteredMatches) { MatchRow(...).tag(match) ... } }`.
- Line 192: list has `.focused($focus, equals: .list)` — already in focus chain, so programmatic `focus = .list` lands without async dispatch.
- Lines 193–205: `.onKeyPress(.return)` / `.onKeyPress(.delete)` gated on `focus == .list`.
- Lines 242–251: `filteredMatches: [Match]` — indexable.
- Lines 278–291: `handleTap(_:)` double-click shim uses `lastClickedId` + `lastClickTime`. Keyboard navigation bypasses it — pre-existing quirk (existing `handleTap` also writes `selectedMatch = match` redundantly with the List binding), not made worse by this design, but see item 5 below.

Reference (`Sources/Sokki/Views/PaletteView.swift:24–34, 67–70, 84–97, 241–247`): Palette uses `selection: UUID?`, `.tag(entry.match.id)`, `.id(entry.match.id)`, and `proxy.scrollTo(entry.match.id, anchor: .center)` — all consistently keyed by UUID. Palette wraps its initial `searchFocused = true` in `DispatchQueue.main.async` because the NSPanel needs a runloop turn to claim first responder; the Settings `Window` does not need this because the list is already mounted and focusable when Esc fires.

**Chosen approach** — graft Palette's keyboard logic onto the existing `MatchListView` TextField and wrap the `List` in a `ScrollViewReader`. Keep `selectedMatch: Match?` (no refactor). Use `Match.id` (UUID) as the scroll target by adding an explicit `.id(match.id)` to each row — matches Palette's belt-and-suspenders pairing.

Selection-type invariant: `List(selection:)` tracks the binding by tag hash. `Match` is `Hashable` (see `Models/Match.swift`). The hash is value-based (all fields participate), so if a field other than `id` changes (e.g., `isEnabled` toggled from the context menu), the tag identity of that row changes and `List` may desync the highlight. This is a pre-existing risk; if it surfaces in practice, migrating `selectedMatch` to `UUID?` (like Palette) is the fix. Out of scope for this change.

## 5. Decision Log

**1. How does Esc behave in focused search?**
- Options: A) Always unfocus · B) Always clear + unfocus · C) Two-step (clear if non-empty, else unfocus)
- Decision: **C)** — matches Spotlight/Xcode. User-confirmed.

**2. Do Up/Down move focus into the list or just move selection?**
- Options: A) Transfer focus · B) Move selection while search stays focused
- Decision: **B)** — user keeps typing flow. Matches Palette. User-confirmed.

**3. Does Enter in search open the selected match?**
- Options: A) No-op · B) Open editor for selection
- Decision: **B)** — consistent with list's existing Return handler. User-confirmed.

**4. Initial selection when Up/Down pressed with nothing selected?**
- Options: A) Down→first, Up→last · B) Both→first · C) Require selection
- Decision: **A)** — standard macOS list feel.

**5. Selection persistence across filtering?**
- Options: A) Keep if still present, else clear · B) Reset to first on every filter change · C) Do nothing (may reference missing row)
- Decision: **A)** — via `.onChange(searchQuery)` hook.

**6. Where does focus go on two-step Esc?**
- Options: A) `.list` · B) `nil`
- Decision: **A)** — enables list Return/Delete immediately.

**7. Selection type refactor?**
- Options: A) Keep `Match?` · B) Switch to `UUID?` like Palette
- Decision: **A)** — YAGNI; current code works and whole-struct selection reads fine.

**8. Extract shared `moveSelection` helper?**
- Options: A) Duplicate · B) Extract
- Decision: **A)** — ~6 LOC, different selection types; abstraction is premature.

## 6. Design

### Changes to `MatchListView.swift`

**a) Add key handlers to the search TextField (around line 153):**

```swift
TextField("Search", text: $searchQuery)
    .textFieldStyle(.roundedBorder)
    .focused($focus, equals: .search)
    .onKeyPress(.upArrow) {
        moveSelection(-1)
        return .handled
    }
    .onKeyPress(.downArrow) {
        moveSelection(+1)
        return .handled
    }
    .onKeyPress(.return) {
        guard let selected = selectedMatch else { return .ignored }
        openEditor(for: selected)
        return .handled
    }
    .onKeyPress(.escape) {
        if !searchQuery.isEmpty {
            searchQuery = ""
            return .handled
        }
        focus = filteredMatches.isEmpty ? nil : .list
        return .handled
    }
```

**b) Add `moveSelection(_:)` helper. Also reset the `handleTap` shim's tracking state so keyboard selection can't be misread as a double-click:**

```swift
private func moveSelection(_ delta: Int) {
    let items = filteredMatches
    guard !items.isEmpty else { return }
    let current = items.firstIndex(where: { $0.id == selectedMatch?.id })
    let next: Int
    if let current {
        next = max(0, min(items.count - 1, current + delta))
    } else {
        next = delta > 0 ? 0 : items.count - 1
    }
    selectedMatch = items[next]
    lastClickTime = nil
    lastClickedId = nil
}
```

**c) Wrap the `List` in `ScrollViewReader` and scroll on selection change. Add explicit `.id(match.id)` to each row (matches Palette):**

```swift
ScrollViewReader { proxy in
    List(selection: $selectedMatch) {
        ForEach(filteredMatches) { match in
            MatchRow(match: match, conflict: store.conflictsByMatchId[match.id])
                .tag(match)
                .id(match.id)
                .contentShape(Rectangle())
                .onTapGesture { handleTap(match) }
                .contextMenu { ... unchanged ... }
        }
        .onDelete { ... unchanged ... }
    }
    .listStyle(.inset(alternatesRowBackgrounds: true))
    .focused($focus, equals: .list)
    .onKeyPress(.return) { ... unchanged ... }
    .onKeyPress(.delete) { ... unchanged ... }
    .onKeyPress(.deleteForward) { ... unchanged ... }
    .onChange(of: selectedMatch) { _, new in
        guard let id = new?.id else { return }
        withAnimation(.easeInOut(duration: 0.12)) {
            proxy.scrollTo(id, anchor: .center)
        }
    }
}
```

The tag remains `.tag(match)` (whole `Match`) to preserve `selectedMatch: Match?` binding. The scroll anchor is `Match.id` via explicit `.id(match.id)` — Palette pairs both, and we follow suit.

**d) Stale-selection cleanup — no code change.**

`pruneSelection()` at `MatchListView.swift:267–270` already fires on `searchQuery` change (line 33). The initial draft proposed adding a duplicate `onChange(searchQuery)` hook — dropped on reviewer feedback. No new code here.

### Non-changes

- List's existing `.onKeyPress(.return)` and `.onKeyPress(.delete)` handlers stay (gated on `focus == .list`).
- Cmd+F hidden button stays.
- Cmd+N add shortcut stays.
- `filteredMatches` computation stays.

### Error handling / edge cases

- **Empty `filteredMatches` + Up/Down** → no-op (early return in `moveSelection`).
- **Selected match filtered out by typing** → `pruneSelection()` (line 267) already clears it; next Up/Down falls through `current == nil` branch and re-seeds (Down→first, Up→last). This accidentally but deliberately satisfies AC6 even for stale selections — don't remove the `else` branch.
- **Enter in search with no selection** → `.onKeyPress(.return)` returns `.ignored`; no `onSubmit` is registered on the TextField, so Return is inert (does not cause any default action).
- **IME / CJK composition** → macOS input methods consume Return while a composition is open; SwiftUI's `.onKeyPress(.return)` only fires after commit. Safe for CJK users.
- **Focus-claim timing on two-step Esc** → `focus = .list` is synchronous. When `filteredMatches` is non-empty, `listBody` is mounted with `.focused($focus, equals: .list)` (line 192) and accepts focus immediately — no `DispatchQueue.main.async` needed. (Palette uses async dispatch only because its initial focus claim fires from `onAppear` on an `NSPanel`, before the responder chain is set up — different situation.)
- **Empty-results Esc** → when `filteredMatches.isEmpty`, `content` renders `ContentUnavailableView` instead of `listBody`, so no view is bound to `focus = .list`. The Esc handler uses `focus = filteredMatches.isEmpty ? nil : .list` to degrade gracefully (blurs the search field rather than sending focus nowhere). AC3 is satisfied in the common case; the empty case blurs search (defensible: no list to navigate to).
- **handleTap double-click shim interaction** → `moveSelection` should also reset `lastClickedId = nil` / `lastClickTime = nil`. Otherwise, if the user keyboard-navigates to a row and then clicks it within 0.5s of a prior click on the same row id, the click is misread as a double-click. Low probability but cheap to fix.

## 7. Files Changed

- `Sources/Sokki/Views/MatchListView.swift` — add four `.onKeyPress` handlers (up/down/return/escape) to the search TextField; add `moveSelection(_:)` helper (also clears `lastClickedId`/`lastClickTime`); wrap `listBody`'s `List` in a `ScrollViewReader` and add `.onChange(selectedMatch)` scroll; add `.id(match.id)` to each row. No new `onChange(searchQuery)` hook — existing `pruneSelection()` already covers it.

## 8. Verification

Build + manual smoke test (no test target exists):

1. [AC1] `swift build` → open Settings → Matches tab → press Cmd+F → search field shows focus ring.
2. [AC2] Type `abc` → press Esc → query is `""`, search still focused.
3. [AC3] Press Esc with empty query → focus ring moves to list (blue row highlight); Return opens selected match.
4. [AC4] Cmd+F → Down repeatedly → selection advances; halts at last row.
5. [AC5] Up repeatedly → selection retreats; halts at first row.
6. [AC6] Cmd+F (with nothing selected) → Down → first row selected; Cmd+F → Up → last row selected.
7. [AC7] Cmd+F → Down → Enter → `MatchEditorView` sheet opens for that match.
8. [AC8] With many matches, navigate past visible area → selected row scrolls into view.
9. [AC9] Regression: Cmd+N opens new-match editor; list-focused Return opens selection; list-focused Delete prompts confirm; Cmd+F re-focuses search.
