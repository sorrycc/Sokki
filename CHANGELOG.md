# Changelog

All notable changes to Sokki are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Releases follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased
- **Match list** search field now supports full keyboard navigation: arrow keys move selection while typing, Return opens the selected match, Esc clears the query (or defocuses to the list when empty), and the selected row auto-scrolls into view.


## 0.0.11 - 2026-04-24
- Fix release problem


## 0.0.10 - 2026-04-24
- **AI settings** now explains upfront that AI features are powered by your locally-installed Claude Code CLI.
- Default log level is now `debug` instead of `error`, and routine AI / palette / Claude-binary messages no longer surface as errors — the log file stays useful without drowning in false alarms.
- **Match list** and **Match editor** now flag trigger conflicts inline: a warning icon and tooltip mark duplicates (red) and triggers shadowed by longer ones (orange), and the editor warns live as you type a trigger that collides with or extends another match.
- **AI settings** gains a "Test connection" button that runs a round-trip `claude` call and shows the resolved binary, model, reply, or a categorised failure reason; the same button cancels an in-flight test, and closing the Settings window aborts it cleanly. Default model for fresh configs is now `sonnet` (existing `model` values are respected).
- **Settings** now supports keyboard shortcuts: ⌘1–⌘4 switch tabs, ⌘F focuses the match list search field, ⌘N adds a new match, and ⌘, opens Settings directly from the command palette.


## 0.0.9 - 2026-04-23
- AI matches now use a top-level `prompt:` template with `{{selection}}` / `{{clipboard}}` placeholders (and `{{s|c}}` fallback) instead of the old `ai:` block. The **Match editor** gained an "AI prompt" toggle to author them without touching YAML; legacy `ai:` entries are flagged on load with a migration hint.
- After granting Accessibility permission, Sokki now prompts with a "Quit & Reopen" alert so the event tap can start without a manual restart. Also added a permanent **Quit & Reopen** entry to the menu bar for other cases (permission revoked while running, stale TCC, etc).
- Match editor's **Source File** field now supports creating a new file inline: click the "+" button next to the picker, type a filename (`.yml` is appended automatically), and the new YAML file is written under `~/.config/sokki/match/` on Save. Typing an existing name (case-insensitive, including cross-extension `foo.yml` ≡ `foo.yaml`) silently selects it.
- Fix: app icon no longer shows a white border in the Dock / Finder. The bundled `AppIcon.icns` had opaque white padding around the squircle; regenerated with transparent corners.
- **Settings match list** now shows a blue `AI · <input>` capsule next to AI-backed matches, matching the command palette's badge.
- Trimmed the starter **match/default.yml** seeded on first launch: removed the two AI-rewrite examples so fresh installs don't reference the `claude` CLI before users opt in.
- Trigger matching is now resilient to in-flight corrections: fix a typo with ⌫ and keep typing, and the expansion still fires when the visible text ends with a trigger. ⌥⌫ / ⌘⌫ / arrow keys / Home / End / PageUp / PageDown safely invalidate the in-progress buffer.
- **AI prompts** with chained placeholders like `{{s|c}}` now fall back to the clipboard when no text is selected, instead of aborting with a "no selection" error. Prompts that require selection (`{{s}}` alone) still fail loudly as before.
- New **AI** tab in Settings for configuring the model, Claude binary path, timeouts, and an animated inline loading indicator while a prompt runs (choose Static, Braille spinner, or Block sweep).


## 0.0.8 - 2026-04-22
- New **Privacy** tab in Settings. Opt-out anonymous crash-report upload and daily DAU ping (both default ON) to a self-hosted worker at `telemetry.sorrycc.dev`. No personal data, keystrokes, match text, or clipboard contents are sent.


## 0.0.7 - 2026-04-22
- Auto-update feed moved to the custom domain `download.sorrycc.dev`, served by a shared Cloudflare Worker (repo: `sorrycc/downloads-worker`). The original `r2.dev` host stays enabled as a fallback for existing installs.
- Fix: "Check for Updates…" from the menu bar now brings Sokki to the foreground so the Sparkle update window is key and frontmost (no extra click needed).


## 0.0.6 - 2026-04-22
- Added AI rewrite triggers: matches can define an `ai:` block that runs the local `claude` CLI and replaces the trigger or selection with the result. Invoke by typing the trigger or by committing from the palette (recommended for `input: selection`).


## 0.0.5 - 2026-04-20
- Show source file per row and in the editor; filter by file; search triggers and replacements; delete selection with the Delete or Backspace key.
- Added a Spotlight-style trigger palette for fuzzy-searching and running matches from a floating input.
- Default the toggle and palette shortcuts to none so new users don't trigger them accidentally.

## 0.0.4 - 2026-04-19
- Fixed settings window activation on macOS Tahoe.
- Cleaned up YAML serialization for matches and config.

## 0.0.3 - 2026-04-19
- Redesigned the menu-bar icon as a keycap with a tab glyph for better recognizability.
- Keep the app alive when the settings window is closed.

## 0.0.2 - 2026-04-19
- Sparkle-based automatic updates. Future releases will auto-install with signature verification; this one requires a manual download.
- "Check for Updates…" menu-bar item and Settings toggle for automatic update checks.
