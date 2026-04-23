# Log Level Fix

## 1. Background

`Logger` (Sources/Sokki/Services/Logger.swift) exposes three levels — `error`, `warning`, `debug` — but a grep across `Sources/` shows ~40 `Logger.shared.error(...)` call sites, most of which describe routine lifecycle traces, success paths, or recoverable conditions rather than genuine errors. The log file in `~/.config/sokki/logs/sokki.log` is therefore full of mis-labelled `[ERROR]` lines, which makes triage of real failures harder.

Root cause: the runtime default level is `.error` (both the Swift fallback in `Logger.swift` and the config-driven default in `AppConfig.swift`/`Bootstrapper.swift`), and only `debug()` is gated. Developers wanting a line to always appear picked `.error()` as the "always-on" channel, ignoring the severity label.

## 2. Requirements Summary

**Goal:** Reclassify every `Logger.shared.error(...)` call so its level reflects the event's actual severity, while preserving the current verbose behaviour of the log file.

**Scope**
- In scope: every `Logger.shared.(error|warning|debug)` call under `Sources/Sokki/`; default runtime level in `Logger.swift`; config-driven default in `AppConfig.swift` and `Bootstrapper.swift`.
- Out of scope: changing the `Logger` API; adding new levels; changing log rotation; `os.Logger` calls in `UpdaterService.swift` (those are already correct); migrating existing user configs.

**Key Decisions (confirmed with user)**
1. Default runtime level → `.debug` so every line still lands in the log file. Because `AppDelegate.applyConfig()` sources the level from `AppConfig.logLevel`, the effective default lives in `AppConfig` + `Bootstrapper` — both flip from `"error"` to `"debug"`.
2. `[AI] failed / cancelled / refused / no selection / render aborted` and all AI-pipeline parse-failure diagnostics → `.warning`.
3. Trace/success lines (`[Palette] …`, `[AI] run …`, `[AI] completed …`, `[AI] parse: ok …`, `[AI] process exit=…`, `[AI] cancelCurrent`, `found at …`, canon diagnostics) → `.debug`.

## 3. Acceptance Criteria

1. Every `Logger.shared.error(...)` call in `Sources/` that describes a non-error event is reclassified to `.debug` or `.warning` per §6.2.
2. Effective default level on a fresh install is `.debug`; all lines continue to appear in `sokki.log`. Existing users keep their configured level (no migration); their logs will show correctly labelled lines filtered to whatever they chose.
3. Real errors (parse/save/IO failures, CGEvent tap creation failure, Sparkle update failures, binary-not-found at the resolver layer, config errors) stay at `.error`.
4. Recoverable/expected failures on the AI pipeline (including CLI protocol violations that surface as `.failed(...)` events) sit at `.warning`.
5. Post-edit, `grep -c "Logger.shared.error(" Sources/Sokki/` returns exactly the count of the "Left as error" list in §6.2 (see §8 for the target number).

## 4. Problem Analysis

**Approach A — Gate `error()` and `warning()` on `Self.level` as well, keep default `.error`.**
Result: Rejected. Drops almost all existing log lines by default; regression for users who rely on the current verbosity. Out of scope.

**Approach B — Add a new `.info` level between warning and debug.**
Result: Rejected. YAGNI; the three existing levels map cleanly onto "broken", "recoverable", "trace". Adds API surface for no user-visible win.

**Chosen — Approach C: Remap call sites + flip the config-driven default level to `"debug"`.**
Preserves the user-visible behaviour on new installs (log file remains verbose) while restoring meaning to the severity labels. Minimal code change; each edit is mechanical.

## 5. Decision Log

**1. Raise the default level?**
- Options: A) Keep `.error` · B) `.warning` · C) `.debug`
- Decision: **C) `.debug`** — user selection; correctness target is labels, not volume.

**2. Where to flip the default?**
- Options: A) Just `Logger.swift`'s Swift fallback · B) `Logger.swift` + `AppConfig.swift` + `Bootstrapper.swift` template
- Decision: **B)** — `AppDelegate.applyConfig()` overwrites `Logger.level` from `config.logLevel` on startup, so the Swift fallback alone is a no-op for real users. The effective default is owned by the config path.

**3. Classification rule for `[AI] failed / cancelled / refused / no selection` AND AI-pipeline parse-failure sites.**
- Options: A) `error` · B) `warning` · C) `debug`
- Decision: **B) `warning`** — recoverable, expected outcomes that the caller already summarises at `.warning` (`KeystrokeMonitor:343`). Logging the detail at `.error` at one frame and the summary at `.warning` one frame up would report the same event at two severities.

