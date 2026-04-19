# Sokki — Ideas

Collected 2026-04-19 via 8 parallel agents, each applying a different lens to the
question:

> *What's the single smartest and most radically innovative and accretive and
> useful and compelling addition you could make to Sokki at this point?*

32 ideas total (4 per lens). Each idea includes pitch, implementation sketch,
accretion thesis, and risk/cost.

## Lenses

| File | Lens | Representative idea |
|---|---|---|
| [01-ai.md](./01-ai.md) | AI / LLM integration | Inline AI rewrites via trigger postfix |
| [02-ux.md](./02-ux.md) | UX polish, delight, onboarding | "Why didn't it fire?" inspector |
| [03-power-user.md](./03-power-user.md) | Power-user automation & scripting | Shell-script snippets with stdin piping |
| [04-compete.md](./04-compete.md) | Competitive differentiation | On-device LLM rewrite triggers |
| [05-privacy.md](./05-privacy.md) | Privacy, local-first, E2EE sync | Verifiable local-only attestation |
| [06-platform.md](./06-platform.md) | Deep native-macOS integration | Apple Foundation Models smart snippets |
| [07-growth.md](./07-growth.md) | Distribution, community, virality | `sokki://` one-click snippet packs |
| [08-engineering.md](./08-engineering.md) | Engineering excellence & reliability | Per-app injection profiles w/ snapshot oracle |

## Cross-lens patterns

Several ideas recur across lenses — these are the strongest signal:

- **On-device LLM rewrites** (AI, Compete, Platform) — trigger-driven inline
  rewriting via Apple Foundation Models. Only a native macOS-only app can ship
  this free and private; kneecaps TextExpander's paid tier.
- **Spotlight-style trigger palette** (UX, Power) — global hotkey fuzzy search
  with live preview; reuses existing injection stack, solves the
  "I have 200 snippets, can't remember any" problem.
- **App / context scoping** (Power, Compete, Platform) — same trigger expands
  differently per frontmost app / Focus mode / URL host, via AX + Focus Filter
  extension. Polymorphic triggers create library lock-in.
- **Snippet-pack distribution** (Growth, Compete) — URL-scheme install links
  + git-native team packs; every shared pack becomes a distribution beacon.
- **Self-writing snippets** (AI, Growth) — on-device n-gram mining of the user's
  own typing surfaces candidate triggers weekly. Inverts the cold-start problem.
- **Shell / Shortcuts / App Intents execution** (Power, Platform, Compete) —
  triggers that run code or Shortcuts and inject the result. Turns cursor into
  a command line.
- **Reliability moat** (Engineering, Privacy) — tap-thread watchdog, replay
  debugger, per-app snapshot oracle, verifiable-local-only attestation. Less
  flashy, higher long-term trust.

## Picking one

If you can ship exactly one first:

- **Fastest wow**: Inline AI rewrites (AI #1 / Compete #1 / Platform #1).
- **Highest leverage for library growth**: Self-writing snippets (AI #3).
- **Deepest moat**: Per-app injection profiles with snapshot oracle (Engineering #3).
- **Fastest distribution loop**: `sokki://` install links (Growth #1).
- **Table-stakes reliability**: Tap-thread watchdog (Engineering #1) — do this
  regardless; it's a week of work and prevents silent-daemon failure modes.
