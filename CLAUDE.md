# Sokki

macOS 14+ menu bar text expander written in Go. Native UI drawn by MyGo (`github.com/egoist/mygo`, package `ui`),
no cgo: macOS APIs MyGo lacks are called through purego. arm64 only.

## Layout

- `cmd/sokki/` — the app (package main): `main.go` startup + wiring, `tray.go` menu bar item, `settings.go` +
  `matches.go` Settings, `editor.go` match editor, `palette.go` palette panel, `hud.go` Recent Activity panel,
  `app_test.go` headless UI tests
- `internal/macos/` — purego bindings: event tap, synthesized keys, Accessibility, secure input, workspace, pasteboard
- `internal/engine/` — trigger trie, buffer, undo, the tap's state machine (`Context`), variables/dates, fuzzy score
- `internal/expander/` — the running engine: tap handler, injector
- `internal/model/`, `internal/config/`, `internal/prefs/`, `internal/telemetry/`, `internal/logx/`
- `docs/designs/` — dated design specs (`YYYY-MM-DD-<slug>.md`); start with `2026-10-16-mygo-rewrite.md`
- `resources/` — bundled into the app (`icon.png`; CI writes the gitignored `telemetry.json`)
- `mygo.json` (name, identifier `com.sokki.Sokki`, version, updates), `CHANGELOG.md`, `scripts/`

## Build & run

```bash
go build ./...
go test ./...            # -short skips pasteboard tests
go vet ./... && go tool mygo vet
scripts/dev.sh           # bare binary rebuilt on change (not mygo dev: macOS 26 hides its menu bar icon), config in ~/.config/sokki-dev
go tool mygo build       # build/darwin-arm64/Sokki.app + .dmg, ad hoc signed
```

## Conventions

- State of `cmd/sokki` lives on MyGo's main thread; other goroutines hand work over with `app.post`. Views are
  functions of state (`ui.View`); don't keep `ui.Element`s past their frame.
- Windows hide instead of closing via `app.hideOnClose`, which lets `App.Quit` through.
- Never log typed characters before the secure-input check in `engine.Context.Feed`.
- Keep the `~/.config/sokki` file format compatible.
- Add a line for every user-facing change under `## Unreleased` in `CHANGELOG.md`.
- Commit style: `type: short summary` (`fix:`, `feat:`, `docs:`, `chore:`).

## Release

`scripts/bump.sh patch|minor|major|<version>` commits + tags; the pushed tag runs `.github/workflows/release.yml`,
which signs, notarizes and publishes to `sorrycc/Sokki`, the same repo. See `CONTRIBUTING.md` for the secrets.

## Search scope

Gitignored dirs to skip: `build/`, `.mygo/`, `.build/`, `Sokki.app/`, `tmp/`.
