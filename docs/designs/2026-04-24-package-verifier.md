# Package Verifier — Preventing DYLD & Bundle.module Ship-Bombs

## 1. Background

Seven crash reports collected from a sibling app built by the same developer/team
(bundle `com.shun.Shun`, team `KU8S35TEW8`) revealed two launch-time crashes that
shipped to end users:

- **DYLD / library missing** — `Library not loaded: @rpath/Sparkle.framework/...`.
  The app binary linked against `@rpath/Sparkle.framework`, but the framework was
  not present in `Contents/Frameworks/` (or the `@executable_path/../Frameworks`
  `LC_RPATH` was missing). dyld aborts at `main()` entry with `SIGABRT`.
- **SwiftPM `Bundle.module` fatalError** — `EXC_BREAKPOINT` inside
  `Bundle.module`'s `fatalError`, reached via `KeyboardShortcuts.RecorderCocoa.init`.
  Cause: the SwiftPM-generated resource bundle
  (e.g. `KeyboardShortcuts_KeyboardShortcuts.bundle`) was built by SwiftPM but
  never copied into `Contents/Resources/` by the packaging script.

Sokki's `Scripts/package_app.sh` already handles the first class *structurally*
(it patches rpath and embeds Sparkle) but performs **no verification**, so a
silent regression — a renamed XCFramework path, a failed `install_name_tool`,
a new SwiftPM dep with resources — would ship broken without any CI or release
gate noticing.

## 2. Requirements Summary

**Goal:** Harden Sokki's packaging pipeline so both failure classes are
impossible to ship, and the fix self-heals for the common SwiftPM-resource case.

**Scope:**
- In scope: a standalone verifier invoked by `package_app.sh` (pre-sign and
  post-sign) and by `release.sh` (post-staple, with a launch smoke test);
  auto-copy of SwiftPM resource bundles into `Contents/Resources/`.
- Out of scope: runtime self-check inside the Sokki app; product-code changes;
  TypeScript/Swift rewrite of the packaging scripts; DMG-internal structural
  checks (existing `spctl` + `stapler validate` cover the DMG).

## 3. Acceptance Criteria

1. `./Scripts/package_app.sh` on a clean checkout exits 0 and prints `[OK]`
   lines for every structural check at both pre-sign and post-sign checkpoints.
2. If `Contents/Frameworks/Sparkle.framework` is deleted between embed and
   sign, `package_app.sh` exits nonzero with
   `[FAIL] Sparkle.framework missing at Contents/Frameworks/`.
3. If the `install_name_tool -add_rpath` step is removed, verification exits
   nonzero with `[FAIL] LC_RPATH @executable_path/../Frameworks not set`.
4. When a SwiftPM dependency with resources is added to `Package.swift`,
   `package_app.sh` auto-copies the generated `*.bundle` into
   `Contents/Resources/`, logs `[COPY] <name>.bundle`, and passes verification
   with no product-code change.
5. If auto-copy is disabled (regression sim), verification exits nonzero with
   `[FAIL] <name>.bundle referenced by binary but missing from Contents/Resources/`.
6. `./Scripts/verify_app.sh build/Sokki.app` runs standalone against any
   pre-built `.app` for ad-hoc debugging.
7. `bun release patch` invokes `verify_app.sh --smoke` after stapling; smoke
   failure exits the release before distribution zip creation.
8. Non-smoke verification overhead < 5 s; smoke path < 10 s.
9. Verifier depends only on macOS-stock tools: `otool`, `codesign`, `file`,
   `find`, `strings`, `open`, `pgrep`, `pkill`.

## 4. Problem Analysis

Today's state of `Scripts/package_app.sh`:

- Lines 76–77 patch `LC_RPATH` via `install_name_tool` — fix is present, no
  check.
- Lines 82–94 locate and embed Sparkle's macos-arm64 slice — correct, no check.
- Lines 144–175 sign inside-out and call `codesign --verify`, the only existing
  check — catches signing defects, not packaging completeness.
