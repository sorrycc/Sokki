# Improve Match List — Source File, Filter, Search, Delete Key

## 1. Background

Sokki's Settings → Matches tab shows a flat list of all expansion rules loaded from every `.yml` file in `~/.config/sokki/match/`. As users accumulate matches across multiple files (e.g. `default.yml`, `espanso.yml`), three pain points emerge:

- The row doesn't show which file a match came from, so it's impossible to tell at a glance where a given rule lives.
- The editor can't change a match's source file, so moving a rule between files requires hand-editing YAML.
- There is no way to filter by file or search within the list, so long lists become unscannable.

This design adds source-file visibility, source-file editing, filter, search, and a Delete-key shortcut while preserving all existing behaviors.

## 2. Requirements Summary

**Goal:** Make the match list navigable and editable across multiple source files.

**Scope:**
- In scope: display `sourceFile` per row; Picker in editor to change source file; filter by source file; case-insensitive substring search on trigger+replace; Delete-key deletes selection.
- Out of scope: creating/renaming/deleting source files from UI; persisting `id`/`sourceFile` into YAML; regex search; bulk ops; drag-and-drop moves; automated tests (project has no test target).

**Key decisions (full list in §5).**

## 3. Acceptance Criteria

1. Each row in the match list displays its `sourceFile` as a trailing secondary-styled label, truncated if long.
2. Opening the editor shows a Source File picker, pre-selected to the current file, listing all known files.
3. Saving the editor with a different file selected moves the match: it disappears from the old YAML file and appears in the new YAML file on disk after `saveMatches`.
4. A filter control above the list (`All Files` + each known file) restricts visible rows to the chosen file.
5. A search field filters visible rows by case-insensitive substring match against `trigger` or `replace`.
6. Filter and search compose with AND semantics.
7. Pressing Delete or Backspace while a row is selected raises the same confirmation alert used by context-menu Delete and the minus button; confirming then deletes the row.
8. Existing behaviors remain: Return opens editor, double-click opens editor, context menu Edit/Delete/Enable-Disable, plus/minus toolbar, disabled capsule, inline error label.
9. Clicking `+` with a specific file filter active pre-fills the editor's Source File to that file; with `All Files` selected it falls back to `default.yml`.
10. When filter+search produce zero rows, the list area shows a `ContentUnavailableView("No matches", ...)` instead of an empty list.

## 4. Problem Analysis

Current state:
- `MatchListView` is a single-column `List` with each row rendered by `MatchRow` (HStack: stacked trigger/replace, Spacer, optional Disabled capsule).
- `MatchEditorView` has Trigger, Replacement, Enabled — no file field.
- `MatchStore.update` force-overwrites `sourceFile` back to the pre-edit value (MatchStore.swift:43), blocking moves.
- `ConfigLoader.saveMatches` already groups by `sourceFile` and rewrites every known file on each save, so a move would work the moment `MatchStore.update` stops overwriting.

Approaches evaluated:
- **A — In-place edits** to `MatchListView`, `MatchEditorView`, and a 1-line fix in `MatchStore.update`; adds view-local `@State` for filter/search. Smallest change that satisfies all criteria.
- **B — Extract a `FilterableMatchListView` wrapper.** Indirection with no reuse target. Rejected (YAGNI).
- **C — `NavigationSplitView` with a sidebar of source files.** Breaks the project's plain `TabView + VStack + List` convention and is heavier than the feature needs. Rejected.

Chosen: **A**.

## 5. Decision Log

**1. Filter+search header placement**
- Options: A) manual `HStack` header above list · B) SwiftUI `.searchable` · C) macOS toolbar
- Decision: **A)** — A Settings pane inside a `TabView` is not a `NavigationStack`; `.searchable` renders either at the window toolbar (not present) or at the top of the view with mandatory chrome. Manual `HStack` keeps the header on the same visual tier as the footer toolbar and matches the project's plain-layout convention.

**2. Filter sentinel representation**
- Options: A) String sentinel `"All Files"` · B) `String?` with `nil = all` · C) enum
- Decision: **B)** — A real filename could collide with a string sentinel; `String?` is the minimum type-correct encoding. `"All Files"` remains only as the *display label* for the nil case.

