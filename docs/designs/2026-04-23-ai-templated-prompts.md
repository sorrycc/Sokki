# AI templated prompts + grouped AI config

Date: 2026-04-23
Status: proposed

## Goal

Replace today's two-field `ai: { prompt, input }` per-match shape with a single
`prompt:` that embeds the inputs as template placeholders, and group all
AI-related app settings under a single `ai:` table in `default.yml`. No
backward compatibility with the old shape.

## 1 · YAML schema

### Matches (`~/.config/sokki/match/*.yml`)

```yaml
# plain text match
- trigger: :hello
  replace: Hi there!

# AI match — `prompt:` at top level, placeholders in {{...}}
- trigger: ";;en"
  prompt: "Translate to English: {{selection}}"

- trigger: ";;pro"
  prompt: "Rewrite professionally:\n{{s|c}}"   # selection, fallback clipboard

- trigger: ";;tldr"
  prompt: "Summarize in 3 bullets:\n{{clipboard}}"
```

Rules

- A match is an AI match iff `prompt:` is present.
- `prompt` and `replace` are mutually exclusive. A match with both set is a
  **load error** (fail loud; almost always user confusion).
- `prompt: ""` (empty string) is a **load error**.
- The old `ai: { prompt, input }` shape produces a load error. No compat —
  the match file's `ai:` key is explicitly rejected.
- Unknown template sources (e.g. `{{foo}}`) are load errors (§2 below).

### App config (`~/.config/sokki/default.yml`)

```yaml
ai:
  binary: /opt/homebrew/bin/claude           # string, optional
  args: ["--tools", "", "--max-turns", "1"]  # string[], optional
  model: haiku                               # optional, default "haiku"
  timeout_seconds: 30
  selection_capture_timeout_ms: 400
```

- Only the AI keys collapse under `ai:`. All other top-level keys (`enable`,
  `paste_shortcut`, `toggle_shortcut`, etc.) stay flat.
- Old flat keys (`ai_binary_path`, `ai_model`, `ai_timeout_seconds`,
  `ai_selection_capture_timeout_ms`) are rejected at load with a clear
  error message. See §3 for how (since today's `AppConfig.init(from:)` uses
  `try?` everywhere and silently swallows unknown keys, we have to add an
  explicit legacy-key check).

## 2 · Template language

Grammar

```
placeholder := "{{" source ( "|" source )* "}}"
source      := "selection" | "s" | "clipboard" | "c"
```

- Sources are case-sensitive.
- **Whitespace around the source names AND around `|` is trimmed.**
  `{{ selection }}`, `{{ s | c }}`, and `{{selection|clipboard}}` all parse
  identically.
- Aliases: `s` = `selection`, `c` = `clipboard`.
- **No escape syntax for literal `{{`.** If a prompt legitimately needs a
  literal `{{`, that's a future change — call it out explicitly so we don't
  add a conflicting escape sequence later.
- **Substitution is single-pass and non-recursive.** `render` replaces each
  placeholder range exactly once. If the resolved value itself contains
  `{{clipboard}}`, it is treated as literal text and **not** re-expanded. The
  implementation must build the output in one left-to-right pass over the
  placeholder ranges recorded at parse time, not a naive
  `str.replacingOccurrences` loop.

Resolution

- Per placeholder, scan the chain left-to-right and pick the **first non-empty
  value**.
- Empty string counts as "empty" for the purposes of fallback.
- **A prompt with zero placeholders is legal.** `prompt: "Tell me a joke."`
  renders to itself, triggers no Cmd-C capture, no clipboard read, and sends
  straight to the AI call. Both `needsSelection` and `needsClipboard` are
  false; `render` returns the raw prompt unchanged.

Abort conditions (no API call; HUD shows `aiFailed`)

- Any placeholder's entire chain resolves to empty → HUD note
  `"no input for {{<chain>}}"` where `<chain>` is rendered verbatim from the
  parsed sources (e.g. `"no input for {{s|c}}"`, `"no input for
  {{selection}}"`). Never the ambiguous `{{…}}`.
- Selection is needed but Cmd-C capture timed out → HUD note `"no selection"`
  (existing behavior preserved).

Load-time validation (all produce load errors, see §3 on surfacing)

