# Confirm Before Match Delete

## 1. Background

Today the match list in Settings offers three destructive paths that are one click/key away from permanent deletion with no undo:

1. Context menu **Delete** on a row (`MatchListView.swift:27`) — `store.remove(match)`.
2. Bottom-bar minus (**−**) button (`MatchListView.swift:61`) — `store.remove(match)`.
3. Swipe-delete / Delete-key via `List.onDelete` (`MatchListView.swift:38`) — `store.remove(at: offsets)`.

There is no undo stack (`MatchStore` only has `add` / `update` / `remove*`), no snapshot, and no backup surfaced in-app. A single mis-click on the minus button or a stray right-click + hit on "Delete" permanently wipes a match from `~/.config/sokki/match/*.yml`. The user has asked to add a confirmation step.

## 2. Requirements Summary

**Goal:** Add a native macOS confirmation alert before destructive match deletions from the context menu **Delete** action and the bottom-bar **−** button in `MatchListView.swift`.

**Scope:**
- **In scope:**
  - New confirmation alert on the two deliberate-click paths.
  - Single shared `.alert` modifier driven by `@State private var pendingDeletion: Match?`.
  - Alert copy per Apple HIG: generic title, trigger in body, `Cancel` default + `Delete` destructive.
- **Out of scope:**
  - Confirmation on `.onDelete` (Delete-key path) — stays instant, matches Finder/Mail convention.
  - Undo stack, Cmd-Z, undo toast.
  - "Don't ask again" preference, `AppConfig` changes, Settings UI toggle.
  - Per-row inline confirmation UI.
  - Replacement-text preview.

## 3. Acceptance Criteria

1. **AC1 — Context menu Delete confirms.** Right-click a row → **Delete** opens a native alert; `MatchStore` is unchanged until the user clicks the alert's **Delete** button.
2. **AC2 — Minus button confirms.** With a row selected, clicking **−** opens the same alert; `MatchStore` is unchanged until confirmed.
3. **AC3 — Delete key stays instant.** Selecting a row and pressing **Delete** (the `.onDelete` path) removes the match immediately, no alert.
4. **AC4 — Alert copy.** Title is `Delete Match?`. Body is `The match "<trigger>" will be permanently deleted. This action cannot be undone.`, where `<trigger>` is the pending match's `trigger` string.
5. **AC5 — Alert buttons.** Two buttons: `Cancel` (default, `.cancel` role) and `Delete` (`.destructive` role).
6. **AC6 — Keyboard.** Escape cancels. Return activates the default (= `Cancel`) → no deletion.
7. **AC7 — Cancel path.** `Cancel` dismisses the alert and clears `pendingDeletion`; `MatchStore` unchanged.
8. **AC8 — Confirm path.** `Delete` calls `store.remove(match)` for the pending match, then clears `pendingDeletion`. If the deleted match was the selected row, `selectedMatch` is cleared too (entry-point agnostic — same check for both context menu and minus paths; supersedes the narrower clear at `MatchListView.swift:62`).
9. **AC9 — Stale pending match.** If an external file change reloads `MatchStore` (via FSEvents, see `AppDelegate.swift:171`) and the `pendingDeletion` id is no longer in `store.matches`, the confirm branch must be a safe no-op (no crash, no misleading success). The alert may remain up; confirm simply doesn't mutate anything.
10. **AC10 — Single source of truth.** Inside `MatchListView.swift`, `store.remove(match)` is called from exactly one site (the alert's Delete button). `store.remove(at:)` in `.onDelete` remains (different overload, Delete-key path).
11. **AC11 — Destructive styling preserved.** Context-menu `Button("Delete", role: .destructive)` still renders with destructive styling after the refactor.
12. **AC12 — Scope.** Only `Sources/Sokki/Views/MatchListView.swift` changes. No changes to `MatchStore`, `AppConfig`, or other views.
13. **AC13 — Build clean.** `swift build` succeeds with no new warnings.

## 4. Problem Analysis

Three approaches considered:

- **A — Native SwiftUI `.alert`** — One modifier at the view root, driven by optional state. Matches the `NSAlert` precedent already in `Services/Bootstrapper.swift:84` and requires no new infrastructure. Single shared alert covers both paths (DRY). **Chosen.**
- **B — Inline per-row "Confirm delete?" state** — Each row manages its own two-step UI. Rejected: introduces per-row state, duplicates UI in each entry point, no precedent in the codebase.
- **C — Optimistic delete + Undo toast / Cmd-Z** — Fastest UX but requires an undo stack on `MatchStore`, a new toast/banner component, and file-persistence rollback. Scope creep against YAGNI for a safety guard. Rejected.

## 5. Decision Log

**1. Confirmation UX?**
- Options: A) Native `.alert` · B) Inline row confirm · C) Optimistic + Undo toast
- Decision: **A)** — Existing NSAlert precedent (`Services/Bootstrapper.swift:84`); zero new infra; macOS 14 target supports `.alert(_:isPresented:presenting:actions:message:)`.

