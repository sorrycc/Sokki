# Spotlight-Style Trigger Palette

## 1. Background

Sokki today is a passive background daemon: users define text-expansion matches in YAML, type a trigger, and Sokki replaces it inline. As a user accumulates more snippets (tens, then hundreds), discovery becomes the bottleneck — the user can't remember which trigger maps to which content, so their "snippet collection" silently grows beyond their working memory and atrophies.

The Spotlight-style trigger palette flips Sokki from passive to active: a global hotkey (default `⌥Space`) opens a Raycast-like overlay that lets the user fuzzy-search all matches by trigger or replacement content, preview the resolved expansion, and inject it into the previously focused app on Return. Discovery stops being memory-dependent.

The feature is purely additive — it complements the existing inline expansion path; nothing in the keystroke-trigger pipeline changes.

## 2. Requirements Summary

**Goal:** Add a Spotlight-style palette that complements inline expansion by letting users fuzzy-search existing matches, preview the resolved replace body live, inject into the previously focused app on Return, or open the match in the editor via `⌘↵`.

**Scope (in):**
- New non-activating `NSPanel` palette UI (plain SwiftUI, system semantic colors, three-pane layout: search field on top, left list reusing the existing `MatchRow` style, right pane showing resolved replace body).
- Global shortcut activation via the existing `KeystrokeMonitor` `CGEventTap` (mirrors `handleModifierTap` in `TapContext`); the chord is swallowed by returning `nil` from the tap.
- `AppConfig.paletteShortcut: String` persisted preference with a Settings `Picker`. Presets: `opt+space`, `ctrl+space`, `ctrl+opt+space`, `ctrl+shift+space`, `none`. Default: `opt+space`.
- Second press of hotkey toggles open ↔ close.
- Fuzzy search over all matches, sorted `(fuzzyScore desc, lastUsedAt ?? distantPast desc, trigger asc)`.
- Live variable preview: `{{cursor}}` shown as `▎` (accent color), `{{clipboard}}` snapshot taken at panel open, `{{date}}` rendered live.
- Optional `label: String?` field on `Match`, surfaced in the list row and searchable.
- Return → `panel.orderOut` → if frontmost app drifted from the snapshot, call `target.activate()` → `TextInjector` injects the resolved replace body.
- `⌘↵` → close palette and open selected match in `MatchEditorView`.
- Esc via SwiftUI `.onKeyPress(.escape)`; click-outside via `NSEvent.addGlobalMonitorForEvents(.leftMouseDown)` while visible.
- Sidecar `~/.config/sokki/recency.json` keyed by trigger string, debounced write on successful injection.

**Scope (out):**
- Palette-only mode (inline expansion remains fully intact).
- `⌥↵` suppress-for-app behavior.
- Per-app recency ranking.
- Frontmost-app-change observer.
- `.hudWindow` blur or custom chrome.
- New match types or variable grammar changes.

## 3. Acceptance Criteria

1. Pressing the configured shortcut (default `opt+space`) with palette closed opens the palette; pressing it again with palette open closes it; the chord is not delivered to the focused app (verified by typing into a text editor — no space character appears).
2. Setting shortcut to `none` in Settings disables activation entirely; no chord is swallowed and palette cannot be opened via hotkey.
3. Changing the shortcut preset in Settings takes effect without app restart, and the new preset is persisted in `AppConfig` across relaunches.
4. When the palette opens, `NSWorkspace.shared.frontmostApplication` is snapshotted; the palette appears without stealing activation from that app (menu bar of the prior app remains active).
5. Typing in the search field filters matches with fuzzy scoring; matches are ordered by `(fuzzyScore desc, lastUsedAt desc with nil last, trigger asc)`; the `label` field (when present) participates in fuzzy matching alongside the trigger.
6. The right pane renders the selected match's replace body with `{{cursor}}` displayed as `▎`, `{{clipboard}}` showing the snapshot captured at palette open (stable across selection changes), and `{{date}}` reflecting the current time; preview updates live as selection changes.
7. Pressing Return closes the palette, restores focus to the snapshotted frontmost app if it has drifted (`target.activate()`), and injects the resolved replace body via `TextInjector` into that app's active text field.
8. Pressing `⌘↵` closes the palette and opens the selected match in `MatchEditorView`; no injection occurs.
9. Pressing Esc or clicking outside the panel closes it with no injection and no side effects; the global mouse-down monitor is registered only while visible and torn down on close.
10. On successful Return injection, `recency.json` in `~/.config/sokki/` is updated for the match's trigger with `lastUsedAt = now`; writes are debounced and the file survives relaunch; corrupt or missing file is tolerated (treated as empty).
11. Inline expansion (existing `KeystrokeMonitor` trigger-then-replace flow) continues to function identically — no regression in trigger matching, modifier-tap behavior, or injection timing.
12. The optional `label` field is nullable on the `Match` model, editable in `MatchEditorView`, and absent-label matches render and behave exactly as before (list row falls back to trigger).

