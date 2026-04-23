# AI Settings Tab

## 1. Background

`AISettings` (model, binary, timeout_seconds, selection_capture_timeout_ms, args, placeholder_style) is currently YAML-only — users have to hand-edit `~/.config/sokki/config/default.yml`. Settings UI already exposes general behaviour / logging / shortcuts / updates; an AI tab brings AI configuration into parity with the rest.

## 2. Requirements Summary

**Goal:** Add an **AI** tab to the Settings TabView, live-bound to `store.config.ai.*`, surfacing every field except `args`.

**Scope:**
- In scope: `placeholder_style`, `model`, `binary` (optional, blank = auto-detect), `timeout_seconds`, `selection_capture_timeout_ms`; new tab in TabView.
- Out of scope: `args` (YAML-only), validation UI (negative numbers, unknown model strings — current codebase doesn't validate these either), `ClaudeBinaryResolver.invalidate()` wiring on config change (pre-existing gap for YAML edits too).

## 3. Acceptance Criteria

1. **AI** tab appears in Settings TabView between General and Privacy, with an appropriate SF Symbol icon.
2. AI tab renders a grouped form with these rows, live-bound to `store.config.ai`:
   - Picker: **Placeholder style** → Static / Braille / Block sweep, bound to `placeholderStyle: PlaceholderStyle`.
   - TextField: **Model** (String), bound to `model`.
   - TextField: **Claude binary** (optional String, blank = auto-detect), bound to `binary` via an empty-string ↔ nil bridge.
   - TextField: **Timeout (seconds)** (Int, number format), bound to `timeoutSeconds`.
   - TextField: **Selection capture timeout (ms)** (Int, number format), bound to `selectionCaptureTimeoutMs`.
3. Blank binary field round-trips: UI empty ↔ `binary == nil`. No sentinel like `"none"` stored in YAML.
4. Changes auto-save via the existing `onChange(of: store.config) { store.saveConfig() }` pattern — no new save wiring.
5. `args` is NOT surfaced; its YAML value is preserved across save cycles (proven by existing Codable round-trip — nothing to do).
6. `swift build` compiles cleanly under Swift 6.

## 4. Problem Analysis

Current state:

- `SettingsView.swift` has three tabs (Matches/General/Privacy) — L9, L12, L15.
- `GeneralSettingsView` is the template: `@Bindable var store: MatchStore`, `Form { Section(...) { rows } }`, `.formStyle(.grouped)`, `.padding()`, `.onChange(of: store.config) { store.saveConfig() }`.
- Every existing Picker tags `String` values; an enum-backed picker for `PlaceholderStyle` is new but supported (enum is `Hashable` via `String` raw).
- No `Optional<String>` live binding exists anywhere in `Views/` — we introduce one via `Binding(get:set:)`.

Approaches evaluated:

- **A — Mirror `MatchEditorView` pattern** (local `@State var binary: String` seeded from `store.config.ai.binary ?? ""`, push to config on change): adds redundant state, needs manual `onChange` + divergence handling if config changes externally. *Rejected.*
- **B — Computed `Binding<String>` that reads `store.config.ai.binary ?? ""` and writes `nil` when the new string is empty, else the string**: single source of truth, automatic updates when config mutates externally, no extra state. *Chosen.*
- **C — New custom `OptionalStringField` component**: over-engineered for one call site. *Rejected.*

## 5. Decision Log

**1. Tab placement?**
- Options: A) between General and Privacy · B) last · C) first (after Matches)
- Decision: **A)** — General → AI → Privacy reads as a natural "behaviour → feature → safety" order.

**2. Tab icon?**
- Options: A) `sparkles` · B) `brain` · C) `wand.and.stars`
- Decision: **A) `sparkles`** — standard macOS convention for AI/ML features.

**3. Optional binary path binding strategy?**
- Options: A) local `@State` + manual sync · B) computed `Binding<String>` with empty ↔ nil · C) custom component
- Decision: **B)** — minimal code, no extra state, idiomatic SwiftUI.

**4. Numeric field input style?**
- Options: A) `TextField(..., value:, format: .number)` · B) `Stepper` · C) both
- Decision: **A)** — matches existing General tab rows at `SettingsView.swift:43,49,52`. No validation UI.

**5. Placeholder style picker label per case?**
- Options: A) raw-value text (`static`, `braille`, `block_sweep`) · B) title-case (`Static`, `Braille`, `Block sweep`) · C) descriptive (`Static text`, `Braille spinner`, `Block sweep`)
- Decision: **B)** — short, scannable, consistent with existing shortcut Picker labels.

**6. Section grouping inside the tab?**
- Options: A) single `Section("AI")` · B) two sections: "Model" + "Placeholder" · C) three sections: "Model", "Placeholder", "Timeouts"
- Decision: **B)** — `Model` (model + binary + timeout_seconds + selection_capture_timeout_ms) + `Placeholder` (placeholderStyle). Groups the Claude-CLI-facing knobs together and keeps the cosmetic picker in its own group. Avoids singleton sections.

**7. Where does `AISettingsView` live?**
- Options: A) nested in `SettingsView.swift` alongside `GeneralSettingsView` · B) new file
- Decision: **A)** — CLAUDE.md says prefer editing existing files; `SettingsView.swift` already hosts `GeneralSettingsView` and `PrivacySettingsView` as nested structs.

**8. Help text (`.help(...)`) on each row?**
- Options: A) on every row · B) only where non-obvious · C) none
- Decision: **B)** — `binary` (auto-detect message) and `timeout_seconds` (units); others are self-explanatory via the label.

## 6. Design

### 6.1 Binary path — empty ↔ nil bridge

Inside `AISettingsView` body:

