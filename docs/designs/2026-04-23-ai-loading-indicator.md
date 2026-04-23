# AI Loading Indicator — Configurable Inline Placeholder

## 1. Background

The inline placeholder shown during an AI expansion is currently the hardcoded string `"▎ thinking…"`, injected once at request start and erased on completion. The `▎` glyph tries to mimic a caret and renders poorly next to the real system caret; the whole string reads noisy in proportional text fields.

We want a user-configurable indicator with one static style and two animated styles, driven from `AISettings` and applied inline only (palette preview stays static and neutral).

## 2. Requirements Summary

**Goal:** Replace the hardcoded `"▎ thinking…"` inline AI placeholder with a user-configurable `placeholder_style` setting supporting one static and two animated indicators.

**Scope:**
- In scope: config field + enum, placeholder injection/animation in `KeystrokeMonitor.runAIFlow`, palette preview glyph, fallback note wording
- Out of scope: HUD changes, palette animation, changing `suppressMatching` semantics, new test target

## 3. Acceptance Criteria

1. `placeholder_style` config field in `AISettings` accepts `static`, `braille`, `block_sweep`; unknown/missing values fall back to `static`.
2. Default behaviour (no config) shows static `generating…` inline (replacing `▎ thinking…`).
3. `braille` style shows `⠋ generating…` → `⠙ generating…` → … cycling through `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏` at ~120 ms per frame.
4. `block_sweep` style shows `▏ generating…` → `▎ generating…` → … cycling through `▏▎▍▌▋▊▉█` at ~120 ms per frame.
5. On AI completion with no user typing, the full current frame is erased and the AI result is injected in its place.
6. On user typing during animation, animation stops within one full frame cycle (worst case ~190 ms: typing at start of a suppressed window is not recorded until listen opens ~70 ms later, then the next frame's peek fires after ~50 ms listen + ~70 ms suppress). No further erase/inject cycles run after the peek observes typing; the last injected frame remains inline and completion appends the result, leaving that frame for manual cleanup.
7. On AI failure with no user typing, the current frame is erased and selection/trigger re-injection proceeds as before.
8. Palette preview caption always shows `⟨generating…⟩` regardless of `placeholder_style` — it is a design preview, not a live indicator.
9. Typed-during-AI HUD note uses generic phrasing: `result appended; loading indicator left inline — delete manually` (no longer hardcodes the old glyph).

## 4. Problem Analysis

Current state — `Sources/Sokki/Engine/KeystrokeMonitor.swift`:
- L142 declares `private static let placeholder = "▎ thinking…"`.
- L271–273 injects the placeholder once via `textInjector.injectDelta(placeholder)` and records `placeholderLength = placeholder.count` as a local `let`.
- L294–340 completion/failure branches use `placeholderLength` to erase.
- L311 hardcodes the glyph string into a user-facing HUD note (second copy).
- `Sources/Sokki/Views/PaletteView.swift:139` holds a third copy for the palette preview caption.

The glyph is hardcoded in three places and the design targets an animated version, so the plan needs to (a) introduce a style enum, (b) make `placeholderLength` mutable across frame updates, and (c) coordinate the animation with the existing self-echo / typed-during-AI protocol without altering its semantics.

Approaches evaluated:

- **A — Animate with `suppressMatching = false` throughout**: Simple, one `setSuppressMatching(true)` at completion as today. *Rejected* — `TapContext.noteUserTypedDuringAI` (L292–300) only ignores events when `suppressMatching` is true. Synthesized backspace/unicode events during frames would trigger false-positive typed-during-AI, breaking the whole flow.

- **B — Animate with `suppressMatching = true` throughout**: No false positives. *Rejected* — real user typing is also dropped during the wait, silently defeating the typed-during-AI protection. Mouse clicks (via `beginAIMouseWatch`, a separate NSEvent monitor) still work, but keyboard typing would be missed.

- **C — Bracket each frame: `suppress=true` during inject/drain, `suppress=false` during listening window**: Frame cycle splits into a ~50 ms suppressed window (erase + inject + drain) and a ~70 ms listening window (real user typing can be recorded). Matches the existing initial-placeholder pattern (L277). *Chosen* — preserves self-echo guard AND typed-during-AI detection. Trade-off: ~42 % of the frame window is deaf to keyboard input, but mouse clicks remain caught throughout via the existing global monitor, and a user typing within a single 50 ms window will still be caught on the very next frame's listen phase.

## 5. Decision Log

**1. Config field name and raw values?**
- Options: A) `placeholder_style: static|braille|block_sweep` · B) `loading_indicator: static|braille|blockSweep` · C) `spinner: none|braille|blocks`
- Decision: **A)** — snake_case matches every existing AISettings field (`timeout_seconds`, `selection_capture_timeout_ms`). `placeholder_style` is honest about what it controls (the inline placeholder, not a HUD spinner).

