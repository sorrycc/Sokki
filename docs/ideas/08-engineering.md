# Engineering Excellence — Performance, Correctness, Reliability

## Idea 1: Tap-Thread Watchdog With Auto-Recovery

**Pitch:** The CGEvent tap is Sokki's single point of failure — macOS silently disables it on timeout, CPU spikes, heavy app switches, or permission flips, and right now `.tapDisabledByTimeout` / `.tapDisabledByUserInput` are acknowledged but never actually re-enabled (look at `TapHandler.handleEvent` — it just returns the event). A proper supervisor turns "silently dead expander" into "heals itself in under 50ms, logs the incident, never loses the user."

**How it works:**
- Add a heartbeat counter incremented on every event, sampled every 500ms from a second GCD timer; if no events land but the system is active (via `CGEventSourceSecondsSinceLastEventType`), escalate.
- On `.tapDisabledByTimeout` / `.tapDisabledByUserInput`, immediately call `CGEvent.tapEnable(tap:enable:true)` using a `TapState` reference (not just `return event`) — then bump a counter; if it flaps more than N times in 60s, tear down and recreate the tap on a fresh thread.
- Watchdog also checks `AXIsProcessTrustedWithOptions` and `CGPreflightListenEventAccess()` transitions — rebuilds the tap when permissions are re-granted so users don't need to relaunch.
- Expose a "Health" status in the menu bar (green/yellow/red dot) derived from: tap alive, secure-input state, last reload success, last expansion latency — no telemetry, pure local signal.
- Persist a terse supervisor journal to `~/.config/sokki/logs/supervisor.log` (rotation reuse) with structured events: `TAP_DISABLED`, `TAP_REBUILT`, `PERM_REVOKED`.

**Why it's accretive:** This is the foundational reliability moat. Every other feature assumes the tap is alive; an auto-healing tap turns Sokki from "works until it doesn't" into "a daemon you forget is running." It also surfaces failure modes that the user currently has zero signal for — building trust through observability, not analytics.

**Risk / cost:** Low code, high discipline. Must be careful not to double-enable or race with `stop()`; a flapping tap must back off exponentially to avoid log-storming. Small Instruments pass to confirm supervisor timer itself never wakes the main thread unnecessarily.

---

## Idea 2: Deterministic Replay-Debugger & Fuzz Harness

**Pitch:** Ship a `SokkiReplay` subsystem that captures the last N keystrokes + timings + `TapContext.State` deltas into a bounded in-memory ring, and a `sokki-replay` dev binary that feeds them back through the pure matcher. Combined with a SwiftPM fuzz target over YAML configs + Unicode streams, you get a matcher you can *prove* correct instead of hope is correct.

**How it works:**
- Introduce a `TraceEvent` (char, keyCode, flags, monotonic ns timestamp, resulting `FeedAction`) and a lock-free SPSC ring in `TapContext` — enabled only when `log_level: debug` or a hidden "record" toggle, zero cost in release-hot path.
- "Export Last 5 Seconds" menu item writes a replay bundle (trace + current `[Match]` snapshot + `AppConfig`) to `~/.config/sokki/replays/`. No keystroke *content* leaves the machine unless user explicitly shares the bundle.
- `sokki-replay <bundle>` reconstructs `TapContext`, feeds events, diffs `FeedAction` stream — catches prefix-disambiguation regressions, ghost matches, pending-match flushes. Runs under XCTest too.
- SwiftPM fuzz target (libFuzzer via `-sanitize=fuzzer`) that generates YAML configs + UTF-8 streams and asserts invariants: never crashes, never double-fires, `triggerLength` always ≤ buffer depth, longest-match property holds.
- Property tests: for any random trigger set and any random keystroke stream, the trie matcher's output equals a reference naive-scanner's output.

**Why it's accretive:** Makes matcher bugs reproducible instead of folkloric. Users can send a replay bundle when `:hello` "sometimes doesn't fire in VSCode"; you get exact ground truth. Property tests + fuzzing convert the matcher from "seems fine" to a component you can refactor fearlessly — which is what every serious expander (TextExpander, aText) secretly wishes they had.

**Risk / cost:** Medium. The recording ring must be genuinely allocation-free on the hot path — use a fixed `[TraceEvent]` with an atomic head index. Privacy story matters: default off, redact by default (hash characters unless user opts in), document clearly.

---

## Idea 3: Per-App Injection Profiles With Snapshot Oracle

**Pitch:** The single biggest correctness failure in every text expander on macOS is "works in TextEdit, silently breaks in Electron / Figma web / Terminal / VSCode / password managers / Arc command bar." Replace the one-size `clipboardThreshold` heuristic with a profile table keyed by bundle ID + input-context signals, validated by an automated snapshot oracle that runs the top 50 apps nightly and scores them.