**4. Classification rule for success/trace lines (panel state, `selection captured`, `placeholder injected`, `completed len=…`, `parse: ok`, `process exit=…`, `run binary=…`, `cancelCurrent`, `found at …`, canon override logs).**
- Options: A) `warning` · B) `debug`
- Decision: **B) `debug`** — routine diagnostics, not operational events. Canon override logs (`[AI] overrode …`, `[AI] inserted …`) mix "user value replaced" with "default inserted" in one array; both are diagnostic traces, not recoverable failures, so the single debug bucket is correct without refactoring the function. Tradeoff: a user who explicitly set a flag in `ai.args` only sees the silent override at `[DEBUG]`, which is invisible on an existing install that still carries `log_level: error`. Acceptable — the override is rare, and users with stale configs can flip the picker if they want the trace back.

**5. Gate `error()`/`warning()` on `Self.level` for symmetry?**
- Options: A) Yes · B) No
- Decision: **B) No** — current write-always behaviour is intentional. YAGNI for the stated problem.

**6. Touch `os.Logger` calls in `UpdaterService.swift`?**
- Decision: **No** — already correct (genuine update failures).

**7. Migrate existing users' `log_level: error` configs to `"debug"`?**
- Options: A) Force migration · B) Leave alone
- Decision: **B) Leave alone** — KISS. Their existing file wins; they'll see only correctly labelled `[ERROR]` lines (the ones that remain after this remap), which is the triage improvement the fix is supposed to deliver. If they want verbose logs, the Settings picker flips `log_level` to `"debug"` in one click.

## 6. Design

### 6.1 Remapping rules

A call is classified by the kind of event it describes:

- **`error`** — a state the program cannot continue from cleanly, or an IO/parse failure that loses user data or surfaces a bug. Examples: "Failed to parse config/default.yml", "Failed to create CGEvent tap", Sparkle "Update aborted", resolver-layer "claude binary not found; checked=…".
- **`warning`** — an expected-but-noteworthy outcome the user might care about. AI pipeline failures, protocol violations from the claude CLI, "another AI run in flight", "no selection (timeout)".
- **`debug`** — routine diagnostics. Panel lifecycle, dispatch info, success summaries, binary-resolution success, canon flag-forcing traces.

### 6.2 Remapping table

**`error` → `debug` (success / trace / routine dispatch):**
- `PaletteController.swift:55, 62, 70, 77, 87, 109, 111, 113, 125, 129` — panel lifecycle traces
- `PaletteView.swift:52, 59, 63, 66` — view focus/query traces
- `ClaudeBinaryResolver.swift:20, 27` — "found at …" / "found via login shell at …"
- `AIRewriter.swift:63` — canon.logs (flag-forcing diagnostics)
- `AIRewriter.swift:65` — "[AI] run binary=…" dispatch line
- `AIRewriter.swift:92` — "[AI] cancelCurrent"
- `AIRewriter.swift:253` — "[AI] process exit=…" per-run summary
- `AIRewriter.swift:301` — "[AI] parse: ok resultLen=…"
- `KeystrokeMonitor.swift:220` — "[AI] match trigger=… promptLen=…"
- `KeystrokeMonitor.swift:241` — "[AI] clipboard snapshot len=…"
- `KeystrokeMonitor.swift:254` — "[AI] selection captured len=… in …ms"
- `KeystrokeMonitor.swift:301` — "[AI] placeholder injected (style=…), dispatching request"
- `KeystrokeMonitor.swift:324` — "[AI] completed len=… elapsed=…ms"

**`error` → `warning` (recoverable / expected failures):**
- `AIRewriter.swift:54` — "[AI] binary not found (override=…)" (caller-side recoverable signal; `ClaudeBinaryResolver.swift:31` logs the root cause at `.error` on the same failure path — the two always co-fire)
- `AIRewriter.swift:260` — "[AI] parse: authRequired stderrTail=…"
- `AIRewriter.swift:266` — "[AI] parse: empty stdout with exit=0"
- `AIRewriter.swift:269` — "[AI] parse: nonZeroExit code=… stderrTail=…"
- `AIRewriter.swift:275` — "[AI] parse: non-object JSON"
- `AIRewriter.swift:291` — "[AI] parse: authRequired (json) msg=…"
- `AIRewriter.swift:294` — "[AI] parse: is_error=true msg=…"
- `AIRewriter.swift:298` — "[AI] parse: missing `result` field keys=…"
- `AIRewriter.swift:306` — "[AI] parse: JSON decode …"
- `KeystrokeMonitor.swift:214` — "[AI] refused — another AI run is in flight"
- `KeystrokeMonitor.swift:256` — "[AI] no selection (timeout=…)"
- `KeystrokeMonitor.swift:268` — "[AI] selection missing — falling back to chain"
- `KeystrokeMonitor.swift:276` — "[AI] render aborted: no input for …"
- `KeystrokeMonitor.swift:343` — "[AI] failed reason=…"
- `KeystrokeMonitor.swift:354` — "[AI] cancelled typedDuringWait=…"

