# Changelog

All notable changes to Sokki are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Releases follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased
- After granting Accessibility permission, Sokki now prompts with a "Quit & Reopen" alert so the event tap can start without a manual restart. Also added a permanent **Quit & Reopen** entry to the menu bar for other cases (permission revoked while running, stale TCC, etc).
- Match editor's **Source File** field now supports creating a new file inline: click the "+" button next to the picker, type a filename (`.yml` is appended automatically), and the new YAML file is written under `~/.config/sokki/match/` on Save. Typing an existing name (case-insensitive, including cross-extension `foo.yml` ≡ `foo.yaml`) silently selects it.
- Fix: app icon no longer shows a white border in the Dock / Finder. The bundled `AppIcon.icns` had opaque white padding around the squircle; regenerated with transparent corners.
- **Settings match list** now shows a blue `AI · <input>` capsule next to AI-backed matches, matching the command palette's badge.


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
