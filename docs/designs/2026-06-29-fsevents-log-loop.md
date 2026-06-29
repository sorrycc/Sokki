# FSEvents → log-write feedback loop fix

## 1. Background

`~/.config/sokki/logs/sokki.log` fills with `updateMatches: 75 enabled; triggers=[…]`
TRACE lines written ~5–6×/second indefinitely, even when the app is idle and no
config is being edited. The volume is a symptom of a self-sustaining loop, not of
real config churn.

## 2. Requirements Summary

Sokki watches its config directory with an FSEvents stream to hot-reload config
on edit. The watched directory is `~/.config/sokki` (`AppDelegate.swift:24`) and
the stream is created with `kFSEventStreamCreateFlagFileEvents`
(`AppDelegate.swift:308`), so it reports individual file writes recursively.

The app's own log file lives **inside** that watched directory
(`~/.config/sokki/logs/sokki.log`, `Logger.swift:30-32`). Every TRACE write is a
file event under the watched tree, so the watcher reloads config, the reload
writes more TRACE lines, and the loop sustains itself at the debounce cadence.

Goal: ignore FSEvents that only touch the `logs/` subdirectory, while still
hot-reloading on genuine `config/` and `match/` edits.

## 3. Acceptance Criteria

1. When the app is idle, writes to `~/.config/sokki/logs/` no longer trigger a
   config reload — the steady `updateMatches` TRACE cadence stops.
2. Genuine edits to `config/*.yml` and `match/*.yml` still trigger a live reload.
3. The callback inspects the FSEvents changed paths and skips the reload **only**
   when every changed path is under the `logs/` subdirectory. If the paths cannot
   be read, it reloads (fail-safe).
4. No regression to debounce or self-save behavior; `swift build` is clean under
   Swift 6 strict concurrency.

## 4. Problem Analysis

The loop, step by step:

1. `updateMatches` writes a TRACE line to `logs/sokki.log` (`TapContext.swift:289`).
2. The in-place `FileHandle` append (`Logger.swift:74-84`) is a file event under
   the watched `configDir`.
3. FSEvents fires → `debouncedReload()` → `reloadConfig()` (`AppDelegate.swift:316,344`).
4. `reloadConfig` → `matchStore.loadAll()` posts `.matchesDidChange`
   (`MatchStore.swift:37` → handler at `AppDelegate.swift:219` → `updateMatches`)
   **and** `applyConfig()` calls `updateMatches` directly (`AppDelegate.swift:156`).
5. Both `updateMatches` calls write TRACE lines → back to step 1.

Approaches evaluated:

- **Move the log dir outside `~/.config/sokki`** — breaks the documented log path
  (`AppDelegate.swift:183` "Show Log" menu item, `README`); user-visible churn. Rejected.
- **Drop `kFSEventStreamCreateFlagFileEvents`** — loses file-level granularity and
  still reports the `logs/` subtree at directory granularity; doesn't actually
  break the loop. Rejected.
- **Filter changed paths in the callback (chosen)** — the stream already sets
  `kFSEventStreamCreateFlagUseCFTypes`, so `eventPaths` is a `CFArray` of
  `CFString` we can read with zero extra allocation. Skip the reload when every
  changed path is under `logs/`. Minimal, local, no user-visible change.

## 5. Decision Log

**1. How to read the changed paths inside the C callback?**

- Options: A) bridge `eventPaths` via `unsafeBitCast(_, to: NSArray.self)` (works
  because `kFSEventStreamCreateFlagUseCFTypes` is set) · B) reinterpret as a raw
  `UnsafePointer` and walk `numEvents` C strings.
- Decision: **A)** — the stream already opts into CFTypes
  (`AppDelegate.swift:307`); the `NSArray` bridge is the idiomatic, allocation-free
  read. Option B reimplements what CFTypes already gives us.

**2. How to test "under the logs/ subdirectory"?**

- Options: A) `path.hasPrefix(logsDir.path + "/")` · B) decompose into `URL`
  path components and compare.
