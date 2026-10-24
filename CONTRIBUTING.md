# Contributing

## Development

```sh
scripts/dev.sh                                      # Sokki rebuilt as sources change, beside the installed Sokki
go test ./...                                       # all tests; the views run headless
go test -short ./...                                # without the tests that touch the pasteboard
SOKKI_SHOTS=/tmp/shots go test ./cmd/sokki          # screenshots of the views
go vet ./... && go tool mygo vet                    # vet, and MyGo's check of build values kept past their frame
go tool mygo build                                  # build/darwin-arm64/Sokki.app and a .dmg, signed ad hoc
```

`scripts/dev.sh` keeps its matches in `~/.config/sokki-dev` (`SOKKI_CONFIG_DIR`), so that it never expands the
triggers of the Sokki you use. It runs a bare binary, `.mygo/dev/sokki`, rebuilt and restarted as sources
change, rather than `go tool mygo dev`: macOS 26 keeps the menu bar icon of MyGo's dev bundle hidden. So in
development the terminal's Accessibility permission applies, Open at Login doesn't work, and System Settings →
Menu Bar lists it as "sokki".

## Releasing

Write the release notes under `## Unreleased` in `CHANGELOG.md`, as [AGENTS.md](AGENTS.md) describes, and commit them, then:

```sh
scripts/bump.sh patch    # or minor, major, or a version such as 0.2.0
```

It sets the version in `mygo.json`, renames `## Unreleased` to `## <version>` in `CHANGELOG.md`, commits and tags
`v<version>`, and after asking pushes `main` and the tag. The [Release workflow](.github/workflows/release.yml)
then builds Sokki (arm64), signs it with the Developer ID, has it notarized, uploads the disk image and the signed
update to the release of the tag on [sorrycc/Sokki](https://github.com/sorrycc/Sokki), and publishes it. Installed
apps check for it once a day, and from **Check for Updates…** in the menu bar; the update window shows the
changelog section.

The first release of the Go rewrite is `scripts/bump.sh minor`, 0.0.16 to 0.1.0.

The workflow writes the release with its own `GITHUB_TOKEN` and needs these secrets in sorrycc/Sokki:

| Secret | What |
|---|---|
| `MYGO_UPDATER_PRIVATE_KEY` | `mygo-update.key` of `go tool mygo keygen`, whose public key is in `mygo.json` |
| `MACOS_CERTIFICATE` | the Developer ID Application certificate and its key, as a base64 `.p12` |
| `MACOS_CERTIFICATE_PASSWORD` | the password of the `.p12` |
| `APPLE_ID`, `APPLE_TEAM_ID` | the Apple ID of the developer account, and its team ID (`KU8S35TEW8`) |
| `APPLE_APP_PASSWORD` | an app-specific password of the Apple ID, from account.apple.com |
| `TELEMETRY_ENDPOINT_URL`, `TELEMETRY_INGEST_KEY` | where telemetry goes and its HMAC key; without them the build sends none |

Sokki's update key was made with `go tool mygo keygen -o "~/Library/Application Support/mygo/update-keys/sokki"`.
Keep `mygo-update.key` in a password manager: installed apps only accept updates signed with it.

Local builds stay signed ad hoc; only the workflow signs with the Developer ID.