- `{{foo}}` — unknown source.
- `{{}}` — empty chain.
- `{{|s}}`, `{{s|}}`, `{{s||c}}` — empty source in chain.
- Unclosed `{{` with no matching `}}`.

## 3 · Models & engine

### Error surfacing

Today `AppConfig.init(from:)` (Models/AppConfig.swift:40–56) and
`Match.init(from:)` (Models/Match.swift:27–37) both use `try?` liberally and
silently decay unknown/invalid data to defaults. That's incompatible with
"old `ai_binary_path` is a load error" and "`prompt: ""` is a load error."

Plan:

- Introduce a thin `ConfigLoadIssue` enum (or reuse whatever the existing
  config-load path surfaces — `ConfigLoader` already shows load errors in the
  HUD / settings UI; verify at implementation time and plug into that).
- `AppConfig` decoder: after the lenient field reads, explicitly check the
  raw container for any of the legacy flat keys
  (`ai_binary_path`, `ai_model`, `ai_timeout_seconds`,
  `ai_selection_capture_timeout_ms`) and throw a
  `ConfigLoadIssue.legacyKey(name, hint: "move under ai:")`.
- `Match` decoder: explicitly check for the legacy `ai` key and throw. Also
  throw on `prompt` + `replace` coexistence and on empty `prompt`.

The goal is "user changes `default.yml`, sees a clear message in the settings
UI or HUD about what to fix." Not a silent regression where AI stops working
and nobody can tell why.

**Legacy-key detection technique.** Since `CodingKeys` only lists known keys,
use a dynamic key type to inspect the raw container:

```swift
private struct AnyStringKey: CodingKey {
    var stringValue: String
    var intValue: Int? { nil }
    init(stringValue: String) { self.stringValue = stringValue }
    init?(intValue: Int) { nil }
}

let raw = try decoder.container(keyedBy: AnyStringKey.self)
for legacy in ["ai_binary_path", "ai_model", "ai_timeout_seconds",
               "ai_selection_capture_timeout_ms"] {
    if raw.contains(AnyStringKey(stringValue: legacy)) {
        throw ConfigLoadIssue.legacyKey(legacy, hint: "move under ai:")
    }
}
```

Same pattern for `Match` detecting the legacy `ai:` key.

### `Models/Match.swift`

