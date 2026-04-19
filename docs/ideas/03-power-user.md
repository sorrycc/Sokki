# Power-User Automation, Scripting, and Extensibility

## Idea 1: Shell-Script Snippets with Stdin Piping

**Pitch:** Let any trigger expand to the stdout of an arbitrary shell/Python/JS command, with the rolling buffer's capture groups, clipboard, and frontmost app piped in as env vars or stdin. This single change turns Sokki from a string replacer into a scriptable macOS automation surface — espanso's killer feature that TextExpander still struggles with after 20 years.

**How it works:**
- New YAML field `exec:` (mutually exclusive with `replace:`) — string command or array (argv) run via `Process` with a 2s timeout and sandboxed env.
- Stdin gets a JSON blob: `{trigger, captures, clipboard, frontmost_app, frontmost_bundle_id, selection, date}`. Env mirrors it as `SOKKI_*` for bash one-liners.
- Stdout is fed right back into `VariableRenderer` so scripts can emit `{{clipboard}}` too — recursive composition for free.
- Exit code non-zero → trigger is re-typed verbatim (safe failure), error logged to `sokki.log` with the captured stderr.
- Ship `shell: /bin/zsh -lc` as default so users get their PATH/nvm/pyenv without weird surprises.

**Why it's accretive:** Every existing variable (`{{date}}`, `{{clipboard}}`) becomes a degenerate case of `exec:`. It unlocks `:weather`, `:gpt <selection>`, `:tr <clipboard>`, `:pw` (1Password CLI), `:jira-<ticket>` — entire categories Sokki can never ship first-party. Zero lock-in, trivially shareable as gists.

**Risk / cost:** Security surface (arbitrary exec). Mitigate with: explicit opt-in per-match (`exec: true` requires a top-level `allow_exec: true` in global config), a one-time consent dialog showing the command, and a default timeout. ~300 LOC, one week.

---

## Idea 2: Form Snippets with Live Inline Editing

**Pitch:** TextExpander-style fill-ins, but the form renders *in place at the cursor* using overlay-window trickery, not a modal popup. User types `:email`, an inline chip appears over the text field with `[to:____] [subject:____] [body:____]`, tab through, hit Enter, real text gets injected. It's what fill-ins should have been.

**How it works:**
- New YAML syntax: `{{field:name|default|type=choice:a,b,c}}` inside `replace:`. Supports `text`, `choice`, `date`, `multiline`, `secret`.
- On match, VariableRenderer detects fields → instead of injecting, spawn a borderless `NSPanel` anchored to the caret rect (via `AXUIElementCopyAttributeValue(kAXFocusedUIElementAttribute)` + `kAXBoundsAttribute`).
- Panel is a compact SwiftUI form with live Tab/Shift-Tab/Enter/Esc. Esc cancels and retypes the trigger.
- Fields can reference earlier fields: `{{field:greeting|Hi {{field:name}}}}` — enables cascading templates.
- Bonus: `cursor:` special marker `{{|}}` drops the caret there after injection (solves the #1 feature request for every expander).

**Why it's accretive:** Forms are the feature that turns expanders from hobbyist toys into daily-driver tools for support, sales, and legal. Doing it *inline at the caret* is a visibly better UX than TextExpander/aText/espanso — immediate demo magic and great viral screencast material.

**Risk / cost:** Caret-rect detection is unreliable in some Electron/Chromium apps (AX tree incomplete). Fallback: center-screen panel. ~2 weeks including choice widgets. Focus-restoration bugs are the real cost.

---

## Idea 3: Regex Triggers with Capture Groups

**Pitch:** Move from literal prefix triggers to regex triggers with named captures, addressable in `replace:` as `{{$1}}` or `{{name}}`. Same trie fast path for literals; regex-flagged matches fall through to a small compiled-regex list evaluated on the tail of the rolling buffer. This is the single highest-leverage matcher change — one feature, a hundred new use cases.

**How it works:**
- YAML: `trigger: "/;ph (\\d{3})-(\\d{4})/"` or the ergonomic form `trigger: ";ph {digits:\\d{7}}"` sugar-desugared to named captures.
- Add `RegexMatcher` alongside `TrieMatcher`; engine runs trie first (O(1)), then regex pool (bounded to ~50 patterns, anchored-to-end, compiled once, re-matched only when the last char could plausibly close a pattern — cheap heuristic).
- Replacement supports `{{$1}}`, `{{$digits}}`, and scripting: combine with Idea 1 → `exec: "units {{$1}} to meters"`.
- Add `app:` filter per match (`app: com.apple.Safari` or `!app: com.apple.Terminal`) — scope triggers so `;git` expands one thing in Slack, another in VS Code. Cheap to implement (check frontmost bundle ID in tap callback).
- Add `word_boundary: true` so `:ha` doesn't fire mid-word inside "shah" — fixes a real annoyance.

**Why it's accretive:** Regex + app-scope + capture groups are the three most-requested features across every expander's issue tracker. Combined they create a DSL people will build configs around. App-scope alone makes Sokki usable by developers who keep their Slack snippets out of their Terminal.

**Risk / cost:** Regex backtracking = potential hang. Enforce NSRegularExpression with `.useUnicodeWordBoundaries` and a per-match wall-clock budget (<1ms). ~500 LOC, 1 week. Needs good docs or users will footgun themselves.

---

## Idea 4: `sokki://` URL Scheme + Shortcuts.app Intents

**Pitch:** Register `sokki://expand?trigger=:sig` and expose Sokki as an App Intent provider, so Shortcuts, Alfred, Raycast, Siri, and any AppleScript/JXA can drive expansions programmatically. Sokki stops being *a* keyboard tool and becomes *the* macOS text-insertion primitive that other tools compose with.

**How it works:**
- Register `CFBundleURLTypes = sokki` in Info.plist; `application(_:open:)` parses `sokki://expand?trigger=...&arg1=...` and fires the injector directly (skipping the trie).
- Add App Intents: `ExpandSnippetIntent(trigger: String, arguments: [String: String])` and `RunSnippetByNameIntent`. These show up in Shortcuts.app and Spotlight automatically on macOS 14+.
- AppleScript dictionary: `tell application "Sokki" to expand trigger ":sig"` via sdef file — 50 lines, but it unlocks Keyboard Maestro, BetterTouchTool, Hammerspoon integration with zero work on their end.
- Command palette: global hotkey (default Cmd+Ctrl+Space) → fuzzy-search list of all triggers with preview → Enter expands at current caret. The "I forgot my trigger" escape hatch that every user hits.
- Clipboard-history variable: `{{clipboard:1}}`, `{{clipboard:2}}` reads from a 10-deep ring kept by `ClipboardManager` (observe `changeCount`). Turns Sokki into a lightweight clip history too.

**Why it's accretive:** Shortcuts/Siri/AppleScript integration is a one-time ~400 LOC investment that permanently makes Sokki a platform citizen. The command palette solves the discoverability problem for casual users while the URL scheme/intents serve power users — same feature, two audiences. Combined with Ideas 1-3, Sokki becomes genuinely scriptable in both directions (Sokki calls out, callers call in).

**Risk / cost:** App Intents require careful `Sendable`/actor plumbing in Swift 6 (you'll hit the main-actor wall). Caret detection for the command palette shares Idea 2's AX fragility. ~1 week for URL scheme + intents, ~3 days for palette, ~2 days for clipboard history.
