# Trigger Conflict Warnings

## 1. Background

Sokki's runtime matcher (`TrieMatcher`) treats one trigger as a prefix of another by holding the shorter match as `pendingMatch` while longer cursors remain live. Concretely: when a user defines both `;f` and `;foooo`, typing `;f` does not fire immediately — it's held until the next keystroke makes the longer path unreachable. Exact-duplicate triggers are silently collapsed in `buildNode` (last-write-wins), so one of the duplicates never fires at all. Both situations are invisible in the UI today; users just notice a trigger "doesn't work."

This change adds non-blocking warnings in the settings list and in the match editor so users can recognize and consciously accept (or fix) these conflicts.

## 2. Requirements Summary

**Goal:** Surface prefix-shadowing and exact-duplicate trigger conflicts non-blockingly in `MatchListView` and `MatchEditorView`.

**Scope in:**
- Detect two conflict kinds across all enabled matches (union of source files):
  - **Prefix shadowing:** trigger A is a strict prefix of trigger B (A ≠ B). A fires delayed.
  - **Exact duplicate:** two matches share an identical trigger. Only one fires.
- Row badge on `MatchRow` in `MatchListView` with tooltip.
- Inline warning under the trigger field in `MatchEditorView`, updating live.
- Case-sensitive comparison, enabled-only, self-exclusion in editor.

**Scope out:**
- Blocking save.
- Any change to `TrieMatcher` runtime semantics.
- Auto-rename / auto-disable suggestions.
- "Show only conflicts" filter in list.

## 3. Acceptance Criteria

1. **AC1** In `MatchListView`, any enabled match whose trigger is a strict prefix of another enabled trigger, or duplicates another enabled trigger, shows a yellow warning icon on its row; hovering reveals a tooltip naming the conflicting trigger(s) and kind.
2. **AC2** Disabling a match removes the warning from it and from any other match whose only conflict was against it.
3. **AC3** In `MatchEditorView`, typing a trigger that conflicts with any enabled match (excluding the one being edited) shows an inline caption below the trigger field describing the conflict kind and the other trigger(s). Text updates live as the user types.
4. **AC4** Conflicts are detected across all `sourceFile` values.
5. **AC5** Save is never disabled because of a conflict. The user can knowingly save.
6. **AC6** Comparison is case-sensitive; trigger strings are compared literally (no trim/normalize) to match runtime behavior.
7. **AC7** No change to `TrieMatcher.swift` behavior; work is additive in `MatchStore`, `MatchRow`, and `MatchEditorView`.

## 4. Problem Analysis

Today, `TrieMatcher.feed` holds the shorter match as `pendingMatch` when longer cursors are live (`TrieMatcher.swift:115–126`). `buildNode` collapses exact duplicates at `TrieMatcher.swift:48` (`nodeMatch = match` inside a loop — last wins). Both behaviors are silent.

Approaches considered:

- **Approach A — warn by blocking save** — reject conflicting triggers in `MatchEditorView.canSave`. Rejected: paternalistic; users may legitimately keep both `;f` and `;foooo` and accept the delay. Doesn't help matches loaded from YAML files edited externally.
- **Approach B — fix the runtime (auto-disable shorter, or shorten timeout)** — changes semantics; risks regressing the current UX that some users rely on; out of scope for a "warn" requirement.
- **Approach C — passive warnings in the UI only (chosen)** — detect in `MatchStore`, render in list and editor. No behavior change, additive, cheap to compute, easy to revert.

## 5. Decision Log

**1. Conflict kinds to detect?**
- Options: A) Prefix-shadow only · B) Exact duplicate only · C) Both
- Decision: **C) Both** — Both produce "trigger won't fire as the user expects" at runtime (`TrieMatcher.swift:48, 115–126`); omitting either leaves a surprising silent failure.

