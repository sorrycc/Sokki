# AI Matches via Palette

Date: 2026-04-21
Status: Design — not yet implemented
Supersedes (for `input: selection`): the typed-trigger path in
`2026-04-21-ai-rewrite-trigger.md`, which is unreliable because typing a
trigger destroys the selection in almost every macOS text field.

## Honesty / known limitations

- **Selection persistence across app switch is app-dependent.** Native AppKit
  apps (TextEdit, Notes, Xcode, Finder Rename, most of macOS) keep the
  selection greyed-out while unfocused, and it comes back on reactivation
  so Cmd+C works. Many Electron / web-backed apps repaint or drop the
  selection on blur — Slack in some message states, Discord, Chrome/Safari
  URL bars, Figma, parts of VS Code (notably the terminal pane). On those,
  the palette path for `input: selection` will silently land on the
  "no selection" HUD even though the user clearly had text selected. This
  is a platform constraint, not a Sokki bug. Document it in the README and
  validate across a real spread of apps (not just TextEdit).
- **Not a substitute for hotkey-bound AI.** A per-match hotkey
  (`hotkey: "cmd+shift+p"`) would bypass the palette entirely for the
  "rewrite selection" case. Tracked in §8 as a follow-up; the palette is
  the MVP surface.

## Goal

Make AI matches invokable from the existing Spotlight-style palette
(⌥Space or configured shortcut). The user selects text first, opens the
palette, picks an AI trigger, and Sokki does the rest. All three AI input
kinds are supported (`selection`, `clipboard`, `none`).

## User flow

1. User selects text in target app (e.g. TextEdit, Notes, VS Code).
2. ⌥Space — palette opens. Sokki becomes frontmost; the target app's
   selection stays live (macOS shows it greyed out while unfocused).
3. User searches/picks an AI match, hits ⏎.
4. Palette closes, target reactivates after the existing ~30 ms delay.
5. Sokki runs the AI flow:
   - **`input: selection`**: Cmd+C → capture selection → replace selection
     with `▎ thinking…` → await AI → erase placeholder → inject result.
   - **`input: clipboard`**: read `NSPasteboard` → inject `▎ thinking…`
     at cursor → await AI → erase placeholder → inject result.
   - **`input: none`**: inject `▎ thinking…` at cursor → await AI →
     erase placeholder → inject result.
6. Esc anywhere during the wait cancels. Errors restore the original
   selection (for `input: selection`) or erase the placeholder only.

## 1. Changes

- `PaletteController.commitInjection(_ match:)` forks on `match.ai != nil`:
  call `monitor.handleAIMatchFromPalette(match)` instead of the existing
  `injector.inject(...)` path. Palette still closes + reactivates target;
  it does **not** schedule the 30 ms wait — see below.
- New MainActor method
  `KeystrokeMonitor.handleAIMatchFromPalette(match:)`. It owns the
  focus-reactivation wait internally (80 ms, see §2), then forwards to the
  shared core.
- Shared core uses an explicit source enum rather than an integer signal:

  ```swift
  enum AIInvocation {
      case typedTrigger(length: Int)
      case palette
  }

  private func runAIFlow(
      match: Match,
      ai: AIConfig,
      invocation: AIInvocation
  ) async
  ```

  Callsites read the way the spec reads:
  - `handleAIMatch` passes `.typedTrigger(length: N)`.
  - `handleAIMatchFromPalette` passes `.palette`.

  Branches inside the helper switch on `invocation` to choose:
  - whether to `eraseInjected(triggerLength)`,
  - whether to post a `Backspace` after Cmd+C (typed path) or skip it
    (palette path — the placeholder replaces the live selection),
  - whether to call `recordExpansion` (typed only — see §5),
  - whether `suppressMatching` was pre-set by the caller (typed: yes, set
    on the tap thread before main dispatch) or needs to be set at helper
    entry (palette: yes, helper is first to know).

  The helper idempotently calls `setSuppressMatching(true)` at entry to
  remove the caller-order ambiguity.
- **Drop the `AIMatchExecutor` protocol.** Sokki is a single binary and
  `AppDelegate` already owns both services; pass the `KeystrokeMonitor`
  directly to `PaletteController.init`. The protocol was unnecessary
  indirection.

## 2. Focus reactivation + selection capture (palette path)

### 2.1 Focus wait

