# Recent Activity HUD ("Why Didn't It Fire?" v1)

## 1. Background

Sokki is a silent menu-bar text expander. When a trigger doesn't fire — because the app is toggled off, a `Cmd+V` just landed, a secure input field is active, or the user simply typo'd — there is today no user-visible signal beyond the menu-bar icon dimming. The engine already distinguishes these cases internally in `TapContext.feed` but collapses every non-firing path to `.none`, so users cannot tell "why nothing happened" apart from "I typed something that was never a trigger."

Idea doc `docs/ideas/02-ux.md` pitches an always-available HUD that explains these decisions. This design ships the honest MVP: a 20-slot ring of the engine's enumerated decision events, rendered in a non-activating floating panel behind a global hotkey. It intentionally does not attempt to explain pure typos (the matcher has no near-miss instrumentation) — that is deferred to v2 alongside regex triggers (`docs/ideas/03-power-user.md`).

## 2. Requirements Summary

**Goal:** Ship a v1 "Recent Activity" HUD that surfaces Sokki's enumerated decision events (matched / tooSoon / disabled) in a masked, hotkey-toggled `NSPanel`, built developer-first with a path to public polish.

**Scope:**

- In scope: non-activating floating `NSPanel` showing a 20-event ring buffer; global hotkey toggle wired through the existing `KeystrokeMonitor.flagsChanged` double-tap mechanism; new `AppConfig.hudShortcut` with domain `double_option | double_shift | double_control | none`, **default `none`** (opt-in; see §5 Decision 7); three MVP verdicts (`matched`, `tooSoon`, `disabled`); `{id, timestamp, verdict, capturedBuffer, matchedTrigger?}` event payload; masked-by-default buffer display with click-to-reveal per row; hard-drop of any event whose source frame was in secure input (no row, no character payload); "Create match from this" action per row reusing `MatchEditorView` via a new `.openMatchEditor` NotificationCenter signal mirroring `.openSettings`; session-only persistence.
- Out of scope: matcher instrumentation / near-miss / fuzzy-typo detection (deferred to v2); app-context capture, per-app scoping, detail panel, event drill-down; persistence across launches; export, search, filter within the HUD; any payload capture inside secure-input fields; verbose keystroke-level tracing (the Phase-1 `diagnostics_verbose` toggle is dropped from v1 — see §5 Decision 10).
- Note on the fourth verdict: Phase-1 summary listed `suppressed` alongside `matched / tooSoon / disabled`. On grounding in `TapContext.swift`, the only "suppressed" state is `state.suppressMatching`, the self-echo guard set by the injector around its own keystrokes — an internal invariant, not a user-meaningful verdict (it would flood the HUD right after every successful expansion). Dropped from v1. See §5 Decision 3.

## 3. Acceptance Criteria

1. Pressing the configured `hudShortcut` (user must explicitly set it — default is `none`) within the existing 0.3s double-tap window toggles a non-activating `NSPanel` without stealing focus from the frontmost app.
2. `AppConfig` exposes `hud_shortcut` with domain `double_option | double_shift | double_control | none`, default `none`, persisted to `~/.config/sokki/config/default.yml` in the same YAML shape as `toggle_shortcut`; value `none` fully disables the hotkey.
3. The HUD displays at most 20 most-recent decision events in reverse-chronological order; the 21st event evicts the oldest.
4. Every row renders verdict chip, timestamp (HH:mm:ss), and a masked `capturedBuffer` (each non-whitespace codepoint rendered as `•`); clicking a row toggles reveal of the raw text for that row only.
5. Events originating from a secure-input frame (`SecureInputMonitor.isSecureInputActive == true`) produce no row and no stored character payload anywhere in memory — the secure check precedes every emission site in `feed`.
6. Only verdicts in `{matched, tooSoon, disabled}` are recorded for v1; every other engine branch (`none`, `undo`, `secureInputChanged`, `toggled`, internal `suppressMatching`) is dropped.
7. Each row exposes a "Create match from this" action (row context menu) that posts `.openMatchEditor` carrying the row's unmasked `capturedBuffer` as the seed `trigger` via `userInfo`; Settings opens and the editor presents pre-filled, exactly as if the user had clicked `+` in `MatchListView`.
8. The event ring is session-only: quitting and relaunching Sokki yields an empty HUD.
9. Inside a single "blocked window" (an active `pasteSuppressionUntil` interval, or a contiguous `isEnabled == false` span), at most one blocked event of that kind is emitted — i.e., typing a sentence while Sokki is off yields one `disabled` row, not one per keystroke.
10. A typed sequence that does not reach any enumerated verdict (e.g., a typo with no sigil) produces zero HUD rows; the empty state is documented in code with a one-line comment on the `.none` arm of `TapContext.feed`.
11. The HUD panel never gains key status on show and never flips `NSApp.setActivationPolicy`; `applicationShouldTerminateAfterLastWindowClosed` still returns `false` — Sokki does not quit when the HUD closes.

## 4. Problem Analysis

**Current state:**
- `TapContext.feed` (`Sources/Sokki/Engine/TapContext.swift:33-99`) branches on every decision: `suppressMatching`, `pasteSuppressionUntil`, secure-input transitions, `isEnabled`, backspace undo, trie match. All non-firing paths collapse to `FeedAction.none`; the caller can't tell a blocked keystroke from a "still typing" keystroke.
- `RollingBuffer` (`Sources/Sokki/Engine/RollingBuffer.swift`) is a 128-char character ring with no notion of words; the backing array is pre-filled with `" "` sentinels and has a `tail(_ length:)` accessor plus a `currentCount` property. Reading the logical contents is `tail(currentCount)` — `String(buffer)` would return sentinel-contaminated garbage.
- `TrieMatcher` treats every `Character` as a trie edge and supports sigil-prefixed triggers like `;;hello`.
- `SecureInputMonitor` exposes a cheap non-mutating `isSecureInputActive` property wrapping `IsSecureEventInputEnabled()` (`Sources/Sokki/Engine/SecureInputMonitor.swift:7-9`), used by the existing `check()` transition logic but also directly callable.
- Sokki has one existing "global hotkey" — `toggleShortcut` — implemented entirely inside the `CGEventTap`'s `flagsChanged` branch (`KeystrokeMonitor.swift:213-220`, `TapContext.swift:107-150`). There is no Carbon `RegisterEventHotKey` / MASShortcut dependency.
- The only existing cross-component IPC for "open a window" is the `.openSettings` / `.checkForUpdates` NotificationCenter pattern (`SokkiApp.swift:16-44`, `AppDelegate.swift:58-66`). `SokkiApp.init` observes `.openSettings`, flips `.accessory → .regular`, activates, finds the window by title, presents it, and installs a `willClose` observer that reverts to `.accessory`.
- `MatchEditorView` already accepts a seed `Match?` + `prefillSourceFile` + `availableFiles` (`MatchEditorView.swift:14-29`); `MatchListView` drives editor presentation via `EditorRequest.new(prefillSourceFile:)` / `.edit(Match)` feeding `.sheet(item:)`.

