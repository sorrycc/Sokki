# Privacy, Local-First, and Trust

## Idea 1: Verifiable Local-Only Attestation

**Pitch:** Ship a cryptographic proof, signed into every release, that Sokki has zero network code paths for keystroke data. Users (and their IT/security teams) can audit this claim in one click — not read a marketing page and trust it.

**How it works:**
- Build-time `spctl`/`codesign` manifest enumerates every linked framework; CI fails if `Network.framework`, `URLSession`, `NWConnection`, or any socket API appears outside a whitelisted `SyncEngine` module.
- Release pipeline produces a signed SBOM + SHA256 of every binary, published to a transparency log (Sigstore-style) and pinned in-app.
- "About Sokki" shows a live badge: *No network symbols linked · Verified build abc123 · [verify yourself]* with a copy-pasteable `otool -L` one-liner.
- Sync, when enabled, lives in a separately-notarized helper XPC process so the main tap binary stays provably airgapped. Revoking sync = deleting a file.
- Reproducible builds so a paranoid user can rebuild from tag and byte-match the shipped binary.

**Why it's accretive:** This is the *moat*. Every competitor (TextExpander, Raycast, aText) says "we respect privacy." Sokki *proves* it — the one text expander a CISO can approve without a SOC 2 audit, because the binary audits itself. It also forces internal discipline: you can never sneak telemetry in.

**Risk / cost:** Reproducible Swift builds on macOS are non-trivial (Xcode version pinning, timestamp scrubbing). Transparency-log infra is ~1 week. Hardest cost is the self-imposed constraint — you can never add "anonymous analytics."

---

## Idea 2: Redaction Mode for Screenshares

**Pitch:** A systemwide hotkey that swaps every snippet trigger for its replacement live in the UI, but during screen recording / Zoom share, expansions render as visual placeholders (`[email]`, `[api-key]`) on the shared stream while the real value still gets typed into the focused app.

**How it works:**
- Detect active `ScreenCaptureKit` sessions via `CGDisplayStreamCreate` observers + `com.apple.screencapture` activity.
- On detection, switch each match to a per-trigger "display alias" (user-defined, e.g. `;pw` → real password typed, but `•••password•••` briefly flashes in Sokki's HUD that *is* on-screen).
- Optional: classify matches as `sensitive: true` in YAML; those silently expand with no HUD, no sound, no menu-bar flash during shares.
- Integrates with macOS 15 "Presenter Preview" — hide Sokki icon entirely during screen share.
- Audit log entry: "12:03 screen-share detected; 3 sensitive expansions suppressed from HUD."

**Why it's accretive:** Every developer has leaked a token on a Loom recording. This makes Sokki the *only* expander safe to leave on during demos, support calls, and pair-programming. Enterprise will pay for this single feature.

**Risk / cost:** ScreenCaptureKit detection is heuristic (no public API tells you "I'm being recorded by Zoom"). False negatives are dangerous — need a conservative "any capture session anywhere = redact" default. ~2 weeks.

---

## Idea 3: CRDT E2EE Sync via Signal Protocol

**Pitch:** Per-device keypairs, pairwise Double-Ratchet sessions, matches stored as an append-only CRDT log encrypted client-side. iCloud/Dropbox/Syncthing/USB stick are all just dumb transport — they see ciphertext. No Sokki server, ever.

**How it works:**
- First launch generates a Curve25519 identity keypair in Secure Enclave (non-exportable, biometric-gated).
- Pair devices via QR code (Signal-style safety number); creates a pairwise X3DH session, devices exchange ratcheting keys.
- Each match edit = a CRDT op (LWW-map keyed by `(device_id, lamport)`); op is encrypted per-device and dropped into `~/Library/Mobile Documents/.../sokki-ops/`.
- iCloud Drive is the transport — Apple sees opaque blobs. Family Sharing / work accounts irrelevant. Users who distrust iCloud point it at a local Syncthing folder instead; zero code change.
- Per-match "vault" flag: vaulted matches require a YubiKey touch (via `TKTokenKeychainItem` / CTAP2) to decrypt before expansion. The match literally cannot be read from disk without the hardware key present.

**Why it's accretive:** CloudKit would be the easy answer but CloudKit keys are Apple-held — not end-to-end encrypted in the threat model that matters. Shipping Signal-grade sync on top of dumb storage means *you* can't be subpoenaed, because you have nothing. Transport-agnostic = works on air-gapped networks, enterprise-proxied Macs, whatever. The YubiKey vault tier is the killer feature for anyone with prod credentials in snippets.

**Risk / cost:** Key rotation, device revocation, and "I lost my phone" recovery are the hard problems of E2EE — get this wrong and users lose data forever. ~6-8 weeks. Requires a well-tested CRDT (Automerge-swift or Yrs binding). YubiKey integration is another ~1 week.

---

## Idea 4: MDM-Deployable Signed Policy Bundles

**Pitch:** Enterprises push a code-signed `.sokkipolicy` bundle via Jamf/Kandji that pins org-approved snippets, forbids sensitive-match sync, and can be verified by the employee as not-a-keylogger. IT gets control; employee gets cryptographic proof IT isn't snooping.

**How it works:**
- Policy = YAML + signature by org's Sokki signing key, installed to `/Library/Managed Preferences/com.sokki.policy`.
- Schema declares: allowed match namespaces (`corp:*`), forbidden variable types (no `{{clipboard}}` in policy-managed matches), sync destinations (allowlist/denylist iCloud, Syncthing paths), redaction-mode-always toggles.
- Sokki menu bar shows "Managed by Acme Corp" with a [inspect policy] button — employee sees the exact YAML IT pushed, cryptographically signed, human-readable.
- Policy *cannot* add keystroke-logging hooks, exfil URLs, or read personal matches — the schema is a hard allowlist, enforced by Sokki, not by IT's goodwill.
- Audit log is written to a user-owned file that IT cannot read unless the user opts in (signed attestation, not silent telemetry).

**Why it's accretive:** This is the enterprise wedge. IT departments currently block TextExpander because they can't audit what leaves the machine. A signed, schema-constrained policy bundle + provable no-exfil (see Idea 1) = the first text expander approvable by a paranoid CISO *and* by paranoid employees in a zero-trust workplace. Neither side has to trust the other — they trust the signed schema.

**Risk / cost:** Requires a well-designed policy schema you can't easily evolve later (v1 decisions are forever). MDM distribution testing needs real Jamf/Kandji environments. ~3-4 weeks, plus ongoing schema stewardship. The bigger cost: you're now a compliance product, which changes your roadmap forever.

---

**Ranking:** Idea 1 first (it's the philosophical spine every other idea hangs from — without provable local-only, the others are just vibes). Then Idea 2 (highest delight-per-week-of-work, creates word-of-mouth). Then Idea 3. Idea 4 only after you have paying enterprise pilots asking for it — building it on spec is YAGNI.
