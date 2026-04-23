# AI prompt: fall back to clipboard when selection is missing

## 1. Background

Prompt templates accept chain placeholders like `{{s|c}}` meaning "use selection,
else clipboard". `PromptTemplate.render` in `PromptRenderer.swift` already honors
this chain semantics correctly. However the capture step in
`KeystrokeMonitor.runAIFlow` aborts early whenever selection capture fails —
before render ever runs — so `{{s|c}}` with no selection never reaches the
clipboard fallback. The user sees a "no selection" failure HUD and stop sound
even though the prompt explicitly authorized clipboard as a fallback.

## 2. Requirements Summary

**Goal:** Honor `{{s|c}}` chain fallback at capture time, not just at render
time.

**In scope:** Capture branch in `KeystrokeMonitor.runAIFlow`.
**Out of scope:** Rendering, HUD/sound system, settings UI, palette path (it
shares `runAIFlow`, so the fix covers both).

## 3. Acceptance Criteria

1. Prompt `{{s|c}}`, no selection, clipboard has text → render uses clipboard,
   AI runs, no "AI failed" HUD, no stop sound.
2. Prompt `{{s|c}}`, no selection, empty clipboard → aborts with
   `no input for {{s|c}}` HUD (existing render-abort path).
3. Prompt `{{s}}` (selection only, no fallback), no selection → still aborts
   with `no selection` HUD (unchanged).
4. Prompt `{{c}}` alone → unchanged, Cmd-C never attempted.
5. Prompt `{{s|c}}`, selection present → uses selection (unchanged).
6. Palette-invoked AI prompts with `{{s|c}}` behave the same as typed triggers.

## 4. Problem Analysis

`KeystrokeMonitor.runAIFlow` at lines 228-247 treats `template.needsSelection`
as "selection is mandatory". That flag actually means "at least one chain
*mentions* selection", so it is true for both `{{s}}` (mandatory) and `{{s|c}}`
(preferred). Conflating the two is the bug.

- **Approach A — Change `render` to treat missing selection as blank.** Rejected
  — `render` already does the right thing; the bug is upstream. Also would lose
  the "hard require selection" error for `{{s}}`.
- **Approach B — Distinguish mandatory-selection from preferred-selection at
  the template level and branch on it in `runAIFlow`.** Chosen — smallest
  surgical change, mirrors existing `needsSelection`/`needsClipboard` property
  pattern, single call site.

## 5. Decision Log

**1. How to detect mandatory-selection placeholders?**
- Options: A) New `requiresSelection` property on `PromptTemplate` · B) Inline
  logic in `runAIFlow` · C) Reinterpret `needsSelection`
- Decision: **A)** — Mirrors existing pattern, testable in isolation, one call
  site. Reinterpreting `needsSelection` would silently change the condition
  that gates Cmd-C.

**2. Clipboard snapshot timing.**
- Options: A) Keep current `if template.needsClipboard` snapshot BEFORE Cmd-C ·
  B) Snapshot only after selection fails
- Decision: **A)** — `needsClipboard` is already true for `{{s|c}}` (it just
  checks whether any chain contains `.clipboard`), and the existing comment at
  line 219 warns that Cmd-C overwrites the pasteboard, so we must snapshot
  first.

**3. HUD signal when selection silently falls back to clipboard?**
- Options: A) No HUD, log only · B) New "falling back" HUD · C) Keep "no
  selection" HUD
- Decision: **A)** — Fallback is the documented semantic of the `|` operator.
  A HUD for a non-error would be noise. Render-abort already covers the
  "neither source resolved" case.

**4. Still attempt Cmd-C when selection is only a preference?**
- Options: A) Always attempt when any chain mentions selection · B) Skip when
  clipboard alone can satisfy every chain
- Decision: **A)** — Keep `needsSelection` unchanged. Selection is usually
  fresher than clipboard; attempting Cmd-C costs ~80 ms timeout on failure,
  which is acceptable.