**Privacy tension with `docs/ideas/05-privacy.md`:**
This HUD stores up to 20 × ~128-char plaintext snippets in RAM for the app's lifetime. That is raw keystroke content, masked only at render time. It is never written to disk, never exported, never observable by other processes (standard macOS process isolation), and cleared on quit (AC8). The ring does not include secure-input content (AC5). `docs/ideas/05-privacy.md` Idea 1 promises no telemetry leaves the process; this design conforms by never creating any network code path or persistence hook. Idea 2's "Redaction Mode for screenshares" is addressed by mask-by-default render + hard-drop of secure input; explicit screenshare hooks are deferred. Future work (v2): if the ring is ever exposed via export or remote debugging, add an encrypted-in-memory wrapper and make reveal-on-click require a re-auth.

**Approaches evaluated:**

- **A — Floating `NSPanel` + inline engine instrumentation.** Extend `TapContext.FeedAction` with new cases for blocked events; add an `HUDStore` (main-actor, `@Observable`); open/close a single `NSPanel` (lazy, non-activating) behind a new `AppConfig.hudShortcut` routed through the existing `handleModifierTap`. Reuses all existing infra.
- **B — SwiftUI `WindowGroup` scene.** Steals focus on every open via `.accessory → .regular` flip (the friction the April 19 "keep app alive on settings close" design explicitly avoids); useless as a "watch while typing" HUD.
- **C — `NSPopover` from the status item.** Requires aiming at the menu bar with the mouse mid-expansion; dismisses on outside click, so users can't observe events happening in another app.
- **D — New Carbon / MASShortcut global hotkey.** New dependency for one chord, duplicates infra we already have.

**Chosen: A.** The double-tap hotkey path is proven and already holds the tap thread; the panel is a 40-line AppKit wrapper around an existing SwiftUI view; the engine instrumentation is three new enum cases and two one-shot flags.

## 5. Decision Log

**1. Where does the ring live?**
- Options: A) `HUDStore` on `AppDelegate`, `@MainActor @Observable` · B) Inside `TapContext.State` (lock-guarded, read by tap thread and main) · C) Singleton service
- Decision: **A)** — Same pattern as `MatchStore` (`AppDelegate.swift:7`). Events are produced from the tap thread but hop to the main actor via `DispatchQueue.main.async` (the exact path `onMatch` / `onToggled` already use at `KeystrokeMonitor.swift:28-52`). SwiftUI can observe via plain `let store: HUDStore` thanks to `@Observable`; no `@Bindable` is needed because the view never mutates store fields (click-to-reveal state is view-local `@State`).

**2. Where is the verdict emitted from?**
- Options: A) Enrich `FeedAction` with new cases per blocked reason; `KeystrokeMonitor` dispatches to `HUDStore` · B) Add a separate `onHUDEvent` side-channel on `TapContext` · C) Main-actor polls `TapContext` state
- Decision: **A)** — `FeedAction` is already the one channel `TapContext → KeystrokeMonitor` uses for everything (match, undo, secureChanged, toggled). A second channel is duplication. New cases `.blockedTooSoon(bufferSnapshot:)` and `.blockedDisabled(bufferSnapshot:)` mirror the shape of existing cases. `.matched` does NOT gain a buffer-snapshot field — see Decision 5.

**3. Drop the `suppressed` verdict from Phase-1 summary.**
- Options: A) Map `suppressMatching` to a HUD `suppressed` verdict · B) Drop; the only code that sets `suppressMatching = true` is the injector's self-echo guard
- Decision: **B)** — `KeystrokeMonitor.swift:33,40,272,276` wraps every injection in `setSuppressMatching(true/false)`; emitting that as a user-visible verdict would fire after every successful expansion and confuse users. MVP verdict set is **three**: `matched`, `tooSoon`, `disabled`.

**4. Blocked-event debounce (AC9).**
- Options: A) Emit on every blocked keystroke · B) One-shot per "blocking window" using two `Bool` flags in `TapContext.State` · C) Time-based debounce
- Decision: **B)** — Option A floods the ring within a single paste or a single sentence typed while disabled. Option C needs a timer that adds state we don't already own. Two flags:
  - `hasEmittedTooSoonForCurrentPaste`: in `handlePasteDetected`, set to `false` **only when** `state.pasteSuppressionUntil == nil || Date() >= state.pasteSuppressionUntil!` (i.e., only when opening a fresh paste window). Re-pressing `Cmd+V` inside the existing 200ms window must not re-arm and cause a second row. Set to `true` on first emission inside `feed`'s paste-suppression branch.
  - `hasEmittedDisabledSinceLastEnable`: reset to `false` at every `isEnabled` false→true transition. There are TWO entry points to that transition (`handleModifierTap`'s double-tap toggle and `updateConfig`'s config-driven apply) and both must reset the flag; the design tolerates the mild duplication because losing either leaves a latent "disabled rows never re-arm" bug.

**5. Buffer snapshot contents for each verdict.**
- Options: A) Always capture the full `RollingBuffer` at decision time · B) For `.matched`, capture only `match.trigger`; for `.blocked*`, capture the current buffer plus the blocked character · C) Always capture nothing, display only verdict + timestamp
- Decision: **B)** — The full 128-char buffer contains up to 100 chars of prior typing that has nothing to do with the current decision; including it in `.matched` leaks uninteresting plaintext and produces visually noisy rows ("last 100 chars of what I typed this paragraph → trigger"). For `.matched`, `capturedBuffer = match.trigger` is the exact diagnostic payload. For `.blocked*`, the buffer short-circuits before the `state.buffer.append(character)` at `TapContext.swift:87`, so the snapshot is computed as `state.buffer.tail(state.buffer.currentCount) + String(character)` inline — short, relevant context for "what was I typing when Sokki blocked me?". `matchedTrigger` on the payload remains separate for display purposes; for `.matched`, `matchedTrigger == capturedBuffer` is tolerable (UI renders the trigger once, via the `matchedTrigger` field, and suppresses the redundant main line — see §6 HUDRow).

