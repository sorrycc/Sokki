# Sokki

macOS 14+ menu bar text expander. (速記 — "shorthand".)

## Build

```bash
swift build                  # debug
swift build -c release       # release
./Scripts/package_app.sh     # bundle Sokki.app (Developer ID signed)
```

Then `cp -r Sokki.app /Applications/`.

## Release

Produces signed + notarized + stapled `.zip` / `.dmg` / `.sha256` in `build/`,
optionally EdDSA-signs the ZIP and publishes `appcast.xml` + artifacts to R2 so
installed copies auto-update.

```bash
bun release patch                       # 0.0.1 -> 0.0.2: build, notarize, commit + tag (local)
bun release patch --push                # also push commit + tag to origin
bun release patch --push --upload       # full publish: also upload to R2 + update appcast
bun release --help
```

Takes 3–5 minutes (Apple's notary service is the bottleneck). arm64-only.

### One-time notary setup

Register the notary keychain profile once per machine. Get the app-specific
password from <https://appleid.apple.com> → Sign-In and Security → App-Specific
Passwords.

```bash
xcrun notarytool store-credentials sokki-notary \
  --apple-id <your-apple-id> \
  --team-id KU8S35TEW8 \
  --password <app-specific-password>
```

### One-time auto-update setup

Required before the first `bun release … --upload`. Steps (see
`docs/designs/2026-04-19-sparkle-r2-autoupdate.md` for full context):

1. `brew install --cask sparkle`
2. Migrate keychain so Shun's and Sokki's EdDSA keys coexist (back up Shun's
   private key first — losing it permanently bricks Shun's auto-updates):
   ```bash
   security find-generic-password -s "https://sparkle-project.org" -a "ed25519" -w
   SHUN_KEY=$(security find-generic-password -s "https://sparkle-project.org" -a "ed25519" -w)
   security delete-generic-password -s "https://sparkle-project.org" -a "ed25519"
   security add-generic-password -s "https://sparkle-project.org" -a "ed25519-shun" -w "$SHUN_KEY"
   "$(ls -d /opt/homebrew/Caskroom/sparkle/*/bin | sort | tail -1)/generate_keys"
   # Copy the printed public key into Scripts/sparkle.config (SPARKLE_PUBLIC_KEY)
   SOKKI_KEY=$(security find-generic-password -s "https://sparkle-project.org" -a "ed25519" -w)
   security delete-generic-password -s "https://sparkle-project.org" -a "ed25519"
   security add-generic-password -s "https://sparkle-project.org" -a "ed25519-sokki" -w "$SOKKI_KEY"
   # Back up $SOKKI_KEY to a password manager labelled "Sokki Sparkle EdDSA private key".
   ```
3. `cp .env.example .env` and fill in R2 credentials (same bucket as Shun;
   only `APP_KEY_PREFIX=sokki` differs).

### Smoke test (optional)

Exercises the whole upload pipeline without burning a notarization slot:

```bash
./Scripts/package_app.sh
mkdir -p build && ditto -c -k --keepParent Sokki.app build/Sokki-$(cat VERSION).zip
./Scripts/upload_r2.ts --version $(cat VERSION) --dry-run
```

## Docs

- `docs/designs/` — design specs