- Zero logic for SwiftPM resource bundles (`Bundle.module` targets).

Approaches evaluated:

- **Inline bash checks in `package_app.sh`** — rejected: would grow the script
  past ~200 lines and block post-hoc use against a pre-built `.app`.
- **Swift script verifier (`verify_app.swift`)** — rejected: adds a Swift
  compile to the packaging path and a second build-system; zero benefit over
  bash for otool/codesign invocations.
- **TypeScript verifier (`verify_app.ts`) mirroring `release.ts` style** —
  rejected: `release.ts` is a thin wrapper over `release.sh`; verification
  logic is heavy shell-invocation, natural fit for bash.
- **Chosen: standalone `Scripts/verify_app.sh`** — reusable, testable, single
  responsibility, matches the bash style of `package_app.sh`/`release.sh`,
  zero extra deps.

## 5. Decision Log

**1. Which failure classes should the verifier cover?**
- Options: A) DYLD only · B) Bundle.module only · C) both · D) both + generic launch probe
- Decision: **C, with D as opt-in via `--smoke`** — matches the observed crash set
  while future-proofing against the latent foot-gun.

**2. How to detect SwiftPM resource bundles at build time?**
- Options: A) hard-code `.build/arm64-apple-macosx/release` · B) `swift build --show-bin-path -c release` · C) broad `find`
- Decision: **B** — robust across arch/config changes, no parsing.

**3. Auto-fix missing resource bundles, or fail only?**
- Options: A) detect-only hard fail · B) silent auto-copy · C) auto-copy + `[COPY]` log + re-verify · D) auto-copy only for known SwiftPM naming pattern
- Decision: **C** — rule is mechanical ("built `.bundle` must ship beside
  binary"); dev time on busywork is wasted, and hard-fail still fires if
  the copy fails for any reason.

**4. Where to place verification in the pipeline?**
- Options: A) pre-sign only · B) post-sign only · C) pre-sign + post-sign · D) post-sign + post-staple
- Decision: **pre-sign + post-sign inside `package_app.sh`; post-staple smoke
  inside `release.sh`** — auto-copy mandates a pre-sign step, `codesign --verify`
  needs post-sign, and the smoke test on the stapled app catches stapler
  corruption.

**5. Hard-fail vs warn taxonomy?**
- Options: A) hard-fail everything · B) hard-fail ship-breakers, warn for
  out-of-band dependencies · C) warn-only · D) grace period per class
- Decision: **B** — `[FAIL]` = missing framework / missing rpath / missing
  bundle / `codesign --verify` failure. `[WARN]` = pre-notarization `spctl`
  rejection, smoke-test failure.

**6. Language / file placement?**
- Options: A) inline · B) standalone `verify_app.sh` · C) `verify_app.swift` · D) `verify_app.ts`
- Decision: **B** — single responsibility, reusable, matches `Scripts/` bash style.

**7. Smoke test: mandatory or opt-in?**
- Options: A) mandatory in every invocation · B) soft-warn on failure · C) off · D) opt-in flag `--smoke`
- Decision: **D** — `package_app.sh` may run on builders without GUI session;
  `release.sh` always runs interactively, so it opts in.

**8. Binary-strings cross-check?**
- Options: A) skip entirely · B) use `strings | grep` · C) derive from `nm -Uj`
- Decision: **A — drop** (updated post-review). The check was redundant with
  Decision #3's enumeration of `$BIN_PATH/*.bundle`: if SwiftPM built a bundle,
  it must ship; if it didn't, there's nothing to verify. `strings` on a
  stripped/hardened binary has false negatives, and the regex has false
  positives. Source of truth is the build directory, not the binary bytes.

**9. Output format?**
- Options: A) silent on success, verbose on fail · B) per-check `[OK]/[COPY]/[WARN]/[FAIL]` lines · C) JSON
- Decision: **B** — matches `release.sh` echo style, grep-able, no tooling needed.

## 6. Design

### 6.1 `Scripts/verify_app.sh`

```
usage: verify_app.sh <path/to/App.app> [--smoke]
```