**3. Source-file list provider**
- Options: A) Compute from `matches` each render · B) Expose `store.availableSourceFiles` · C) Read disk each time
- Decision: **B)** — One source of truth. Union of `knownFiles` + distinct `sourceFile` values from `matches`, always including `"default.yml"`, sorted alphabetically. Computed O(n) per read is acceptable for expected list sizes.

**4. `MatchStore.update` semantics**
- Options: A) Drop the overwrite entirely and trust the caller · B) Fall back to existing when incoming `sourceFile` is nil
- Decision: **A)** — The editor always supplies a non-nil `sourceFile` (Picker cannot yield nil). A defensive fallback is dead code that masks real nil bugs.

**5. Default-file rule lives in one place**
- Options: A) Multiple call sites decide · B) Resolve at the list view's `openEditor`/`+` call site and pass a non-optional `prefillSourceFile: String` to the editor
- Decision: **B)** — DRY. List view maps `(fileFilter, match.sourceFile)` → one final string; editor just uses it.

**6. Add-with-filter behavior**
- Options: A) Always default.yml · B) Prefill editor's Source File from active filter
- Decision: **B)** — User-confirmed. `nil` filter → `default.yml`; specific file filter → that file. `MatchStore.add`'s own `"default.yml"` fallback is kept as a deep defensive default for any non-editor caller.

**7. Delete-key binding + focus scope**
- Options: A) Bind to List only · B) Gate with `@FocusState` so search TextField's backspace doesn't fire List delete · C) Use `focusedValue`
- Decision: **B)** — Critical: without gating, backspacing in the search field would silently delete the selected row (data-loss bug). Use `@FocusState` enum with cases `list`, `search`; bind `.onKeyPress(.delete)`, `.onKeyPress(.deleteForward)`, and the existing `.onKeyPress(.return)` to only fire when `focused == .list`. Keyboard Delete sets `pendingDeletion = selectedMatch`, routing through the **existing confirmation alert** used by the context menu and minus button — this matches shipped behavior and prevents accidental data loss.

**8. Editor view-state refresh across presentations**
- Options: A) Keep `.sheet(isPresented:)` and add `.id(editingMatch?.id)` · B) Switch to `.sheet(item:)` keyed by the edited match · C) Make editor drive off `@Bindable` draft object
- Decision: **B)** — `.sheet(item:)` recreates the editor view per presentation, so every `@State` field (including the new `sourceFile`) is re-initialized from the current match. Simpler than manual `.id()` juggling.

**9. Empty-state component**
- Options: A) Plain centered `Text` · B) `ContentUnavailableView`
- Decision: **B)** — macOS 14+ stock API, minimal code.

**10. Source-file picker position in editor**
- Options: A) After Trigger · B) After Toggle · C) Grouped with Toggle
- Decision: **A)** — Identity/location attribute belongs near the top with Trigger.

**11. Filter `Picker` style and affordance**
- Options: A) `.menu` plain · B) `.menu` with leading SF Symbol (`doc`) label · C) segmented
- Decision: **B)** — `.menu` scales to N files; prefix the currently-selected label with a `doc` symbol so the control reads as a file filter even with `.labelsHidden()`; also helps VoiceOver.

**12. Search field type**
- Options: A) `TextField` with leading magnifier + `.roundedBorder` · B) `.searchable` · C) custom rounded background
- Decision: **A)** — Stock `.textFieldStyle(.roundedBorder)` with a leading `magnifyingglass` Image; no hand-rolled background chrome. `.searchable` rejected per Decision 1.

**13. Empty matchDir on cold start**
- Options: A) Require user to create dir first · B) `ConfigLoader.saveMatches` ensures `matchDir` exists
- Decision: **B)** — This design exposes the first-save path (empty install → add first match). Add `FileManager.createDirectory(at: matchDir, withIntermediateDirectories: true)` at the top of `saveMatches`.

**14. Testing**
- Decision: **No automated tests** — project has no test target; verification is manual per convention.

## 6. Design

### Data layer — `Sources/Sokki/Models/MatchStore.swift`

Add a computed property (one source of truth for file names the UI can offer):

```swift
var availableSourceFiles: [String] {
    var set = knownFiles
    for m in matches { if let f = m.sourceFile { set.insert(f) } }
    set.insert("default.yml")
    return set.sorted()
}
```

Drop the `update(_:)` overwrite entirely (Decision 4):