## 6. Design

### 6.1 New property on `PromptTemplate`

```swift
/// True iff some placeholder chain contains ONLY `.selection` — meaning the
/// prompt cannot be rendered without a selection, regardless of clipboard.
var requiresSelection: Bool {
    placeholders.contains { $0.chain == [.selection] }
}
```

Note the check is `== [.selection]`, not `.contains(.selection)`. A chain like
`[.selection, .clipboard]` has a fallback so it does not require selection;
only a chain that is *exactly* `[.selection]` mandates it.

### 6.2 Capture branch change in `runAIFlow`

Current (lines 239-247, paraphrased):

```swift
} else {
    Logger.shared.error("[AI] no selection …")
    if case .typedTrigger = invocation { textInjector.injectDelta(match.trigger) }
    emitHUD(.aiFailed, trigger: match.trigger, note: "no selection")
    tapContext.setSuppressMatching(false)
    return
}
```

New:

```swift
} else {
    Logger.shared.error("[AI] no selection (timeout=…, elapsed=…ms)")
    if template.requiresSelection {
        if case .typedTrigger = invocation { textInjector.injectDelta(match.trigger) }
        emitHUD(.aiFailed, trigger: match.trigger, note: "no selection")
        tapContext.setSuppressMatching(false)
        return
    }
    // Soft-fail: leave selectionText = nil; render() will try the next source
    // in the chain. If every chain is unresolved, the guard at the render site
    // surfaces `no input for {{…}}`.
    Logger.shared.error("[AI] selection missing — falling back to chain")
}
```

### 6.3 Data flow (new path for `{{s|c}}`, no selection)

1. Parse template → `needsClipboard=true`, `needsSelection=true`,
   `requiresSelection=false`.
2. Snapshot clipboard before Cmd-C.
3. Attempt Cmd-C → timeout → `captured == nil`.
4. New branch: log only, keep `selectionText = nil`, continue.
5. `template.render(selection: nil, clipboard: <text>)` → resolves chain via
   clipboard.
6. Placeholder injection, AI request, result injection — unchanged.

### 6.4 Error handling unchanged paths

- Empty clipboard + empty selection + `{{s|c}}` → render returns nil →
  existing `guard let rendered` path emits `no input for {{s|c}}`.
- `{{s}}` alone, no selection → still aborts pre-render with `no selection`.
- Concurrency guard, secure-input check, bad-prompt parse — untouched.

## 7. Files Changed

- `Sources/Sokki/Engine/PromptRenderer.swift` — add `requiresSelection`
  computed property on `PromptTemplate`.
- `Sources/Sokki/Engine/KeystrokeMonitor.swift` — in `runAIFlow`, on
  selection-capture failure, branch on `template.requiresSelection`; preserve
  abort for mandatory-selection prompts, fall through for preferred-selection
  prompts.

## 8. Verification

Manual, against a running `swift run` build:

1. [AC1] Config a match with prompt `{{s|c}}`. Copy some text (clipboard). Type
   trigger in a target app with nothing selected → expansion runs on the
   clipboard text, no red HUD, no stop sound.
2. [AC2] Same match, clear clipboard (copy empty / nothing), no selection →
   red HUD reads `no input for {{s|c}}`.
3. [AC3] Config a match with prompt `{{s}}`. No selection → red HUD reads
   `no selection` (regression guard).
4. [AC4] Config a match with prompt `{{c}}`. Trigger it → AI runs using
   clipboard; no Cmd-C synthesized (check logs: no `[AI] clipboard snapshot`
   vs `[AI] selection captured` ordering regressions).
5. [AC5] Same `{{s|c}}` match, select text → expansion uses the selection
   (check log `[AI] selection captured len=…`).
6. [AC6] Same `{{s|c}}` match, invoke via palette (no selection, clipboard has
   text) → expansion runs on clipboard.
