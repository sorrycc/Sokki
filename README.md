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

Produces signed + notarized + stapled `.zip` / `.dmg` / `.sha256` in `build/`.

```bash
bun release major            # 0.0.0 -> 1.0.0: build, notarize, commit + tag v1.0.0 (local)
bun release patch --push     # bump patch, ship, push commit and tag to origin
bun release --help
```

Takes 3–5 minutes (Apple's notary service is the bottleneck). arm64-only.

### One-time setup

Register the notary keychain profile once per machine. Get the app-specific
password from <https://appleid.apple.com> → Sign-In and Security → App-Specific
Passwords.

```bash
xcrun notarytool store-credentials sokki-notary \
  --apple-id <your-apple-id> \
  --team-id KU8S35TEW8 \
  --password <app-specific-password>
```

## Docs

- `docs/designs/` — design specs
