# Competitive Differentiation

Sokki competes against espanso (cross-platform OSS), TextExpander (subscription), Raycast Snippets (bundled), aText, Rocket Typist, and Apple's built-in Text Replacements. These ideas exploit what the incumbents *structurally cannot* match — either because they're cross-platform, subscription-locked, or platform-neutered.

## Idea 1: On-Device LLM Rewrite Triggers

**Pitch:** Text expansion stops being "paste a template" and becomes "rewrite what I just typed." Type `:prof` after a sloppy Slack sentence and Sokki feeds the last N chars (or the entire focused text field) to Apple's on-device Foundation Models, then replaces it with a tone-shifted version — professional, concise, translated, de-snarked — all offline, all free, all private.

**How it works:**
- Use the `FoundationModels` framework (macOS 15.1+ Apple Intelligence) for fully on-device inference; fall back to MLX + a quantized Qwen/Gemma model on older hardware.
- New match type in YAML: `mode: rewrite` with `prompt: "Rewrite professionally, keep meaning"` and `scope: last_sentence | selection | paragraph`.
- Pull source text via `AXUIElement` (kAXSelectedTextAttribute / kAXFocusedUIElementAttribute) — no clipboard dance, no permissions beyond what Sokki already has.
- Stream tokens into a transient overlay window (NSPanel, floating) so the user sees the rewrite forming; commit on Enter, cancel on Esc.
- Cache prompt+model state; first token in <300ms on M-series.

**Why it's accretive — competitors structurally cannot copy:**
- **espanso** is cross-platform Rust; they will never bundle a 2GB model per OS, and Linux/Windows have no FoundationModels equivalent. Their architecture is "string in, string out."
- **TextExpander** is a subscription business — adding free on-device AI kneecaps their upsell path and they've already committed to cloud AI as a paid tier. They can't undercut their own SKU.
- **Raycast Snippets** is bundled inside Raycast, which routes AI through their paid cloud proxy; free on-device rewrite would cannibalize Raycast Pro's AI quota.
- **Apple Text Replacements / aText / Rocket Typist** — no AI team, no inference runtime, not on the roadmap.

**Risk / cost:** FoundationModels is macOS 15.1+ and Apple Silicon only — need to gate cleanly or you'll strand your macOS 14 users. Fix: ship both paths. AX read of focused field fails in Electron apps' secure inputs; need graceful clipboard fallback. ~1 week of engineering plus prompt-craft iteration.

---

## Idea 2: Focus-Mode & App-Context Scoping

**Pitch:** A trigger that expands to your customer's name in Mail but your wife's name in iMessage — and disables itself entirely during a "Deep Work" Focus. Every other expander forces you to memorize distinct triggers per context; Sokki lets one trigger be polymorphic based on macOS's native signals.

**How it works:**
- Extend YAML with `when:` clauses: `when: { app: "com.apple.mail", focus: "Work", url_host: "github.com", stage_manager_set: "Writing" }`.
- Read frontmost app via `NSWorkspace.shared.frontmostApplication.bundleIdentifier` on every tap (already free).
- Subscribe to `INFocusStatusCenter` (Focus filter extension) for current Focus mode.
- For browsers, poke AXWebArea to grab the URL host via Accessibility — scope snippets per-site (e.g. `:addr` expands to shipping address only on shopify.com, work address on jira).
- Ship a simple UI predicate builder; evaluation is cheap (dictionary lookup per keystroke).

**Why it's accretive — competitors structurally cannot copy:**
- **espanso** has app-scoping but no Focus, no Stage Manager, no URL-scoping via AX — it's cross-platform, so it only exposes lowest-common-denominator signals.
- **TextExpander** has app filters but not Focus filters — Focus requires entitlements and a Focus Filter extension, which requires the Mac App Store distribution path they haven't prioritized.
- **Raycast** won't do per-URL scoping because Raycast Snippets is a dumb textarea inside their extension — they don't run a system-wide AX observer.
- **Apple Text Replacements** has zero scoping. Ever.

**Risk / cost:** Focus status requires an entitlement from Apple; approval is routine but non-instant. Getting URL from arbitrary browsers via AX is fragile (Chrome vs. Safari vs. Arc differ). Ship with a known-good list + graceful fallback. ~3–4 days including entitlement paperwork.