```swift
func update(_ match: Match) {
    guard let i = matches.firstIndex(where: { $0.id == match.id }) else { return }
    matches[i] = match
    saveMatches()
}
```

`MatchStore.add`'s `"default.yml"` fallback stays as a deep last-resort for non-editor callers.

`ConfigLoader.saveMatches` gets a one-liner at the top:

```swift
try? FileManager.default.createDirectory(at: matchDir, withIntermediateDirectories: true)
```

…to fix cold-start when `~/.config/sokki/match/` doesn't yet exist.

### Editor — `Sources/Sokki/Views/MatchEditorView.swift`

New signature:

```swift
init(match: Match?,
     availableFiles: [String],
     prefillSourceFile: String,              // non-optional, resolved by caller
     onSave: @escaping (Match) -> Void,
     onCancel: @escaping () -> Void)
```

New `@State var sourceFile: String = match?.sourceFile ?? prefillSourceFile`.

Defensive: if `match?.sourceFile` is present but not in `availableFiles` (file deleted externally), the caller guarantees it is added to the list it passes (see List view below). We trust this and don't re-check inside the editor.

Form layout (addition between Trigger and Replacement):

```swift
TextField("Trigger", text: $trigger) ...
Picker("Source File", selection: $sourceFile) {
    ForEach(availableFiles, id: \.self) { Text($0).tag($0) }
}
Section("Replacement") { TextEditor(text: $replace) ... }
Toggle("Enabled", isOn: $isEnabled)
```

On Save, build `Match(..., sourceFile: sourceFile)`.

### List — `Sources/Sokki/Views/MatchListView.swift`

View state:

```swift
@State private var fileFilter: String? = nil        // nil = All Files (Decision 2)
@State private var searchQuery: String = ""
@State private var selectedMatch: Match?
@State private var sheetRequest: EditorRequest?     // drives .sheet(item:)
@FocusState private var focus: Focus?               // Decision 7

private enum Focus: Hashable { case list, search }
```

Both "edit existing" and "add new" go through one `EditorRequest` conforming to `Identifiable`:

```swift
enum EditorRequest: Identifiable {
    case edit(Match)
    case new(prefillSourceFile: String)

    var id: String {
        switch self {
        case .edit(let m): return "edit-\(m.id)"
        case .new:         return "new"
        }
    }
}
```

Distinct ids for each edited match force `.sheet(item:)` to recreate the view (fresh `@State`) per presentation (Decision 8).

Filter + search composition:

```swift
private var filteredMatches: [Match] {
    let q = searchQuery.trimmingCharacters(in: .whitespaces)
    return store.matches.filter { m in
        let fileOK = fileFilter.map { m.sourceFile == $0 } ?? true
        let searchOK = q.isEmpty
            || m.trigger.localizedCaseInsensitiveContains(q)
            || m.replace.localizedCaseInsensitiveContains(q)
        return fileOK && searchOK
    }
}
```

Body (no Dividers — Decision 1, project convention):

```
VStack(spacing: 0) {
    header
    listOrEmptyState
    footerToolbar
}
```

Header (Decisions 11, 12):

```swift
private var filterLabelText: String { fileFilter ?? "All Files" }

HStack(spacing: 8) {
    Picker(selection: $fileFilter) {
        Text("All Files").tag(nil as String?)
        ForEach(store.availableSourceFiles, id: \.self) { f in
            Text(f).tag(Optional(f))
        }
    } label: {
        Label(filterLabelText, systemImage: "doc")   // drives the closed-menu chip
    }
    .pickerStyle(.menu)
    .fixedSize()

    HStack(spacing: 4) {
        Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
        TextField("Search", text: $searchQuery)
            .textFieldStyle(.roundedBorder)
            .focused($focus, equals: .search)
    }
}
.padding(.horizontal, 8)
.padding(.vertical, 6)
```

Why this form: `.menu` Picker renders the `label:` closure as the collapsed chip, so the SF Symbol is guaranteed to appear. Menu contents stay plain `Text` to avoid per-item icon rendering quirks. The optional-tag pair (`nil as String?` + `Optional(f)`) is the idiomatic way to bind a `String?` selection.

List / empty-state (Decision 9):