**2. Animation format — glyph alone vs glyph + suffix?**
- Options: A) glyph only (`⠋`) · B) glyph + ` generating…` · C) glyph + ` thinking…`
- Decision: **B)** — switching between `static` and animated modes only swaps the leading indicator; the `generating…` word stays visible so the user always sees what's happening.

**3. Frame interval?**
- Options: A) 80 ms · B) 120 ms · C) 200 ms
- Decision: **B) 120 ms** — 8 fps reads as "alive" while each erase+inject roundtrip through CGEvent has breathing room. Split as **70 ms suppressed (inject+drain) + 50 ms listening**. The suppressed window covers the worst-case inject time: ~13 backspace posts + `TextInjector`'s 10 ms tail sleep (L69) + unicode inject + drain for the tap thread to consume them before `suppressMatching` flips back.

**4. Where does the animation Task live?**
- Options: A) `private var animationTask: Task<Void, Never>?` on `KeystrokeMonitor` · B) local var in `runAIFlow` captured by `defer` · C) on `TapContext`
- Decision: **A)** — `KeystrokeMonitor` is `@MainActor`, the completion/failure branches already live there, and a single optional property is trivially cancellable from any branch.

**5. How does animation detect user typing without consuming the flag?**
- Options: A) Add `peekUserTypedDuringAI()` to `TapContext` · B) Poll a separate flag · C) Have `runAIFlow` cancel the animation Task externally
- Decision: **A)** — introduces one narrow read accessor; the atomic consume at L295 is preserved for the completion path. (C) doesn't work because `runAIFlow` is blocked at `await aiRewriter.run`.

**6. Where is `placeholderLength` stored so animation frames can mutate it?**
- Options: A) promote to local `var` inside `runAIFlow` · B) property on `KeystrokeMonitor` · C) struct passed to animation Task
- Decision: **B) property on `KeystrokeMonitor`** (`private var aiPlaceholderLength: Int = 0`). The animation Task and the completion/failure branches all need to read/write it, and passing a shared `var` into a `Task` closure requires `@MainActor` reference semantics anyway. Single property on the MainActor class is cleanest.

**7. Fallback HUD note wording at L311?**
- Options: A) hardcode `"generating…"` · B) capture last frame · C) generic `"loading indicator left inline"`
- Decision: **C)** — the exact frame is ephemeral; generic phrasing survives style changes and reads naturally.

**8. Palette preview glyph?**
- Options: A) static `⟨generating…⟩` always · B) reflect user's config · C) mini-animate
- Decision: **A)** — it is a static design preview in a settings-like caption, not a live indicator.

**9. File placement for `PlaceholderStyle` enum?**
- Options: A) in `AISettings.swift` (Models/) · B) in `KeystrokeMonitor.swift` (Engine/)
- Decision: **A)** — the enum is `Codable` and part of the user-facing config surface; keeping it alongside the struct that decodes it avoids coupling model semantics to the engine file. `AIInvocation` lives in `KeystrokeMonitor.swift` only because it is a private engine-internal flag.

## 6. Design

### 6.1 Enum + config field

Add to `Sources/Sokki/Models/AISettings.swift`:

```swift
enum PlaceholderStyle: String, Codable, Sendable, Equatable {
    case `static`  = "static"
    case braille    = "braille"
    case blockSweep = "block_sweep"
}
```

Append to `AISettings`:

```swift
var placeholderStyle: PlaceholderStyle = .static
```

Extend `CodingKeys` with `case placeholderStyle = "placeholder_style"` and extend `init(from:)` with lenient decode: `(try? container.decode(PlaceholderStyle.self, forKey: .placeholderStyle)) ?? .static` — this matches every other field's lenient pattern; unknown strings or wrong types fall back to `.static`.

Note: `AISettings.init()` (L16) stays empty — the stored-property default `.static` covers default construction.

### 6.2 Frame source

Static word and animation frame sets live on `KeystrokeMonitor` as private statics. A small helper maps a style + frame index to the rendered string:

```swift
private static let placeholderWord = "generating…"
private static let brailleFrames: [Character] = ["⠋","⠙","⠹","⠸","⠼","⠴","⠦","⠧","⠇","⠏"]
private static let blockFrames:   [Character] = ["▏","▎","▍","▌","▋","▊","▉","█"]

private static func placeholderFrame(style: PlaceholderStyle, index: Int) -> String {
    switch style {
    case .static:     return placeholderWord
    case .braille:    return "\(brailleFrames[index % brailleFrames.count]) \(placeholderWord)"
    case .blockSweep: return "\(blockFrames[index % blockFrames.count]) \(placeholderWord)"
    }
}
```