**6. HUD surface: `NSPanel`, how non-activating?**
- Options: A) `NSPanel` style mask `[.titled, .closable, .nonactivatingPanel, .hudWindow]`, `level = .floating`, `becomesKeyOnlyIfNeeded = true`, `hidesOnDeactivate = false`, `collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]` · B) Borderless panel with custom close control · C) Translucent overlay window
- Decision: **A)** — Stock AppKit HUD style. `[.nonactivatingPanel]` is the load-bearing flag: `orderFrontRegardless()` shows it without making Sokki frontmost, so the user's caret context in the target app is preserved (verified mental model for AC1 and AC11). `canJoinAllSpaces` means the HUD follows Space switches without re-show. Close button (from `.closable`) and "click the panel title bar to make it key, then Escape or Cmd+W to close" are sufficient dismissal affordances; no custom global keyboard monitor is added (a non-activating panel cannot observe Escape while the user is typing in a different app without a privacy-hostile global monitor — see Decision 9).

**7. Hotkey infra: extend existing `handleModifierTap`, and default `hudShortcut` to `none`.**
- Options: A) Extend `handleModifierTap` to support two chords simultaneously; default `hudShortcut` to `double_control` · B) Same, but default `hudShortcut` to `none` (opt-in) · C) Separate `handleHUDModifierTap` · D) New Carbon hotkey registration
- Decision: **B)** — The state vars `lastModifierTapTime` / `lastModifierKey` already track one chord; extending to N is a `switch` on the resolved chord against both `toggleShortcut` and `hudShortcut`. Collision policy: if both config fields happen to be set to the same value, `hudShortcut` wins (new feature doesn't silently do nothing if user misconfigures). Default `none` matches the project's existing "opt-in hotkey" philosophy (`toggleShortcut` also defaults to `none`, per `Bootstrapper.swift:48`) and prevents silently stealing an upgrader's `toggle_shortcut: double_control` setting. `Bootstrapper.writeDefaultConfig` adds a `hud_shortcut: "none"` line so fresh installs see the knob.

**8. `FeedAction.hudRequested` as the new action for hotkey emission.**
- Options: A) Reuse `.toggled` semantics with a separate callback parameter · B) New `FeedAction.hudRequested` case and a new `onHUDRequested: (() -> Void)?` callback on `KeystrokeMonitor` · C) Post a notification directly from the tap thread
- Decision: **B)** — Mirrors `onToggled` exactly. Tap thread dispatches to main via `DispatchQueue.main.async`, which calls `AppDelegate.hudPanelController.toggle()`. No notification overhead; typed API.

**9. Panel ownership and lifecycle.**
- Options: A) Lazy singleton created on first hotkey, retained on `AppDelegate` · B) Allocated at launch, hidden until first hotkey · C) Recreated each show
- Decision: **A)** — Ships zero overhead for users who never press the hotkey. Controller is `HUDPanelController` on `AppDelegate`. `toggle()`: create-if-nil, `isVisible ? orderOut(nil) : orderFrontRegardless()`. No `makeKeyAndOrderFront` — that would activate Sokki. Escape dismissal is **explicitly NOT wired** via a global event monitor (privacy cost + overkill); the panel's close button (`.closable` style) plus clicking its title bar to make it key (which then makes standard `Cmd+W` work through `performClose:`) are sufficient dismissal paths. The hotkey itself is the primary toggle — press it again to hide.

**10. Drop the `diagnostics_verbose` field from v1.**
- Options: A) Ship the verbose toggle (per Phase-1 Q3 answer E) alongside the MVP · B) Decode-but-don't-read · C) Drop entirely; re-add with a one-line defensive decode when v2 needs it
- Decision: **C)** — A field that appears in `AppConfig` but is read nowhere is dead code that invites "what does this do?" questions in review without paying rent. Swift's `try?`-with-default decode means v2 can add the field with zero migration concern. Drop from v1. Phase-1 Q3's E-level verbose pipe is a v2 concern.

**11. "Create match from this" payload — plain notification, no store slot.**
- Options: A) Pass the trigger string via `.openMatchEditor` `userInfo` dictionary · B) Add `MatchStore.pendingSeed: Match?` and post an empty notification · C) Full seed `Match` via `userInfo`
- Decision: **A)** — `userInfo` accepts any value (`[AnyHashable: Any]?`); carrying a plain `String` under key `"trigger"` is zero-ceremony and requires no new state slot on `MatchStore`. (The earlier worry that `userInfo` forces `NSObject` bridging was incorrect — Foundation's dictionary is perfectly happy with Swift value types.) `MatchListView` observes `.openMatchEditor`, pulls `trigger`, and opens `MatchEditorView` via `EditorRequest.new(prefillSourceFile: resolvedPrefill(), initialTrigger: trigger)`. Dropping `pendingSeed` removes one cross-component data coupling and one cleanup race (the Round 1 reviewer item #18).

**12. Masking representation.**
- Options: A) Replace each non-whitespace codepoint with `•` (preserves length, hides content, whitespace stays for natural wrapping) · B) Fixed-length redaction bar · C) Blur effect
- Decision: **A)** — Length preserved makes "I typed a 12-char thing" recoverable visually without exposing content. Whitespace is passed through unchanged so wrapping still looks natural. Revealed state uses monospaced font for the buffer to match the trigger styling in `MatchEditorView`.

**13. Row interaction: click-to-reveal + right-click context.**
- Options: A) Left-click reveals; right-click opens a context menu with "Create match from this" + "Copy" · B) Single reveal-toggle button per row · C) Hover reveals
- Decision: **A)** — Cheapest and reuses the familiar right-click pattern already present in `MatchListView` (`MatchListView.swift:156-169`). Hover-reveal is a privacy hazard (accidental screenshare reveal). `"Copy"` copies the raw text to `NSPasteboard.general` directly (one-liner; no need to route through `ClipboardManager`, which is wired to injection semantics not UI clipboard).

**14. Ring size.**
- Options: A) 20, hidden constant · B) 20, configurable via `AppConfig` · C) 50
- Decision: **A)** — YAGNI on configurability. The constant lives as `HUDStore.capacity`. Twenty is enough for a work-session glance; add config if users ask.

**15. `EditorRequest.new` gets an optional `initialTrigger`.**
- Options: A) Add a new enum case `.newWithSeed` · B) Extend `.new(prefillSourceFile:)` to `.new(prefillSourceFile:, initialTrigger: String?)` · C) Reuse `.edit(Match)` by constructing a synthetic match
- Decision: **B)** — Two call sites in `MatchListView` gain a `nil` argument; one new call site (the `.onReceive(.openMatchEditor)` observer) passes the trigger. `.edit(Match)` (option C) would break because `onSave` routes through `store.update`, which silently no-ops for a match not in `store.matches` (the Round 1 reviewer item #17). `MatchEditorView` gains an optional `initialTrigger: String? = nil` parameter; its `@State var trigger = match?.trigger ?? initialTrigger ?? ":"` resolution picks the seed when no full `Match` is supplied. Three-line change to the editor, zero new enum cases.

**16. Seed `sourceFile` left nil; `MatchStore.add` fallback applies.**
- Options: A) Hardcode `sourceFile: "default.yml"` at the seed construction site · B) Pass `nil` and trust `MatchStore.add`'s existing fallback at `MatchStore.swift:42`
- Decision: **B)** — `add` already coerces nil → `"default.yml"`; duplicating that default in the HUD path is drift-risk. For v1 the editor's `prefillSourceFile` is `resolvedPrefill()` (active list filter or `"default.yml"`), which the user can change in the Picker before saving.