## 4. Problem Analysis

This is a greenfield UI feature; no prior palette exists in Sokki. The relevant existing primitives that the design rides on:

- **CGEventTap (`KeystrokeMonitor.swift`)** — already runs as a `.defaultTap` (can swallow events). Adding a chord branch keeps the global hotkey infra to a single tap with no new permissions.
- **`TapContext.handleModifierTap`** — proven thread-safe pattern for matching a configured shortcut on the tap thread; the palette chord detector is a near-clone.
- **`TextInjector`** — frontmost-targeted injection (synthesized Cmd+V or per-character key events). Reusable verbatim provided the palette restores frontmost focus before injection.
- **`MatchEditorView`** — already accepts `Match?` directly, so `⌘↵` editor open is pure wiring.
- **`VariableRenderer`** — regex-driven; trivially extensible with a `preview: Bool` flag and a new `cursor` case.
- **`ConfigLoader`** — owns `~/.config/sokki/`; recency sidecar fits next to existing match YAML.

Approaches considered for **shortcut infrastructure**:
- *MASShortcut / `KeyboardShortcuts` SPM lib* — adds dep #3 just for one chord; rejected on YAGNI.
- *Carbon `RegisterEventHotKey`* — duplicates a layer we already operate at; rejected on DRY.
- *`NSEvent.addGlobalMonitorForEvents`* — cannot consume the event; the system would receive `⌥Space` (input source switcher); rejected on correctness.
- *Chosen: piggyback the existing `CGEventTap`* — zero deps, mirrors a battle-tested in-repo pattern, can `return nil` to swallow.

Approaches considered for **window class**:
- *Activating `NSWindow` + re-activate previous app on inject* — visible Dock flicker per invocation; degrades the Spotlight feel; rejected.
- *`NSPanel` non-activating only* — happy-path correct; risk of SwiftUI/IME focus quirks in non-activating panels.
- *Chosen: `NSPanel` non-activating + frontmost snapshot fallback* — happy path is identical to A; snapshot is a 3-line insurance policy if focus drifts.

Approaches considered for **recency storage**:
- *Embed `lastUsedAt` in `Match` YAML* — every expansion would rewrite hand-edited YAML and clobber comments/ordering; rejected.
- *In-memory only* — loses state across relaunch; pointless for a menu-bar app.
- *Chosen: sidecar `recency.json` keyed by trigger* — keeps user YAML pristine, survives relaunch, trivially nukeable.

## 5. Decision Log

**1. Palette-only matches vs pure complement view?**
- Options: A) Pure complement · B) Palette-only mode · C) Per-match mode flags
- Decision: **A) Pure complement** — Trie at `TrieMatcher.swift:45` doesn't safely tolerate empty triggers; dedup at `ConfigLoader.swift:84` keys on trigger. Avoid schema/engine surgery (YAGNI).

**2. Hotkey infrastructure?**
- Options: A) Carbon RegisterEventHotKey · B) `KeyboardShortcuts` SPM dep · C) `NSEvent.addGlobalMonitorForEvents` · D) SwiftUI `.keyboardShortcut` · E) Piggyback existing `CGEventTap`
- Decision: **E) Piggyback** — Existing `.defaultTap` in `KeystrokeMonitor.swift:128` already sees and can consume every key. Mirrors `TapContext.handleModifierTap` (`TapContext.swift:107-150`). Zero deps. C cannot consume, D is not global, A and B are duplicate infra.

**3. Window class & focus restoration?**
- Options: A) NSPanel non-activating only · B) Activating NSWindow + snapshot/re-activate · C) NSPanel + snapshot as fallback · D) Prototype A first
- Decision: **C) NSPanel non-activating + snapshot fallback** — `TextInjector.swift:62-71` synthesizes `Cmd+V`, which goes to whoever is frontmost at post time. Non-activating panel preserves that. Snapshot is a 3-line insurance policy against SwiftUI/IME quirks.

**4. v1 scope cuts?**
- Options: A) Minimal palette only · B) + `⌘↵` editor · C) + live variable preview · D) Everything (incl. `⌥↵` suppress + per-app ranking)
- Decision: **C)** — `⌘↵` is ~15 LOC (Match is `Identifiable`, `MatchEditorView` accepts `Match?` directly). Live preview is ~50 LOC (one new case in `VariableRenderer.resolveVariable`). Per-app suppress + ranking would require new persistence + bundle-id capture + lock surgery in `TapContext`; defer until justified by usage.