**2. Which removal paths confirm?**
- Options: A) All three · B) Only context menu + minus (swipe/Delete-key instant) · C) Only minus
- Decision: **B)** — Matches Finder/Mail; Delete-key/swipe is a deliberate gesture; avoids double-confirm friction.

**3. Alert copy?**
- Options: A) Generic, no trigger · B) Trigger in title · C) Generic title + trigger in body · D) Trigger + replacement preview
- Decision: **C)** — Apple HIG for destructive alerts; handles long triggers without title overflow.

**4. Default button?**
- Options: A) `Cancel` default · B) `Delete` default
- Decision: **A)** — Apple HIG for destructive confirmations; prevents accidental Return-key deletions.

**5. "Don't ask again" preference?**
- Options: A) None (mandatory every time) · B) In-alert checkbox · C) Settings toggle · D) Hidden config flag
- Decision: **A)** — YAGNI; power users keep two fast paths (swipe, Delete key); preference is cheap to add later if demand emerges.

Remaining implementation choices (state location, `.alert` overload, selection-cleanup point) are resolved inline in §6 — they're mechanical, not contested.

## 6. Design

### State

Add one line to `MatchListView`:

```swift
@State private var pendingDeletion: Match?
```

No other state changes. `selectedMatch`, `showingEditor`, `editingMatch`, `lastClickTime`, `lastClickedId` all stay.

### Trigger sites

Replace the two direct `store.remove(match)` calls with `pendingDeletion = match`:

```swift
// Context menu (around line 27)
Button("Delete", role: .destructive) {
    pendingDeletion = match
}

// Minus button (around line 61)
Button(action: {
    if let match = selectedMatch {
        pendingDeletion = match
    }
}) { ... }
```

The `.onDelete { offsets in store.remove(at: offsets) }` handler is untouched — Delete-key path stays instant per AC3.

### Alert modifier

Attached to the outer `VStack` adjacent to the existing `.sheet(isPresented: $showingEditor)` (line 88). Alert and sheet can coexist — no trigger site opens both, and SwiftUI serializes them:

```swift
.alert(
    "Delete Match?",
    isPresented: Binding(
        get: { pendingDeletion != nil },
        set: { if !$0 { pendingDeletion = nil } }
    ),
    presenting: pendingDeletion
) { match in
    Button("Cancel", role: .cancel) { }
    Button("Delete", role: .destructive) {
        guard store.matches.contains(where: { $0.id == match.id }) else { return }
        store.remove(match)
        if selectedMatch?.id == match.id {
            selectedMatch = nil
        }
    }
} message: { match in
    Text("The match \"\(match.trigger)\" will be permanently deleted. This action cannot be undone.")
}
```

Why this shape:
- `.alert(_:isPresented:presenting:actions:message:)` (macOS 14+) unwraps the optional so both closures receive `Match` by value — no stale-state risk inside the alert (AC4, AC8).
- `@State` on `MatchListView` matches every other piece of view-local state in the file (`showingEditor`, `editingMatch`, etc.).
- The custom `Binding<Bool>` maps `isPresented == false` back to `pendingDeletion = nil`, clearing state on Cancel, outside-tap, window close, or confirm (AC7). It's the standard optional-to-bool bridge for `.alert`, which has no `item:` overload.
- `store.matches.contains(where: …)` guards the FSEvents race (AC9): an external YAML edit reloads `MatchStore` and regenerates ids (`Match.init(from:)` regenerates `id` per decode), so the old `Match.id` may no longer exist. The guard makes the Delete click a safe no-op in that case.
- Clearing `selectedMatch` only when its `id` matches the pending match is entry-point agnostic: context-menu Delete on the selected row also clears the now-orphan selection; context-menu Delete on an unselected row leaves selection alone. Supersedes the narrower `selectedMatch = nil` at line 62 (AC8).
- SwiftUI's default "Return activates the default button" combined with `.cancel` role on `Cancel` gives AC6 for free.