```swift
private var binaryBinding: Binding<String> {
    Binding(
        get: { store.config.ai.binary ?? "" },
        set: { newValue in
            store.config.ai.binary = newValue.isEmpty ? nil : newValue
        }
    )
}
```

Exactly matches AC3: empty UI → `nil`, else the literal string (no trimming — respect user intent; pathological trailing space is their choice). This is the only non-trivial binding; everything else uses direct `$store.config.ai.<field>`.

### 6.2 View skeleton

```swift
struct AISettingsView: View {
    @Bindable var store: MatchStore

    var body: some View {
        Form {
            Section("Model") {
                TextField("Model", text: $store.config.ai.model)

                TextField("Claude binary", text: binaryBinding, prompt: Text("Auto-detect"))
                    .help("Path to the `claude` CLI. Leave blank to auto-detect: ~/.claude/local/claude, ~/.local/bin/claude, /opt/homebrew/bin/claude, /usr/local/bin/claude, then shell lookup.")

                TextField("Timeout (seconds)", value: $store.config.ai.timeoutSeconds, format: .number)
                    .help("Max seconds to wait for the AI response before failing.")

                TextField("Selection capture timeout (ms)", value: $store.config.ai.selectionCaptureTimeoutMs, format: .number)
            }

            Section("Placeholder") {
                Picker("Placeholder style", selection: $store.config.ai.placeholderStyle) {
                    Text("Static").tag(PlaceholderStyle.static)
                    Text("Braille").tag(PlaceholderStyle.braille)
                    Text("Block sweep").tag(PlaceholderStyle.blockSweep)
                }
            }
        }
        .formStyle(.grouped)
        .padding()
        .onChange(of: store.config) { _, _ in
            store.saveConfig()
        }
    }

    private var binaryBinding: Binding<String> { /* §6.1 */ }
}
```

Note: `PlaceholderStyle.static` compiles as-is — Swift allows reserved words after `.` in member-access position, so no backticks are needed inside `.tag(...)`. The enum's case declaration uses `` case `static` ``; that's the only place backticks are required.

### 6.3 TabView wiring

In `SettingsView.body`, insert between General and Privacy:

```swift
AISettingsView(store: store)
    .tabItem { Label("AI", systemImage: "sparkles") }
```

### 6.4 Data flow

```
User edits field in AI tab
  ↓ @Bindable propagates mutation to store.config.ai.X
  ↓ store.config changes (AppConfig is Equatable)
  ↓ .onChange(of: store.config) fires in AISettingsView
  ↓ store.saveConfig() → configLoader.saveConfig(config)
  ↓ YAMLEncoder writes full AppConfig to ~/.config/sokki/config/default.yml
```

`AppDelegate.startFSEventsWatcher` (L300–351) calls `matchStore.loadAll()` via a debounced reload (~600 ms). `loadAll()` re-decodes the YAML and reassigns `store.config`. Because `AppConfig` is `Equatable` and the decoded value equals the in-memory value we just wrote, SwiftUI's `.onChange(of: store.config)` will NOT re-fire — no save loop.

The FSEvents path also re-runs `AppDelegate.applyConfig()` (it's how every other config tab's changes reach the engine today), which is the mechanism by which AI changes propagate to `KeystrokeMonitor` and friends. AI-tab edits therefore take effect after the ~600 ms FSEvents debounce, same as every other General-tab field. No explicit engine-update call is needed from the view.

### 6.5 Error handling

- Empty binary TextField → `binary = nil` → resolver auto-detects (documented in `.help`).
- Non-numeric input in Int TextFields → SwiftUI's `.number` formatter rejects the keystroke at the field level; the Int stays at its last valid value. Consistent with existing General tab rows.
- `args` is absent from the UI; existing YAML value is preserved because `AppConfig` is Codable and the full struct round-trips on save.

### 6.6 Non-goals / known gaps (flagged, not fixed here)

- `ClaudeBinaryResolver` caches its resolved URL; no existing call site invalidates it when `ai.binary` changes via YAML either. Changing the binary path in the UI will only take effect on the next AI request if the resolver re-reads config (no cache hit) OR after an app restart. This is a **pre-existing gap for YAML edits**, not introduced by this change. Fix is out of scope here; follow-up: call `ClaudeBinaryResolver.invalidate()` from `AppDelegate.applyConfig()` when `ai.binary` changes.
- No numeric validation (negative timeouts, zero). Matches existing behaviour of General tab. If a user sets `timeoutSeconds = 0`, the AI flow fails immediately — observable, not destructive.
- `args` stays YAML-only by user decision. Users editing YAML manually keep full control.

## 7. Files Changed

- `Sources/Sokki/Views/SettingsView.swift` — add `AISettingsView` nested struct; insert `AI` tab between General and Privacy.

No other files touched.

## 8. Verification

1. [AC1] Open Settings → see tabs: Matches, General, **AI** (sparkles icon), Privacy, in that order.
2. [AC2] AI tab shows: Model section (Model / Claude binary / Timeout / Selection capture timeout) + Placeholder section (Placeholder style picker).
3. [AC3a] With a blank Claude binary field: save, inspect `~/.config/sokki/config/default.yml` → `ai.binary` key is absent (or YAML null).
3. [AC3b] Type a path, tab away: `ai.binary` appears in YAML with that path; clear the field, tab away: key disappears again.
4. [AC4] Change placeholder style from Static to Braille, close Settings, re-open: selection remembered; YAML contains `placeholder_style: braille`.
5. [AC5] Add `ai.args: ["--output-format", "json"]` to YAML by hand, restart the app so it decodes, then change the Model field in the AI tab. After the save, diff the YAML: `args` list must be byte-for-byte identical (order and values), since the Codable round-trip is the only way it can regress.
6. [AC6] `swift build` — clean compile under Swift 6.