**17. Testing.**
- Decision: **No automated tests** — the project has no test target; all existing designs (including `2026-04-20-improve-match-list.md:106`) ship with manual verification. This feature follows the same convention. See §8.

## 6. Design

### Data layer

**New file — `Sources/Sokki/Models/HUDEvent.swift`**

```swift
import Foundation

struct HUDEvent: Identifiable, Sendable, Equatable {
    enum Verdict: String, Sendable, Equatable {
        case matched
        case tooSoon
        case disabled
    }

    let id: UUID
    let timestamp: Date
    let verdict: Verdict
    let capturedBuffer: String     // may be raw; view masks on render
    let matchedTrigger: String?    // only set for .matched (== capturedBuffer)
}
```

**New file — `Sources/Sokki/Models/HUDStore.swift`**

```swift
import Foundation
import Observation

@Observable
@MainActor
final class HUDStore {
    private(set) var events: [HUDEvent] = []
    static let capacity: Int = 20

    func record(_ event: HUDEvent) {
        events.insert(event, at: 0)
        if events.count > Self.capacity {
            events.removeLast(events.count - Self.capacity)
        }
    }

    func clear() { events.removeAll() }
}
```

**Update — `Sources/Sokki/Models/AppConfig.swift`**

Add `var hudShortcut: String = "none"` immediately after `toggleShortcut` (line 11). Add `case hudShortcut = "hud_shortcut"` immediately after `.toggleShortcut = "toggle_shortcut"` in `CodingKeys` (line 21). Append the defensive decode as the last line of `init(from:)`: `self.hudShortcut = (try? container.decode(String.self, forKey: .hudShortcut)) ?? "none"`. Do not add `diagnosticsVerbose` (Decision 10).

**Update — `Sources/Sokki/Models/MatchStore.swift`**

Add the notification name only (no `pendingSeed`):

```swift
extension Notification.Name {
    static let openMatchEditor = Notification.Name("Sokki.openMatchEditor")
}
```

### Engine layer

**Update — `Sources/Sokki/Engine/RollingBuffer.swift`**

Add an accessor that reuses the existing `tail` logic — do NOT use `String(buffer)`, which would include sentinel spaces:

```swift
func toString() -> String { tail(currentCount) }
```

**Update — `Sources/Sokki/Engine/TapContext.swift`**

Extend `FeedAction`:

```swift
enum FeedAction: Sendable {
    case none
    case matched(match: Match, triggerLength: Int)   // unchanged signature
    case undo(restoreTrigger: String)
    case secureInputChanged(isSecure: Bool)
    case toggled(isEnabled: Bool)
    case hudRequested
    case blockedTooSoon(bufferSnapshot: String)
    case blockedDisabled(bufferSnapshot: String)
}
```

Extend `State`:

```swift
var hudShortcut: String = "none"
var hasEmittedTooSoonForCurrentPaste: Bool = false
var hasEmittedDisabledSinceLastEnable: Bool = false
```

Reset-flag logic (Decision 4):

- `handlePasteDetected`: at the top of the lock-guarded block, check
  `if state.pasteSuppressionUntil == nil || Date() >= state.pasteSuppressionUntil! { state.hasEmittedTooSoonForCurrentPaste = false }`
  *before* assigning the new `pasteSuppressionUntil`. This prevents a `Cmd+V` repeated within the existing 200ms window from re-arming the flag.
- `updateConfig`: on observed false→true transition of `isEnabled`, set `hasEmittedDisabledSinceLastEnable = false`.
- `handleModifierTap`: inside the double-tap toggle path, after `state.isEnabled.toggle()`, if the new value is `true`, set `hasEmittedDisabledSinceLastEnable = false`.

**Critical ordering in `feed` (AC5):** the secure-input check must precede the tooSoon and disabled emission arms, so that a blocked keystroke arriving during secure input never emits. Reorder to:

```swift
func feed(character: Character, keyCode: UInt16, modifierFlags: CGEventFlags) -> FeedResult {
    lock.withLock { state in
        // 1) self-echo suppression (unchanged)
        if state.suppressMatching { return FeedResult(action: .none) }

        // 2) secure input FIRST — any active secure context hard-drops everything below.
        let secureTransition = state.secureInput.check()
        switch secureTransition {
        case .enteredSecure:
            state.buffer.clear(); state.matcher.reset()
            return FeedResult(action: .secureInputChanged(isSecure: true))
        case .exitedSecure:
            state.buffer.clear(); state.matcher.reset()
            return FeedResult(action: .secureInputChanged(isSecure: false))
        case .stillSecure:
            return FeedResult(action: .none)
        case .notSecure:
            break
        }

        // 3) paste-too-soon (now guaranteed non-secure)
        if let until = state.pasteSuppressionUntil, Date() < until {
            let snapshot = state.buffer.toString() + String(character)
            if !state.hasEmittedTooSoonForCurrentPaste {
                state.hasEmittedTooSoonForCurrentPaste = true
                return FeedResult(action: .blockedTooSoon(bufferSnapshot: snapshot))
            }
            return FeedResult(action: .none)
        }
        state.pasteSuppressionUntil = nil

        // 4) disabled (now guaranteed non-secure, not-paste-blocked)
        guard state.isEnabled else {
            let snapshot = state.buffer.toString() + String(character)
            if !state.hasEmittedDisabledSinceLastEnable {
                state.hasEmittedDisabledSinceLastEnable = true
                return FeedResult(action: .blockedDisabled(bufferSnapshot: snapshot))
            }
            return FeedResult(action: .none)
        }

        // 5) backspace undo (unchanged)
        // 6) buffer append + matcher feed (unchanged); .matched is emitted with the
        //    existing signature — capturedBuffer for the HUD is set to match.trigger in
        //    KeystrokeMonitor (see Bridging). `.none` here is intentionally not an event
        //    (AC10).
    }
}
```

Hotkey resolution (Decision 7) — refactor `handleModifierTap`:

