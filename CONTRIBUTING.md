# Contributing

## Setting up a fresh clone

Sokki needs two gitignored local files to build + release:

- `.env` — R2 credentials for `bun release --upload`
- `Scripts/telemetry.config` — telemetry endpoint + HMAC key (without it, the packaged app ships with no-op telemetry)

If you already have a working Sokki clone on this machine, copy them over:

```bash
Scripts/sync_local_secrets.sh /path/to/existing/sokki
```

Otherwise, fill them in by hand from `.env.example` and `Scripts/telemetry.config.example`.

One-time machine setup (notary profile, Sparkle EdDSA key) lives in `README.md` and is shared across clones via Keychain — no per-clone work needed.