**Inputs:**
- `$1` = path to `.app` bundle (must exist).
- `--smoke` (optional) = run the launch probe.

**Environment:**
- Script `cd`s to repo root (same pattern as `package_app.sh`/`release.sh`).
- Resolves `BIN_PATH` via `swift build --show-bin-path -c release` **only if
  a build output is present**. When `BIN_PATH` is empty or has zero `*.bundle`
  entries, check 5 is skipped with `[WARN] no SwiftPM bundles in build output
  (post-hoc verification mode)`. This keeps AC6 (standalone use against a past
  `.app`) honest: we don't trust today's source tree to describe yesterday's
  build.

**Checks (sequential; any `[FAIL]` sets exit 1):**

1. **App bundle sanity** — directory exists; `Contents/MacOS/$BINARY` exists
   where `$BINARY` = basename of app minus `.app`.
2. **Framework presence** — `Contents/Frameworks/Sparkle.framework/Sparkle`
   exists and `file` reports Mach-O.
3. **rpath set** — `otool -l <binary>` contains `LC_RPATH` entry
   `@executable_path/../Frameworks`.
4. **`@rpath` deps resolve** — for each `@rpath/<rel>` in `otool -L <binary>`,
   assert `Contents/Frameworks/<rel>` exists.
5. **SwiftPM resource bundles shipped** — if `BIN_PATH` resolves and contains
   `*.bundle` entries, every such bundle must also exist at
   `Contents/Resources/<name>`. Orphan `Contents/Resources/*.bundle` entries
   that don't match a build-dir bundle log `[WARN]` (possibly stale copy) but
   don't fail. When `BIN_PATH` is empty, skip with a `[WARN]`.
6. **Codesign** (skipped when binary is unsigned — detect via non-zero
   `codesign -dv` exit) — `codesign --verify --deep --strict --verbose=2 <app>`
   exits 0.
