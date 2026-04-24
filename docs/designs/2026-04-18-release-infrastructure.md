# Release Infrastructure for Sokki

**Date**: 2026-04-18
**Status**: approved, not yet implemented
**Source playbook**: `/Users/chencheng/Projects/shun/docs/architecture/swift-macos-release-playbook.md`

## Goal

Adopt the Shun Swift/macOS release playbook verbatim so that a single command —
`bun release patch --push` — produces signed, notarized, stapled `.zip` + `.dmg`
+ `.sha256` artifacts that open on any modern Mac without Gatekeeper warnings.

## Approach

**Mechanical transcription of the playbook.** Copy the four scripts, substitute
Sokki's placeholder values, commit a pre-generated `.icns`. No enhancements, no
new abstractions. The playbook is battle-tested on Shun (4 successful releases
as of 2026-04-18); YAGNI says don't re-invent.

**Alternatives considered and rejected:**
- *Preserving the existing `package_app.sh`'s cosmetic quirks* — no real
  benefit; mixing styles just creates drift from the upstream playbook.
- *Adding GitHub-release uploads / changelog automation on top* — out of scope
  for this task; can be layered on later if needed.

## Placeholder values

| Placeholder | Value |
|---|---|
| `<APP_NAME>` | `Sokki` |
| `<BUNDLE_ID>` | `com.sokki.Sokki` |
| `<SIGN_IDENTITY>` | `Developer ID Application: chen cheng (KU8S35TEW8)` |
| `<TEAM_ID>` | `KU8S35TEW8` |
| `<NOTARY_PROFILE>` | `sokki-notary` |
| `<MIN_OS_VERSION>` | `14.0` |
| `LSUIElement` | `true` (keep — menu bar app) |

## File changes

### New files

- `VERSION` → `0.0.0\n` (first release will be `bun release major` → `1.0.0`)
- `Scripts/release.sh` → verbatim from playbook §4c, placeholders filled
- `Scripts/release.ts` → verbatim from playbook §4d
- `package.json` → `{"name":"sokki","private":true,"scripts":{"release":"./Scripts/release.ts"}}`
- `Resources/AppIcon.icns` → pre-generated once from the master PNG, then committed
- `Resources/sokki-logo.png` → master icon source, committed (moved out of gitignored `tmp/`). `.icns` is a derived artifact; keeping the master in git means we can regenerate at any resolution later.

### Rewritten

- `Scripts/package_app.sh` → replace with playbook §4b version:
  - Read `VERSION` from file (not hard-coded)
  - Developer ID signing + `--options runtime` + `--timestamp` (not ad-hoc)
  - `cp Resources/AppIcon.icns …` (drop the `sips` / `iconutil` dynamic
    generation; icon is a committed artifact)
  - Build target: **arm64-only** (`swift build -c release` default on
    Apple Silicon hosts). Intel Macs are not supported. Universal binary
    rejected: doubles build time + artifact size for a shrinking Intel
    user base in 2026. Revisit if Intel support is requested.

### Permissions

After copying the scripts, grant execute bit — required or `./Scripts/release.ts`
won't run:

```bash
chmod +x Scripts/package_app.sh Scripts/release.sh Scripts/release.ts
```

### Modified

- `.gitignore`
  - Fix stale `/HotApp.app` → `/Sokki.app`
  - Add `/build` (artifacts dir)
- `README.md`
  - Add a "Release" section with `bun release major|minor|patch` usage
  - Add one-time notary registration as a full, copy-pasteable one-liner
    (see §"User one-time setup" below). README is where future-you on a new
    machine will look; don't bury it in `docs/designs/`.

### Required un-tracking before `.gitignore` takes effect

`Sokki.app/` is **currently tracked** (4 files: `Contents/Info.plist`,
`Contents/MacOS/Sokki`, `Contents/Resources/AppIcon.icns`,
`Contents/_CodeSignature/CodeResources`). The new `.gitignore` entry is a
no-op on already-tracked paths. Un-track first:

```bash
git rm -r --cached Sokki.app
```

Commit this alongside the `.gitignore` change so the working tree and the
repo agree.

## Data flow