**5. Recency storage model?**
- Options: A) None · B) In-memory only · C) Add `lastUsedAt` to `Match` model · D) Sidecar JSON
- Decision: **D) Sidecar `recency.json`** — Matches are hand-editable YAML at `~/.config/sokki/match/*.yml`. Embedding runtime state into them would rewrite the file on every expansion (`MatchStore.saveMatches` at `MatchStore.swift:67`, `YAMLEncoder` at `ConfigLoader.swift:114`). Sidecar isolates the concern, survives relaunch, easy to nuke.

**6. Dismissal triggers?**
- Options: A) Esc + 2nd hotkey only · B) + click-outside · C) + frontmost-app-change observer · D) Auto-dismiss after paste
- Decision: **B)** — Esc via SwiftUI `.onKeyPress(.escape)` (no coupling to the tap). Click-outside via `NSEvent.addGlobalMonitorForEvents(.leftMouseDown)` while visible. Frontmost-app-change observer is YAGNI (covered by `windowDidResignKey` + the mouse monitor).

**7. Result row layout & visual style?**
- Options: A) Single-line dense (Alfred) · B) Two-line Raycast · C) Three-pane split · D) Custom
- Decision: **C) Three-pane split** — Matches the user's spec ("Right pane: rich preview"). Reuses `MatchRow` (`MatchListView.swift:283-322`) for left-pane rows. Plain SwiftUI aesthetic, no `.hudWindow` blur, system semantic colors. Width ~640-720pt.

**8. Fuzzy match algorithm?**
- Options: A) Naive subsequence + score · B) Char-position weighted · C) External lib
- Decision: **A) Naive subsequence with bonuses** — ~40 LOC, fine for ~200 matches. Bonuses: consecutive char run, prefix match, label/trigger field weighting.

**9. Where does `PaletteController` live & how is it owned?**
- Options: A) `Services/PaletteController.swift`, owned by `AppDelegate` · B) Inside `StatusItemController`
- Decision: **A)** — Mirrors `StatusItemController` ownership; clean single-purpose class.

**10. YAML schema for `label`?**
- Options: A) Top-level optional key, omitted on encode when nil · B) Always present (default empty string)
- Decision: **A)** — Backward compatible; existing files untouched. Encode skips nil to keep YAML clean.

**11. Window position?**
- Options: A) Center of main screen · B) Center of screen with cursor · C) Remember last position
- Decision: **B) Cursor's screen, horizontally centered, ~25% from top** — Matches Spotlight behavior in multi-monitor setups. No new persisted state.

**12. Shortcut presets surfaced in Settings?**
- Options: per user choice
- Decision: **`opt+space` (default), `ctrl+space`, `ctrl+opt+space`, `ctrl+shift+space`, `none`** — Per user input.

## 6. Design

### 6.1 Architecture overview

```
┌─────────────────────────────────────────────────────────────┐
│ AppDelegate                                                  │
│  • owns PaletteController (new)                              │
│  • wires keystrokeMonitor.onPaletteToggleRequested →         │
│      paletteController.toggle()                              │
│  • forwards AppConfig.paletteShortcut → keystrokeMonitor     │
└─────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│ KeystrokeMonitor / TapContext (extended)                     │
│  • TapContext.paletteShortcut: String (lock-protected)       │
│  • TapContext.handlePaletteChord(keyCode:flags:) -> Bool     │
│    invoked from TapHandler.handleEvent before keyDown branch │
│  • on match: fire onPaletteToggleRequested() callback        │
│    (parallel to onToggled), return nil from tap callback     │
│    to swallow the event. Frontmost-PID capture is done on    │
│    main inside PaletteController.toggle() — see §6.3.        │
│  • paletteShortcut hot-reloads via existing                  │
│    TapContext.updateConfig(AppConfig) — no new channel       │
└─────────────────────────────────────────────────────────────┘
                           │ (callback hops to main thread)
                           ▼
┌─────────────────────────────────────────────────────────────┐
│ PaletteController (new)                                      │
│  • owns NSPanel (non-activating, .nonactivatingPanel mask)   │
│  • owns dedicated NSWindow for ⌘↵ MatchEditor presentation   │
│  • toggle() reads NSWorkspace.frontmostApplication on main   │
│  • installs/removes global mouse-down monitor                │
│  • toggle(): if visible → close; else → show                 │
│  • commitInjection(match): orderOut → re-activate target if  │
│      drifted → render preview (full mode) → inject           │
│  • commitOpenEditor(match): orderOut → present sheet/window  │
└─────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│ PaletteView (SwiftUI, hosted in NSHostingView)               │
│  • reads MatchStore (@ObservedObject), RecencyStore          │
│  • search field + filtered list (left) + preview pane (right)│
│  • .onKeyPress: ↑↓ navigate, Return commit, ⌘↵ edit, Esc close│
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│ RecencyStore (new)                                           │
│  • [String: Date] keyed by trigger                           │
│  • load() at init, save() debounced 1s via DispatchSourceTimer│
│  • path: ~/.config/sokki/recency.json                        │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│ FuzzyScorer (new, file-private to PaletteView)               │
│  • score(query: String, target: String) -> Int?              │
│  • subsequence; +5 prefix, +3 consecutive run, +2 label hit  │
└─────────────────────────────────────────────────────────────┘
```

