# Sokki in Go with MyGo (0.1.0)

Sokki 0.1.0 is a rewrite of the Swift app (0.0.16) in Go, its windows drawn
by [MyGo](https://github.com/egoist/mygo)'s native UI, as
[Mayoi](https://github.com/sorrycc/Mayoi) is. It keeps every feature, the
configuration files and their format, and the bundle identifier; it replaces
SwiftUI, AppKit, Sparkle and the Bun release scripts.

This document replaces the design documents of the Swift app, which
described its code. It says how the Go app is put together and where it
differs.

## Layout

```
cmd/sokki/            the app: startup and wiring (main.go), the menu bar
                      item (tray.go), Settings (settings.go, matches.go),
                      the match editor (editor.go), the palette (palette.go)
                      and the Recent Activity HUD (hud.go), with headless
                      UI tests (app_test.go)
internal/macos/       purego bindings of what MyGo leaves out: the event tap,
                      synthesized keys, Accessibility, secure input, the
                      frontmost app and NSWorkspace notifications, the
                      pasteboard with its change count
internal/engine/      the trigger trie, the typed buffer, undo by backspace,
                      the state machine the tap feeds (Context), variables
                      and dates, fuzzy scoring: pure Go
internal/expander/    the running engine: the tap's handler, injection
                      (keys or paste), the AI flow
internal/model/       matches, the configuration, AI prompts, HUD events
internal/config/      ~/.config/sokki: load, save, first-launch files,
                      recency.json, the in-memory Store
internal/ai/          the claude CLI: finding it, its arguments, running it
internal/prefs/       the app's own preferences (were UserDefaults)
internal/telemetry/   the daily ping and crash reports
internal/logx/        ~/.config/sokki/logs/sokki.log
```

## Threads

- **The main thread** (MyGo's) owns the app's state, the windows and the
  Store. Everything else hands work to it with `app.post`, which keeps the
  order of what was posted.
- **The tap thread** is a goroutine locked to an OS thread running a
  `CFRunLoop` with two taps: the keyboard tap (key down, flags changed),
  which can swallow events, and a listen-only tap of mouse presses in other
  apps. Their callbacks are made once with `purego.NewCallback` and dispatch
  to the tap that runs. A tap that macOS disables is enabled again at once.
- **The expander's queue** is a goroutine that runs the steps following
  keys in order, one at a time, as the Swift app's main queue did:
  expansions, undo, handlers.
- **The AI flow** runs on a goroutine of its own, and its placeholder's
  animation on another. The injector's lock makes each injection whole.
- `engine.Context` takes a lock in every method, as `TapContext` did.

## The engine

A straight port of the Swift engine, with the same decisions and the same
trace lines: the trie fires the longest trigger once no longer one can
follow, and holds a shorter one for `conflict_resolve_delay_ms` (400 ms by
default) when a longer one may; backspace rewinds the buffer; ⌥⌫, ⌘⌫ and the
caret keys clear it; as many backspaces as an expansion typed, within 3
seconds, restore its trigger; Cmd-V blocks expansion for 200 ms; secure input
drops everything before anything is logged; double-tapping a modifier
toggles Sokki or shows the HUD; the palette's chord is swallowed. A click in
another app, an app switch or a shortcut drops a pending match.

Characters are runes rather than Swift's grapheme clusters, which differ
only for triggers with combining marks.

### Dates

`{{date:format}}` takes Unicode (ICU) patterns as before, such as
`yyyy-MM-dd`, formatted by `engine.FormatDate`. A format with a `%` is
strftime's, such as `%Y-%m-%d`, which the Swift app's starter file used but
did not understand.

## Windows

| Swift | Go |
|---|---|
| `NSStatusItem` with a menu rebuilt as it opens | `mygo.Tray`, its menu rebuilt as the state changes; dimmed with an icon at 35% rather than `appearsDisabled` |
| SwiftUI `Window` "Sokki Settings" and its `TabView` | a MyGo window with `ui.Tabs`, hidden rather than closed |
| the editor as a sheet, and a window from the palette | one editor window for Settings, the palette and the HUD |
| borderless `NSPanel` made key with the app made `.regular` | a MyGo `Panel`: non-activating, it takes the keyboard while the app in front keeps the front and its focus, so nothing needs reactivating before injecting |
| HUD `NSPanel` | a MyGo `Panel` shown with `ShowInactive` |
| `ActivationPolicyCoordinator` | `app.acquire` / `app.release`, for Settings and the editor |

Windows that hide when closed let `App.Quit` close them (`app.quitting`,
set in `OnBeforeQuit`); otherwise preventing the close would cancel the
quit.

## Configuration

`~/.config/sokki` is unchanged: `config/default.yml`, `match/*.yml`,
`recency.json` and `logs/`. Loading is as lenient as before: a key of the
wrong type keeps its default, the legacy `ai_*` keys and `ai:` blocks are
refused with the same messages. `SOKKI_CONFIG_DIR` moves it, for
`scripts/dev.sh`.

The watcher is MyGo's `WatchFiles`, which never opens `logs/`, so Sokki's own
log writes cannot trigger reloads (the 0.0.16 fix, made structural).
A reload keeps the IDs of the matches by file and trigger, so that a choice
in the list and an open editor survive the reload that follows every save.

## Preferences and telemetry

What the Swift app kept in UserDefaults (the telemetry switches, the install
ID, the last ping, the crash reports sent) is in
`~/Library/Application Support/Sokki/prefs.json`, copied once from the
`com.sokki.Sokki` defaults with `/usr/bin/defaults`. Launch at login is
MyGo's `App.SetOpenAtLogin`; automatic update checks are the updater
plugin's.

A Go crash leaves no `.ips` report, so `telemetry.KeepCrashes` has the
runtime write it to `crash.log` (`debug.SetCrashOutput`), moved into
`crashes/` at the next launch and uploaded from there, with any Sokki `.ips`
reports. The telemetry endpoint and key come from `telemetry.json` among the
app's resources, which the release workflow writes from its secrets; builds
without it send nothing, as builds without the Info.plist keys did.

## Updates and releases

MyGo's updater replaces Sparkle: signed updates (Ed25519, the key of
`mygo keygen`), delta updates, and the native update window. Releases are
on the public `sorrycc/Sokki`, where installed apps read them; the private
`sorrycc/Sokki-source` builds them in GitHub Actions when `scripts/bump.sh`
pushes a tag. Copies of 0.0.16 keep reading the old Sparkle feed, which gets
no new version: they move to 0.1.0 by downloading it once.

## Tests

`go test ./...`: the engine's state machine, the trie, the config loader,
prompts, the CLI's arguments and output (with a fake `claude`), telemetry
against a test server, and the views through `ui.NewTester`.
`SOKKI_SHOTS=dir go test ./cmd/sokki` saves screenshots of the views. The
macOS bindings test the pasteboard (not with `-short`) and the event tap,
which posts a Shift press and skips without the Accessibility permission.