**2. Case sensitivity?**
- Options: A) Case-insensitive · B) Case-sensitive
- Decision: **B) Case-sensitive** — `TrieMatcher` keys on raw `Character` at `TrieMatcher.swift:50, 77`; `KeystrokeMonitor` passes raw input. Detection must match runtime to avoid false positives/negatives.

**3. Include disabled matches?**
- Options: A) Include · B) Exclude
- Decision: **B) Exclude** — `buildTrie` filters `isEnabled` at `TrieMatcher.swift:37`; disabled matches never reach the trie, so they can't conflict.

**4. Block save?**
- Options: A) Block · B) Warn only
- Decision: **B) Warn only** — Mirrors the existing `promptError` pattern (`MatchEditorView.swift:94–98`); `canSave` is only gated by emptiness today (`:135–139`). KISS + user agency.

**5. Where to compute?**
- Options: A) Ad-hoc in each view · B) Derived `@Observable` property on `MatchStore`
- Decision: **B) On the store** — `MatchStore` is `@Observable` (`MatchStore.swift:4`); a single `private(set) var conflictsByMatchId: [UUID: ConflictReport]` recomputed in `loadAll` and `saveMatches` automatically invalidates dependent views. Avoids duplicate logic and per-row recomputation.

**6. Algorithm & perf?**
- Options: A) Naive O(n²) scan · B) Sort-and-walk · C) Reuse the trie
- Decision: **A) Naive O(n²)** — Typical user has ≤200 matches; n² ≈ 40k char comparisons on mutations only (not per-keystroke). KISS; avoid premature optimization.

**7. Which side gets the list badge?**
- Options: A) Shadowed (shorter) + duplicate only · B) Both shadowed and the shadowing (longer) · C) All involved
- Decision: **A) Shadowed side only** — The longer trigger fires fine at runtime; only the shorter/delayed/duplicate side "misbehaves". Keeps signal-to-noise high. The editor, by contrast, warns on both directions because the author is deciding *to cause* a delay by adding a longer conflicting trigger.

**8. Editor warning: how many conflicts to list?**
- Options: A) First one only · B) Up to 3, then "+N more" · C) All
- Decision: **B) Up to 3 + overflow** — Bounded size, still informative for the common case. Avoids runaway caption height.

**9. Severity visualization?**
- Options: A) Same yellow for both kinds · B) Red duplicate, yellow shadow
- Decision: **B) Red for duplicates, yellow for shadowing** — Duplicate is strictly broken (one match never fires); shadowing is functional but delayed. Visual tier reflects real severity. Implemented with the same `exclamationmark.triangle.fill` icon in different colors to stay consistent with the existing iconography (`MatchListView.swift:223`).

**10. Self-exclusion in editor?**
- Options: A) Include self · B) Exclude self
- Decision: **B) Exclude self** — `MatchEditorView` has `existingId: UUID?` (`MatchEditorView.swift:16`). Editing an existing match must not flag it as conflicting with itself.

**11. Live editor feedback trigger?**
- Options: A) On save attempt · B) `.onChange(of: trigger)` live
- Decision: **B) Live** — Matches the value of the warning (user learns before committing); no network, so recompute cost is negligible.

## 6. Design

### 6.1 Data model

Add a new file `Sources/Sokki/Models/TriggerConflict.swift` with plain value types:

```swift
enum TriggerConflictKind: Int, Sendable, Equatable, Comparable {
    case duplicate = 0   // another enabled match has the identical trigger
    case shadowedBy = 1  // this trigger is a strict prefix of another (runtime delays it)
    case shadows = 2     // this trigger strictly extends another (editor-only)

    static func < (lhs: Self, rhs: Self) -> Bool { lhs.rawValue < rhs.rawValue }
}

struct TriggerConflict: Sendable, Equatable, Hashable {
    let kind: TriggerConflictKind
    let otherTrigger: String
}

struct ConflictReport: Sendable, Equatable {
    let conflicts: [TriggerConflict]   // deterministic order: kind, then otherTrigger lex
    var isEmpty: Bool { conflicts.isEmpty }
    var hasDuplicate: Bool { conflicts.contains { $0.kind == .duplicate } }
}
```