### 6.2 Data model changes

Both `Match` and `AppConfig` use **manual** `init(from:)` and (for `Match`) `encode(to:)` implementations. Adding fields requires editing the manual codec — relying on synthesized Codable will not work because the structs already opt out.

`Match.swift` (current shape per `Match.swift:3-40`):
```swift
struct Match: Identifiable, Codable, Sendable, Equatable, Hashable {
    let id: UUID
    var trigger: String
    var replace: String
    var label: String? = nil      // NEW
    var isEnabled: Bool
    var sourceFile: String?

    enum CodingKeys: String, CodingKey {
        case trigger, replace, label    // NEW: label
        case isEnabled = "enabled"
    }

    init(id: UUID = UUID(), trigger: String, replace: String, label: String? = nil,   // NEW: label param
         isEnabled: Bool = true, sourceFile: String? = nil) {
        self.id = id
        self.trigger = trigger
        self.replace = replace
        self.label = label
        self.isEnabled = isEnabled
        self.sourceFile = sourceFile
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.id = UUID()
        self.trigger = try c.decode(String.self, forKey: .trigger)
        self.replace = try c.decode(String.self, forKey: .replace)
        self.label = try c.decodeIfPresent(String.self, forKey: .label)   // NEW
        self.isEnabled = (try? c.decode(Bool.self, forKey: .isEnabled)) ?? true
        self.sourceFile = nil
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(trigger, forKey: .trigger)
        try c.encode(replace, forKey: .replace)
        if let label, !label.isEmpty {                       // NEW: skip when nil/empty
            try c.encode(label, forKey: .label)
        }
        if !isEnabled { try c.encode(isEnabled, forKey: .isEnabled) }
    }
}
```

`AppConfig.swift`:
```swift
struct AppConfig: Codable, ... {
    // existing fields...
    var paletteShortcut: String = "opt+space"   // NEW

    enum CodingKeys: String, CodingKey {
        // ...existing keys...
        case paletteShortcut
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        // ...existing field decodes...
        self.paletteShortcut = try c.decodeIfPresent(String.self, forKey: .paletteShortcut) ?? "opt+space"
    }
}
```

`ConfigLoader` needs **no changes** — it just hands the YAML buffer to `YAMLDecoder/Encoder`, which call into the structs' own Codable. `MatchEditorView` adds an optional label `TextField` above the trigger field.

### 6.3 Chord detection in `TapContext`

```swift
// New, parallel to handleModifierTap:
func handlePaletteChord(keyCode: Int64, flags: CGEventFlags) -> Bool {
    state.withLock { s in
        guard let chord = s.parsedPaletteChord else { return false }
        let activeMods = flags.intersection([.maskCommand, .maskAlternate, .maskControl, .maskShift])
        return keyCode == chord.keyCode && activeMods == chord.modifiers
    }
}
```

`paletteShortcut` is parsed once per config update inside the existing `TapContext.updateConfig(AppConfig)` (`TapContext.swift:172`) — no new setter, no new channel. The settings → save YAML → FSEvents → `AppDelegate.applyConfig` → `keystrokeMonitor.updateConfig` pipeline already in place for `toggleShortcut` covers AC3 verbatim.

Presets:
- `opt+space` → (kVK_Space=49, .maskAlternate)
- `ctrl+space` → (49, .maskControl)
- `ctrl+opt+space` → (49, [.maskControl, .maskAlternate])
- `ctrl+shift+space` → (49, [.maskControl, .maskShift])
- `none` → nil (palette disabled)

`KeystrokeMonitor.TapHandler.handleEvent` calls `handlePaletteChord` immediately before its existing `keyDown` character path. On match:
1. Invoke a new `onPaletteToggleRequested: @Sendable () -> Void` closure (parallel to the existing `onToggled` at `KeystrokeMonitor.swift`). The closure dispatches to the main thread.
2. Return `nil` from the tap callback to swallow the chord.

No `NSWorkspace` access from the tap thread — `NSWorkspace` is an AppKit API not officially documented as thread-safe off-main, and there's no race to win: Sokki is `LSUIElement` (`.accessory`), the palette panel is non-activating (§6.4 step 4), so Sokki never becomes frontmost on the hotkey path. The frontmost-app snapshot is instead read on main inside `PaletteController.toggle()` (§6.4) where AppKit access is sanctioned. No `pid_t` plumbing across the thread boundary.

No new `FeedAction` enum case — the chord branch never enters the character-feed path, so wedging it into `FeedAction` would mix concerns.