```swift
func handleModifierTap(flags: CGEventFlags) -> FeedResult {
    lock.withLock { state in
        let modifierMask: CGEventFlags = [.maskShift, .maskControl, .maskAlternate, .maskCommand]
        let activeModifiers = flags.intersection(modifierMask)

        func targetFlag(for shortcut: String) -> CGEventFlags? {
            switch shortcut {
            case "double_option":  return .maskAlternate
            case "double_shift":   return .maskShift
            case "double_control": return .maskControl
            default:               return nil
            }
        }

        let hudFlag = targetFlag(for: state.hudShortcut)
        let toggleFlag = targetFlag(for: state.toggleShortcut)

        let resolved: (kind: String, flag: CGEventFlags)?
        if let h = hudFlag, activeModifiers == h {
            resolved = ("hud", h)                      // hud wins on collision (Decision 7)
        } else if let t = toggleFlag, activeModifiers == t {
            resolved = ("toggle", t)
        } else {
            state.lastModifierTapTime = nil
            return FeedResult(action: .none)
        }

        let r = resolved!
        let now = Date()
        if let lastTime = state.lastModifierTapTime,
           state.lastModifierKey == r.flag,
           now.timeIntervalSince(lastTime) < 0.3 {
            state.lastModifierTapTime = nil
            state.lastModifierKey = nil
            switch r.kind {
            case "hud":
                return FeedResult(action: .hudRequested)
            case "toggle":
                state.isEnabled.toggle()
                state.buffer.clear()
                state.matcher.reset()
                if state.isEnabled { state.hasEmittedDisabledSinceLastEnable = false }
                return FeedResult(action: .toggled(isEnabled: state.isEnabled))
            default:
                return FeedResult(action: .none)
            }
        }

        state.lastModifierTapTime = now
        state.lastModifierKey = r.flag
        return FeedResult(action: .none)
    }
}
```

Update `updateConfig`:

```swift
func updateConfig(_ config: AppConfig) {
    lock.withLock { state in
        let wasDisabled = !state.isEnabled
        state.isEnabled = config.isEnabled
        state.toggleShortcut = config.toggleShortcut
        state.hudShortcut = config.hudShortcut
        if wasDisabled && state.isEnabled {
            state.hasEmittedDisabledSinceLastEnable = false
        }
    }
}
```

### Bridging to main actor

**Update — `Sources/Sokki/Engine/KeystrokeMonitor.swift`**

Add two callbacks on `KeystrokeMonitor` alongside the existing ones:

```swift
var onHUDRequested: (() -> Void)?
var onHUDEvent: ((HUDEvent) -> Void)?
```

Plumb through `TapHandler` with matching **non-optional** `@Sendable` closures (same pattern as the existing `onMatch` / `onUndo` / `onSecureInputChanged` / `onToggled` at `KeystrokeMonitor.swift:186-189, 199-202`). `TapHandler` stores them as `let`:

```swift
let onHUDRequested: @Sendable () -> Void
let onHUDEvent: @Sendable (HUDEvent) -> Void
```

**The main-queue hop lives inside the closures built in `KeystrokeMonitor.start()`, not in `TapHandler.handleEvent`** — identical to the existing `onMatch` / `onUndo` / `onSecure` / `onToggle` pattern at `KeystrokeMonitor.swift:27-52`. `TapHandler` calls the closures directly (synchronously, on the tap thread); the closures themselves dispatch to main:

```swift
// inside KeystrokeMonitor.start(), alongside the existing onMatch / onUndo / onSecure / onToggle:
let onHUDRequest: @Sendable () -> Void = { [weak self] in
    DispatchQueue.main.async { self?.onHUDRequested?() }
}
let onHUDEv: @Sendable (HUDEvent) -> Void = { [weak self] event in
    DispatchQueue.main.async { self?.onHUDEvent?(event) }
}
// pass onHUDRequest / onHUDEv to TapHandler.init.
```

`flagsChanged` branch in `TapHandler.handleEvent` — add the `.hudRequested` arm; note the direct (non-dispatched) closure calls:

```swift
if type == .flagsChanged {
    let result = context.handleModifierTap(flags: flags)
    switch result.action {
    case .toggled(let enabled):
        onToggled(enabled)          // closure dispatches to main
    case .hudRequested:
        onHUDRequested()            // closure dispatches to main
    default: break
    }
    return Unmanaged.passRetained(event)
}
```

`.keyDown` branch — emit the HUD event on the tap thread via the direct closure call. The closure's internal `DispatchQueue.main.async` serializes the HUD record with the injection on main, so the `.matched` arm's textual order between `onHUDEvent` and `context.setSuppressMatching` / `onMatch` is observationally irrelevant (both main-hops land in post order anyway). Keep the existing pattern — `setSuppressMatching` then `onMatch` — and add `onHUDEvent` adjacent:

```swift
case .matched(let match, let triggerLength):
    // HUD: capturedBuffer is the trigger, not the full rolling buffer (Decision 5).
    onHUDEvent(HUDEvent(
        id: UUID(), timestamp: Date(),
        verdict: .matched, capturedBuffer: match.trigger, matchedTrigger: match.trigger
    ))
    context.setSuppressMatching(true)
    onMatch(match, triggerLength)

case .blockedTooSoon(let snapshot):
    onHUDEvent(HUDEvent(
        id: UUID(), timestamp: Date(),
        verdict: .tooSoon, capturedBuffer: snapshot, matchedTrigger: nil))

case .blockedDisabled(let snapshot):
    onHUDEvent(HUDEvent(
        id: UUID(), timestamp: Date(),
        verdict: .disabled, capturedBuffer: snapshot, matchedTrigger: nil))
```

`.undo`, `.secureInputChanged`, `.toggled`, `.none` — do not emit HUD events (AC6, AC10). `.undo` explicitly does not retroactively mutate the preceding `Matched` row — the matched event happened; the row remains as historical record.

### UI layer

**New file — `Sources/Sokki/HUDPanelController.swift`** — placement mirrors the existing top-level `StatusItemController.swift`.

```swift
import AppKit
import SwiftUI

@MainActor
final class HUDPanelController {
    private var panel: NSPanel?
    private let store: HUDStore
    private let onCreateMatch: (String) -> Void   // main-actor implicit

    init(store: HUDStore, onCreateMatch: @escaping (String) -> Void) {
        self.store = store
        self.onCreateMatch = onCreateMatch
    }

    func toggle() {
        if let panel, panel.isVisible {
            panel.orderOut(nil)
            return
        }
        if panel == nil { panel = makePanel() }
        panel?.orderFrontRegardless()   // non-activating; frontmost app keeps key
    }

    private func makePanel() -> NSPanel {
        let p = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: 380, height: 420),
            styleMask: [.titled, .closable, .nonactivatingPanel, .hudWindow],
            backing: .buffered, defer: false
        )
        p.title = "Sokki Recent Activity"
        p.level = .floating
        p.isFloatingPanel = true
        p.becomesKeyOnlyIfNeeded = true
        p.hidesOnDeactivate = false
        p.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        p.isReleasedWhenClosed = false
        p.center()
        p.contentView = NSHostingView(rootView: HUDView(
            store: store,
            onCreateMatch: { [weak self] buffer in self?.onCreateMatch(buffer) }
        ))
        return p
    }
}
```