Store-level detector (pure functions) in the same file:

```swift
enum TriggerConflictDetector {
    /// Compute conflicts for every enabled match. Only `.duplicate` and
    /// `.shadowedBy` are emitted — `.shadows` is editor-only. Key: id of the
    /// match whose runtime behavior is impaired.
    static func computeList(matches: [Match]) -> [UUID: ConflictReport]

    /// For the editor: given a draft trigger and the current matches,
    /// return all conflicts (both directions) excluding `excludingId`.
    static func computeEditor(
        draftTrigger: String,
        matches: [Match],
        excludingId: UUID?
    ) -> ConflictReport
}
```

### 6.2 MatchStore changes

Make recomputation structural by using `didSet` on `matches`:

```swift
var matches: [Match] = [] {
    didSet { conflictsByMatchId = TriggerConflictDetector.computeList(matches: matches) }
}

private(set) var conflictsByMatchId: [UUID: ConflictReport] = [:]
```

This covers every existing write path — `loadAll` (line 23), `add` (43), `update` (49), `remove(_:)` (58), `remove(at:)` (53), context-menu enable-toggle (`MatchListView.swift:170–173` calls `store.update`), and swipe/keyboard delete (`MatchListView.swift:177–180`, `onDelete` → `store.remove`) — without adding any new hook. Any future write path is also covered for free, removing the maintenance footgun of forgetting to call a separate `recompute()` helper.

`MatchStore` is already `@Observable` (`MatchStore.swift:4`); no `objectWillChange` plumbing is needed. Any SwiftUI view that reads `store.conflictsByMatchId[id]` re-renders when the map changes.

### 6.3 List view (MatchRow)

In `Sources/Sokki/Views/MatchListView.swift`, thread the conflict report into `MatchRow`:

```swift
MatchRow(match: match, conflict: store.conflictsByMatchId[match.id])
```

Place the warning icon **inside the leading `VStack`** in `MatchRow` (next to the trigger, above the subtitle) rather than on the trailing side. The trailing HStack already holds `sourceFile` / `AI` / `Disabled` capsules with no `layoutPriority`, so any trailing badge is the first thing truncated on narrow widths — exactly when users need it most. The leading VStack has `layoutPriority(1)` (line 304), protecting the badge.

```swift
HStack(spacing: 6) {
    if let c = conflict, !c.isEmpty {
        Image(systemName: "exclamationmark.triangle.fill")
            .foregroundStyle(c.hasDuplicate ? .red : .orange)
            .help(conflictTooltip(c))
            .accessibilityLabel(conflictTooltip(c))
    }
    Text(match.trigger)
        .font(.system(.body, design: .monospaced))
        .foregroundStyle(match.isEnabled ? .primary : .secondary)
}
```

(`.orange` chosen over `.yellow` for macOS HIG warning contrast; duplicates use `.red`.)

Tooltip format (single line, up to 3 triggers + "+N more"; each kind reports a count of *distinct* other triggers):
- Duplicate only: `Duplicate of ";f" — only one will fire`
- Duplicate with many collisions: `Duplicate — 3 other matches use ";f"` (use a count when there are ≥2 other duplicates of the same trigger)
- Shadowed by one: `Delayed by longer trigger ";foooo"`
- Shadowed by many: `Delayed by longer triggers ";foooo", ";fbar", ";fbaz" (+2 more)`
- Mixed kinds: join with `; ` separator, duplicates first

### 6.4 Editor view

In `Sources/Sokki/Views/MatchEditorView.swift`:

- Accept a snapshot: `allMatches: [Match]` passed in at construction (from `MatchListView.sheetContent`, which reads `store.matches`). The sheet is modal, so the snapshot captured at sheet-open time is sound. This matches the existing editor pattern (`MatchListView.swift:80–102`) of passing explicit inputs + `onSave`/`onCancel` callbacks rather than threading the store.
- Keep `existingId` as the self-exclusion id.
- Add `@State private var triggerConflict: ConflictReport = ConflictReport(conflicts: [])`.
- Recompute **only** via `.onChange(of: trigger)`. Do NOT use `.task { recompute() }`. Rationale: the new-match sentinel trigger `":"` (`MatchEditorView.swift:35`) would otherwise produce a false-positive conflict banner the instant the sheet opens if the user happens to have a `:` trigger. Requiring a user edit first avoids spam and matches the intent of "warn-don't-nag".
- For the edit-existing case, seed an initial recompute from the loaded trigger only if it differs from the sentinel. This is implemented as: run recompute in `.onChange(of: trigger)` with `initial: true`, then inside the handler, skip when `trigger == ":"` (the new-match sentinel).
- Render a caption below the trigger field (mirrors `promptError` styling at `MatchEditorView.swift:94–98`):

```swift
if !triggerConflict.isEmpty {
    Text(editorConflictMessage(triggerConflict))
        .font(.caption)
        .foregroundStyle(triggerConflict.hasDuplicate ? .red : .orange)
}
```

Editor messages (active voice, user-centric framing):
- `.duplicate` (1 other): `Another match already uses ";f" — only one will fire`
- `.duplicate` (multiple): `3 other matches already use ";f" — only one will fire`
- `.shadowedBy` (1): `Delayed by longer trigger ";foooo"`
- `.shadowedBy` (multi): `Delayed by longer triggers ";foooo", ";fbar" (+1 more)`
- `.shadows` (1): `Extends existing trigger ";f" — ";f" will fire with a delay`
- `.shadows` (multi): `Extends existing triggers ";f", ";foo" — they'll fire with a delay`
- Mixed kinds: show on separate `Text` lines in kind order (duplicates first).

`canSave` remains unchanged (AC5).

### 6.5 Detection algorithm

```swift
static func computeList(matches: [Match]) -> [UUID: ConflictReport] {
    let enabled = matches.filter(\.isEnabled)
    var out: [UUID: [TriggerConflict]] = [:]

    // Duplicates: group by trigger. Emit ONE .duplicate entry per match in a
    // collision group, carrying the shared trigger string. Do NOT emit one
    // entry per other match (that would fill the tooltip with the same
    // quoted trigger repeated). Count is recovered from group.count - 1 in
    // the UI when needed.
    var byTrigger: [String: [Match]] = [:]
    for m in enabled { byTrigger[m.trigger, default: []].append(m) }
    for (trigger, group) in byTrigger where group.count > 1 {
        for m in group {
            out[m.id, default: []]
                .append(.init(kind: .duplicate, otherTrigger: trigger))
        }
    }

    // Prefix shadowing: m.trigger is a strict prefix of other.trigger.
    // The shorter match (m) is flagged with .shadowedBy.
    for m in enabled {
        guard !m.trigger.isEmpty else { continue }
        for other in enabled where other.id != m.id {
            if other.trigger.count > m.trigger.count,
               other.trigger.hasPrefix(m.trigger) {
                out[m.id, default: []]
                    .append(.init(kind: .shadowedBy, otherTrigger: other.trigger))
            }
        }
    }

    return out.mapValues { conflicts in
        // Stable ordering: kind (duplicate < shadowedBy < shadows), then trigger lex.
        // Swift's sorted(by:) is stable on Apple platforms.
        let sorted = conflicts.sorted { a, b in
            if a.kind != b.kind { return a.kind < b.kind }
            return a.otherTrigger < b.otherTrigger
        }
        return ConflictReport(conflicts: sorted)
    }
}
```