### 6.4 Window choreography

**State source of truth:** `PaletteController` is `@MainActor` and owns `private var isShowing: Bool = false`. **All** open/close/toggle decisions read and write `isShowing`, never `panel.isVisible` (which has subtle semantics during orderOut animation and occlusion). `toggle()` flips based on `isShowing`; `show()` sets it true *before* `makeKeyAndOrderFront`; `close()` sets it false *before* `orderOut`. This eliminates the "user mashes ⌥Space mid-animation" race.

**Panel construction (one-time, in `init`):**
- `NSPanel(contentRect:, styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)`
- `panel.level = .floating`
- `panel.isFloatingPanel = true`
- `panel.hidesOnDeactivate = false`        // survive brief focus losses; we manage close ourselves
- `panel.isMovableByWindowBackground = false`
- `panel.becomesKeyOnlyIfNeeded = false`   // we want the search field to receive keys
- `panel.collectionBehavior = [.canJoinAllSpaces, .stationary, .ignoresCycle]`
- Borderless mask means: no traffic lights, no drag-by-titlebar, no chrome-vs-content dismissal-path divergence. Position is recomputed each open (not draggable in v1; YAGNI).

**Show (`PaletteController.show()`, on main):**
1. If `isShowing` already, just `panel.makeKeyAndOrderFront(nil)` and return (idempotent).
2. Snapshot frontmost app: `let target = NSWorkspace.shared.frontmostApplication`. Safe because Sokki is `.accessory` and the panel is non-activating, so frontmost is still the user's prior app at this moment.
3. `let clipboardSnapshot = NSPasteboard.general.string(forType: .string)` — snapshot for `{{clipboard}}` preview.
4. Compute frame: cursor's screen, horizontally centered, top edge at ~25% screen height. Width 720, initial height 420.
5. Install/refresh `NSHostingView(rootView: PaletteView(...))` as `panel.contentView` so each open gets fresh state (`query == ""`, `selection == filtered.first`).
6. `isShowing = true`.
7. `panel.makeKeyAndOrderFront(nil)` — do NOT call `NSApp.activate(...)`, do NOT touch `setActivationPolicy`.
8. Install global mouse monitor:
   `NSEvent.addGlobalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown, .otherMouseDown]) { [weak self] _ in self?.close() }`.
9. Make the SwiftUI `TextField` first responder.

Cmd-Tab dismissal is intentionally out of scope for v1 (YAGNI; `windowDidResignKey` on a non-activating panel fires inconsistently because the panel is rarely "key" in the traditional sense). Esc, second-hotkey-press, and click-in-other-app via the global mouse monitor cover the realistic dismissal paths.

**Commit (Return) → `commitInjection(match)`:**
1. `panel.orderOut(nil)` — must precede activation/inject so AppKit doesn't bounce focus back.
2. If `NSWorkspace.shared.frontmostApplication != target`, call `target?.activate(options: [])`.
3. `let resolved = renderer.render(match.replace)` — production renderer (NOT the `▎` preview variant).
4. `injector.inject(resolved)`.
5. `recency.bump(trigger: match.trigger)`.
6. Tear down mouse monitor.

**Commit (⌘↵) → `commitOpenEditor(match)`:**
1. `panel.orderOut(nil)`.
2. Lazily create a single `editorWindow: NSWindow?` stored on `PaletteController` (style mask `[.titled, .closable]`, content size ~480×400, `isReleasedWhenClosed = false`). On second open, **reuse it**: swap `editorWindow.contentView` to a fresh `NSHostingView` and call `makeKeyAndOrderFront`. The window's actual `MatchEditorView` initializer (per `MatchEditorView.swift:14`) is:
   ```swift
   MatchEditorView(
       match: match,
       availableFiles: matchStore.availableSourceFiles,    // existing list
       prefillSourceFile: match.sourceFile ?? "default.yml",   // matches MatchListView.swift:78
       onSave: { [weak self] updated in
           matchStore.update(updated)                       // existing MatchStore API
           self?.editorWindow?.close()
       },
       onCancel: { [weak self] in self?.editorWindow?.close() }
   )
   ```
   `MatchListView.swift:72-97` is the existing call-site model — copy its `onSave` / `onCancel` shape verbatim.
3. Activation policy: ref-counted via `ActivationPolicyCoordinator` on `AppDelegate` (see §6.4.1) so editor + Settings windows can coexist without thrashing. `editorWindow` registers an "active" lease on open and releases it on `NSWindow.willCloseNotification`; coordinator only flips back to `.accessory` when the lease count returns to zero. This is acceptable for the editor path because editing is a deliberate, multi-second user action (unlike injection which must feel instant).

