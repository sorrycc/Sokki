# Sparkle + R2 Auto-Update for Sokki

**Date**: 2026-04-19
**Status**: approved, not yet implemented
**Source playbook**: `/Users/chencheng/Projects/shun/docs/architecture/swift-macos-sparkle-r2-autoupdate-playbook.md`
**Predecessor**: `docs/designs/2026-04-18-release-infrastructure.md` (Developer ID signing + notarization; required prereq — implemented)

## Goal

Wire Sparkle 2 into Sokki so that a single command — `bun release patch --push --upload` — produces a signed, notarized, stapled `.dmg` + `.zip`, EdDSA-signs the ZIP, publishes artifacts + `appcast.xml` to Cloudflare R2, and pushes the tag. Installed Sokki instances then check the appcast on a daily schedule and offer one-click install with codesign + EdDSA verification.

## Approach

**Mechanical transcription of the autoupdate playbook**, mirroring the pattern of `2026-04-18-release-infrastructure.md`. Copy Shun's working `upload_r2.ts` verbatim; substitute Sokki's placeholders; patch `package_app.sh` and `release.ts` per playbook §8 / §10. No new abstractions.

**Reuse Shun's R2 bucket** via `APP_KEY_PREFIX=sokki`. Same credentials, same `.env`-at-repo-root pattern. Public host is `https://download.sorrycc.dev` (served by `~/Projects/downloads-worker` — a Cloudflare Worker with an R2 binding, shared with Shun). Feed at `https://download.sorrycc.dev/sokki/appcast.xml`. The `pub-35387f8a923241ffa2dc24251ae7b9d0.r2.dev` subdomain remains enabled as a fallback for any pre-custom-domain installs.

**Fresh EdDSA keypair for Sokki**, with one-time keychain migration so Sokki's and Shun's private keys coexist (see §3).

**YAGNI cuts**: no beta channel, no Sparkle delta updates, no release-notes HTML customization beyond what the playbook provides. (The custom-domain Worker at `download.sorrycc.dev` exists now, shared with Shun — see `~/Projects/downloads-worker`.)

### Alternatives considered and rejected

- *Shared EdDSA keypair across both apps* — violates least-privilege; a Shun key leak would compromise Sokki updates too.
- *New dedicated R2 bucket for Sokki* — more ceremony, no benefit at current scale. `APP_KEY_PREFIX` already namespaces cleanly.
- *Settings UI as new `Updates` tab* — YAGNI; a Form section in the existing General tab matches the playbook and is one fewer scene to maintain.
- *Custom Cloudflare Worker / domain from day one* — originally deferred; now shipped as the shared `downloads-worker` at `download.sorrycc.dev`. The Worker is append-only to this design: r2.dev stays enabled so pre-migration installs keep updating.

## 1. File changes

### New files

