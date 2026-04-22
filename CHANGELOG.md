# Changelog

All notable changes to Sokki are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Releases follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased


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
### Added
- Sparkle-based automatic updates. Future releases will auto-install with signature verification; this one requires a manual download.
- "Check for Updates…" menu-bar item and Settings toggle for automatic update checks.