Why a dedicated window rather than reusing Settings → Matches tab? Settings is a `WindowGroup` scene tied to the SwiftUI `Settings` API; programmatically driving it to a specific tab + opening a sheet on a specific row from outside SwiftUI is awkward and brittle. A purpose-built ~20 LOC NSWindow is the simpler, project-conventional choice.

#### 6.4.1 ActivationPolicyCoordinator

```swift
@MainActor
final class ActivationPolicyCoordinator {
    private var leases = 0
    func acquire() {
        leases += 1
        if leases == 1 {
            NSApp.setActivationPolicy(.regular)
            NSApp.activate(ignoringOtherApps: true)
        }
    }
    func release() {
        leases = max(0, leases - 1)
        if leases == 0 { NSApp.setActivationPolicy(.accessory) }
    }
}
```

Owned by `AppDelegate`. The Settings-window flow in `SokkiApp.swift:22-38` is migrated to call `acquire()` / `release()` instead of touching `setActivationPolicy` directly. The editor window does the same. Coexistence: opening the editor while Settings is up keeps `leases == 2`; closing either keeps `.regular` until both are gone. No flicker.

**Close (Esc / click-outside / 2nd hotkey):**
1. `panel.orderOut(nil)`.
2. Tear down mouse monitor.
3. No injection, no recency update, no app activation.

### 6.5 SwiftUI view (`PaletteView.swift`)

```swift
struct PaletteView: View {
    @Bindable var matchStore: MatchStore       // MatchStore is @Observable (Models/MatchStore.swift:4),
                                               // not ObservableObject — use @Bindable per project convention
                                               // (mirrors MatchListView.swift:4).
    let recency: RecencyStore
    let clipboardSnapshot: String?
    let onCommit: (Match) -> Void
    let onEdit: (Match) -> Void
    let onClose: () -> Void

    @State private var query = ""
    @State private var selection: UUID?
    @FocusState private var searchFocused: Bool

    var body: some View {
        VStack(spacing: 0) {
            TextField("Search…", text: $query)
                .textFieldStyle(.plain)
                .font(.system(size: 16))
                .padding(12)
                .focused($searchFocused)
                // Arrow + Return handlers must attach to the TextField itself —
                // when a SwiftUI TextField is first responder, AppKit's field editor
                // consumes arrow/return keys before they reach ancestor .onKeyPress.
                // Returning .handled stops the field editor from also acting on them.
                .onKeyPress(.upArrow)   { moveSelection(-1); return .handled }
                .onKeyPress(.downArrow) { moveSelection(+1); return .handled }
                .onKeyPress(.return) { press in
                    guard let m = selectedMatch else { return .ignored }
                    if press.modifiers.contains(.command) { onEdit(m) } else { onCommit(m) }
                    return .handled
                }
            Divider()
            HStack(spacing: 0) {
                resultsList                    // ~280pt
                Divider()
                previewPane                    // remainder
            }
        }
        .frame(width: 720, height: 420)
        .onKeyPress(.escape) { onClose(); return .handled }
        .onAppear { searchFocused = true; selection = filtered.first?.id }
        .onChange(of: query) { _, _ in selection = filtered.first?.id }
    }

    private var filtered: [(Match, Int)] { /* score, sort, take */ }
    private var selectedMatch: Match? { /* lookup */ }
    private var resultsList: some View { /* List with MatchRow-style */ }
    private var previewPane: some View { /* Text(previewAttributed) */ }
}
```

Key handler design notes:
- Use `KeyPress.modifiers` from the `.onKeyPress { press in … }` closure, not `NSEvent.modifierFlags` (which is process-global polled state and can race with IME / external hotkey events).
- Arrow + Return handlers attach to the `TextField`, not to the outer `VStack`, because a focused SwiftUI `TextField` is backed by `NSTextField` whose field editor consumes arrow / return keys before they bubble to ancestors. Attaching at the field gives `.onKeyPress` first crack.

### 6.6 Fuzzy scorer (file-private)

```swift
enum FuzzyScorer {
    static func score(query: String, target: String) -> Int? {
        if query.isEmpty { return 0 }
        let q = query.lowercased()
        let t = target.lowercased()
        var qi = q.startIndex, ti = t.startIndex
        var score = 0, run = 0, firstHitIdx: Int?
        var idx = 0
        while qi < q.endIndex, ti < t.endIndex {
            if q[qi] == t[ti] {
                if firstHitIdx == nil { firstHitIdx = idx }
                run += 1
                score += 1 + run         // consecutive bonus
                qi = q.index(after: qi)
            } else { run = 0 }
            ti = t.index(after: ti); idx += 1
        }
        guard qi == q.endIndex else { return nil }
        if firstHitIdx == 0 { score += 5 }   // prefix bonus
        return score
    }
}
```

**Disabled matches are excluded from the palette.** The list iterated by the scorer is `matchStore.matches.filter { $0.isEnabled }`. Disabling a match means the user has chosen to suppress it from inline expansion; the palette respects that — no toggle to "show disabled in palette" in v1.