- Delete `AIConfig` and `AIInput` types.
- Add `var prompt: String?` on `Match` (plain storage).
- Convenience: `var isAI: Bool { prompt != nil }`.
- Codable: encode `prompt` when non-nil; encode `replace` only when `prompt`
  is nil (no more "AI match with non-empty legacy `replace`" carve-out —
  we're rejecting that shape at load).

### `Models/AppConfig.swift` + `Models/AISettings.swift`

- Remove flat `aiBinaryPath`, `aiModel`, `aiTimeoutSeconds`,
  `aiSelectionCaptureTimeoutMs` from `AppConfig`.
- Add `var ai: AISettings = .init()`.
- **File layout.** `AISettings` lives in `Models/AISettings.swift`, replacing
  the deleted `Models/AIConfig.swift`. Matches the four-folder convention in
  `CLAUDE.md` and keeps `AppConfig.swift` focused on top-level app settings.
- New struct:

  ```swift
  struct AISettings: Codable, Sendable, Equatable {
      var binary: String? = nil
      var args: [String]? = nil
      var model: String = "haiku"
      var timeoutSeconds: Int = 30
      var selectionCaptureTimeoutMs: Int = 400

      enum CodingKeys: String, CodingKey {
          case binary, args, model
          case timeoutSeconds = "timeout_seconds"
          case selectionCaptureTimeoutMs = "selection_capture_timeout_ms"
      }
  }
  ```

- `ai.model` default stays `"haiku"` (always injected as `--model haiku`
  when user `args` doesn't already specify `--model`).
- `ai.binary` tilde expansion: at decode time, if the string starts with `~`,
  expand via `(path as NSString).expandingTildeInPath` before handing to
  `ClaudeBinaryResolver`. Today's resolver does not expand tildes; verify at
  implementation time and either fix upstream or expand here. Users will
  write `~/bin/claude`.
- `ai.args` validation at decode time (rejects with `ConfigLoadIssue`):
  - Any element containing `=` in a flag-like position (e.g.
    `--output-format=json`) is rejected with hint "use separated form:
    `--output-format`, `json`". This is structural and
    version-independent. Keeps AIRewriter arg-manipulation logic trivial
    and unambiguous.
  - **No dangling-flag check.** Flag-only args like `--verbose`,
    `--no-session-persistence`, or any future `--no-foo` are legitimate
    and an allowlist would drift with Claude CLI versions. Let the CLI
    reject genuinely malformed invocations at runtime — we surface
    non-zero exit through the existing `nonZeroExit` HUD path.
    `forceValue`'s scanner tolerates "flag without value" by simply not
    finding a match to overwrite.
  - Non-`[String]` shapes fall out naturally via `Codable` decode error.

### `Engine/PromptRenderer.swift` (new)

Small, pure module:

```swift
enum PromptSource { case selection, clipboard }

struct PromptPlaceholder {
    let chain: [PromptSource]          // e.g. [.selection, .clipboard]
    let raw: Substring                 // full "{{...}}" token, for replacement
}

struct PromptTemplate {
    let raw: String
    let placeholders: [PromptPlaceholder]
    var needsSelection: Bool { placeholders.contains { $0.chain.contains(.selection) } }
    var needsClipboard: Bool { placeholders.contains { $0.chain.contains(.clipboard) } }

    static func parse(_ s: String) throws -> PromptTemplate
    func render(selection: String?, clipboard: String?) -> String?  // nil = abort
}
```

- `parse` walks the string, scanning for `{{...}}`, splitting on `|`, trimming
  whitespace, mapping aliases to sources, and throwing on unknown / empty /
  unclosed tokens.
- `render` replaces each placeholder with the first non-empty source value in
  its chain; returns `nil` if any placeholder chain is all-empty.

**Storage / lifecycle.** `PromptTemplate` is *not* Codable and must not live on
`Match` (which is `Codable, Equatable, Hashable` plain data). Instead,
`MatchStore` keeps a side map `[UUID: PromptTemplate]` keyed by `Match.id`,
populated whenever matches are (re)loaded from disk and cleared/rebuilt on
reload. The AI path looks up the template by `match.id`.

**The side-map update MUST be synchronous with the match-array swap**, inside
the same `@MainActor`-isolated call that mutates `MatchStore.matches`. An
expansion firing between "matches swapped" and "templates swapped" would miss
the lookup and log a spurious error. Build the new `[UUID: PromptTemplate]`
first, then assign matches and templates together.

**Testing.** Sokki has no test target today (per `CLAUDE.md`). This is the
first genuinely pure, unit-testable module in the codebase. Add a minimal
`SokkiTests` target in `Package.swift` covering:

- `PromptRenderer` parse/render edge cases: aliases, fallback chains,
  whitespace, zero-placeholder prompts, all error variants
  (unknown/empty/unclosed/empty-chain source).
- **Arg canonicalisation.** Extract the force-floor + convenience logic out
  of `AIRewriter.run` into a pure static function
  `AIRewriter.canonicalizeArgs(userArgs: [String]?, model: String, systemPromptAppend: String) -> (args: [String], logs: [String])`.
  Positional-array scanning is exactly the code that rots silently — cover
  the insert / override / no-op triad for each of `--print`,
  `--output-format`, `--append-system-prompt`, the `--model` convenience,
  and the nil/empty-`userArgs` default path. Returning `logs` alongside
  `args` lets tests assert log shape without stubbing the Logger.

Strictly scoped — don't expand the target beyond these two modules.

### `Engine/KeystrokeMonitor.swift` (AI path)

Current flow (roughly lines 180–310):

1. Check `ai != nil` → enter AI path.
2. Check `isAIActive` guard.
3. Switch on `ai.input` → Cmd-C capture / read clipboard / no input.
4. Build `AIRewriter.Request(prompt, input, model, timeout)`.

New flow:

1. Check `match.prompt != nil` → enter AI path. Look up the `PromptTemplate`
   in `MatchStore` by `match.id`.
2. Same `isAIActive` guard, same suppression, same erase-trigger.
3. Capture **only** what the template needs, **in this order**:
   - If `template.needsClipboard` → read the pasteboard string **first**.
     Snapshot it into a local. This is critical: the Cmd-C capture below
     overwrites the system pasteboard, so reading it after selection capture
     would return the user's own selection, not the intended clipboard.
   - If `template.needsSelection` → Cmd-C capture (existing helper). Note
     that existing `preserve_clipboard` behavior already save-and-restores
     the pasteboard around capture; our pre-read is independent of that and
     runs first regardless.
4. `template.render(selection:, clipboard:)` using the snapshotted values.
   - `nil` → HUD `aiFailed` with note `"no input for {{<chain>}}"` naming
     the offending placeholder, re-inject the typed trigger (typed path) or
     no-op (palette path), return.
5. Inject placeholder (`▎ thinking…`), dispatch `AIRewriter.Request(prompt:
   rendered, model:, timeout:)`.

### `Services/AIRewriter.swift`

- `Request` drops the `input` field. stdin is simply the rendered prompt,
  UTF-8 encoded. No more `payload += "\n\n" + input` concat.
- Argument assembly:

  ```
  DEFAULT_ARGS = [
      "--print",
      "--tools", "",
      "--output-format", "json",
      "--max-turns", "1",
      "--no-session-persistence",
      "--append-system-prompt", systemPromptAppend,
  ]

  base = config.ai.args ?? DEFAULT_ARGS

  # FORCE floor: user cannot break our output contract. These overwrite any
  # conflicting value the user passed, and log a warning when overwritten.
  forceFlag(base, "--print")                              # flag-only
  forceValue(base, "--output-format", "json")             # must be json
  forceValue(base, "--append-system-prompt", systemPromptAppend)
      # Without this, Claude returns chatty prose like
      # "Sure, here's the translation: ..." which we would inject verbatim
      # into the user's text field. That's a worse failure than --output-format
      # text because it LOOKS like success. Category-equivalent to the JSON
      # parsing contract; therefore part of the floor, not a default.

  # CONVENIENCE: if user args doesn't already specify --model, inject it
  # from config.ai.model. Default "haiku" is always set, so this is
  # effectively "--model <model>" unless the user opted out via args.
  if "--model" not in base:
      base += ["--model", config.ai.model]

  args = base
  ```

  - `forceFlag` / `forceValue` overwrite rather than just "ensure present."
    This is the actual floor — anything milder lets a user silently break
    JSON parsing or the output contract.
  - Because `ai.args` is canonicalised at load time (no `=`-form, no
    dangling flags — see `AISettings` decode), `forceValue` can assume a
    `[... , "--foo", "value", ...]` shape: scan for the flag, replace
    `index+1`. Simple, no regex, no value-or-no-value ambiguity.
  - Log (at error level, matching existing AIRewriter log style) in three
    distinct shapes so diagnostic output stays unambiguous:
    - absent, we insert: `[AI] inserted --output-format=json (user did not set)`
    - present, different value, we overwrite: `[AI] overrode --output-format=json (user had: text)`
    - present, exact match: **no log** (no-op).
    Same triad applies to `--print` and `--append-system-prompt`. For
    `--append-system-prompt` the "user had" value is truncated to the first
    40 chars to keep log lines readable.

- `binary` resolution: `ClaudeBinaryResolver.resolve(explicitPath:
  config.ai.binary)` — unchanged semantics, just reading from the new key
  (now tilde-expanded at decode time, see `AISettings` above).

## 4 · UI impact

- `PaletteView` (Sources/Sokki/Views/PaletteView.swift:103, 120, 125): drop
  the "AI · \(input.rawValue)" subtitle. Show `AI` badge + first line of the
  prompt with placeholders rendered as a muted glyph (e.g. `⟨selection⟩`,
  `⟨clipboard⟩`, `⟨selection | clipboard⟩`) rather than raw `{{…}}`. Raw
  braces in a subtitle look like a bug.
- `MatchListView` (Sources/Sokki/Views/MatchListView.swift:317): same
  glyph-rendering treatment; drop the `input.rawValue` piece.
- `MatchEditorView`: the AI toggle switches between `replace` and `prompt`
  editing. Drop the `input:` segmented control (selection/clipboard/none). Add
  a small hint below the prompt field:
  > Use `{{selection}}` / `{{clipboard}}`, alias `s`/`c`, or fallback
  > `{{s|c}}`.

  **Validation on save.** When the user hits save with `prompt` non-empty,
  run it through `PromptTemplate.parse`. On throw, block the save and show
  the parse error inline below the prompt field (e.g. *"Unknown source
  `foo`"*, *"Unclosed `{{`"*). This keeps the UI path at the same rigor as
  the YAML load path — there's no way to produce an invalid file through
  the editor. Reuses the exact same parser; no separate UI-side validation
  logic.
- Surface load errors (§3) in whatever path `ConfigLoader` already uses to
  show "match file failed to load" — same entry point, new error variants.

## 5 · Failure modes (summary)

| Situation                                     | Behavior                                       |
|-----------------------------------------------|------------------------------------------------|
| `prompt` has unknown / empty / unclosed `{{}}` | Load error; surfaced via existing config-error UI |
| `prompt: ""`                                  | Load error                                     |
| Match has both `prompt` and `replace`         | Load error                                     |
| Match file uses legacy `ai: { prompt, input }` | Load error with "move to top-level `prompt:`" hint |
| `default.yml` uses legacy flat `ai_*` keys    | Load error with "move under `ai:`" hint        |
| Cmd-C capture timed out                       | HUD `aiFailed` note `"no selection"` (unchanged) |
| All sources in a placeholder chain empty      | HUD `aiFailed` note `"no input for {{<chain>}}"`, chain rendered verbatim |
| `ai.binary` missing / unresolved              | HUD `aiFailed`, reason `binaryMissing` (unchanged) |
| `ai.args` uses `=`-form or dangling flag      | Load error at decode; won't start              |
| User `ai.args` sets `--output-format text`    | Forced back to `json`; warning logged          |
| User `ai.args` omits `--print`                | Inserted; warning logged                       |
| User `ai.args` omits/overrides `--append-system-prompt` | Forced back to our value; warning logged |
| User `ai.args` includes `--model`             | `config.ai.model` is ignored (args wins)       |

## 6 · Migration (one-shot break)

- Bump nothing formally; just update the seeded `default.yml` and any docs.
- Users with existing `ai: { prompt, input }` entries or flat `ai_*` keys will
  see a clear load error on startup with a "move to X" hint. Tolerable
  because AI support is recent and the only shipping AI example was removed
  in `549f595`.
- Supersedes the `input:` section of
  `docs/designs/2026-04-21-ai-rewrite-trigger.md` (add a pointer note at the
  top of that doc).

## 7 · Known limitations (not addressing now)

- **No size cap on stdin payload.** A 10 MB clipboard or selection gets
  passed in full to `claude --print`. Same behavior as today. If this bites
  users we add a cap (and HUD warning) later.
- **YAML multiline caveats.** Inside a YAML double-quoted string,
  `"{{s|c}}"` is safe. Inside a folded `>` block scalar, also safe
  (`|` is literal text there, not a YAML control char outside the scalar
  header position). Inside a flow-style mapping without quotes, the `|`
  may look ambiguous but YAML only treats `|` as a control character at
  the start of a scalar. No action needed, but worth mentioning to users in
  docs.

## 8 · Out of scope

- Per-match model / timeout / args overrides. Easy to add later on top of the
  nested `ai:` table (just make the per-match AI settings a subset of
  `AISettings`).
- Additional template sources (`{{date}}`, `{{cursor}}`, `{{env.FOO}}`). The
  parser is structured to accept new source variants, but nothing is wired.
- Prompt-level variables (`{{var.name}}`) integrating with the existing
  `Variable` system in `Models/Variable.swift`. Out of scope here.
- Escape syntax for literal `{{` in a prompt. If needed, design the escape
  carefully at that point — don't let a future change conflict with today's
  grammar.
- **Configurable `ai.system_prompt_append`.** The "output only the transformed
  text" append-string stays locked down as part of the output contract
  (forced floor in AIRewriter). Exposing it as a config key would let users
  disable it and get chatty prose injected verbatim — the same footgun that
  drove us to force-floor it in the first place. If a future need arises
  (e.g. per-match personas), revisit — but as a per-match key, not a
  global one.