| Path | Source | Notes |
|---|---|---|
| `Scripts/sparkle.config` | Playbook §7a | `SPARKLE_PUBLIC_KEY=<fresh Sokki key>`, feed `https://download.sorrycc.dev/sokki/appcast.xml`, base `https://download.sorrycc.dev`. Committed. |
| `Scripts/upload_r2.ts` | Copy from `/Users/chencheng/Projects/shun/Scripts/upload_r2.ts` with two edits | (1) Swap hardcoded `"Shun"` → `"Sokki"` in appcast `<title>` / `<link>` / `<description>`. (2) **Add `--account ed25519-sokki`** to the `sign_update` invocation (Shun's current copy has no `--account` flag; required once we rename the keychain entries per §3a). `APP_KEY_PREFIX` drives the rest via env. |
| `.env.example` | Playbook §7b | Committed template. |
| `.env` | Same, with real creds | **Gitignored** (`/.env`). Created manually before first `--upload`. |
| `CHANGELOG.md` | Playbook §7c | Keep-a-Changelog header. `## Unreleased` is pre-populated for the first release — see §4 (release-preflight requires non-empty body on `--upload`). |
| `Sources/Sokki/Services/UpdaterService.swift` | Playbook §4 | `SparkleDelegate` (NSObject) + `@MainActor UpdaterService` facade. **Dual-log update errors**: `os.Logger` (subsystem `com.sokki.Sokki`, category `updater`) for Console.app / system-wide visibility, **and** `Sokki.Logger.shared.error(…)` so failures appear in `~/.config/sokki/logs/sokki.log` — the file surfaced by the menu bar's "Show Log" item, which is where users will actually look when auto-updates misbehave. |

### Modified files

- **`Package.swift`** — add `.package(url: "https://github.com/sparkle-project/Sparkle", .upToNextMinor(from: "2.7.0"))` and `"Sparkle"` in target deps.
- **`Scripts/package_app.sh`** — stacked patches per playbook §8:
  - **§8a** Source `Scripts/sparkle.config` and validate `SPARKLE_PUBLIC_KEY` with regex `^[A-Za-z0-9+/]{43}=$`.
  - **§8b** 🔴 `install_name_tool -add_rpath "@executable_path/../Frameworks"` **after** copying the binary and **before** any `codesign` step. Ordering is critical both ways — binary must exist; Mach-O header mutation invalidates prior signatures.
  - **§8c** Embed `Sparkle.framework` under `Contents/Frameworks/`; sign **inside-out**: nested `.xpc` / `.app` deepest-first (`find … | sort -rz`), then each `Versions/*/Autoupdate` helper, then the framework, then the main app. **Pin slice selection**: `find .build/artifacts -type d -path "*macos-arm64*" -name "Sparkle.framework"` rather than `| head -n 1` on an unfiltered find — defends against future Sparkle XCFrameworks that add a separate `macos-x86_64` slice which could sort first.
  - **§8d** Info.plist additions: `SUFeedURL`, `SUPublicEDKey`, `SUEnableAutomaticChecks=true`, `SUScheduledCheckInterval=86400` (daily), `SUAllowsAutomaticUpdates=true`, `SUEnableSystemProfiling=false` (privacy pin).
- **`Scripts/release.ts`** — playbook §10:
  - Add `--upload` flag.
  - Preflight: require non-empty `## Unreleased` body when `--upload`.
  - Promote `## Unreleased` → `## <version> - <UTC date>`; insert a fresh empty `## Unreleased` at top.
  - Pipeline order: build → commit (VERSION + CHANGELOG) + tag → upload → push. Upload **before** push so a failure leaves local tag intact and recoverable, never publishes a tag pointing at empty R2.
- **`Sources/Sokki/AppDelegate.swift`** — playbook §5:
  - Guarded `UpdaterService()` init: skip if `Bundle.main.object(forInfoDictionaryKey: "SUFeedURL") == nil` (keeps `swift run` dev builds silent).
  - Observe `.checkForUpdates` notification → call `updater?.checkForUpdates()` on `@MainActor`.
- **`Sources/Sokki/StatusItemController.swift`** — add "Check for Updates…" `NSMenuItem` between "Show Log" and the separator in `menuNeedsUpdate`. Wire via closure callback (`var onCheckForUpdates: (() -> Void)?`) to match Sokki's existing pattern (`onShowLog`, `onOpenSettings`, etc.) — **not** the playbook's inline `NotificationCenter.post`, which would fork the menu's style. AppDelegate's `setupStatusItemActions` posts the notification on invocation.
- **`Sources/Sokki/SokkiApp.swift`** — add `extension Notification.Name { static let checkForUpdates = Notification.Name("checkForUpdates") }` (colocated with existing app-wide notification names).
- **`Sources/Sokki/Views/SettingsView.swift`** — in `GeneralSettingsView`'s `Form`, add:
  ```swift
  if updatesConfigured {
    Section("Updates") {
      HStack { Text("Current version"); Spacer(); Text(bundleVersion).foregroundStyle(.secondary) }
      Toggle("Automatically check for updates", isOn: $automaticallyCheckForUpdates)
      HStack { Spacer(); Button("Check for Updates…") { NotificationCenter.default.post(name: .checkForUpdates, object: nil) } }
    }
  }
  ```
  with `@AppStorage("SUEnableAutomaticChecks") private var automaticallyCheckForUpdates = true` and a `updatesConfigured` computed property. Guards prevent showing a dead section in dev builds.
- **`package.json`** — add `"upload": "./Scripts/upload_r2.ts"` to scripts; add `"marked": "^12.0.0"` devDep (used by `upload_r2.ts` to render release notes to HTML).
- **`.gitignore`** — add `/.env`.
- **`README.md`** — add "Auto-Update" section: one-time R2 + Sparkle setup, `bun release patch --push --upload` usage, dry-run smoke test.

### Out of scope

- Website "Download" button (stable `sokki-latest.dmg` URL will exist once uploaded, but Sokki has no website yet).
- Beta channel, delta updates, Sparkle usage analytics.
- GitHub Releases mirror.

## 2. R2 layout

Single bucket shared with Shun. Keys under `sokki/`:

```
sokki/
  appcast.xml
  Sokki-latest.dmg          # stable alias for future website download button
  Sokki-0.0.2.dmg
  Sokki-0.0.2.zip           # Sparkle enclosure
  Sokki-0.0.3.dmg
  Sokki-0.0.3.zip
  …
```

Convention: Sparkle enclosure is the ZIP (smaller, no mount/unmount failure modes); DMG is for manual human download only.

## 3. One-time operational setup

### 3a. Keychain migration (preserve Shun's key, generate Sokki's)

`generate_keys` writes to a fixed slot (`service=https://sparkle-project.org`, `account=ed25519`). Naive run overwrites Shun's private key → would permanently brick Shun's auto-updates.

**Confirmed** via `sign_update -h`: the tool accepts `--account <account>`, so each app can own a distinct keychain account under the same service. Plan:

```bash
# 1. Back up Shun's private key (base64) to password manager FIRST.
security find-generic-password -s "https://sparkle-project.org" -a "ed25519" -w

# 2. Rename Shun's entry → account "ed25519-shun"
SHUN_KEY=$(security find-generic-password -s "https://sparkle-project.org" -a "ed25519" -w)
security delete-generic-password -s "https://sparkle-project.org" -a "ed25519"
security add-generic-password -s "https://sparkle-project.org" -a "ed25519-shun" -w "$SHUN_KEY"

# 3. Generate Sokki's fresh keypair (lands in the default ed25519 slot)
"$(ls -d /opt/homebrew/Caskroom/sparkle/*/bin | sort | tail -1)/generate_keys"
# → prints Sokki's public key; paste into Scripts/sparkle.config

# 4. Rename Sokki's entry → account "ed25519-sokki"
SOKKI_KEY=$(security find-generic-password -s "https://sparkle-project.org" -a "ed25519" -w)
security delete-generic-password -s "https://sparkle-project.org" -a "ed25519"
security add-generic-password -s "https://sparkle-project.org" -a "ed25519-sokki" -w "$SOKKI_KEY"

# 5. Back up Sokki's private key to password manager (labelled "Sokki Sparkle EdDSA private key").

# --- To RESTORE on a new machine (or after keychain wipe): ---
security add-generic-password -s "https://sparkle-project.org" -a "ed25519-sokki" -w "<paste-base64-from-password-manager>"
# Then verify:  sign_update --account ed25519-sokki some_test_file.zip
```

**Ripple effects**:
- Sokki's `Scripts/upload_r2.ts` must call `sign_update --account ed25519-sokki <zip>`. Shun's current copy of `upload_r2.ts` invokes `sign_update` with no `--account` flag; our copy gets patched at copy-time (see §1 file table).
- **Shun's `upload_r2.ts` also needs updating** the next time Shun releases — its signer currently reads from the (now empty) default `ed25519` slot. Out of scope for this design, but flag it in Shun's repo before next Shun release. (One-line PR: add `--account ed25519-shun`.)

### 3b. R2 credentials (reuse Shun's)

Copy Shun's `.env` values into Sokki's `.env` at repo root. Only `APP_KEY_PREFIX` differs:

```
R2_ACCOUNT_ID=<same as Shun>
R2_ACCESS_KEY_ID=<same as Shun>
R2_SECRET_ACCESS_KEY=<same as Shun>
R2_BUCKET=<same as Shun>
APP_KEY_PREFIX=sokki
```

## 4. First release flow

### Version-number choice

Sokki is at `0.0.1`. First Sparkle-enabled release = `0.0.2` (`bun release patch`). Picked patch because no user-facing Sokki behavior changes — Sparkle is pure release-infrastructure. `0.1.0` (minor) would also be defensible since auto-update is a real capability; pre-1.0 semver is lax enough that either reads fine. Calling it explicitly so the choice isn't re-litigated at release time.

### Initial `## Unreleased` body (pre-written)

The preflight requires a non-empty `## Unreleased` section when `--upload` is passed. The first release also doubles as the one-time handoff to Sparkle, so the content serves users too. Pre-writing here so it's not improvised:

```markdown
### Added
- Sparkle-based automatic updates. Future releases will auto-install with signature verification; this one requires a manual download.
- "Check for Updates…" menu-bar item and Settings toggle for automatic update checks.
```

### Pipeline

```
bun release patch --push --upload
  └─ release.ts
      ├─ preflight: clean tree, no duplicate tag, ## Unreleased non-empty
      ├─ bump VERSION 0.0.1 → 0.0.2
      ├─ promote "## Unreleased" → "## 0.0.2 - 2026-04-19" (UTC), insert fresh Unreleased at top
      ├─ release.sh
      │   ├─ preflight (cert + notary profile)
      │   ├─ package_app.sh: swift build → bundle → rpath patch → embed Sparkle
      │   │                  → inside-out codesign → Info.plist with SUFeedURL / SUPublicEDKey / …
      │   ├─ DMG build + sign
      │   ├─ notarize + staple (app + DMG; fallback for app-only submission on failure)
      │   ├─ ditto zip (of stapled .app) + sha256
      │   └─ verify (codesign + stapler + spctl)
      ├─ git commit (VERSION + CHANGELOG) + tag v0.0.2
      ├─ upload_r2.ts:
      │   ├─ sign_update ZIP (-a ed25519-sokki or fallback re-alias)
      │   ├─ fetch existing appcast.xml (404 fallback: empty template on first release)
      │   ├─ compose new <item>, upload {zip, dmg, sokki-latest.dmg alias, appcast.xml}
      │   ├─ verify via public HEAD (not Bun.s3.stat — returns 0 on R2)
      │   └─ byte-count strings as UTF-8 to dodge Content-Length mismatches
      └─ git push origin master && git push origin v0.0.2
```

### First-release handoff

`0.0.1` installs have no Sparkle and cannot auto-update to `0.0.2`. Manual re-download required once. At Sokki's current scale (internal / pre-public) this is zero-impact: the one affected user is you. From `0.0.2` onward, auto-updates work.

### Upload-failure recovery

If `--upload` fails after the commit+tag step (bad `.env`, R2 5xx, `sign_update` hiccup), the local tag stays. Local state is recoverable; don't amend or reset. Fix the underlying issue, then:

```bash
./Scripts/upload_r2.ts --version 0.0.2          # retries the full upload for the tagged version
git push origin master && git push origin v0.0.2 # only after upload succeeds
```

Do **not** `git push` before upload succeeds — an origin-visible tag pointing at empty R2 is the failure mode this ordering exists to prevent.

## 5. Smoke test before first real upload

`upload_r2.ts --dry-run` always invokes `sign_update` on `build/Sokki-<version>.zip`, so the artifact must exist. The playbook's suggested path uses the full `release.sh` (which notarizes — 3–5 min + burns a notary slot) just to produce a ZIP we won't ship. That cost buys only the final R2-upload step of coverage. Use the **cheap** path instead — EdDSA signing doesn't care about notarization:

```bash
# 1. Build an unsigned-but-bundled Sokki.app (with Sparkle embedded, rpath patched, Info.plist complete):
./Scripts/package_app.sh                       # ~20s

# 2. ditto-zip to match the name upload_r2.ts expects:
mkdir -p build
ditto -c -k --keepParent Sokki.app build/Sokki-$(cat VERSION).zip

# 3. Dry-run the upload pipeline:
./Scripts/upload_r2.ts --version $(cat VERSION) --dry-run
```

Catches: `sparkle.config` typos, missing `sign_update` binary, wrong `--account ed25519-sokki`, `.env` loading bugs (undefined `R2_*` vars fail preflight), malformed CHANGELOG parsing. No R2 writes, no notary submission.

If even this is too much ceremony: skip smoke-testing entirely. The first real `bun release patch --push --upload` exercises the pipeline end-to-end; any failure leaves the local tag intact per §"Upload-failure recovery", so you fix and retry `./Scripts/upload_r2.ts --version 0.0.2` without re-building or re-notarizing.

## 6. Test plan

1. `swift run` — dev build launches; guarded `UpdaterService` init is skipped silently; no Settings "Updates" section appears.
2. `./Scripts/package_app.sh` → `open Sokki.app` — menu bar icon appears, app does not crash at launch (rpath patch working; no dyld "Library not loaded").
3. `./Scripts/upload_r2.ts --version 0.0.1 --dry-run` — preflight passes, XML + upload plan print cleanly.
4. Edit `## Unreleased`, then `bun release patch --push --upload` for `0.0.2`. Full ~5 min pipeline.
5. `curl -sI https://download.sorrycc.dev/sokki/appcast.xml` returns `200`; `curl` the body and verify `sparkle:version="0.0.2"`, `sparkle:edSignature` present, enclosure URL matches uploaded ZIP.
6. Install `0.0.2` manually (drag DMG to `/Applications`). Settings → General → Updates shows version `0.0.2`, toggle defaults on.
7. Bump `## Unreleased`, run `bun release patch --push --upload` for `0.0.3`. Installed `0.0.2` should surface the update via "Check for Updates…" menu; one-click install succeeds.

## 7. Risks and gotchas

From playbook §13, filtered for Sokki-specific relevance:

- **Missing `@executable_path/../Frameworks` rpath** — #1 crash mode. `install_name_tool` step must run after binary copy and before any codesign. Comment in `package_app.sh` to flag both ordering constraints.
- **`.env` location** — must live at repo root (`release.ts` already does `process.chdir(projectRoot)`). `.env.example` comment to warn.
- **Version-field mismatch** — Sparkle ranks updates by `CFBundleVersion`, not `CFBundleShortVersionString`. `package_app.sh` sets both to `${VERSION}`; `upload_r2.ts` appcast `<item sparkle:version>` must match. Invariant: keep `CFBundleVersion == CFBundleShortVersionString == VERSION` until a build-number scheme is adopted.
- **UTF-8 byte length in appcast** — baked into Shun's `upload_r2.ts` via `TextEncoder`. Don't regress when editing `<title>` / `<description>` strings.
- **Keychain slot collision with Shun** — addressed in §3a. `sign_update -a` flag to be validated during implementation; re-alias fallback documented.
- **First-release auto-update gap** — existing `0.0.1` install (just the author) needs one manual re-download. Zero external impact.
- **`r2.dev` rate limit** (~10 req/s) — no longer on the critical path; traffic flows through `download.sorrycc.dev` (Worker + R2 binding). r2.dev stays enabled so any pre-migration installs continue to update. Never change `SUFeedURL` for an already-shipped version.
- **Sparkle minor-version bumps can break delegate contracts** — `.upToNextMinor(from: "2.7.0")` pin limits exposure. Revisit when bumping.
- **Hardened-runtime entitlements for Sparkle XPC** — the 2026-04-18 design asserted "no entitlements file needed." Sparkle embeds `.xpc` helpers (`Installer`, `Downloader`) which might require entitlements under hardened runtime in some configurations. **Action during implementation**: build, notarize, launch the bundled app, trigger "Check for Updates…", and confirm Console.app shows no `EXC_BAD_INSTRUCTION` / XPC sandbox denial from `org.sparkle-project.Sparkle.*`. If denials appear, add a minimal `.entitlements` file (most likely `com.apple.security.temporary-exception.mach-lookup.global-name` for the XPC services) and re-sign. Shun presumably did not need this; verify but don't pre-empt.

## 8. Rollback plan

If a release ships and turns out to be broken (crash on launch, data loss, wrong behavior), options in decreasing preference:

1. **Release a fixed version immediately.** `bun release patch --push --upload` on a hotfix branch. Users on the broken version auto-update to the fix within ~1 day (or instantly via "Check for Updates…"). This is the normal path and works for the vast majority of incidents.
2. **Remove the bad `<item>` from `appcast.xml`.** Manually edit the appcast on R2 (download, delete the offending `<item>` block, re-upload). Users who haven't updated yet stop seeing the offer; users who already updated are unaffected (Sparkle doesn't roll back). Use when the fix needs more than a few hours and you want to stop the bleed.
3. **Raise `sparkle:minimumSystemVersion` on the bad item** to exclude it retroactively. Niche — only useful if the bug is confined to a specific OS.

**Not an option**: auto-downgrade. Sparkle won't install an older version over a newer one, regardless of what the appcast says.

Don't delete the DMG/ZIP from R2 even after removing the appcast item — versioned URLs may be referenced elsewhere (issues, support threads). The `sokki-latest.dmg` alias should be re-pointed to the last-known-good release via re-upload.

## References

- Autoupdate playbook: `/Users/chencheng/Projects/shun/docs/architecture/swift-macos-sparkle-r2-autoupdate-playbook.md`
- Shun's implementation (reference): `/Users/chencheng/Projects/shun/Scripts/{package_app.sh,release.ts,upload_r2.ts,sparkle.config}`
- Release playbook (prereq): `/Users/chencheng/Projects/shun/docs/architecture/swift-macos-release-playbook.md`
- Predecessor design: `docs/designs/2026-04-18-release-infrastructure.md`
- Sparkle 2 docs: https://sparkle-project.org/documentation/
- Keep a Changelog: https://keepachangelog.com/en/1.1.0/
