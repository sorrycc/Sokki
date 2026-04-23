# New Source File From Match Editor

## 1. Background
The Match Editor currently restricts the **Source File** field to files that already exist under `~/.config/sokki/match/` (see `MatchEditorView.swift:47` — a plain `Picker` over `availableFiles`). To route a new match into a new YAML file, the user has to quit Sokki, hand-create an empty `.yml`, and restart — friction that undermines the "menu bar, fast" positioning of the app.

The storage layer already supports arbitrary filenames: `ConfigLoader.saveMatches` writes one YAML per unique `sourceFile` encountered (`ConfigLoader.swift:112-128`). So this is purely a UI affordance task.

## 2. Requirements Summary
**Goal:** Let users create a new source YAML file directly from the New/Edit Match editor.

**Scope:**
- In scope: UI affordance in `MatchEditorView` to enter a new filename, reusing existing persistence path
- Out of scope: deleting/renaming files, file browser dialog, editing YAML contents externally, multi-file operations

## 3. Acceptance Criteria
1. `MatchEditorView` Source File row exposes a control to add a new file
2. Activating the new-file control reveals an input with auto-focused cursor
3. Invalid input (empty, `/`, `\`, leading `.`, > 255 chars) disables the Create action
4. Missing `.yml`/`.yaml` extension auto-appends `.yml`
5. Saving a match whose `sourceFile` did not previously exist creates the YAML file under `~/.config/sokki/match/` (only on editor Save, not on "Create" of the new-file input)
6. The new file appears in Settings' All Files filter picker after the editor Save
7. Existing-file selection still works (no regression in Picker styling or behavior)
8. Entering an existing name (case-insensitive, including cross-extension `foo.yml` ≡ `foo.yaml`) silently selects the existing file and surfaces a subtle hint before Create

## 4. Problem Analysis
- **Approach A — Free `TextField` replacing the Picker** → loses discoverability of existing files; typos become new files. Rejected.
- **Approach B — `Picker` with a "New File…" sentinel** → selection binding needs a magic value; fragile. Rejected.
- **Approach C — `Menu` + `.alert(TextField)`** → `.alert` with `TextField` on macOS 14 does not reliably auto-focus; alert nested inside an editor sheet adds a second dismissal layer. Rejected after review.
- **Approach D — Keep existing `Picker` for selection, add a small "+" button next to it that reveals an inline `TextField` row below with auto-focus via `@FocusState`** → preserves native Picker styling, zero focus issues, single sheet layer, inline validation hint. Chosen.

## 5. Decision Log

**1. UI affordance for "create new"**
- Options: A) Picker with sentinel · B) Menu + alert · C) "+" button + inline TextField row
- Decision: **C)** — keeps existing Picker styling untouched (`Picker(.menu)` with Label, mirroring `MatchListView.swift:130-138`), isolates the input UI, allows `@FocusState` auto-focus, and no nested sheet/alert.

**2. Focus handling**
- Options: A) Rely on default SwiftUI focus · B) `@FocusState` + assign on reveal
- Decision: **B)** — `.alert` TextField focus is unreliable on macOS; explicit `@FocusState` on an inline TextField is trivial and reliable.

**3. Validation timing**
- Options: A) On submit only · B) Live disable of Create button
- Decision: **B)** — reuse a single `normalizedFilename(_:) -> String?` helper for both `.disabled` and commit (DRY).

**4. Normalization location**
- Options: A) Private helper in `MatchEditorView` · B) Static on `Match` · C) In `ConfigLoader`
- Decision: **A)** — single call site; YAGNI.

**5. Duplicate filename (case-insensitive, cross-extension)**
- Options: A) Silently select · B) Hard error · C) Silent select + inline hint
- Decision: **C)** — matches intent (user typed a real filename) without surprise; hint appears under the TextField only when a match is detected.

**6. Extra rejection rules**
- Options: A) Only empty + `/` · B) Also reject `\`, leading `.`, length > 255 (APFS limit)
- Decision: **B)** — one-line guards, prevents hidden files, backslash-separator confusion, and filenames APFS itself would reject.

## 6. Design

### State additions in `MatchEditorView`
```swift
@State private var isAddingNewFile = false
@State private var newFileDraft = ""
@State private var extraFiles: [String] = []   // holds just-committed new filenames so the Picker can render them pre-save
@FocusState private var newFileFieldFocused: Bool
```

`displayedFiles` is a computed property that merges persisted + just-added:
```swift
private var displayedFiles: [String] {
    var seen = Set<String>()
    return (availableFiles + extraFiles).filter { seen.insert($0).inserted }
}
```

### Source File row
Replace lines 47-52 of `MatchEditorView.swift` with a single row matching `MatchListView.swift:130-138`'s Picker pattern plus a trailing "+" button:

```swift
if isAddingNewFile {
    newFileRow
} else {
    HStack(spacing: 4) {
        Picker("Source File", selection: $sourceFile) {
            ForEach(displayedFiles, id: \.self) { file in
                Text(file).tag(file)
            }
        }
        .help("The YAML file where this match is stored.")

        Button {
            newFileDraft = ""
            isAddingNewFile = true
        } label: {
            Image(systemName: "plus.circle")
        }
        .buttonStyle(.borderless)
        .help("Create a new source file")
    }
}
```

This keeps `Picker("Source File", selection:)` as a native form row (identical label/alignment to sibling rows), with the "+" button tucked in the same `HStack` — no `LabeledContent` wrapper.

### Inline new-file row
```swift
@ViewBuilder
private var newFileRow: some View {
    VStack(alignment: .leading, spacing: 4) {
        HStack(spacing: 4) {
            TextField("filename.yml", text: $newFileDraft)
                .textFieldStyle(.roundedBorder)
                .focused($newFileFieldFocused)
                .onSubmit { commitNewFile() }
                .onExitCommand { cancelNewFile() }

            Button("Create") { commitNewFile() }
                .disabled(normalizedFilename(newFileDraft) == nil)
                .keyboardShortcut(.defaultAction)

            Button("Cancel", role: .cancel) { cancelNewFile() }
        }
        if let existing = matchingExistingFile(newFileDraft) {
            Text("Already exists — will use \(existing)")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
    .task { newFileFieldFocused = true }   // .task fires after the view is in the hierarchy, dodging onAppear focus races
}
```

### Helpers
Two helpers, no helper chain:
```swift
private func normalizedFilename(_ raw: String) -> String? {
    let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !trimmed.isEmpty,
          !trimmed.hasPrefix("."),
          !trimmed.contains("/"),
          !trimmed.contains("\\"),
          trimmed.count <= 255        // APFS filename limit
    else { return nil }
    let lower = trimmed.lowercased()
    if lower.hasSuffix(".yml") || lower.hasSuffix(".yaml") { return trimmed }
    return trimmed + ".yml"
}

private func matchingExistingFile(_ raw: String) -> String? {
    guard let name = normalizedFilename(raw) else { return nil }
    func stem(_ s: String) -> String {
        let lower = s.lowercased()
        if lower.hasSuffix(".yml")  { return String(s.dropLast(4)).lowercased() }
        if lower.hasSuffix(".yaml") { return String(s.dropLast(5)).lowercased() }
        return lower
    }
    let target = stem(name)
    return displayedFiles.first { stem($0) == target }
}

private func commitNewFile() {
    guard let name = normalizedFilename(newFileDraft) else { return }
    let chosen = matchingExistingFile(newFileDraft) ?? name
    if !displayedFiles.contains(chosen) { extraFiles.append(chosen) }
    sourceFile = chosen
    isAddingNewFile = false
    newFileDraft = ""
}

private func cancelNewFile() {
    isAddingNewFile = false
    newFileDraft = ""
}
```

### Data flow
No changes to `MatchStore`, `ConfigLoader`, or `Match`. Flow:
1. User clicks the "+" button → inline row replaces Picker; TextField gains focus
2. User types `snippets` → Create becomes enabled → Enter (or click Create) → `sourceFile = "snippets.yml"`; row collapses back to Picker showing the new value (which is now in `availableFiles` only after editor Save, but Picker's selection binding stores the string itself, so it renders correctly even if not yet in the list)
3. User clicks editor Save → `store.add(match)` → `ConfigLoader.saveMatches` groups by `sourceFile`, writes `~/.config/sokki/match/snippets.yml`
4. `matchesDidChange` posts → Settings re-renders → `availableSourceFiles` includes `snippets.yml` (via `for m in matches` in `MatchStore.availableSourceFiles`)

Note: if the user cancels the editor sheet after creating a new filename, nothing is persisted — consistent with the editor's "Cancel discards" semantics elsewhere.

### Picker rendering a not-yet-persisted value
The Picker iterates `displayedFiles` (persisted `availableFiles` ∪ `extraFiles`). After `commitNewFile` inserts the new name into `extraFiles`, the Picker has a matching tag and renders the selection correctly even before editor Save. If the user then cancels the editor, `extraFiles` is discarded with the view — nothing hits disk, matching AC5's "only on editor Save".

### Error handling
No new failure modes. Disk errors in `ConfigLoader.saveMatches` are already logged (`ConfigLoader.swift:125`).

## 7. Files Changed
- `Sources/Sokki/Views/MatchEditorView.swift` — replace the bare Picker with an `HStack { Picker; Button("+") }` form row and a switched-in inline new-file row (`TextField` + Create/Cancel + duplicate hint) using `@FocusState`; add state (`isAddingNewFile`, `newFileDraft`, `extraFiles`, `newFileFieldFocused`), the `displayedFiles` computed, and helpers `normalizedFilename`, `matchingExistingFile`, `commitNewFile`, `cancelNewFile`

## 8. Verification
1. [AC1,7] Open Settings → New Match → Source File row shows Picker + "+" button; Picker opens listing existing files
2. [AC1,2] Click "+" → Picker is replaced by TextField (auto-focused) + Create + Cancel
3. [AC3a] Empty field → Create disabled
4. [AC3b] `foo/bar`, `foo\bar`, `.hidden`, 256-char string → Create disabled
5. [AC4] Type `snippets` → Create enabled → press Enter → Source File shows `snippets.yml`, row collapses
6. [AC5] Fill trigger + replacement → click editor Save → `~/.config/sokki/match/snippets.yml` exists and contains the match
7. [AC6] Reopen Settings → All Files filter picker includes `snippets.yml`
8. [AC7] Edit an existing match → Picker still renders native menu style; selecting another existing file saves as before
9. [AC8a] Click "+", type `default` → hint "Already exists — will use default.yml" appears → Create → sourceFile = `default.yml`
10. [AC8b] With existing `snippets.yml`, click "+", type `snippets.yaml` → hint surfaces → Create → sourceFile = `snippets.yml` (cross-extension dedupe)
11. [cancel] Click "+", type `foo`, press Esc or click Cancel → row collapses, sourceFile unchanged