### 6.3 MainActor state

Two new private properties on `KeystrokeMonitor`:

```swift
private var animationTask: Task<Void, Never>?
private var aiPlaceholderLength: Int = 0
```

Promote the local `let placeholder` / `let placeholderLength` at L271–273 to use the style-driven initial frame, storing into `aiPlaceholderLength`. Every downstream reference to `placeholderLength` in the completion/failure branches reads `aiPlaceholderLength`.

### 6.4 `runAIFlow` changes

Around L269–283 (before dispatching the request):

```swift
let style = appConfig.ai.placeholderStyle
let initialFrame = Self.placeholderFrame(style: style, index: 0)
textInjector.injectDelta(initialFrame)
aiPlaceholderLength = initialFrame.count

try? await Task.sleep(nanoseconds: 50_000_000) // existing drain

tapContext.setActiveAIRequest(true)
tapContext.setSuppressMatching(false)
beginAIMouseWatch()
emitHUD(.aiThinking, trigger: match.trigger, note: nil)
startAnimationTaskIfNeeded(style: style)    // no-op for .static
```

After the `await aiRewriter.run(...)` returns, assert suppression BEFORE cancelling the animation so cooperative cancellation cannot race a listen-window resume into flipping suppression back to false:

```swift
tapContext.setSuppressMatching(true)   // fence first
animationTask?.cancel()
animationTask = nil
endAIMouseWatch()
let typed = tapContext.consumeUserTypedDuringAI()
```

If the animation Task wakes from its listen-window sleep on the very next MainActor turn, its first action is a `Task.isCancelled` check which will be true — it exits without touching `suppressMatching` or injecting further. `aiPlaceholderLength` is whatever the last completed frame set it to — the completion/failure branches use that value to erase.

Early-return paths in `runAIFlow` (bad prompt L190, AI busy L200, no selection L245, render abort L265) **never start the animation Task**, so no cleanup is required there. The post-await cancel is the sole teardown.

All references to the old local `placeholderLength` use `aiPlaceholderLength` instead.

### 6.5 Animation loop

```swift
private func startAnimationTaskIfNeeded(style: PlaceholderStyle) {
    guard style != .static else { return }
    animationTask?.cancel()
    animationTask = Task { @MainActor [weak self] in
        var index = 1 // frame 0 already injected by runAIFlow

        // Show frame 0 for a full listen window before the first swap, so frame 0
        // isn't visibly shorter than subsequent frames.
        try? await Task.sleep(nanoseconds: 50_000_000)

        while !Task.isCancelled {
            guard let self else { return }
            if self.tapContext.peekUserTypedDuringAI() { return }

            self.tapContext.setSuppressMatching(true)
            self.textInjector.eraseInjected(count: self.aiPlaceholderLength)
            let frame = Self.placeholderFrame(style: style, index: index)
            self.textInjector.injectDelta(frame)
            self.aiPlaceholderLength = frame.count

            // Drain synthesized events before re-enabling user-typing detection.
            try? await Task.sleep(nanoseconds: 70_000_000)
            if Task.isCancelled { return }
            self.tapContext.setSuppressMatching(false)

            // Listening window (typing detectable).
            try? await Task.sleep(nanoseconds: 50_000_000)
            index &+= 1
        }
    }
}
```

Cancellation behaviour: if cancelled mid-frame (between erase and drain), the frame completes and the Task exits after the `Task.sleep` throws. `suppressMatching` may be left in either state; the caller re-asserts `setSuppressMatching(true)` after `consumeUserTypedDuringAI` regardless.

### 6.6 `TapContext` peek accessor

Add to `Sources/Sokki/Engine/TapContext.swift`:

```swift
func peekUserTypedDuringAI() -> Bool {
    lock.withLock { state in state.userTypedDuringAI }
}
```

Non-destructive read. `consumeUserTypedDuringAI` remains the atomic read+clear used by the completion branch.

### 6.7 HUD note at L311

Change:

```swift
note: "result appended; “▎ thinking…” left inline — delete manually"
```

to:

```swift
note: "result appended; loading indicator left inline — delete manually"
```

Removes the third hardcoded copy of the old glyph.

### 6.8 Palette preview

`Sources/Sokki/Views/PaletteView.swift:139`:

```swift
private static let placeholderText = "⟨generating…⟩"
```

No config lookup. The interpolated caption at L129 continues to work unchanged.

### 6.9 Data flow (animated style)

