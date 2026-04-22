# AI Rewrite Triggers (Idea 1, Claude CLI variant)

Date: 2026-04-21
Status: Design — not yet implemented

## Goal

Let a match trigger invoke an AI rewrite/generation instead of a static
string. Backend is the `claude` CLI (haiku model). Two flows:

1. **Rewrite selection** — user selects text, types `;;pro`, the selection is
   replaced by the AI output.
2. **Generate from context** — trigger expands into AI-generated text whose
   input is the clipboard (`input: clipboard`) or nothing (`input: none`).

A loading indicator appears inline at the cursor until the response lands.

## Reality check (read this first)

A live test of
`claude -p "hi" --output-format stream-json --verbose --model haiku` revealed
three facts that reshape the design:

1. **No per-token streaming.** Events arrive at **message granularity**, not
   token. For a single-turn rewrite the CLI emits: `system:hook_started` →
   `system:hook_response` → `system:init` → `assistant` (thinking block) →
   `assistant` (the full text in one event) → `rate_limit_event` → `result`.
   The entire assistant text lands in one blob. The original "streaming
   tokens materialize live" UX is **not achievable** on this backend.
2. **Expensive and slow in default mode.** The unmitigated "hi" round-trip:
   - `duration_ms: 3533` → 3.5 s end-to-end.
   - `total_cost_usd: 0.03974` → ~$0.04 per call.
   - Input tokens ~30 000 — the Claude Code CLI loads its full session
     init (tools, skills, hooks, project `CLAUDE.md`).

   We mitigate this with `--bare` + `--tools ""` + scratch cwd (§2.2, §2.6),
   which is documented to skip hooks/skills/plugins/MCP/memory/`CLAUDE.md`.
   **Re-measure `duration_ms` and `total_cost_usd` with the full flag set
   before shipping** and update these numbers here. If cost is still >$0.01
   per call at typical rewrite lengths, the feature is arguably unshippable
   on this backend — flag it to the user and consider the direct-API
   follow-up (§11).
3. **Agent mode by default.** The `init` event reports `tools: [Bash, Edit,
   Write, WebFetch, …]` and `permissionMode: "acceptEdits"`. A text-rewrite
   trigger must not run tools. `--tools ""` (§2.2) is the documented
   disable-all syntax.

**Implications:**

- UX pivots from "live token stream" to **"spinner → single reveal"**.
- Sokki should be honest in docs that AI triggers cost real money per call,
  take ~3 s minimum, and ship user text to Anthropic.
- The `claude` CLI is not the right long-term backend for this feature.
  Track migrating to the direct Anthropic API (SSE streaming, lean system
  prompt, ~100× cheaper) as an explicit follow-up in §10. Keep this CLI MVP
  scoped and disposable.

## Decisions (from brainstorming + reality check)

- Both flows (selection rewrite + clipboard/none generation) in MVP.
- Selection capture: Cmd+C snapshot with `changeCount`-gated polling.
- Delivery: **single reveal** (no per-token streaming). Spinner placeholder
  until the `result` event arrives, then inject the final text in one event.
- Loading state: inline placeholder at cursor (`▎ thinking…`).
- Prompt schema: YAML `ai:` block on the match.
- Esc cancels in-flight; errors restore on a best-effort basis (§3.3).
- Hard timeout: 30 seconds.

## 1. Schema & config

Extend `Match` with an optional `ai` block. If present it takes precedence;
`replace` is ignored.

```yaml
- trigger: ";;pro"
  ai:
    prompt: "Rewrite professionally, keep meaning."
    input: selection          # selection | clipboard | none
- trigger: ";;tldr"
  ai:
    prompt: "Summarize in 3 bullets."
    input: clipboard
- trigger: ";;stand"
  ai:
    prompt: "Draft a 3-line standup update."
    input: none
```

Swift:

```swift
struct AIConfig: Codable, Sendable, Equatable {
    let prompt: String
    let input: AIInput       // default: .selection when decoded without key
}

enum AIInput: String, Codable, Sendable { case selection, clipboard, none }
```

`AppConfig` gains:

- `aiBinaryPath: String?` — explicit override (nil → resolved at startup).
- `aiModel: String` — default `"haiku"`.
- `aiTimeoutSeconds: Int` — default `30`.
- `aiSelectionCaptureTimeoutMs: Int` — default `400` (see §3.1).

## 2. New service: `AIRewriter`

`Sources/Sokki/Services/AIRewriter.swift`. `@MainActor` facade; the actual
`Process` is hidden behind a `CommandRunner` protocol so tests can feed fake
NDJSON without a real `claude` binary.

```swift
protocol CommandRunner: Sendable {
    func run(
        executable: URL,
        args: [String],
        stdin: Data?,
        onStdoutLine: @Sendable (String) -> Void,
        onStderrLine: @Sendable (String) -> Void
    ) async throws -> Int32   // exit code
}

@MainActor
final class AIRewriter {
    struct Request {
        let prompt: String
        let input: String?       // selection/clipboard body, or nil
        let model: String
        let timeout: TimeInterval
    }
    enum Event: Sendable {
        case completed(String)   // final text, one event
        case failed(Reason)
        case cancelled
    }
    enum Reason: Sendable {
        case timeout
        case binaryMissing
        case authRequired
        case nonZeroExit(code: Int32, stderrTail: String)
        case protocolError(String)   // bad JSON / unexpected shape
    }
    init(runner: CommandRunner = ProcessCommandRunner(),
         resolver: ClaudeBinaryResolver)
    func run(_ req: Request) async -> Event
    func cancelCurrent()
}
```

Note the shape change from earlier drafts: because the CLI does not stream
tokens, `AIRewriter.run` returns a single `Event` rather than an
`AsyncStream`. Cancellation still works — `cancelCurrent()` sends SIGTERM
and the in-flight `await` resolves to `.cancelled`.

### 2.1 Binary resolution (`ClaudeBinaryResolver`)

`/usr/bin/env claude` fails under launchd (minimal PATH). Resolve once at
startup, cache an absolute `URL`, and re-resolve on demand if the cached
path disappears. Search order:

1. `AppConfig.aiBinaryPath` if set.
2. `~/.claude/local/claude` (official installer).
3. `~/.local/bin/claude`.
4. `/opt/homebrew/bin/claude` (Apple Silicon Homebrew).
5. `/usr/local/bin/claude` (Intel / older).
6. Login-shell PATH: `zsh -lc 'command -v claude'`.

If none succeed, AI requests fail with `.binaryMissing`. First-time failure
fires a HUD error plus a one-time menu-bar notification pointing at the
README (persistence key `UserDefaults: "aiBinaryMissingWarningShown"`,
reset when any valid binary is later resolved).

### 2.2 Command line

Flag names verified against `claude-code-docs/docs/cli-reference.md`:

```
<resolved-claude>
    --print
    --bare                         # skip CLAUDE.md/skills/hooks/plugins/MCP/memory
    --tools ""                     # disable ALL tools (empty string = none)
    --model haiku                  # alias; resolves to claude-haiku-4-5
    --output-format json           # single-reveal blob, not NDJSON
    --max-turns 1                  # no agentic loops; one round-trip
    --no-session-persistence       # don't write a session file per call
    --append-system-prompt
        "Output only the transformed text. Do not add preamble, quotes,
         explanations, or commentary."
```

Spawned with `cwd = <sokki-scratch-temp-dir>` (see §2.6) so even if
`--bare` fails to suppress every context source, there is no project
`CLAUDE.md` at the cwd to discover.

Notes on each flag:

- **`--print`** — canonical non-interactive flag; same as `-p`.
- **`--bare`** — documented as *"skip auto-discovery of hooks, skills,
  plugins, MCP servers, auto memory, and CLAUDE.md so scripted calls start
  faster"*. This directly addresses the ~30 000-token session-init bloat
  observed in the live test and prevents the user's CLAUDE.md persona
  (e.g. *"You are Linus Torvalds"*) from leaking into rewrites. Expect
  meaningfully lower cost and faster cold start; measure before shipping
  and update the Reality-check numbers.