```
bun release patch
  └─ release.ts
      ├─ verify clean tree, tag doesn't already exist
      ├─ bump VERSION 1.0.0 → 1.0.1
      ├─ release.sh
      │   ├─ preflight (cert + notary profile present)
      │   ├─ package_app.sh  (swift build → bundle → Developer ID sign)
      │   ├─ build DMG → sign DMG
      │   ├─ notarytool submit --wait (~3-5 min, --timeout 20m)
      │   ├─ staple .app + .dmg (fallback: separate app submission)
      │   ├─ ditto zip (of stapled .app)
      │   ├─ sha256 of {zip,dmg}
      │   └─ verify (codesign + stapler + spctl)
      └─ git commit "release: 1.0.1" + tag v1.0.1 [+ push]
```

## Icon source

`Resources/AppIcon.icns` is the canonical source of truth for the app icon —
it is the file the build pipeline consumes (see `Scripts/package_app.sh`).
`Resources/sokki-logo.png` is a 1024×1024 preview extracted from the icns for
README/docs use; regenerate it from the icns rather than editing it directly:

```bash
ICONSET=$(mktemp -d)/AppIcon.iconset
iconutil -c iconset Resources/AppIcon.icns -o "$ICONSET"
cp "$ICONSET/icon_512x512@2x.png" Resources/sokki-logo.png
```

To replace the icon entirely, author a new `AppIcon.icns` (≥1024×1024 master,
all standard slots via `iconutil -c icns`) and regenerate the PNG preview as
above.

## User one-time setup (outside repo, before first `bun release`)

```bash
xcrun notarytool store-credentials sokki-notary \
  --apple-id <your-apple-id> \
  --team-id KU8S35TEW8 \
  --password <app-specific-password>
```

The keychain profile is a local alias; no secrets land in the repo. Same Apple
ID / team as Shun; only the profile name differs.

## Test plan

1. `./Scripts/package_app.sh` → `open Sokki.app` — signed build works locally.
2. `bun release major` (no `--push`) — first real release: `0.0.0` → `1.0.0`.
   Full notarization round-trip, produces stapled `.zip` + `.dmg` + `.sha256`
   in `build/`. Expect 3–5 min.
3. `spctl --assess --type open --context context:primary-signature -v build/Sokki-1.0.0.dmg`
   — verify Gatekeeper accepts the DMG.
4. End-to-end permission check: open the notarized `.dmg`, drag to
   `/Applications`, launch, confirm the Accessibility prompt appears and
   global expansion works. (Hardened runtime + notarization can surface
   Info.plist gaps — usage strings, etc. — that ad-hoc builds mask.)
5. If all green: `git push origin master && git push origin v1.0.0` (or re-run
   `bun release` subsequent bumps with `--push`).

## Risks & notes

- **No existing Node tooling**: adding `package.json` is purely for the
  `bun release` alias — no `node_modules`, no lockfile, no dependencies.
  Accepted per playbook.
- **Stapling fallback**: playbook's `release.sh` includes a fallback to
  re-submit the app separately if DMG-sourced stapling fails. Kept as-is.
- **First-release boot**: requires the user to have registered `sokki-notary`
  in keychain beforehand; `release.sh` preflight fails clearly if missing.
- **`tmp/` after migration**: `tmp/sokki-logo.png` is superseded by
  `Resources/sokki-logo.png` (committed). `tmp/` can be left gitignored for
  throwaway scratch work or removed entirely.
- **Hardened-runtime Info.plist gaps**: Sokki captures global keyboard events
  via the Accessibility API. The Accessibility prompt is triggered by the
  system and does *not* require a hardened-runtime entitlement or usage
  string. **Verified** (grep of `Sources/`): no AppleScript, `osascript`,
  `NSAppleScript`, or `NSAppleEventDescriptor` usage — so
  `NSAppleEventsUsageDescription` is not needed. No entitlements file is
  required for the initial release. Re-audit if Apple Events or Automation
  APIs are added later.
- **arm64-only distribution**: Intel Macs cannot run the shipped binary.
  Acceptable per current user base; watch for GitHub issues requesting
  Intel support before reconsidering a universal build.

## References

- Playbook: `/Users/chencheng/Projects/shun/docs/architecture/swift-macos-release-playbook.md`
- Shun's implementation: `/Users/chencheng/Projects/shun/Scripts/{package_app,release}.sh`, `release.ts`
- Shun's design discussion: `/Users/chencheng/Projects/shun/docs/designs/2026-04-16-signing-and-notarization.md`
