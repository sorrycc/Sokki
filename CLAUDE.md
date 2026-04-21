# Sokki

macOS 14+ menu bar text expander written in Swift 6 (strict concurrency, `@MainActor`-heavy). arm64 only.

## Layout

- `Sources/Sokki/` — `@main` app (`SokkiApp.swift`), `AppDelegate.swift`, status item + HUD panel controllers
  - `Engine/` — keystroke capture and expansion: `KeystrokeMonitor`, `TapContext`, `TrieMatcher`, `RollingBuffer`, `TextInjector`, `VariableRenderer`, `ExpansionUndoManager`, `SecureInputMonitor`
  - `Models/` — plain data: `Match`, `MatchStore`, `Variable`, `AppConfig`, `HUDEvent`, `HUDStore`
  - `Services/` — lifecycle + infra: `Bootstrapper`, `ConfigLoader`, `ClipboardManager`, `Logger`, `PaletteController`, `RecencyStore`, `UpdaterService`
  - `Views/` — SwiftUI: `SettingsView`, `MatchListView`, `MatchEditorView`, `PaletteView`, `HUDView`
- `Scripts/` — `package_app.sh`, `release.ts`, `upload_r2.ts`, `sparkle.config`
- `docs/designs/` — dated design specs (`YYYY-MM-DD-<slug>.md`); `docs/ideas/` is exploration-only, not authoritative
- `Resources/` — bundled assets
- `Package.swift`, `package.json` (Bun scripts), `VERSION`, `CHANGELOG.md`

## Build & run

```bash
swift build                  # debug
swift build -c release
swift run                    # or: bun dev
./Scripts/package_app.sh     # bundle + sign Sokki.app
```

No test target yet — don't invent `swift test` invocations.

## Release

`bun release patch|minor|major [--push] [--upload]` drives the pipeline: build → notarize → staple → zip/dmg → (optionally) push tag → (optionally) upload to R2 + update `appcast.xml`. Takes 3–5 min; notarization is the bottleneck. One-time notary + Sparkle/R2/keychain setup lives in `README.md` — don't re-run it.

`.env` holds R2 credentials and is gitignored. Never commit it or print its contents.

## Conventions

- Swift 6 language mode — respect `Sendable`, actor isolation, and `MainActor.assumeIsolated` patterns already in the code
- Prefer editing existing files over adding new ones; keep new types inside the existing four-folder module split
- Design docs are large (20–50 KB). Before reading one in full, check `docs/designs/` filenames and skim for the relevant date/slug
- Commit style: `type: short summary` (see `git log`: `fix:`, `docs:`, `release:`)

## Search scope

Heavy artifact dirs that are gitignored but still on disk — exclude from Glob/Grep unless explicitly needed:

- `.build/` (~800 MB SwiftPM build cache)
- `build/` (release artifacts: zip/dmg/sha256)
- `Sokki.app/` (packaged bundle)
- `tmp/` (~50 MB of PNG mockups)

Default to searching `Sources/`, `Scripts/`, `docs/`, and top-level config files.