7. **Smoke (`--smoke` only)** — capture PID: `open -gn "$APP"`, then poll
   `pgrep -nf "$APP/Contents/MacOS/$BINARY"` to capture the freshly-spawned
   PID. Poll 0.5 s × 10 = 5 s; success = same PID observed alive at the end.
   Cleanup trap kills **only the captured PID** via `kill "$PID"` (never
   `pkill -f`, which would kill a user's dev instance of the same app).
   Limitations documented inline: a hung launch also "passes" this check —
   smoke catches crashing-at-main(), not functional correctness.

**Output style:**
```
[OK]   Contents/MacOS/Sokki is Mach-O
[OK]   Sparkle.framework embedded
[OK]   LC_RPATH @executable_path/../Frameworks present
[OK]   @rpath deps resolve (1 checked)
[OK]   SwiftPM resource bundles shipped (0 checked)
[WARN] spctl rejects pre-notarization (expected)
[OK]   codesign --verify --deep --strict passed
```

### 6.2 `Scripts/package_app.sh` additions

Insert **after the Sparkle embed block** (currently ends line 94):

```bash
# ---- Auto-copy SwiftPM resource bundles ----
# SwiftPM targets declaring `resources:` emit <Pkg>_<Target>.bundle next to the
# binary. Bundle.module resolves to this folder at runtime. Shipping without it
# = fatalError at first use. Each copied bundle gets its own codesign pass to
# match Sparkle's inside-out pattern — outer `codesign --force` does not re-sign
# nested bundles that contain Mach-O helpers, which would fail notarization.
BIN_PATH=$(swift build --show-bin-path -c release)
mapfile -t SPM_BUNDLES < <(compgen -G "${BIN_PATH}/*.bundle" || true)
for bundle_path in "${SPM_BUNDLES[@]}"; do
    bundle_name=$(basename "${bundle_path}")
    echo "[COPY] ${bundle_name}"
    cp -R "${bundle_path}" "${CONTENTS}/Resources/"
    codesign --force --options runtime --timestamp \
             --sign "${SIGN_IDENTITY}" \
             "${CONTENTS}/Resources/${bundle_name}" 2>/dev/null || \
        codesign --force --timestamp \
                 --sign "${SIGN_IDENTITY}" \
                 "${CONTENTS}/Resources/${bundle_name}"
done
```

`compgen -G` returns empty on no match without tripping `set -e` and avoids
mutating global `shopt` state. Two-phase codesign: resource-only bundles
don't accept `--options runtime`, so we retry without it.

Insert **before the signing block** (currently starts ~line 144):

```bash
# ---- Pre-sign structural verification ----
./Scripts/verify_app.sh "${APP_BUNDLE}"
```

Insert **after the final `codesign --verify`** (currently line 175) — **keep**
the existing inline `codesign --verify` as belt-and-suspenders (zero cost,
catches a verifier bug):

```bash
# ---- Post-sign structural + codesign verification ----
./Scripts/verify_app.sh "${APP_BUNDLE}"
```

### 6.3 `Scripts/release.sh` additions

Insert **after `xcrun stapler staple "${DMG_PATH}"`** (line 78), before
distribution zip creation (line 82):

```bash
echo "Launch smoke test on stapled app..."
./Scripts/verify_app.sh "${APP_BUNDLE}" --smoke
```

### 6.4 Error handling

- `set -euo pipefail` at top of `verify_app.sh`.
- Trap-based cleanup for smoke test (kills any Sokki process the script spawned).
- Non-fatal `codesign -dv` probe to detect unsigned binary without aborting
  (so the script works pre-sign).
- Every `[FAIL]` prints the observed vs. expected state in one line for quick
  diagnosis.

## 7. Files Changed

- `Scripts/verify_app.sh` — new; standalone verifier with 7 checks + optional
  smoke test.
- `Scripts/package_app.sh` — add SwiftPM-resource-bundle auto-copy block;
  invoke verifier pre-sign and post-sign; drop the now-redundant inline
  `codesign --verify`.
- `Scripts/release.sh` — invoke `./Scripts/verify_app.sh --smoke` after
  stapling the DMG, before creating the distribution zip.

## 8. Verification

Mapping to Acceptance Criteria:

1. **[AC1]** Run `./Scripts/package_app.sh` on clean checkout — expect exit 0
   with two `[OK]` blocks (pre-sign, post-sign).
2. **[AC2]** Run verifier directly against a manually-mangled copy:
   `cp -R Sokki.app /tmp/t.app && rm -rf /tmp/t.app/Contents/Frameworks/Sparkle.framework && ./Scripts/verify_app.sh /tmp/t.app`.
   Expect exit 1, `[FAIL] Sparkle.framework missing`.
3. **[AC3]** Run verifier against the raw build artifact **before** rpath
   patching: `cp .build/release/Sokki /tmp/unpatched && ./Scripts/verify_app.sh`
   on a bundle whose `MacOS/Sokki` is the unpatched binary. Expect exit 1,
   `[FAIL] LC_RPATH @executable_path/../Frameworks not set`. (Exercises the
   check without mutating the real packaging path.)
4. **[AC4]** Add a throwaway SwiftPM dep with resources to `Package.swift`,
   rebuild. Expect `[COPY] <name>.bundle` log line, pass.
5. **[AC5]** After AC4, `rm -rf` the bundle from `Contents/Resources/` and
   re-run `verify_app.sh`. Expect `[FAIL] <name>.bundle referenced by binary
   but missing from Contents/Resources/`.
6. **[AC6]** Run `./Scripts/verify_app.sh build/Sokki.app` against a past
   stapled build. Expect exit 0, all `[OK]`.
7. **[AC7]** Kick off `bun release patch` and assert the stapled-app smoke
   line runs; force-fail by injecting a broken binary and confirm the release
   aborts before zip.
8. **[AC8]** `time ./Scripts/verify_app.sh Sokki.app` < 5 s; with `--smoke` < 10 s.
9. **[AC9]** `which` every tool used is under `/usr/bin`, `/usr/libexec`,
   `/usr/bin/xcrun` provided by Command Line Tools — no brew, no npm.