Composition (per Match):
```swift
func matchScore(query: String, m: Match) -> Int? {
    if query.isEmpty { return 0 }   // empty query: every match passes; final order is recency then trigger
    let triggerScore = score(query: query, target: m.trigger)
    let labelScore   = (m.label.flatMap { score(query: query, target: $0) }).map { $0 + 2 }
    let replaceHead  = String(m.replace.prefix(200))   // bound cost on long bodies
    let replaceScore = score(query: query, target: replaceHead).map { max(0, $0 - 2) }
    return [triggerScore, labelScore, replaceScore].compactMap { $0 }.max()
}
```
- Each field returns `Int?`; nil means "query is not a subsequence of this field." Nils drop out of `max`.
- Label bonus = +2 (when present); replace penalty = -2 floored at 0 (avoid negative scores polluting comparisons).
- Replace scan capped at first 200 chars — bounds per-keystroke cost on long bodies.
- Empty-query short-circuit returns 0 for every match; final sort is `(0 desc, lastUsedAt desc, trigger asc)` → recency dominates the empty-query state.

### 6.7 Variable preview

`VariableRenderer` extension:
```swift
func renderPreview(_ template: String, clipboardSnapshot: String?) -> AttributedString {
    // For each variable token:
    //   {{date[:fmt]}}     -> live now()
    //   {{clipboard}}      -> clipboardSnapshot ?? ""
    //   {{cursor}}         -> "▎" (highlighted via AttributedString)
    // Returns AttributedString so the cursor glyph can be tinted.
}
```

The production `render(_:)` path is unchanged. Preview takes a snapshot string so the right pane is stable as the user navigates.

### 6.8 Recency store

```swift
final class RecencyStore: @unchecked Sendable {
    private let url: URL
    private var map: [String: Date] = [:]
    private var pending: DispatchSourceTimer?
    private let queue = DispatchQueue(label: "sokki.recency")

    init(configDir: URL) {
        self.url = configDir.appendingPathComponent("recency.json")
        load()
    }
    func lastUsed(for trigger: String) -> Date? { queue.sync { map[trigger] } }
    func bump(_ trigger: String, now: Date = .init()) {
        queue.async { [self] in map[trigger] = now; scheduleSave() }
    }
    private func scheduleSave() { /* 1s debounce via DispatchSourceTimer */ }
    private func load() { /* try? JSONDecoder, tolerate missing/corrupt */ }
    private func save() { /* atomic write */ }
}
```

Owned by `PaletteController`; passed read-only to `PaletteView`.

Keying note: keyed by raw `trigger` string, not `Match.id`. `Match.id` is regenerated fresh on every YAML load (`Match.swift:25`), so it is not stable across restarts and would break recency persistence. Trade-off: if the user renames a trigger (`:hi` → `:hello`), the old recency entry orphans and the new trigger starts from zero. Acceptable for v1; a nightly prune of unknown keys is a trivial follow-up if it becomes annoying.

### 6.9 Settings UI

Add a `Picker` row to `SettingsView.swift` next to the existing toggle-shortcut picker:
```swift
Picker("Palette Shortcut", selection: $config.paletteShortcut) {
    Text("⌥ Space").tag("opt+space")
    Text("⌃ Space").tag("ctrl+space")
    Text("⌃⌥ Space").tag("ctrl+opt+space")
    Text("⌃⇧ Space").tag("ctrl+shift+space")
    Text("None").tag("none")
}
```
No new propagation channel needed: `SettingsView.onChange(of: store.config)` already calls `store.saveConfig()`, which writes YAML. The existing FSEvents watcher fires `AppDelegate.applyConfig`, which calls `keystrokeMonitor.updateConfig(newConfig)`, which forwards to `TapContext.updateConfig` (`TapContext.swift:172`). Adding `paletteShortcut` parsing alongside the existing `toggleShortcut` parsing in `updateConfig` covers AC3 verbatim with zero new wiring.

### 6.10 Error / edge cases

- Shortcut conflict with another global hotkey: Sokki's `.defaultTap` runs at `.headInsertEventTap`, so it consumes first; user-visible behavior is "the other binding stops working." Documented as a tradeoff.
- Target app quit while palette open: `target?.activate()` returns false; `TextInjector` still sends to whatever is frontmost (best effort). Acceptable.
- Recency file corrupted or missing: treated as empty map; first save overwrites cleanly.
- Variable resolution failure inside preview: render the raw token (`{{unknown}}`) instead of throwing.
- User opens palette with zero matches: render generic "No matches" placeholder in the left pane.

## 7. Files Changed