Editor variant (`computeEditor`):
- Build `enabled = matches.filter { $0.isEnabled && $0.id != excludingId }`.
- For each `other` in `enabled`, take the first branch that matches (mutually exclusive by length):
  - `other.trigger == draftTrigger` → `.duplicate(otherTrigger: draftTrigger)`.
  - `other.trigger.count > draftTrigger.count && other.trigger.hasPrefix(draftTrigger)` → `.shadowedBy(other.trigger)`.
  - `draftTrigger.count > other.trigger.count && draftTrigger.hasPrefix(other.trigger)` → `.shadows(other.trigger)`.
- **Dedupe by `(kind, otherTrigger)` before emitting**: if the user has two matches both equal to the draft trigger, we emit only one `.duplicate` entry (the multi-duplicate count is still conveyed by list-side warnings on those other rows). Implementation: accumulate into a `Set<TriggerConflict>` or filter duplicates after collection.
- Apply the same sort comparator.
- Skip the entire computation when `draftTrigger.isEmpty` or `draftTrigger == ":"` (new-match sentinel). **Known limitation:** a user who legitimately wants `:` as a trigger will not see a warning until they type a second character. Acceptable — the warning isn't blocking, and the alternative is a spurious warning every time the sheet opens.

### 6.6 Error handling / edge cases

- Empty trigger: no conflicts emitted (the editor's `canSave` already blocks saves).
- Whitespace-only trigger: compared literally (matches runtime). No special case.
- Very long triggers in tooltip: truncated via the existing `Text` line-limit behavior; no code needed.
- `MatchEditorView` initial trigger is `":"` (new-match sentinel at `MatchEditorView.swift:35`): treated like any other — if the user happens to have `:` as a match, they'll see a duplicate warning immediately. Acceptable and accurate.

### 6.7 Testing strategy

No test target exists (per CLAUDE.md). Manual verification per §8. The pure detector (`TriggerConflictDetector.computeList`) is written as free functions on a caseless `enum`, so it's trivially test-ready if a target is added later.

## 7. Files Changed

- `Sources/Sokki/Models/TriggerConflict.swift` — **new**: `TriggerConflictKind`, `TriggerConflict`, `ConflictReport`, `TriggerConflictDetector`.
- `Sources/Sokki/Models/MatchStore.swift` — add `conflictsByMatchId` with `didSet` recompute on `matches`.
- `Sources/Sokki/Views/MatchListView.swift` — thread conflict into `MatchRow`; render warning badge with tooltip; pass `store.matches` into the editor.
- `Sources/Sokki/Views/MatchEditorView.swift` — accept `allMatches` and `excludingId`; add live `triggerConflict` state; render inline caption below trigger field.

## 8. Verification

Manual checklist mapped to acceptance criteria:

1. **[AC1]** Create two matches `;f` → `foo` and `;foooo` → `fooooobar`. The `;f` row shows a yellow warning badge; hover reveals `Delayed by longer trigger ";foooo"`.
2. **[AC1]** Create two matches with identical trigger `;dup`. Both rows show red warning badges; tooltip reads `Duplicate of ";dup" — only one will fire`.
3. **[AC2]** Disable `;foooo`. The warning on `;f` disappears immediately.
4. **[AC2]** Re-enable `;foooo`. Warning reappears.
5. **[AC3]** Open the editor for a new match. Type `;f` while `;foooo` already exists. The caption `Delayed by longer trigger ";foooo"` appears live in orange.
6. **[AC3]** Type `;food` while `;f` and `;foo` exist. Caption reads `Extends existing triggers ";f", ";foo" — they'll fire with a delay` in orange.
7. **[AC3]** In the editor for an existing `;f` match, type without changing → no self-conflict warning.
8. **[AC4]** Put `;f` in `a.yml` and `;foooo` in `b.yml` (use the "new source file" flow). Conflict still detected on `;f`.
9. **[AC5]** With any conflict visible, the Save button is enabled. Click Save → match persists.
10. **[AC6]** Create `;Foo`. Then type `;foo` in the editor → no warning (case-sensitive).
11. **[AC7]** `TrieMatcher.swift` is unchanged (verify via `git diff`).