```swift
Group {
    if filteredMatches.isEmpty {
        ContentUnavailableView("No matches",
            systemImage: "line.3.horizontal.decrease.circle",
            description: Text(emptyStateDescription))
    } else {
        List(selection: $selectedMatch) {
            ForEach(filteredMatches) { match in
                MatchRow(match: match)
                    .tag(match)
                    /* existing tap, contextMenu, onDelete */
            }
        }
        .listStyle(.inset(alternatesRowBackgrounds: true))
        .focused($focus, equals: .list)
        .onKeyPress(.return)       { guard focus == .list else { return .ignored }; return openForSelected() }
        .onKeyPress(.delete)       { guard focus == .list else { return .ignored }; return deleteSelected() }
        .onKeyPress(.deleteForward){ guard focus == .list else { return .ignored }; return deleteSelected() }
    }
}
```

`@FocusState` gating (Decision 7) observes focus: when the user clicks the search field, `focus = .search` and the List's `.onKeyPress` handlers return `.ignored` so backspace edits the query. When the user clicks a row, AppKit gives focus to the List and `focus` becomes `.list` (or nil, which also blocks the search-case). The key point is: `@FocusState` does not *drive* macOS's first-responder chain — clicks do — it *observes* it, which is enough to gate the handlers. Manual verification (§8) must cover both directions.

Selection hygiene — clear selection when it's filtered out:

```swift
.onChange(of: fileFilter)   { _, _ in pruneSelection() }
.onChange(of: searchQuery)  { _, _ in pruneSelection() }
.onChange(of: store.matches){ _, _ in pruneSelection() }

private func pruneSelection() {
    if let sel = selectedMatch, !filteredMatches.contains(sel) { selectedMatch = nil }
}
```

Watching the three inputs (filter, search, matches) is cheaper and more explicit than watching a computed array. Per-keystroke filter evaluation is O(n matches × m query) — acceptable for expected sizes (<1000 rows).

