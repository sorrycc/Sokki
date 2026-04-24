# AI Settings Improvements

## 1. Background

The AI tab exposes raw fields but offers no way to verify the `claude` CLI
actually works from Sokki's sandbox/launch environment. Users hit failures
only mid-expansion, via a terse HUD. The current default model `haiku` also
under-utilises what Sokki is commonly used for (rewrites benefit from
`sonnet`-class quality).

## 2. Requirements Summary

- Change the default model to `sonnet`.
- Add an in-settings button to run a round-trip AI test and show detailed
  diagnostics on failure.

## 3. Acceptance Criteria

1. Fresh configs default `ai.model` to `sonnet`.
2. Configs missing `ai.model` on disk fall back to `sonnet` at decode time.
3. AI tab has a visible "Test connection" button.
4. While testing: button disabled, spinner visible.
5. Success state shows resolved binary path, model, and trimmed CLI reply.
6. Failure state shows categorised reason (timeout / binary missing / auth /
   non-zero exit / protocol), with stderr tail or hint where relevant.

## 4. Problem Analysis

`AISettings` currently hardcodes `"haiku"` in the struct default and in the
`init(from:)` fallback path. The `AIRewriter` is instantiated once inside
`KeystrokeMonitor` and serialises work via `cancelCurrent()`; running a test
from the settings view through that same instance would abort any in-flight
engine/palette request. Instantiating a second rewriter in the settings view
with a fresh `ClaudeBinaryResolver` is ~3 lines and fully isolates the two.

## 5. Decision Log

**1. Where does the test run from?**
- Options: A) Reuse engine rewriter · B) New rewriter in view
- Decision: **B)** — avoids cancelling active engine work; matches KISS.

**2. Test prompt?**
- Options: A) empty · B) "ping" + one-word reply instruction · C) arbitrary
- Decision: **B)** — shortest round-trip that exercises auth + JSON parse.

**3. Error detail level?**
- Options: A) one-line (like HUD) · B) multi-line with paths and stderr tail
- Decision: **B)** — the user asked explicitly for detail.

**4. Migration for users on explicit `haiku`?**
- Options: A) silent rewrite · B) respect user value · C) prompt
- Decision: **B)** — user config wins; only missing/malformed → `sonnet`.

**5. Share scratch-dir helper with engine?**
- Options: A) extract · B) inline 5 lines
- Decision: **B)** — YAGNI.

## 6. Design

### AISettings
Change struct default and decoder fallback from `"haiku"` → `"sonnet"`. No
CodingKeys change, no migration code — configs with a serialised `model`
continue to deserialise unchanged.

### AISettingsView
Add a new `Section("Test")` containing:
- `Button("Test connection")` — disabled while running
- `ProgressView()` shown while running
- A status block rendered below the button. States:
  - `.idle`  → hidden
  - `.running` → "Running…"
  - `.ok(binary, model, reply)` → green check + three labeled lines
  - `.fail(detail)` → red X + multi-line `detail`

A fresh `AIRewriter` + `ClaudeBinaryResolver` is created lazily per view
instance via `@State`. The scratch cwd comes from a 5-line local helper
that mirrors `KeystrokeMonitor.aiScratchDirectory`.

### Reason → detail mapping
- `.binaryMissing` → "`claude` binary not found. Set a path in 'Claude
  binary' above, or install Claude Code CLI."
- `.authRequired` → "Not signed in. Run `claude` in a terminal and sign in."
- `.timeout` → "Timed out after <N>s. Increase Timeout above or check
  network."
- `.nonZeroExit(code, tail)` → "exit <code>\n<tail (≤500 chars)>"
- `.protocolError(s)` → "Protocol error: <s>"

Each failure message is prefixed with resolved binary path (if any) so the
user can verify Sokki picked the right CLI.

### Running the request
```
AIRewriter.Request(
    prompt: "ping",
    model: store.config.ai.model,
    timeout: TimeInterval(store.config.ai.timeoutSeconds),
    userArgs: store.config.ai.args
)
```
Use the configured `ai.binary` as the explicit path override.

## 7. Files Changed

- `Sources/Sokki/Models/AISettings.swift` — default & fallback `haiku` →
  `sonnet`.
- `Sources/Sokki/Views/SettingsView.swift` — new Test section, local
  `AIRewriter`/resolver, reason-to-detail mapping, status rendering.

## 8. Verification

1. [AC1] Delete `~/Library/Application Support/Sokki/config.toml` (or the
   equivalent path), relaunch, open AI tab → model field shows `sonnet`.
2. [AC2] Hand-edit config removing `ai.model` → relaunch → model shows
   `sonnet`; other fields preserved.
3. [AC3] AI tab shows "Test connection" button.
4. [AC4] Click button → spinner appears, button disables, reenables on
   completion.
5. [AC5] Happy path → green status, binary path + model + reply shown.
6. [AC6] Failure paths (rename `claude` binary, set wrong path, unplug
   network with tiny timeout) each render distinct detailed messages.