---

## Idea 3: Shortcuts & AppleScript As Expansions

**Pitch:** A trigger doesn't have to emit a string — it can run a Shortcut, an AppleScript, a shell command, or an MCP tool, and inject the *result*. `:weather` literally calls the Weather Shortcut. `:jira` runs a shell curl to your Jira and injects the ticket summary. This turns Sokki from an expander into a keyboard-level automation rail.

**How it works:**
- New YAML field: `run: { shortcut: "Summarize Selection" }` or `run: { applescript: "..." }` or `run: { shell: "gh pr view --json title -q .title" }`.
- Shortcuts invocation via `x-callback-url` (`shortcuts://run-shortcut?name=…&input=…`) or the `shortcuts` CLI; pipe stdout as replacement.
- AppleScript via `NSAppleScript` — preserve entitlements, run off-main, timeout 2s.
- Shell via `Process` inside a sandboxed helper; env from user's login shell; always confirm-on-first-run per script (security UX).
- Pass the current selection / clipboard / surrounding paragraph as stdin — makes scripts composable.

**Why it's accretive — competitors structurally cannot copy:**
- **espanso** has shell execution but zero Shortcuts integration — Shortcuts is macOS-only and Apple-proprietary. They'd never ship it.
- **TextExpander** removed arbitrary shell execution years ago for App Store / Gatekeeper reasons and refuses to add it back — it's a liability for their enterprise posture.
- **Raycast** has Shortcuts *but* inside their launcher — not from your keyboard mid-sentence in any app. Sokki's wedge is "keystroke-triggered inline inside the text field you're already typing in."
- **Apple Text Replacements** — lol.

**Risk / cost:** Shell execution is a massive security foot-gun; mitigate with per-script approval prompts, code-signing the script body hash in YAML, and a kill switch. Sandboxing constraints if you ever go Mac App Store — probably means you stay Developer-ID-signed. 1 week; the security UX is the hard part, not the plumbing.

---

## Idea 4: Time-Travel / Expansion Timeline

**Pitch:** Every expansion Sokki ever fired is recorded locally, searchable, and re-injectable. "What did I paste into that customer email last Tuesday?" becomes a Cmd-Shift-H lookup. It's a clipboard manager + expander + personal keystroke corpus — and because it's all on-device, it's actually GDPR-safe unlike cloud-synced TextExpander.

**How it works:**
- On every successful expansion, write `{trigger, rendered_text, app_bundle, timestamp, focused_field_role}` to a local SQLite (or `Data`-encoded SwiftData store).
- Spotlight integration: index entries via `CSSearchableIndex` so Cmd-Space finds them system-wide.
- Menu bar "History" panel (SwiftUI) with fuzzy search, filter by app/date; click to re-inject at current cursor.
- Optional: derive *suggested new triggers* from repeated manual typing patterns — watch the buffer for strings typed >5 times in 7 days, surface "Want to make `regards, chencheng` a snippet?"
- Encrypted at rest using `SecKeyCreateRandomKey` + Keychain-stored symmetric key; never leaves device.

**Why it's accretive — competitors structurally cannot copy:**
- **TextExpander** sells history/sync as their paid-tier moat — giving it away free and *local* (better privacy story) directly attacks their pricing page.
- **espanso** has no UI layer to build this in — they're CLI-first and their users actively prefer no database.
- **Raycast** has Clipboard History but not expansion history, and their sync is cloud — you can't ship "local-only" inside a cloud product without confusing their positioning.
- **Apple Text Replacements** doesn't log anything.
- Pattern-mined trigger suggestions require a 24/7 keystroke buffer — only a permanently-running native tap can do this at acceptable battery cost.

**Risk / cost:** Keystroke corpus is a juicy target; must encrypt, must offer "clear history" front and center, must exclude secure-input sessions. Disk growth: cap at 10k entries or 30 days by default. ~4–5 days including Spotlight integration.

---

**Top pick:** Idea 1 (on-device LLM rewrite). It's the only one that actually makes a TextExpander power user uninstall. The others are great accretions; LLM rewrite is a *category shift* from "expander" to "intelligent typing assistant" — and only a native macOS-only app with Foundation Models access can give it away for free.