### Data flow

```
User gesture (context menu Delete | minus click)
    └─► pendingDeletion = match
         └─► isPresented becomes true
              └─► alert renders with the unwrapped match
                   ├─► Cancel   → pendingDeletion = nil (via Binding.set)
                   └─► Delete   → store.remove(match); clear selectedMatch if needed; pendingDeletion = nil
```

Delete-key path is parallel and untouched:

```
Delete key on selected row → List.onDelete(offsets) → store.remove(at: offsets)
```

### Edge cases

- **External file reload while alert is open (FSEvents).** `AppDelegate.swift:171` watches `~/.config/sokki/` and calls `store.loadAll()` on changes; `Match.init(from:)` regenerates ids on every decode (`Models/Match.swift:25`). If this fires between open and confirm, the pending match's id will not exist in `store.matches`. The `contains` guard above makes confirm a no-op in that case (AC9).
- **Settings window closes while alert is open.** SwiftUI dismisses the alert; the `Binding<Bool>.set(false)` fires and clears `pendingDeletion`. If the user reopens Settings, `MatchListView` is re-instantiated with fresh `@State` — no leaked state.
- **Other gestures while alert is up.** A SwiftUI `.alert` is modal for the attached window: underlying row taps, double-clicks, Return-to-edit, and the minus button are all blocked until the alert dismisses. No additional suppression logic needed.

### Error handling

`MatchStore.remove` is infallible (`matches.removeAll { … }` + `saveMatches`); any file-save issue surfaces via `store.lastError` which `MatchListView` already renders at lines 73–78. Nothing new to handle.

### Testing strategy

Manual smoke test in the running app (no unit tests exist for `MatchListView` today — out of scope to add harness):
- Context menu Delete → alert appears → Cancel → no change.
- Context menu Delete → alert appears → Delete → row gone; `default.yml` updated.
- Minus button → alert appears → Cancel → row still selected; no change.
- Minus button → alert appears → Delete → row gone, selection cleared.
- Select row, press Delete key → row gone immediately, no alert.
- Long trigger (100+ chars) → alert body wraps, title stable.

## 7. Files Changed

- `Sources/Sokki/Views/MatchListView.swift` — add `pendingDeletion` state, route context-menu Delete and minus-button click into it, add shared `.alert` modifier with HIG-compliant copy and buttons. `.onDelete` handler untouched.

## 8. Verification

Mapped to acceptance criteria:

1. **[AC1]** Right-click a row → **Delete** → alert shows; confirm `MatchStore.matches.count` unchanged until `Delete` button pressed.
2. **[AC2]** Select a row → click **−** → alert shows; same store-unchanged check.
3. **[AC3]** Select a row → press Delete key → row gone, no alert appeared.
4. **[AC4]** Visually verify title and body strings; verify `<trigger>` is the pending match's trigger (not another row's).
5. **[AC5]** Alert has exactly two buttons labeled `Cancel` and `Delete`; `Delete` renders in destructive red.
6. **[AC6]** With alert open, press Escape → dismissed, no deletion. Re-trigger, press Return → dismissed (Cancel is default), no deletion.
7. **[AC7]** `Cancel` from either path → `pendingDeletion == nil`, store unchanged, selection preserved.
8. **[AC8]** `Delete` from the minus path with the row selected → row gone AND `selectedMatch == nil`. `Delete` from context menu on an unselected row → row gone, prior `selectedMatch` preserved. `Delete` from context menu on the currently-selected row → row gone AND `selectedMatch == nil`.
9. **[AC9]** Open alert from either confirm path, edit the corresponding YAML externally so FSEvents reloads the store, then click `Delete` → no crash, no deletion, no misleading success log.
10. **[AC10]** Grep `MatchListView.swift` for `store.remove(` → exactly two occurrences: one `store.remove(match)` inside the alert's Delete button, one `store.remove(at: offsets)` inside `.onDelete`.
11. **[AC11]** Context-menu `Button("Delete", role: .destructive)` signature is unchanged; visually verify red foreground in the context menu.
12. **[AC12]** `git diff --stat` shows only `Sources/Sokki/Views/MatchListView.swift` modified (plus this design doc).
13. **[AC13]** `swift build` exits 0 with no new warnings.