**New file — `Sources/Sokki/Views/HUDView.swift`**

```swift
import SwiftUI

struct HUDView: View {
    let store: HUDStore                   // plain let; @Observable drives re-render
    let onCreateMatch: (String) -> Void
    @State private var revealed: Set<UUID> = []

    var body: some View {
        Group {
            if store.events.isEmpty {
                ContentUnavailableView(
                    "No recent activity",
                    systemImage: "clock.arrow.circlepath",
                    description: Text("Type in another app — matches and blocks appear here.")
                )
            } else {
                List(store.events) { ev in
                    HUDRow(
                        event: ev,
                        isRevealed: revealed.contains(ev.id),
                        onToggleReveal: { toggle(ev.id) },
                        onCreateMatch: { onCreateMatch(ev.capturedBuffer) }
                    )
                }
                .listStyle(.inset(alternatesRowBackgrounds: true))
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .frame(minWidth: 380, minHeight: 420)
    }

    private func toggle(_ id: UUID) {
        if revealed.contains(id) { revealed.remove(id) } else { revealed.insert(id) }
    }
}

private struct HUDRow: View {
    let event: HUDEvent
    let isRevealed: Bool
    let onToggleReveal: () -> Void
    let onCreateMatch: () -> Void

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            VerdictChip(verdict: event.verdict)

            VStack(alignment: .leading, spacing: 2) {
                // For .matched, capturedBuffer == matchedTrigger; suppress the main line
                // to avoid the duplicate trigger rendered once as primary and once as "→ …".
                if event.verdict != .matched {
                    Text(renderedBuffer)
                        .font(.system(.body, design: .monospaced))
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                if let t = event.matchedTrigger {
                    Text(event.verdict == .matched ? t : "→ \(t)")
                        .font(event.verdict == .matched ? .system(.body, design: .monospaced) : .caption)
                        .foregroundStyle(event.verdict == .matched ? .primary : .secondary)
                }
            }
            Spacer(minLength: 8)
            Text(Self.timeFormatter.string(from: event.timestamp))
                .font(.caption.monospacedDigit())
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 2)
        .contentShape(Rectangle())
        .onTapGesture { onToggleReveal() }
        .contextMenu {
            Button("Create match from this") { onCreateMatch() }
            Button("Copy") { copy() }
        }
    }

    private var renderedBuffer: String {
        if isRevealed { return event.capturedBuffer }
        return String(event.capturedBuffer.map { $0.isWhitespace ? $0 : "•" })
    }

    private func copy() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(event.capturedBuffer, forType: .string)
    }

    private static let timeFormatter: DateFormatter = {
        let f = DateFormatter(); f.dateFormat = "HH:mm:ss"; return f
    }()
}

private struct VerdictChip: View {
    let verdict: HUDEvent.Verdict

    var body: some View {
        Text(label)
            .font(.caption).bold()
            .foregroundStyle(.primary)
            .padding(.horizontal, 6).padding(.vertical, 2)
            .background(tint.opacity(0.25), in: Capsule())
    }

    private var label: String {
        switch verdict {
        case .matched:  return "Matched"
        case .tooSoon:  return "Too soon"
        case .disabled: return "Disabled"
        }
    }
    private var tint: Color {
        switch verdict {
        case .matched:  return .green
        case .tooSoon:  return .orange
        case .disabled: return .secondary
        }
    }
}
```

### Wiring

**Update — `Sources/Sokki/AppDelegate.swift`**

```swift
let hudStore = HUDStore()                       // stored property, @MainActor
private var hudPanelController: HUDPanelController!

// in applicationDidFinishLaunching, after statusItemController and matchStore are set up:
//
// IMPORTANT: this closure must NOT capture `self` — HUDPanelController is retained by
// AppDelegate, and AppDelegate is retained by NSApp; any `self`-capture here creates a
// cycle. All the work here is notification posting, which needs no self reference.
hudPanelController = HUDPanelController(store: hudStore) { buffer in
    // Open Settings first (lazy-instantiates MatchListView on first open).
    NotificationCenter.default.post(name: .openSettings, object: nil)
    // Defer the editor-open notification one runloop turn so MatchListView's
    // .onReceive subscription has landed before the post (first-open race; see §Error
    // handling). Subsequent opens are no-ops on the Settings side and the async hop
    // costs ~nothing.
    DispatchQueue.main.async {
        NotificationCenter.default.post(
            name: .openMatchEditor, object: nil,
            userInfo: ["trigger": buffer]
        )
    }
}

keystrokeMonitor.onHUDRequested = { [weak self] in
    self?.hudPanelController.toggle()
}
keystrokeMonitor.onHUDEvent = { [weak self] event in
    self?.hudStore.record(event)
}
```

**Update — `Sources/Sokki/Views/MatchListView.swift`**

`EditorRequest` stays private to the view. Extend `.new` with an optional `initialTrigger` (Decision 15):

```swift
enum EditorRequest: Identifiable, Equatable {
    case edit(Match)
    case new(prefillSourceFile: String, initialTrigger: String? = nil)

    var id: String {
        switch self {
        case .edit(let m):                          return "edit-\(m.id.uuidString)"
        case .new(let f, let t):                    return "new-\(f)-\(t ?? "")"
        }
    }
}
```

Existing `.new` call sites (add-via-`+`) pass `initialTrigger: nil` (the default applies so they compile unchanged). Add the observer:

```swift
.onReceive(NotificationCenter.default.publisher(for: .openMatchEditor)) { note in
    let trigger = (note.userInfo?["trigger"] as? String) ?? ""
    guard !trigger.isEmpty else { return }
    sheetRequest = .new(prefillSourceFile: resolvedPrefill(), initialTrigger: trigger)
}
```

Update `sheetContent(for:)` to destructure the new associated value on the `.new` arm (pattern match binds both values, unlike the constructor call site which can rely on the default):

```swift
case .new(let prefill, let initial):
    MatchEditorView(
        match: nil,
        availableFiles: store.availableSourceFiles,
        prefillSourceFile: prefill,
        initialTrigger: initial,
        onSave: { m in store.add(m); sheetRequest = nil },
        onCancel: { sheetRequest = nil }
    )
```

**Update — `Sources/Sokki/Views/MatchEditorView.swift`**