```
trigger fires
  ↓
[suppress=true] erase trigger → capture inputs → render prompt
  ↓
inject frame 0 ("⠋ generating…")
  ↓
sleep 50ms (drain) → setActiveAIRequest(true) → [suppress=false]
  ↓
startAnimationTaskIfNeeded → Task { loop }
  ↓
await aiRewriter.run(...)    ← animation Task drives frames concurrently
  ↓
animationTask.cancel()        ← sync, task bails on next sleep resume
consumeUserTypedDuringAI()    ← atomic
[suppress=true]
  ↓
completion / failure / typed branch (uses aiPlaceholderLength)
```

### 6.10 Error handling & concurrency notes

- Unknown `placeholder_style` value in YAML: lenient decode falls back to `.static` silently (matches every other AISettings field — no log warning, intentionally consistent with existing config pattern).
- Animation Task cancelled during `Task.sleep`: sleep returns (possibly via throw), loop exits at `Task.isCancelled` check — no partial frame state leaks.
- User types during a 70 ms suppress window: `noteUserTypedDuringAI` ignores events while `suppressMatching=true`, so the keystroke is dropped until the listen window opens; the very next listen window records it, and the following frame's `peekUserTypedDuringAI` check aborts the loop. Worst case: one extra frame is rendered after the user's keystroke (AC6 wording accounts for this).
- `peekUserTypedDuringAI` (non-destructive) and `consumeUserTypedDuringAI` (atomic read+clear) are disjoint concerns: peek only gates further animation frames, consume drives the post-await completion branch. Double-observation is harmless.
- Config reload mid-flight (`updateConfig`) is intentionally ignored by the in-flight animation — `style` is captured into the Task closure at start, so a user toggling `placeholder_style` mid-wait does NOT retroactively change frames. This is KISS-correct and avoids races against `aiPlaceholderLength`; new config takes effect on the next AI request.
- `aiPlaceholderLength` stays consistent across frames because only the animation Task and the synchronous completion/failure branch ever touch it. They cannot run simultaneously on MainActor; between the `await aiRewriter.run` return and the next await (none, until the outer function exits), the animation Task cannot interleave, so `aiPlaceholderLength` is stable when completion reads it.
- Back-to-back AI requests: `isAIActive` guard at L197 refuses overlap. The previous run's `animationTask` is already nil'd by its own completion branch before the next request can observe `isAIActive == false`; defensive `animationTask?.cancel()` at the top of `startAnimationTaskIfNeeded` handles any pathological leak.

## 7. Files Changed

- `Sources/Sokki/Models/AISettings.swift` — add `PlaceholderStyle` enum, `placeholderStyle` field, CodingKey, lenient decode.
- `Sources/Sokki/Engine/KeystrokeMonitor.swift` — remove hardcoded `"▎ thinking…"`, add style-driven frame generator, `animationTask` + `aiPlaceholderLength` properties, `startAnimationTaskIfNeeded`, cancel hook post-await, generic L311 note.
- `Sources/Sokki/Engine/TapContext.swift` — add `peekUserTypedDuringAI()`.
- `Sources/Sokki/Views/PaletteView.swift` — replace preview glyph with `⟨generating…⟩`.

## 8. Verification

1. [AC1] Add `ai: { placeholder_style: bogus }` to `~/.config/sokki/match/default.yml`, reload, confirm behaviour matches `static` (`generating…` appears inline).
2. [AC2] Default config (no `placeholder_style`) triggers an AI match → `generating…` appears inline, not `▎ thinking…`.
3. [AC3] Set `placeholder_style: braille`, trigger AI → inline text cycles `⠋ generating…` → `⠙ generating…` → … at ~8 fps.
4. [AC4] Set `placeholder_style: block_sweep`, trigger AI → inline text cycles `▏ generating…` → `▎ generating…` → … at ~8 fps.
5. [AC5] With `placeholder_style: braille`, trigger AI, wait for completion without typing → full frame disappears, AI result appears cleanly.
6. [AC6] With `placeholder_style: braille`, trigger AI, type a letter during the wait → animation stops within ~120 ms, last visible frame remains, completion appends result after last frame + typed letter.
7. [AC7] With `placeholder_style: block_sweep`, trigger AI then force a failure (e.g. invalid model) → current (possibly mid-sweep) frame is erased fully and the trigger literal is restored (typed path) or nothing (palette path). This is the highest-risk behaviour change from the old code (was `let`, now `var` mutated by animation); verify no residual glyphs or partial frames remain.
8. [AC8] Open palette for an AI match → caption shows `⟨generating…⟩` regardless of `placeholder_style` setting.
9. [AC9] Reproduce typed-during-AI: HUD note reads `result appended; loading indicator left inline — delete manually`.
10. Run `bun build` — must compile cleanly under Swift 6 strict concurrency.