**Left as `error` (genuine errors):**
1. `ClaudeBinaryResolver.swift:31` — resolver reports no candidate found
2. `KeystrokeMonitor.swift:204` — "[AI] template parse failed (should not happen)"
3. `KeystrokeMonitor.swift:581` — "Failed to create CGEvent tap"
4. `AppDelegate.swift:281` — "Quit & Reopen: failed to spawn open"
5. `AppDelegate.swift:286` — "Quit & Reopen: open exited … aborting terminate"
6. `AppDelegate.swift:325` — "Failed to create FSEvents stream"
7. `Bootstrapper.swift:22` — "Bootstrap failed: …"
8. `ConfigLoader.swift:48` — "Failed to parse config/default.yml"
9. `ConfigLoader.swift:95` — "Failed to parse \(filename)"
10. `ConfigLoader.swift:126` — "Failed to save \(filename)"
11. `ConfigLoader.swift:139` — "Failed to save config"
12. `MatchStore.swift:29` — "Config load error"
13. `RecencyStore.swift:65` — "Failed to save recency.json"
14. `SettingsView.swift:121` — "Failed to set launch at login"
15. `UpdaterService.swift:10` — "Update aborted: …"
16. `UpdaterService.swift:21` — "Update cycle error"

Total `error`-level sites after remap: **16**.

### 6.3 Default-level change (config-driven)

Three coordinated edits so the runtime default becomes `.debug` on fresh installs:

`Sources/Sokki/Services/Logger.swift:21`

```swift
// before
nonisolated(unsafe) private static var _level: Level = .error
// after
nonisolated(unsafe) private static var _level: Level = .debug
```

`Sources/Sokki/Models/AppConfig.swift:10,59`

```swift
// before
var logLevel: String = "error"
...
self.logLevel = (try? container.decode(String.self, forKey: .logLevel)) ?? "error"
// after
var logLevel: String = "debug"
...
self.logLevel = (try? container.decode(String.self, forKey: .logLevel)) ?? "debug"
```

`Sources/Sokki/Services/Bootstrapper.swift:47` (the YAML template seeded on first run):

```yaml
# before
log_level: "error"               # "error" (default) or "debug"
# after
log_level: "debug"               # "debug" (default) or "error"
```

`AppDelegate.applyConfig()` at line 154 already maps `"debug"` → `.debug` and anything else → `.error`; no change needed there.

Existing users whose config file already contains `log_level: "error"` keep that setting — see §5 decision 7.

## 7. Files Changed

- `Sources/Sokki/Services/Logger.swift` — `_level` default → `.debug`
- `Sources/Sokki/Models/AppConfig.swift` — `logLevel` default → `"debug"` (two sites: property default and decoder fallback)
- `Sources/Sokki/Services/Bootstrapper.swift` — first-run YAML template `log_level: "debug"`
- `Sources/Sokki/Services/PaletteController.swift` — 10 calls: `error` → `debug`
- `Sources/Sokki/Views/PaletteView.swift` — 4 calls: `error` → `debug`
- `Sources/Sokki/Services/ClaudeBinaryResolver.swift` — 2 calls: `error` → `debug`
- `Sources/Sokki/Services/AIRewriter.swift` — 5 calls: `error` → `debug`; 9 calls: `error` → `warning`
- `Sources/Sokki/Engine/KeystrokeMonitor.swift` — 5 calls: `error` → `debug`; 6 calls: `error` → `warning`

## 8. Verification

1. [AC1, AC3, AC4, AC5] After edits, `grep -c "Logger.shared.error(" Sources/Sokki/` returns exactly **16** (matches §6.2 "Left as `error`"). Spot-check: the three classes of site demoted in this pass — `[Palette] …`, `[AI] parse: …`, and `[AI] completed …` — must NOT appear in `grep -n "Logger.shared.error(" Sources/`.
2. [AC2] `Sources/Sokki/Services/Logger.swift:21` shows `_level: Level = .debug`; `AppConfig.swift:10,59` show `"debug"`; `Bootstrapper.swift:47` template shows `"debug"`.
3. `swift build` exits 0 (label-only change; trivial, run as sanity check).
4. Smoke check: `swift run`, open the palette, trigger one AI expansion on a happy path and one forced failure (no selection). Tail `~/.config/sokki/logs/sokki.log` — every `[ERROR]`/`[WARN]`/`[DEBUG]` label must match the table in §6.2.