Add `initialTrigger: String? = nil` to the initializer; use it as the `@State` seed when no full `Match` is provided:

```swift
init(
    match: Match? = nil,
    availableFiles: [String],
    prefillSourceFile: String,
    initialTrigger: String? = nil,
    onSave: @escaping (Match) -> Void,
    onCancel: @escaping () -> Void
) {
    self._trigger = State(initialValue: match?.trigger ?? initialTrigger ?? ":")
    self._replace = State(initialValue: match?.replace ?? "")
    self._isEnabled = State(initialValue: match?.isEnabled ?? true)
    self._sourceFile = State(initialValue: match?.sourceFile ?? prefillSourceFile)
    self.existingId = match?.id
    self.availableFiles = availableFiles
    self.onSave = onSave
    self.onCancel = onCancel
}
```

**Update — `Sources/Sokki/Views/SettingsView.swift`**

Add a "HUD Shortcut" section mirroring the existing "Toggle Shortcut" Picker (`SettingsView.swift:59-67`):

```swift
Section("HUD Shortcut") {
    Picker("Recent Activity HUD", selection: $store.config.hudShortcut) {
        Text("Double Option").tag("double_option")
        Text("Double Shift").tag("double_shift")
        Text("Double Control").tag("double_control")
        Text("None").tag("none")
    }
    .help("Double-tap to open the Recent Activity HUD. HUD wins over Toggle if both set to same chord.")
}
```

**Update — `Sources/Sokki/Services/Bootstrapper.swift`**

In `writeDefaultConfig`, add one line after `toggle_shortcut` so fresh installs see the field in their config:

```
hud_shortcut: "none"              # "none" (default), "double_option", "double_shift", or "double_control"
```

### Data flow

1. User double-taps their configured HUD chord in app X → `flagsChanged` event → `TapContext.handleModifierTap` → `.hudRequested` → tap thread dispatches `self?.onHUDRequested?()` to main → `HUDPanelController.toggle()` → panel orders front via `orderFrontRegardless()`; app X retains key-window status.
2. User types `;;hello` in app X; trigger fires → `TapContext.feed` returns `.matched(..., triggerLength:)` → on the tap thread, `KeystrokeMonitor` dispatches `onHUDEvent` (main-hop: `HUDStore.record`) BEFORE calling `context.setSuppressMatching(true)` and `onMatch`. Both the HUD record and the injection land on main; SwiftUI sees the updated `store.events` and re-renders the HUD.
3. User presses `Cmd+V` then immediately types `;;em` → `handlePasteDetected` opens a fresh window, arming `hasEmittedTooSoonForCurrentPaste = false`. First blocked keystroke inside the window returns `.blockedTooSoon(snapshot: "…;")` and sets the flag true; subsequent blocked keystrokes return `.none`. Another `Cmd+V` inside the still-active window does NOT re-arm the flag (Decision 4).
4. User toggles Sokki off via the configured toggle chord, types a sentence → first keystroke returns `.blockedDisabled(snapshot: …)`; the flag is only cleared on the next false→true transition (reached via either `handleModifierTap` or `updateConfig`).
5. User right-clicks a HUD row → "Create match from this" → `HUDPanelController.onCreateMatch(buffer)` → `AppDelegate` posts `.openSettings` then `.openMatchEditor` with `userInfo: ["trigger": buffer]`. `SokkiApp` opens Settings per its existing observer; `MatchListView`'s `.onReceive(.openMatchEditor)` reads the trigger, opens `MatchEditorView` via `EditorRequest.new(prefillSourceFile: resolvedPrefill(), initialTrigger: trigger)`.

### Error handling

- **Secure-input events never reach `HUDStore`.** The reordered `feed` checks secure input FIRST (before tooSoon / disabled), guaranteeing AC5 at the emission site. Tests must confirm this by typing into a password field while a prior `pasteSuppressionUntil` window is still live.
- **Non-character events (flagsChanged, modifier-only keys):** never reach the HUD-event emission path because they don't hit `TapContext.feed`.
- **HUD panel shown when Sokki has been quit and relaunched:** `HUDStore` is instance state on `AppDelegate`; relaunch creates a fresh store → empty ring (AC8).
- **Collision between `toggleShortcut` and `hudShortcut`:** HUD wins (Decision 7); `help` text in Settings documents this. Default `hudShortcut = none` means an upgrader who had `toggle_shortcut: double_control` keeps their toggle working and opts into the HUD explicitly.
- **Both shortcuts set to `none`:** `handleModifierTap` resolves neither; falls through to the "unmapped" reset arm (`lastModifierTapTime = nil`) and returns `.none`. No-op, no state corruption. See §8 verification.
- **Concurrent writes to `HUDStore`:** always dispatched to main via `DispatchQueue.main.async` from the tap thread's `onHUDEvent` closure (same pattern as `onMatch`). The `@MainActor` class-level isolation guarantees serialization.
- **AppConfig migration from pre-HUD versions:** `init(from:)` uses `try? container.decode(...) ?? default` for every field; existing configs without `hud_shortcut` decode to the default `"none"` without error. Same idiom as the other fields at `AppConfig.swift:28-35`.
- **`.undo` after a `.matched`:** the HUD row recording the match remains visible as historical record; undo does not mutate it.
- **HUD hotkey fired during secure input:** `handleModifierTap` is called unconditionally in the `flagsChanged` branch — it does not check secure-input state. Opening the HUD while a password field is focused is safe: the panel floats, adds no new rows for secure keystrokes (AC5), and does not steal focus from the secure field.
- **First-time Settings open race:** SwiftUI's `Window` scene lazy-creates `MatchListView` on first `makeKeyAndOrderFront`. `MatchListView.onReceive(.openMatchEditor)` subscribes during the first `body` evaluation. Posting `.openMatchEditor` synchronously immediately after `.openSettings` could fire before the subscription is live. Mitigation: `AppDelegate`'s `onCreateMatch` closure wraps the `.openMatchEditor` post in `DispatchQueue.main.async` so it lands one runloop turn later, after the view has instantiated. On subsequent opens (Settings already created), the deferred post is still harmless — same cost.
- **"Copy" context menu on matched rows (known minor redundancy):** for `.matched` rows, `capturedBuffer == match.trigger`, so "Copy" copies the user's own trigger. Not useful, but not harmful; leaving uniform behavior rather than conditionally hiding the menu item to keep the row's interaction model predictable.

### Testing strategy

None automated — project convention. Manual verification in §8.

## 7. Files Changed