MatchRow with trailing file label (Round 1 feedback #15):

```swift
HStack {
    VStack(alignment: .leading, spacing: 2) {
        Text(match.trigger).font(.system(.body, design: .monospaced)) ...
        Text(match.replace).font(.caption).foregroundStyle(.secondary).lineLimit(1)
    }
    .layoutPriority(1)

    Spacer(minLength: 8)

    if let file = match.sourceFile, !file.isEmpty {
        Text(file)
            .font(.caption)
            .foregroundStyle(.secondary)
            .lineLimit(1)
            .truncationMode(.middle)
    }

    if !match.isEnabled { disabledCapsule }
}
.padding(.vertical, 2)
```

`.layoutPriority(1)` guarantees a long filename cannot squash the primary trigger/replace stack.

"+" and "Edit" paths — single resolution site for `prefillSourceFile` (Decision 5):

```swift
private func resolvedPrefill() -> String { fileFilter ?? "default.yml" }

// "+":
editingMatch = nil
sheetRequest = .new(prefillSourceFile: resolvedPrefill())

// Edit existing:
sheetRequest = .edit(storedMatch)    // storedMatch.sourceFile ?? "default.yml" (coerced at presentation)
```

Sheet presentation (Decision 8):

```swift
.sheet(item: $sheetRequest) { req in
    switch req {
    case .edit(let m):
        MatchEditorView(
            match: m,
            availableFiles: store.availableSourceFiles,
            prefillSourceFile: m.sourceFile ?? "default.yml",
            onSave: { updated in store.update(updated); sheetRequest = nil },
            onCancel: { sheetRequest = nil }
        )
    case .new(let prefill):
        MatchEditorView(
            match: nil,
            availableFiles: store.availableSourceFiles,
            prefillSourceFile: prefill,
            onSave: { m in store.add(m); sheetRequest = nil },
            onCancel: { sheetRequest = nil }
        )
    }
}
```

No `ensureContains` helper is needed: since `availableSourceFiles` already unions `knownFiles ∪ matches.sourceFile ∪ {"default.yml"}`, any match currently in `store.matches` has its file present in the list. The `resolvedPrefill()` for `+` returns either a file from `availableSourceFiles` (current filter value) or `"default.yml"` — both guaranteed present.

File name sort order in `availableSourceFiles` is lexicographic (Swift default `<`), not localized — intentional; filenames are user-typed and a stable, predictable order beats locale-specific surprises.

### Data flow

1. User types in search → `searchQuery` updates → `filteredMatches` recomputes → List re-renders.
2. User changes filter Picker → `fileFilter` (String?) updates → same recompute path.
3. User edits a match, Picker to new Source File, clicks Save → `MatchStore.update(match)` with new `sourceFile` → `saveMatches` → `ConfigLoader.saveMatches` rewrites every file in `knownFiles ∪ grouped.keys ∪ {default.yml}`, so the old file loses the row and the new file gains it.
4. User presses Delete with focus on List and a row selected → `store.remove(match)` → `saveMatches` → row disappears from on-disk file.

### Error handling

- **Picker value not in options:** caller always injects the current/prefill value via `ensureContains`.
- **Match with nil `sourceFile`:** editor coerces to `"default.yml"` at presentation; row display also coerces (no blank trailing label).
- **Empty `matchDir`:** `saveMatches` creates it before writing.
- **Long filename:** `.layoutPriority(1)` on primary content + `.lineLimit(1).truncationMode(.middle)` on file label.
- **Search field vs List key conflicts:** `@FocusState` gating on all List `.onKeyPress` handlers.
- **Filtered-out selection:** `.onChange(of: filteredMatches)` clears `selectedMatch` if stale.

### Testing strategy

None automated — project has no test target. Manual verification in §8, mapped to every AC.

## 7. Files Changed

- `Sources/Sokki/Views/MatchListView.swift` — header (filter Picker with `String?` + search `TextField`), `filteredMatches` computed, empty-state via `ContentUnavailableView`, `@FocusState`-gated `.onKeyPress(.return / .delete / .deleteForward)`, trailing file label in `MatchRow` with `.layoutPriority` on primary content, `EditorRequest` enum for `.sheet(item:)`, stale-selection reset on filter/search change.
- `Sources/Sokki/Views/MatchEditorView.swift` — new `availableFiles`, non-optional `prefillSourceFile`, `@State sourceFile`, `Picker("Source File")` between Trigger and Replacement, propagate `sourceFile` into saved `Match`.
- `Sources/Sokki/Models/MatchStore.swift` — `availableSourceFiles` computed; drop `update(_:)` overwrite of `sourceFile`.
- `Sources/Sokki/Services/ConfigLoader.swift` — ensure `matchDir` exists before writing in `saveMatches`.
- `CHANGELOG.md` — one-line entry.

## 8. Verification

Manual, mapped to acceptance criteria:

1. **[AC1]** Open Settings → Matches; each row shows filename at the right (`default.yml`, `espanso.yml`, etc.) in secondary style, truncated if long.
2. **[AC2]** Double-click a row; editor shows Source File picker pre-selected to the match's file.
3. **[AC3]** In editor, change picker to a different file → Save → inspect `~/.config/sokki/match/` YAML files: row is gone from old file, present in new file.
4. **[AC4]** Change filter Picker to a specific file → only rows from that file are visible; `All Files` → all rows visible.
5. **[AC5]** Type in the search field (e.g. `br`); only rows where trigger or replace contain `br` (case-insensitive) are visible; empty string → all rows visible (subject to filter).
6. **[AC6]** Combined: filter=`default.yml` + search=`hello` → only matches where both file=default.yml AND (trigger or replace contains `hello`) are visible.
7. **[AC7]** Select a row, press Delete (or Backspace) → row disappears; file on disk no longer contains the row.
8. **[AC8]** Confirm Return opens editor on selected row; double-click opens editor; context-menu Edit/Delete/Enable-Disable works; +/− toolbar buttons work; disabled matches show the Disabled capsule; bad YAML still shows the inline error label.
9. **[AC9]** Select filter=`espanso.yml` → click `+` → editor opens with Source File pre-selected to `espanso.yml`. Select filter=`All Files` → click `+` → editor opens with `default.yml` pre-selected.
10. **[AC10]** Set filter to a file with no matches or a search query with no hits → `ContentUnavailableView("No matches", ...)` appears in place of the List.

Focus-scope regression checks (critical, from Decision 7):

- Click into search field, type and press Backspace to erase characters → selected row is **not** deleted.
- Click into search field, press Return → editor does **not** open.
- Click a row, press Delete or Backspace → row **is** deleted.
- Click a row, press Return → editor opens.

Build check: `swift build` succeeds (project-standard readiness check).

CHANGELOG style: one-line bullet under `## Unreleased` matching the plain-bullet style of releases 0.0.3 / 0.0.4 (no `### Added` subsection).