`PaletteController.commitInjection` closes the palette and calls
`target.activate(options:)`. The existing non-AI palette path waits 30 ms
before direct injection — empirically fine for key injection, but **too
short for Cmd+C** bouncing through the target's event loop, especially on
Electron apps just leaving the background.

`handleAIMatchFromPalette` does the wait itself:

```swift
// Prefer an active-frontmost poll over a blind sleep; fall back to a hard
// deadline so we never stall. 80 ms is the hard cap.
let targetBundle = /* passed in or re-captured */
let deadline = Date().addingTimeInterval(0.08)
while Date() < deadline,
      NSWorkspace.shared.frontmostApplication?.bundleIdentifier != targetBundle {
    try? await Task.sleep(nanoseconds: 10_000_000) // 10ms
}
```

This adds up to ~80 ms in the worst case and typically returns in <30 ms
when the target reactivates quickly. Centralising the wait in the monitor
keeps `commitInjection` a one-liner.

### 2.2 Selection capture

With focus settled, the selection is live. Sequence for
`invocation == .palette, input == .selection`:

```
before = NSPasteboard.general.changeCount
clipboardManager.save()
postCmdC()
poll changeCount until > before, up to aiSelectionCaptureTimeoutMs (400 ms)
if changed:
    read NSPasteboard.general.string(forType: .string)
    clipboardManager.restore()
    injectDelta(placeholder)     // replaces the still-live selection
else:
    clipboardManager.restore()
    HUD .aiFailed "no selection"; return
```

No explicit Backspace after Cmd+C in the palette path —
`keyboardSetUnicodeString` typing the placeholder replaces the selection
on its own (standard macOS typing semantics).

Contrast with typed-trigger path: there we already did `eraseInjected` for
the trigger, leaving no selection; that path DOES post an explicit
Backspace after Cmd+C before placeholder injection. The `runAIFlow`
helper switches on `invocation` to pick the right post-Cmd+C behaviour.

## 3. Flow sequence (condensed)

```
palette commit (match.ai != nil)
  ├─ recency.bump(match.trigger)           (symmetry with non-AI palette commits)
  ├─ palette.close(reactivateTarget: true)
  └─ monitor.handleAIMatchFromPalette(match)
         └─ focus-poll up to 80ms (§2.1)
         └─ runAIFlow(match, ai, invocation: .palette)

handleAIMatch (tap-thread dispatch, typed trigger)
  └─ runAIFlow(match, ai, invocation: .typedTrigger(length: N))

runAIFlow(match, ai, invocation):
  ├─ if tapContext.isAIActive:             (§5.2 concurrency guard)
  │       HUD .aiFailed "AI busy"; return
  ├─ setSuppressMatching(true)             (idempotent; palette needs it,
  │                                         typed already has it)
  ├─ switch invocation {
  │     .typedTrigger(let n): eraseInjected(n)
  │     .palette:             (no-op)
  │  }
  ├─ capture input:
  │     .selection → Cmd+C snapshot (see §2.2)
  │                   switch invocation {
  │                     .typedTrigger: postBackspaceOnce   // delete selection
  │                     .palette:      (no-op — placeholder replaces)
  │                   }
  │     .clipboard → NSPasteboard.string
  │     .none      → nil
  ├─ injectDelta(placeholder); placeholderLength = placeholder.count
  ├─ try? Task.sleep 50ms                  (drain HID pipeline)
  ├─ setActiveAIRequest(true); setSuppressMatching(false)
  ├─ beginAIMouseWatch()                   (§5.1 click-during-wait)
  ├─ emitHUD(.aiThinking)
  ├─ event = await aiRewriter.run(...)
  ├─ endAIMouseWatch()
  ├─ typed = consumeUserTypedDuringAI()    (atomic read-and-clear;
  │                                         true if typed OR clicked)
  ├─ setSuppressMatching(true)
  ├─ .completed(text):
  │     if !typed: eraseInjected(placeholderLength); injectDelta(text)
  │     else:      injectDelta(text); HUD note "appended; delete placeholder"
  │     if case .typedTrigger = invocation {
  │         recordExpansion(trigger: match.trigger, replacement: text)
  │     }                                 // skip for .palette — no typed
  │                                       // trigger in the text to undo back to
  │     clearBuffer                       // always — prevent trie poisoning
  │ .failed / .cancelled:
  │     if !typed: eraseInjected(placeholderLength); if selection: inject original
  └─ setSuppressMatching(false)
```

## 4. Palette UI

Row for an AI match:

- Small "AI" chip to the right of the trigger (same visual weight as the
  existing match-type chips if any, else a new subtle capsule).
- Secondary line shows `ai.prompt` (first ~60 chars) instead of the
  `replace` template preview.
- Input kind shown as a tiny leading glyph next to the chip, e.g.
  `sel` / `clip` / `—`. Optional for MVP; skip if it feels noisy.

No new panels, no new icons, no tooltips.

## 5. Error / cancel / undo

Reused verbatim from the typed-trigger flow:

- `activeAIRequest` / `userTypedDuringAI` live in `TapContext.State`.
- Esc-swallow on the tap thread when `context.isAIActive`.
- `consumeUserTypedDuringAI()` for atomic read-and-clear on completion.
- Failure restores original selection (if `input: selection`) or just
  erases the placeholder (clipboard/none).

**Undo difference for palette commits.** `recordExpansion` is **not**
called on `.palette` completion. Backspace-undo restores a typed trigger;
there is no typed trigger in the text when the palette invoked the match,
so recording an expansion against a non-existent trigger would make
backspace-undo delete N user chars and leave no coherent restore. This
matches what the existing non-AI palette path already does (it skips
`recordExpansion` too).

### 5.1 Mouse-click detection during AI wait

**Data-loss risk:** the tap only sees keystrokes. A user who clicks
elsewhere in the target app between placeholder injection and AI
completion moves the caret. On `.completed(text)` with `!typed`, we then
`eraseInjected(placeholderLength)` at the **new** caret position —
backspacing over unrelated user text. Nasty.

**Fix:** treat mouse clicks as "user interacted during wait", same as
typing. Add a global mouse monitor during the AI window; `.leftMouseDown`
(and right/other) flips the same flag:

```swift
// In KeystrokeMonitor (or a small helper).
// Installed when setActiveAIRequest(true); removed on consume.
private var aiMouseMonitor: Any?

private func beginAIMouseWatch() {
    aiMouseMonitor = NSEvent.addGlobalMonitorForEvents(
        matching: [.leftMouseDown, .rightMouseDown, .otherMouseDown]
    ) { [weak self] _ in
        self?.tapContext.noteUserTypedDuringAI()
    }
}

private func endAIMouseWatch() {
    if let m = aiMouseMonitor { NSEvent.removeMonitor(m); aiMouseMonitor = nil }
}
```

Install right after `setActiveAIRequest(true)` in `runAIFlow`; remove
right after `consumeUserTypedDuringAI()`. Clicks on Sokki's own windows
don't reach a *global* monitor, so our palette/HUD clicks don't false-
positive. The existing `noteUserTypedDuringAI` guard
(`activeAIRequest && !suppressMatching`) keeps our own activity from
counting.

The flag now means "user keystroke OR click during wait". The
detect-and-degrade path (append result, leave placeholder, suggest ⌘Z)
applies identically.

**Limitation still present:** a user who selects text with keyboard
modifiers (shift-arrow) and then doesn't click is detected via the
keystroke path; one who uses *only* mouse drag to reselect is detected
via the mousedown. A trackpad-only caret teleport via AX-injected
accessibility action would be invisible — not a realistic vector.

### 5.2 Concurrent AI runs

If a typed-trigger AI is already in flight and the user opens the palette
and commits another AI match (or vice versa), we would end up with an
orphaned placeholder (`▎ thinking…`) from the first run stuck in the
first target app while the second run proceeds elsewhere.

**Rule:** at most one AI run at a time, and new invocations are refused
cleanly. `runAIFlow` guards at entry:

```swift
if tapContext.isAIActive {
    Logger.shared.error("[AI] refused — another AI run is in flight")
    emitHUD(.aiFailed, trigger: match.trigger, note: "AI busy")
    // Do NOT call setSuppressMatching — we didn't mutate tap state yet.
    return
}
```

Guard placement is **before** `setSuppressMatching(true)` / any injection
/ any `clipboardManager.save()` so we don't corrupt the in-flight run's
clipboard or suppress-state. The user sees a single HUD row explaining
why nothing happened; the first run continues undisturbed. The user can
cancel the first with Esc, then retry.

Alternative considered: abort the prior run (cancel + erase its
placeholder + restore its selection) before starting the new one. Harder
— we'd need to find and focus the prior target app to issue backspaces,
and we don't track target-per-run. Rejected for MVP; "AI busy" is
acceptable UX given AI runs are ~3–5 s.

## 6. File-level touchpoints

Modified:

- `Sources/Sokki/Engine/KeystrokeMonitor.swift` — extract `runAIFlow`,
  add `handleAIMatchFromPalette` (owns the focus-poll internally),
  introduce the `AIInvocation` enum, switch on it for erase / backspace /
  `recordExpansion`. Add `aiMouseMonitor` field +
  `beginAIMouseWatch` / `endAIMouseWatch` (§5.1). Add concurrency guard
  at `runAIFlow` entry (§5.2).
- `Sources/Sokki/Services/PaletteController.swift` — fork
  `commitInjection` on `match.ai != nil`; `recency.bump` then call
  `monitor.handleAIMatchFromPalette(match)`. No delay scheduled here;
  the monitor owns timing.
- `Sources/Sokki/AppDelegate.swift` — pass `keystrokeMonitor` to
  `PaletteController.init`.
- `Sources/Sokki/Views/PaletteView.swift` — show the AI chip and prompt
  preview in rows for AI matches (minor; visual only).

New types: none. (The earlier `AIMatchExecutor` protocol was dropped —
unnecessary indirection in a single-binary app.)

## 7. Manual validation checklist

### Native AppKit apps (should work reliably)

- [ ] TextEdit: select, ⌥Space, `;;pro`, ⏎ → selection replaced by
      `▎ thinking…`, then by AI output.
- [ ] Notes, Xcode, Finder Rename, Safari URL bar (may fail — see below).
- [ ] Mail compose window.

### Electron / web-backed apps (expected to be flaky — validate the fallback)

- [ ] Slack channel input — if selection is dropped on blur, expect
      clean "no selection" HUD; verify no partial corruption.
- [ ] VS Code editor pane, VS Code terminal pane.
- [ ] Discord, Figma text elements.
- [ ] Chrome URL bar and Chrome <textarea>.

### Core flows

- [ ] Nothing selected + `input: selection` picked from palette → HUD
      "no selection", no placeholder injected, clipboard unchanged.
- [ ] Palette pick of `;;tldr` with text on clipboard → placeholder at
      cursor, then AI-summarised result replaces it.
- [ ] Palette pick of `;;stand` (`input: none`) → placeholder at cursor,
      then generated text replaces it.
- [ ] Focus poll (§2.1): on a typically-slow Electron app, Cmd+C does NOT
      fail due to premature firing.

### Cancel / failure parity

- [ ] Esc during AI wait cancels; selection restored (for `.selection`)
      or placeholder erased (for `.clipboard` / `.none`).
- [ ] User typing during wait → detect-and-degrade path (placeholder
      stays, result appended, HUD suggests ⌘Z).
- [ ] **User clicks elsewhere in target app during wait** → same
      detect-and-degrade path (placeholder stays, result appended).
      Verify no user text is eaten by a mis-aimed erase.
- [ ] Clicking on Sokki's HUD during wait does NOT false-positive as
      "user interacted" (global monitor skips our own windows).
- [ ] Network offline / 30 s timeout → clean restore, HUD error.
- [ ] `claude` binary missing / not signed in → clean HUD error, no
      corruption in the target app.

### Concurrency

- [ ] While a typed-trigger AI is in flight, opening the palette and
      committing another AI match → HUD "AI busy", no placeholder in
      the second target, first run completes normally.
- [ ] Symmetrical: palette-AI in flight, typing a typed-AI trigger →
      HUD "AI busy", first run continues.
- [ ] After "AI busy", cancelling the first run with Esc then retrying
      the second works cleanly.

### Undo semantics

- [ ] After palette-AI completion, Backspace does NOT trigger
      `ExpansionUndoManager` restore (no `recordExpansion` was called).
      Plain Backspace deletes one char of the AI result, as expected.
- [ ] After typed-trigger AI completion, Backspace-undo still restores
      the original trigger (regression).

### Recency

- [ ] Committing an AI match via palette surfaces it near the top in the
      next palette open (recency bumped, matching non-AI behaviour).

### Regression

- [ ] Typed-trigger AI flow still works end to end.
- [ ] Palette still works for non-AI matches.
- [ ] Non-AI typed triggers, undo, toggle, secure-input guard all
      unchanged.

## 8. Out of scope

- AI-match authoring UI in `MatchEditorView` (YAML-only, noted already).
- Hotkey-bound AI matches (separate, bigger feature).
- Streaming token injection (CLI doesn't emit per-token deltas).
- Per-app filtering of AI matches in the palette.