- `Sources/Sokki/Models/Match.swift` — add optional `label: String?` field; extend `CodingKeys` with `.label`; extend manual `init(from:)` with `decodeIfPresent` for label; extend manual `encode(to:)` to skip label when nil/empty.
- `Sources/Sokki/Models/AppConfig.swift` — add `paletteShortcut: String` (default `"opt+space"`); extend `CodingKeys` with `.paletteShortcut`; extend manual `init(from:)` with `decodeIfPresent` defaulting to `"opt+space"`.
- `Sources/Sokki/Engine/TapContext.swift` — add `paletteShortcut` state + parser (extend `updateConfig` to read `config.paletteShortcut`), and `handlePaletteChord(keyCode:flags:) -> Bool`.
- `Sources/Sokki/Engine/KeystrokeMonitor.swift` — call `handlePaletteChord` in `TapHandler.handleEvent` before keyDown char path; on match, invoke a new `onPaletteToggleRequested: @Sendable () -> Void` callback (parallel to existing `onToggled`), return `nil` from tap to swallow. No `NSWorkspace` access from tap thread.
- `Sources/Sokki/Engine/VariableRenderer.swift` — add `renderPreview(_:clipboardSnapshot:) -> AttributedString` with `{{cursor}}` support.
- `Sources/Sokki/Services/RecencyStore.swift` — NEW. Load/save sidecar JSON with debounced writes.
- `Sources/Sokki/Services/PaletteController.swift` — NEW. Owns `NSPanel`, snapshot, mouse monitor, commit choreography.
- `Sources/Sokki/Views/PaletteView.swift` — NEW. SwiftUI three-pane layout, fuzzy filtering, key handlers.
- `Sources/Sokki/Views/MatchEditorView.swift` — add `@State private var label: String` (initialized to `match?.label ?? ""`), an optional `TextField("Label (optional)", text: $label)` row above Trigger in the Form, and pass `label: label.isEmpty ? nil : label` into the `Match(...)` initializer in the Save button (`MatchEditorView.swift:67-74`). Also add `label:` to the `Match.init(id:trigger:replace:label:isEnabled:sourceFile:)` signature in `Match.swift:15`.
- `Sources/Sokki/Views/SettingsView.swift` — add palette shortcut `Picker`.
- `Sources/Sokki/AppDelegate.swift` — instantiate `PaletteController`, `RecencyStore`; set `keystrokeMonitor.onPaletteToggleRequested = { [weak self] in DispatchQueue.main.async { self?.paletteController.toggle() } }`. Shortcut config changes ride the existing FSEvents → `applyConfig` → `keystrokeMonitor.updateConfig` pipeline; no new code path.

## 8. Verification

Manual test plan, mapped to acceptance criteria:

1. **[AC1, AC2, AC3]** Build, launch. With default shortcut, hit `⌥Space` in TextEdit — no space inserted, palette appears. Hit `⌥Space` again — palette closes. Open Settings, change to `none`, save — `⌥Space` no longer toggles. Change to `ctrl+space`, hit — palette appears. Restart app — preference persists.
2. **[AC4]** With Safari frontmost, open palette via `⌥Space`. Verify Safari's menu bar still shows in the menu bar, no Dock bounce, no app switch.
3. **[AC5]** Type a few chars in the search field; verify results re-rank (recency bias visible after using the same trigger 2-3 times).
4. **[AC6]** Define a match with body `Hi {{date}}, paste from clipboard: {{clipboard}}{{cursor}} done.`. Copy "FOO" to clipboard. Open palette. Right pane should show `Hi <today's date>, paste from clipboard: FOO▎ done.` with `▎` tinted in the accent color. Wait 5 sec — date does not change unless selection changes (acceptable; updates on selection change).
5. **[AC7]** Open palette in Notes.app, select a match, hit Return. Verify Notes regains focus and the resolved text appears at the cursor.
6. **[AC8]** Open palette, select a match, hit `⌘↵`. Verify `MatchEditorView` opens; no text injected.
7. **[AC9]** Open palette, hit Esc — closes, no injection. Re-open, click outside the panel — closes, no injection.
8. **[AC10]** Inject a match via Return. Quit app. Inspect `~/.config/sokki/recency.json` — contains `{"trigger": "ISO date"}`. Re-launch, open palette — recently used match floats up.
9. **[AC11]** With palette feature shipped, type an inline trigger (e.g. `:hi`) in TextEdit — verify expansion still works exactly as before. Verify modifier-double-tap still toggles enable/disable if configured.
10. **[AC12]** Add a `label: "Daily Standup"` to one match (via editor). Open palette — left-pane row shows label. Search by label fragment — match ranks. Old matches without label still render trigger-as-title.

Build and bring-up:
```bash
swift build               # debug build
./Sokki.app/Contents/MacOS/Sokki   # run from .app or use existing package script
```
No new dependencies; `swift build` should succeed without `Package.resolved` changes.