- `Sources/Sokki/Models/AppConfig.swift` — add `hudShortcut` field with CodingKey `"hud_shortcut"` and defensive decode (default `"none"`).
- `Sources/Sokki/Models/MatchStore.swift` — add `.openMatchEditor` notification name.
- `Sources/Sokki/Models/HUDEvent.swift` — NEW (event payload + verdict enum).
- `Sources/Sokki/Models/HUDStore.swift` — NEW (`@Observable` main-actor ring buffer).
- `Sources/Sokki/Engine/RollingBuffer.swift` — add `toString() -> String` implemented via `tail(currentCount)`.
- `Sources/Sokki/Engine/TapContext.swift` — extend `FeedAction` with `.hudRequested`, `.blockedTooSoon`, `.blockedDisabled`; add `hudShortcut` / `hasEmittedTooSoonForCurrentPaste` / `hasEmittedDisabledSinceLastEnable` to `State`; **reorder `feed` so the secure-input check runs before the tooSoon/disabled branches**; refactor `handleModifierTap` for dual-chord resolution; reset-flag logic in `handlePasteDetected` (guarded by fresh-window check) / `updateConfig` / `handleModifierTap`.
- `Sources/Sokki/Engine/KeystrokeMonitor.swift` — add `onHUDRequested` and `onHUDEvent` callbacks; propagate through `TapHandler`; handle new `FeedAction` cases in `handleEvent`; emit HUD event BEFORE `setSuppressMatching` on the `.matched` arm.
- `Sources/Sokki/HUDPanelController.swift` — NEW (lazy `NSPanel` controller; mirrors top-level placement of `StatusItemController.swift`).
- `Sources/Sokki/Views/HUDView.swift` — NEW (SwiftUI row list, mask, reveal, context menu; plain `let store`, no `@Bindable`).
- `Sources/Sokki/Views/SettingsView.swift` — add "HUD Shortcut" Picker section.
- `Sources/Sokki/Views/MatchListView.swift` — extend `EditorRequest.new(...)` with `initialTrigger: String?`; add `.onReceive(.openMatchEditor)` observer; pass `initialTrigger` through `sheetContent`.
- `Sources/Sokki/Views/MatchEditorView.swift` — add `initialTrigger: String? = nil` init parameter; use it as the `@State` seed when no full `Match` is supplied.
- `Sources/Sokki/AppDelegate.swift` — own `HUDStore` and `HUDPanelController`; wire `onHUDRequested` / `onHUDEvent`; post `.openSettings` + `.openMatchEditor` with `userInfo["trigger"]` on create-from-HUD.
- `Sources/Sokki/Services/Bootstrapper.swift` — add `hud_shortcut: "none"` line to `writeDefaultConfig`.
- `CHANGELOG.md` — one-line entry under `## Unreleased`.

## 8. Verification

Manual, mapped to acceptance criteria:

1. **[AC1]** With `hud_shortcut` set to a non-`none` chord, in any other app, double-tap that chord → HUD panel appears floating above the frontmost app without that app losing key-window status; caret still blinks in the target app's text field.
2. **[AC2]** Fresh install: `~/.config/sokki/config/default.yml` contains a `hud_shortcut: "none"` line. Open Settings → General → "HUD Shortcut" Picker shows four options, preselected to `None`; change to `Double Shift`; Settings saves; inspect YAML: field now `hud_shortcut: double_shift`; change to `None` again; double-tapping any chord no longer opens the HUD.
3. **[AC3]** Trigger ~25 events rapidly (mix of matches, typing while disabled, typing immediately after `Cmd+V`); HUD shows 20 rows, newest at top, oldest evicted.
4. **[AC4]** Every row shows `•`-masked buffer text (whitespace preserved); click a row — that row's text reveals in monospace; click again — remasks. Other rows stay masked.
5. **[AC5]** Focus a password field (macOS login screen, Keychain unlock, Safari password field); type characters; quit back to non-secure field; open HUD — no rows reflect typing inside the secure field. Repeat while the previous app had an active `pasteSuppressionUntil` window: secure-input rows still do not appear (secure check runs before the tooSoon branch).
6. **[AC6]** Trigger an `undo` (type trigger → expansion fires → press Backspace once); verify the `Matched` row remains and no `undo` row appears.
7. **[AC7]** In HUD, right-click any row → "Create match from this"; Settings window comes forward; `MatchEditorView` sheet appears with `trigger` pre-filled to the row's captured buffer (`match.trigger` for `matched` rows; buffer + blocked character for `tooSoon` / `disabled` rows), `replace` empty. Save a match; it appears in the list and persists to `~/.config/sokki/match/default.yml`.
8. **[AC8]** Quit Sokki → relaunch → open HUD → empty state. Type a match → one row appears.
9. **[AC9]** Press `Cmd+V` then type 5 characters within 200ms → HUD shows exactly one `Too soon` row. Re-press `Cmd+V` within the still-active 200ms window, type more — still exactly one row (the flag did not re-arm). Wait >200ms, press `Cmd+V` again, type — a second row appears (fresh window).
   Toggle Sokki off (double-tap the toggle chord) → type a full sentence → HUD shows exactly one `Disabled` row. Toggle on → toggle off → type → a new `Disabled` row appears (flag re-armed via `handleModifierTap`). Toggle on via Settings → toggle off via hotkey → type → a new `Disabled` row appears (flag re-armed via `updateConfig` path).
10. **[AC10]** Type gibberish with no sigil (`hlelo world`) → no row appears in HUD.
11. **[AC11]** Open HUD → close via the panel's close button → Sokki is still running (status item present, menu bar icon visible); opening Settings and closing it behaves unchanged from pre-HUD (accessory policy preserved).

Collision and regression checks:

- `hud_shortcut=double_control`, `toggle_shortcut=double_control` → double-tap Control opens the HUD and does NOT toggle Sokki (hud-wins, Decision 7).
- `hud_shortcut=none`, `toggle_shortcut=none` → double-tap any modifier → nothing happens; no `lastModifierTapTime` state corruption (next real hotkey press still works once user configures one).
- Upgrade from pre-HUD config (no `hud_shortcut` key in YAML) → `AppConfig.hudShortcut` decodes to `"none"`; user's existing `toggle_shortcut: double_control` keeps working unchanged.
- Successfully expand a match → both the existing expansion behavior AND a new `Matched` row in the HUD; no double-injection, no echo loop (the `suppressMatching` guard is intact; HUD emission runs BEFORE `setSuppressMatching` on the tap thread).
- Open Settings while HUD is open → Settings becomes key; HUD stays floating above; closing Settings doesn't close the HUD.
- Click HUD's title bar to make it key → press `Cmd+W` → panel closes via standard `performClose:` path; Sokki does not quit.

Build check: `swift build` succeeds (project-standard readiness check).

CHANGELOG style: one-line bullet under `## Unreleased` matching the plain-bullet style of releases 0.0.3 / 0.0.4 (no `### Added` subsection).