- Decision: **A)** — a string prefix with a trailing slash is correct (the slash
  prevents a sibling like `logsfoo` from matching) and trivial. URL-component
  comparison is overkill (YAGNI).

**3. When should the reload be skipped?**

- Options: A) skip iff `!paths.isEmpty && paths.allSatisfy { under logs/ }` ·
  B) skip if any path is under logs/.
- Decision: **A)** — only a logs-*only* batch is noise. A mixed batch (config edit
  whose resulting log writes landed in the same 0.3s FSEvents window) must still
  reload. Empty/unreadable paths fall through to a reload (fail-safe, AC3).

**4. Fix the double `updateMatches` per reload too?**

- Options: A) leave it · B) dedupe so `reloadConfig` updates matches once.
- Decision: **A)** — it is redundant but harmless and not the loop's cause. The
  `.matchesDidChange` handler is independently required for Settings-UI saves
  (`MatchStore.swift:70`), which never go through `applyConfig`. Out of scope (YAGNI).

## 6. Design

Read the changed paths in the FSEvents callback and route through a new
`handleFSEvents(_:)` helper on `AppDelegate` that decides whether to reload.

Callback (replaces the discarding `{ _, info, _, _, _, _ in }`):

```swift
{ _, info, _, eventPaths, _, _ in
    guard let info else { return }
    let delegate = Unmanaged<AppDelegate>.fromOpaque(info).takeUnretainedValue()
    let paths = unsafeBitCast(eventPaths, to: NSArray.self) as? [String] ?? []
    DispatchQueue.main.async {
        delegate.handleFSEvents(paths)
    }
}
```

Helper (new, `@MainActor` like the rest of `AppDelegate`):

```swift
private func handleFSEvents(_ paths: [String]) {
    // Ignore batches that only touch our own log directory. Otherwise each TRACE
    // write would retrigger a reload, which writes more TRACE lines — a loop
    // throttled only by the debounce. A mixed batch still reloads; unreadable
    // paths fall through to a reload (fail-safe).
    let logsPrefix = configDir.appendingPathComponent("logs").path + "/"
    let onlyLogs = !paths.isEmpty && paths.allSatisfy { $0.hasPrefix(logsPrefix) }
    if onlyLogs { return }
    debouncedReload()
}
```

`debouncedReload()`, `reloadConfig()`, and the 0.3s debounce timer are unchanged.
The helper uses `NSArray`, which needs `Foundation` — `AppDelegate.swift` already
imports it (line 2), so no new import is required.

Convergence note: a config edit and its resulting log writes can land in one 0.3s
FSEvents batch. That batch contains a non-log path → one reload runs (correct).
The TRACE writes from that reload form a logs-only batch → skipped. The loop
terminates after at most one redundant reload.

## 7. Files Changed

- `Sources/Sokki/AppDelegate.swift` — change the `FSEventStreamCreate` callback
  closure body from discarding its args to reading `eventPaths`, and add a new
  `handleFSEvents(_:)` helper that skips the reload for logs-only batches.

## 8. Verification

1. [AC1] Run the app, leave it idle, `tail -f ~/.config/sokki/logs/sokki.log`:
   `updateMatches` TRACE lines stop appearing on a steady cadence.
2. [AC2] Edit `~/.config/sokki/config/default.yml` (or a `match/*.yml`): a single
   reload runs and the new config takes effect (one `updateMatches` line appears).
3. [AC3] Confirm in code that a logs-only batch returns early while a mixed/empty
   batch reaches `debouncedReload()`.
4. [AC2/AC3] End-to-end mixed-batch check: edit a `match/*.yml` while the app
   runs and confirm exactly one `Config reloaded` line (`AppDelegate.swift:209`)
   with no resumed `updateMatches` cadence afterward — this exercises the only
   part with real timing risk (a config edit + its TRACE writes in one batch).
5. [AC4] `swift build` succeeds with no new warnings.