- **`--tools ""`** — documented syntax for "disable all". Cleaner than
  `--disallowedTools "*"`. The live-test `init` event confirmed default
  tool access includes Bash/Edit/Write; for a text rewrite, zero tools is
  the safe default. Do **not** pass `--dangerously-skip-permissions` —
  with no tools there is nothing to prompt for.
- **`--model haiku`** — the docs confirm alias support (`sonnet`/`opus`);
  the live test showed `haiku` resolved to `claude-haiku-4-5-20251001`.
- **`--output-format json`** — emits a single JSON blob containing the
  `result` field. Drops the NDJSON line-buffering parser entirely (we
  were never using the stream, only the final event).
- **`--max-turns 1`** — hard cap at one turn. Belt-and-suspenders with
  `--tools ""` to guarantee no multi-step agent behaviour.
- **`--no-session-persistence`** — avoids writing a resumable session
  file to disk on every AI trigger.
- **`--append-system-prompt`** — verified real (CLI reference §"System
  prompt flags"). Appends to the default prompt without replacing it.

### 2.3 Payload

Always pipe via stdin. No argv length limits, no shell quoting, no
weirdness with newlines / quotes / unicode in the selection.

```
<prompt>\n\n<input or "">
```

### 2.4 Parsing the response

With `--output-format json`, `claude -p` emits a **single JSON document**
on stdout — no NDJSON, no line buffering. Read stdout to EOF, `JSONDecode`
once.

Shape (subset matching the live-test `result` event):

```json
{
  "type": "result",
  "subtype": "success",
  "is_error": false,
  "result": "Hey! I'm ready to help with Sokki. …",
  "duration_ms": 3533,
  "total_cost_usd": 0.03974125,
  "session_id": "…"
}
```

The `.result` field is the final assistant text with all thinking blocks
stripped. That is the only field we use.

Error cases:

- `is_error: true` (even with exit code 0) → `.failed(.nonZeroExit(code, …))`
  with the JSON's error message in the HUD reason.
- Non-zero exit + stderr parse → `.failed(.nonZeroExit(code, stderrTail))`.
- Stderr containing "log in" / "authenticate" / "not signed in" →
  `.failed(.authRequired)`. HUD: *"run `claude auth login` to sign in"*.
- Malformed JSON → `.failed(.protocolError(…))`.

### 2.4.1 Pre-flight auth check (optional optimisation)

`claude auth status` is documented to exit 0 when signed in, 1 when not.
Cheap enough (~100 ms) to run once at startup in `Bootstrapper`. If it
returns 1, surface a one-time HUD nudge *"sign in with `claude auth login`
to enable AI triggers"* and skip the binary-missing notification path.
Re-check lazily on first AI trigger failure.

### 2.5 Lifecycle

- Timeout: a sibling task that calls `process.terminate()` at
  `aiTimeoutSeconds`. Resolve the awaited `Event` as `.failed(.timeout)`.
- Cancel: `cancelCurrent()` → `process.terminate()` (SIGTERM). No process-
  group handling; simple parent-only termination is sufficient.
- Generation counter: each `run()` increments a counter; a late completion
  from a terminated run checks the counter and is dropped, so a straggling
  `result` cannot fire a completion on an already-cancelled request.
- Only one in-flight request per `AIRewriter`. A new AI trigger cancels the
  previous.

### 2.6 Scratch cwd

`Bootstrapper` creates (once) a directory at
`~/Library/Application Support/Sokki/ai-scratch/` that contains no
`CLAUDE.md`, no `.claude/`, nothing. Every `claude` invocation sets this as
its `Process.currentDirectoryURL`. Combined with `--bare`, project memory
cannot leak into rewrites.

## 3. Flow through `KeystrokeMonitor` → `TextInjector`

```
match.ai != nil
  ├─ erase trigger
  ├─ capture input:
  │     .selection  → see §3.1
  │     .clipboard  → NSPasteboard.string (no Cmd+C)
  │     .none       → input = nil
  ├─ inject "▎ thinking…" placeholder via injectViaKeys
  ├─ TapContext: activeAIRequest = true; suppressMatching = true;
  │  userTypedDuringAI = false
  ├─ remember placeholderLength, originalForRestore
  ├─ await AIRewriter.run(...)
  │     .completed(text):   §3.2  (inject final, record undo, clean up)
  │     .failed | .cancelled: §3.3 (restore or leave-with-notice)
  └─ Esc while activeAIRequest: AIRewriter.cancelCurrent(); swallow event
```

### 3.1 Selection capture (robust Cmd+C snapshot)

```
let before = NSPasteboard.general.changeCount
clipboardManager.save()
postCmdC()

// Poll up to aiSelectionCaptureTimeoutMs (default 400 ms) — Electron/Slack
// on slow hardware can take 80–150 ms to populate the pasteboard.
let deadline = Date().addingTimeInterval(Double(timeoutMs) / 1000)
while Date() < deadline,
      NSPasteboard.general.changeCount == before {
    try await Task.sleep(for: .milliseconds(10))
}

guard NSPasteboard.general.changeCount > before,
      let selection = NSPasteboard.general.string(forType: .string),
      !selection.isEmpty
else {
    clipboardManager.restore()
    hud(.aiFailed, reason: "no selection")
    return                           // do NOT send stale clipboard to AI
}

postBackspaceOnce()                   // replaces the still-selected text
clipboardManager.restore()
```

Gating on `changeCount > before` is essential: with no selection, Cmd+C is
a no-op, and a naïve read would ship the previous clipboard contents to the
AI.

### 3.2 Completion (`.completed(text)`)

**Order of operations matters** — `setActiveAIRequest(false)` also clears
`userTypedDuringAI` (§7). Read the flag first, then clear state. Use the
atomic `consumeUserTypedDuringAI() -> Bool` helper on `TapContext` that
returns the current value AND clears both flags in a single locked call:

```swift
let typed = context.consumeUserTypedDuringAI()   // reads + clears under lock
if !typed {
    textInjector.eraseInjected(count: placeholderLength)
    textInjector.injectDelta(text)               // chunked, §3.4
} else {
    textInjector.injectDelta(text)               // append after user's chars
    hud(.aiCompleted, note: "result appended; ▎ thinking… left inline — delete manually")
}
context.recordExpansion(trigger: match.trigger, replacement: injectedText)
context.clearBuffer()                            // prevent trigger poisoning
```

- Chunked injection: each CGEvent carries ≤200 UTF-16 code units
  (`keyboardSetUnicodeString` has a practical per-event limit near that).
- `recordExpansion` keeps backspace-undo working.
- `clearBuffer` prevents the injected tail from prefix-matching a real
  trigger on the user's next keystroke.

### 3.3 Restore on failure / cancel

- If `!userTypedDuringAI`: backspace `placeholderLength` chars, then re-
  inject the original:
  - `.selection`: the captured selection text.
  - `.clipboard` / `.none`: the trigger literal.
  - HUD `.aiFailed` with reason.
- If `userTypedDuringAI`: do **not** erase — would eat user input. Leave
  placeholder + user chars in place, HUD `.aiFailed` with note
  *"stopped — press ⌘Z to undo"*.

Rationale for the typed-during-AI branch: we chose not to freeze user keys
(feels broken), so we detect-and-degrade instead. The common case stays
clean; the edge case defers to native undo.

### 3.4 Injection helpers

- `TextInjector.injectDelta(_:)` — batched unicode-string injection. One
  `CGEvent` per ≤200 UTF-16 code units of the payload, using
  `keyboardSetUnicodeString`. Avoids per-char autocorrect/IME trips in
  Messages, Chrome URL bar, etc. Between chunks sleep `1–2 ms` (reuse
  `config.injectDelay` if non-zero, else `usleep(1500)`) so Electron apps
  with input throttling do not drop events on long outputs.
- `TextInjector.eraseInjected(count:)` — promoted/renamed from the existing
  private `eraseCharacters(count:)`.

Secure input: if `SecureInputMonitor` reports secure input, AI matches are
skipped entirely — no clipboard read, no CLI spawn.

## 4. Loading + HUD

- Inline placeholder `"▎ thinking…"` uses `VariableRenderer.cursorGlyph`.
- `HUDEvent.Verdict` gains `.aiStreaming`, `.aiCompleted`, `.aiFailed`
  (with optional reason string).
- Fire `.aiStreaming` at trigger match; HUD shows a spinner row until a
  follow-up `.aiCompleted` or `.aiFailed`.
- No new panels. No caret tracking.

## 5. Cancel & error handling

- **Esc during stream**: the tap thread reads `activeAIRequest` under lock
  (see §7). When true and keyCode == 0x35 (Esc), call
  `AIRewriter.cancelCurrent()` and swallow the event (return `nil`). When
  false, Esc passes through untouched — native app behaviour (dismiss
  sheet/popover) is unaffected.
- **Non-zero exit / `is_error:true` / parse failure / timeout**: restore
  path per §3.3. HUD reason is stderr tail (truncated to ~120 chars), JSON
  error message, or `"timeout after 30s"`.
- **Binary missing**: §2.1. HUD error + one-time notification.
- **Auth missing**: §2.4. HUD reason *"run `claude` once to sign in"*.

## 6. Undo

Existing `ExpansionUndoManager`: on `.completed`, call
`recordExpansion(trigger: match.trigger, replacement: injectedText)`. The
3 s undo window starts at completion. Backspace-to-undo restores the
original trigger.

Known limitation (preserved, not introduced): undo requires backspacing
`replacement.count` chars for long AI outputs. A single-gesture undo is a
follow-up.

## 7. Thread-safety for `activeAIRequest`

The tap callback runs on the tap thread, not MainActor. AI state must
follow the same pattern `TapContext` already uses for `suppressMatching`:
fields inside `TapContext.State`, mutated through dedicated setters, read
in `handleEvent` under the existing `OSAllocatedUnfairLock<State>`.

```swift
// In TapContext.State
var activeAIRequest = false
var userTypedDuringAI = false

// In TapContext
func setActiveAIRequest(_ active: Bool) {
    lock.withLock { state in
        state.activeAIRequest = active
        if !active { state.userTypedDuringAI = false }
    }
}

func noteUserTypedDuringAI() {
    lock.withLock { state in
        if state.activeAIRequest { state.userTypedDuringAI = true }
    }
}

/// Atomically read `userTypedDuringAI` AND clear both AI flags.
/// Callers on the completion/failure path must use this, never
/// `setActiveAIRequest(false)` followed by a separate read — the setter
/// clears `userTypedDuringAI` as a side effect.
func consumeUserTypedDuringAI() -> Bool {
    lock.withLock { state in
        let typed = state.userTypedDuringAI
        state.activeAIRequest = false
        state.userTypedDuringAI = false
        return typed
    }
}

var isAIActive: Bool { lock.withLock { $0.activeAIRequest } }
```

In `TapHandler.handleEvent`:

- On Esc (keyCode 0x35) when `context.isAIActive`: dispatch to MainActor to
  call `AIRewriter.cancelCurrent()`, return `nil` to swallow.
- On any non-Esc `keyDown` when `context.isAIActive`: call
  `noteUserTypedDuringAI()`, still pass the event through.

## 8. File-level touchpoints

New:

- `Sources/Sokki/Services/AIRewriter.swift`
- `Sources/Sokki/Services/ClaudeBinaryResolver.swift`
- `Sources/Sokki/Services/CommandRunner.swift` (protocol + `ProcessCommandRunner`)
- `Sources/Sokki/Models/AIConfig.swift`

Modified:

- `Sources/Sokki/Models/Match.swift` — optional `ai: AIConfig?`.
- `Sources/Sokki/Models/AppConfig.swift` — `aiBinaryPath`, `aiModel`,
  `aiTimeoutSeconds`, `aiSelectionCaptureTimeoutMs`.
- `Sources/Sokki/Models/HUDEvent.swift` — add `.aiStreaming`, `.aiCompleted`,
  `.aiFailed` (with optional reason).
- `Sources/Sokki/Engine/TapContext.swift` — `activeAIRequest` and
  `userTypedDuringAI` in `State`; setters; Esc/typing branches.
- `Sources/Sokki/Engine/KeystrokeMonitor.swift` — AI branch on match; Esc-
  cancel; completion/failure handling; buffer-clear on completion.
- `Sources/Sokki/Engine/TextInjector.swift` — `injectDelta` (batched,
  one CGEvent per ≤200 UTF-16 units), `eraseInjected(count:)`.
- `Sources/Sokki/Services/Bootstrapper.swift` — invoke
  `ClaudeBinaryResolver` at startup, cache result.
- `Sources/Sokki/Views/HUDView.swift` — spinner, completed, error rows.
- `Sources/Sokki/Services/ConfigLoader.swift` — decode new YAML keys.

## 9. Settings UI (deferred)

`MatchEditorView` has no fields for `ai.prompt` / `ai.input`. MVP is
YAML-only. Follow-up adds an "AI" tab on the editor with prompt textarea
and input selector. Noted so it doesn't get lost.

## 10. Manual validation checklist

No test target exists. Verify by hand:

- [ ] `;;pro` with selection in TextEdit, Slack, VS Code, Terminal, Xcode.
- [ ] `;;pro` with **nothing selected** → HUD "no selection", no AI call.
- [ ] `;;tldr` with text on clipboard (`input: clipboard`).
- [ ] `;;stand` with `input: none`.
- [ ] Typing nothing during wait → clean placeholder replacement.
- [ ] Typing during wait → placeholder remains; result appended; HUD
      suggests ⌘Z.
- [ ] Esc cancels mid-wait; original trigger/selection restored (clean
      path).
- [ ] Esc outside of an AI wait still passes through to the focused app.
- [ ] `claude` binary missing → clean HUD error, no crash.
- [ ] `claude` resolved from `~/.claude/local/claude`, `~/.local/bin/claude`,
      `/opt/homebrew/bin/claude`, `/usr/local/bin/claude`.
- [ ] Launch via launchd (minimal PATH) still finds `claude`.
- [ ] `claude auth status` pre-flight: signed-out surfaces sign-in nudge.
- [ ] Not signed in mid-run → HUD reason *"run `claude auth login`"*.
- [ ] Network offline / 30 s timeout → clean restore, HUD error.
- [ ] Secure-input field (password) → AI trigger is skipped entirely.
- [ ] Backspace-undo after completion restores trigger.
- [ ] Firing a second AI trigger while one is in flight cancels the first
      (and a late `result` from the first is dropped).
- [ ] Selection with embedded newlines / unicode / quotes round-trips via
      stdin.
- [ ] AI response with `"Sure, here's..."` preamble does **not** appear —
      `--append-system-prompt` suppresses it.
- [ ] Injecting into Messages / Chrome URL bar does not trip autocorrect.
- [ ] Long output (>1000 chars) injects fully without dropped chars in a
      throttling Electron app.
- [ ] `CLAUDE.md` persona does NOT leak into rewrite output — run with the
      current machine's `~/.claude/CLAUDE.md` present (which mentions
      Linus Torvalds) and confirm the rewrite is neutral.
- [ ] Scratch cwd contains no `CLAUDE.md` after first use.
- [ ] Re-measure `duration_ms` and `total_cost_usd` on a real rewrite with
      the final flag set; update §Reality-check with the observed numbers.
- [ ] Rolling buffer does not prefix-match a real trigger on the keystroke
      after completion.

## 11. Follow-ups / out of scope

- **Migrate off `claude` CLI to the direct Anthropic API**: SSE token
  streaming, a lean system prompt (~200 tokens vs ~30 000), ~100× cheaper,
  subsecond latency. This is the real version of this feature; the CLI MVP
  exists to validate the UX, not to ship at scale.
- Per-app scoping / registers (Idea 2).
- Typing-history mining / snippet suggestions (Idea 3).
- Tool-using agents with shell/http/MCP (Idea 4).
- Apple Foundation Models backend alongside the CLI.
- Ollama/MLX fallback.
- Response cache `(trigger, sha256(input)) → text` LRU to cut repeat
  latency and cost.
- Settings UI fields for authoring AI triggers.
- Single-gesture undo for long AI outputs.
- Configurable placeholder string.