**How it works:**
- Extend `TextInjector` to consult a `ProfileRegistry` (YAML at `~/.config/sokki/profiles/`, shipped defaults overridable) keyed by `NSWorkspace.frontmostApplication.bundleIdentifier`. Per-app knobs: injection strategy (`keys` | `paste` | `unicodeHotkey`), `backspaceDelay`, `paste_shortcut` (Cmd+V vs Ctrl+V vs Cmd+Shift+V for Terminal), `preAsciiNormalize`, `injectDelay`.
- Detect Electron / web-view apps via a small table (Slack, Notion, Figma, Linear, Arc, VSCode, Discord) and default them to paste+longer restore delay — the known-broken-with-keys class.
- Snapshot oracle: a harness runs in CI on a macOS runner, uses `osascript`/Accessibility APIs to focus each target app, types a seeded trigger, reads the resulting text back, and asserts round-trip equality. Produces a per-app compatibility matrix checked into repo.
- When frontmost app changes (`NSWorkspace.didActivateApplicationNotification` — already observed), resolve the profile once, stash it on `TapContext` under the same lock so the hot path does a single pointer read.
- First-class "Report a broken app" menu item: captures bundle ID + a minimal reproducer profile, writes a profile stub the user can tune.

**Why it's accretive:** This is where Sokki wins against espanso and TextExpander — not by having more features but by *demonstrably working everywhere*. A checked-in compatibility matrix ("Sokki passes 48/50 apps, here's the list") is the single most convincing artifact you can ship. It also protects against Apple/Electron behavior drift: the oracle detects regressions before users do.

**Risk / cost:** Medium-high. Nightly CI on macOS runners costs money; app-automation via AX is fiddly. Profile YAML risks config sprawl — keep the schema tight and mostly-defaults. Start with 10 apps, expand.

---

## Idea 4: Latency Budget + App-Native Undo Integration

**Pitch:** Define a hard p99 budget of 5ms from keystroke receipt to first injected keystroke, measure it continuously, and fix the one thing that makes Cmd-Z feel wrong today: expansions aren't a single undo group in the target app. Make Sokki the only expander where (a) latency is a posted guarantee and (b) Cmd-Z undoes the whole expansion in one stroke, app-native.

**How it works:**
- Instrument `TapContext.feed` → `onMatch` → `TextInjector.inject` with `mach_absolute_time()` spans; maintain an HDR-histogram-style bucketed counter in `TapContext` (no allocations). Expose p50/p99 in the menu bar health popover and fail loudly (yellow dot) if p99 > 5ms for 30s.
- Move injection off `DispatchQueue.main` for the key-simulation path — it doesn't touch AppKit. Post CGEvents directly from the tap thread (or a dedicated "InjectionQ") to cut a main-actor hop worth ~1-3ms of jitter. Keep clipboard path on main because pasteboard API requires it.
- For Cmd-Z integration: in paste mode, before posting Cmd+V, post a synthetic marker that many apps honor (NSTextView, WebKit inputs) to group the backspace+paste into one undo record. For key-sim mode, batch the backspace+inject sequence with tight timing (<2ms inter-event) so input managers coalesce it; fall back to paste mode automatically in apps detected (via the profile registry above) to split undo.
- Add a post-expansion "undo barrier": if the user presses Cmd-Z within 3s, Sokki intercepts, restores the trigger, and *swallows* the Cmd-Z so the app doesn't then undo the user's previous real edit. Current undo-via-backspace stays as a fallback.
- Publish a `PERFORMANCE.md` with the budget, measurement methodology, and the current p50/p99 from the test harness — a credibility artifact.

**Why it's accretive:** Latency and undo are the two visceral "feels native" signals. A posted budget + live measurement turns performance from a vibe into a contract; shaving the main-thread hop is measurable and free. App-native undo removes the single most common "wait, that undid the wrong thing" complaint about text expanders, which is the bug everyone tolerates because nobody has fixed it.

**Risk / cost:** Main-thread removal needs audit — `TextInjector` currently is `@MainActor` and uses `DispatchQueue.main.asyncAfter` for clipboard restore; careful separation of key-sim vs clipboard paths. Cmd-Z interception is delicate (don't break real undo); gate it behind a config flag and the per-app profile. Histogram must be genuinely O(1) — pre-bucketed, atomic increments only.

---

**Sequence:** Idea 1 (watchdog) is table stakes — do it this week, it's small. Idea 3 (per-app profiles + snapshot oracle) is the *moat*: the one thing no competitor will copy because it requires ongoing operational discipline. Ideas 2 and 4 are how you keep the moat from leaking.

**Relevant files:**
- `Sources/Sokki/Engine/KeystrokeMonitor.swift` — tap re-enable TODO lives around lines 206-210
- `Sources/Sokki/Engine/TapContext.swift` — add trace ring + latency histogram here
- `Sources/Sokki/Engine/TextInjector.swift` — profile dispatch + main-thread removal target
- `Sources/Sokki/Engine/TrieMatcher.swift` — property-test / fuzz target
- `docs/designs/2026-03-20-sokki.md` — extend with per-app profile schema
