# AI Tag in Settings Match List

## 1. Background

The command palette (`PaletteView.paletteRow`) already shows a blue `AI · <input>` capsule next to matches configured with an `AIConfig`. The settings match list (`MatchListView.MatchRow`) does not, so users browsing or editing their matches cannot distinguish AI-backed matches from plain replacements at a glance.

## 2. Requirements Summary

**Goal:** Show an AI badge in the settings match list row for matches with `ai != nil`, using the palette's existing visual treatment.

**Scope:**
- In scope: `MatchRow` in `Sources/Sokki/Views/MatchListView.swift`
- Out of scope: palette, editor, filtering/sorting, model changes

**Key Decisions:**
1. AI data source: `Match.ai: AIConfig?` (already exists — `Sources/Sokki/Models/Match.swift:9`)
2. Existing pattern: `PaletteView.paletteRow` lines 102–108
3. Tag style: match palette exactly (user choice)

## 3. Acceptance Criteria

1. A match with `ai != nil` renders a blue `AI · <input>` capsule in its list row
2. A match with `ai == nil` renders no capsule
3. Styling (font, colors, padding, shape) matches `PaletteView.paletteRow`
4. Coexists with the existing `Disabled` chip when both apply
5. `swift build` succeeds

## 4. Problem Analysis

The palette already solved the same visual problem:

```swift
// PaletteView.swift:102-108
if let ai = match.ai {
    Text("AI · \(ai.input.rawValue)")
        .font(.caption.bold())
        .foregroundStyle(.white)
        .padding(.horizontal, 6).padding(.vertical, 1)
        .background(Color.blue.opacity(0.7), in: Capsule())
}
```

Approaches considered:
- **Extract `AITagView`** — DRY across palette + list -> rejected: only 2 call sites of 6 lines. Extraction costs a new file for no current reuse benefit. Revisit when a third caller appears.
- **Duplicate inline** — copy the 6-line block into `MatchRow` -> chosen: minimal change, zero indirection.

## 5. Decision Log

**1. Reuse via extraction or duplicate inline?**
- Options: A) Extract shared `AITagView` · B) Duplicate inline
- Decision: **B) Duplicate inline** — only 2 call sites, both trivial. KISS/YAGNI.

**2. Ordering in the trailing HStack of `MatchRow`?**
- Options: A) sourceFile → AI → Disabled · B) AI → sourceFile → Disabled
- Decision: **A)** — groups the AI and Disabled chips together; sourceFile is metadata.

**3. Respect `isEnabled` styling (dim when disabled)?**
- Options: A) Dim · B) Full opacity
- Decision: **B) Full opacity** — palette doesn't dim; the `Disabled` chip already signals state.

## 6. Design

Add a single `if let ai = match.ai` block in `MatchRow.body`, inserted into the trailing HStack between the `sourceFile` label and the `Disabled` chip. Copy the exact modifiers from `PaletteView.paletteRow` so the two views stay visually identical.

```swift
if let ai = match.ai {
    Text("AI · \(ai.input.rawValue)")
        .font(.caption.bold())
        .foregroundStyle(.white)
        .padding(.horizontal, 6).padding(.vertical, 1)
        .background(Color.blue.opacity(0.7), in: Capsule())
}
```

No changes to data model, store, or filter logic. `AIConfig` and `AIInput.rawValue` are already `Sendable` and usable on `@MainActor`.

## 7. Files Changed

- `Sources/Sokki/Views/MatchListView.swift` — add AI capsule in `MatchRow`

## 8. Verification

1. [AC1, AC3] Launch app; open Settings; a match with `ai:` in its YAML shows the blue `AI · selection` (or `clipboard`/`none`) capsule
2. [AC2] A plain replacement match shows no capsule
3. [AC4] Disabling an AI match shows both `AI · …` and `Disabled` chips
4. [AC5] `swift build` completes with no new warnings
