# usage-pi: every subscription pi uses — report

## What changed

**Phase 1 research** (`docs/tasks/round-5/usage-pi-research.md`, sent to herd-native before building):
- pi's `openai-codex` login is confirmed the same ChatGPT OAuth account Codex uses.
- pi's `anthropic` login is a separate token store from Claude Code's own (pi's own `/login` flow) — no shared account id to compare, and it's currently `invalid` on this Mac, so it's checked live via `pi auth check`, never assumed.
- OpenCode Go (`opencode-go`) is a flat API-key plan (not OAuth), with no documented usage/quota API. Stopped at that per the brief's stop condition — no key read, no undocumented endpoint called.
- Go-ahead from herd-native: ship the OpenCode Go card without numbers, `usedBy` from `pi auth check` (only `"ready"` counts), no credential reads.

**Bridge** (`bridge/Sources/RelayCore/Usage*.swift`):
- `UsageModels.swift`: `UsageProvider` gets `usedBy: [String]` (default `[]`).
- `UsageSources.swift`: new `PiAuthProbe` — spawns `pi auth check --provider <id> --json`, returns `isReady(provider:)`. Never reads the credential, only its validity.
- `UsageParsers.swift`: `parseCodex`/`parseClaude` take `piReady: Bool` and add `"pi"` to `usedBy` when true; `codex`'s label changed from "Codex / pi" to "ChatGPT" (subscription-named, per-provider `usedBy` now carries the "who" instead of the label). New `openCodeGoProvider(piReady:now:)` — builds the calm no-data card from `pi auth check` alone, returns `nil` (card omitted, not shown empty) when pi isn't authenticated to `opencode-go`.
- `UsageMonitor.swift`: `pollAll` runs three `pi auth check` calls (openai-codex, anthropic, opencode-go) alongside the existing two probes, gated the same way (only while a `/ws` client is connected); `opencode-go` isn't rate-limited/backed-off like the others since there's nothing to fetch — it's dropped from the cache outright when pi loses that credential. `snapshot()` never marks `opencode-go` stale. `recordFailure` now preserves the previous `usedBy` across a failed poll, same as it already did for `windows`.
- `api.md` "Usage": rewritten — per-subscription framing, `usedBy` semantics, the `opencode-go` card's shape and why it's `stale: false` with a permanent `unavailableReason`, `docs/fixtures/usage.json` now has all three providers.

**iOS**:
- `ios/RelayKit/Sources/RelayKit/Models.swift` (not my path — see below): `UsageProvider.usedBy: [String]`, decoded with `decodeIfPresent` defaulting to `[]` so a bridge without the field still decodes.
- `Relay/Features/Usage/UsageView.swift`: a "Used by" line under the plan name — small glyph + name per harness (reuses the existing `KindIcon` glyph mapping locally, since `KindIcon` itself lives in ios-polish's `Features/Chat/`). The no-data state (`windows.isEmpty && !stale && unavailableReason != nil`) gets its own calm treatment — an `info.circle` icon, full opacity, no "Stale" badge — distinct from a genuinely failed/stale poll, which keeps the existing dimmed card + orange "Stale" badge + `clock.arrow.circlepath` icon.
- `Relay/Stores/UsageStore.swift`: unchanged — it already forwards whatever `UsageProvider` shape the backend returns.

## Shared files touched outside my paths

- `ios/RelayKit/Sources/RelayKit/Models.swift` — owned by ios-harden. Flagged the need in `docs/qa/requests.md`; herd-native confirmed ios-harden was finished for the round and cleared me to make the one-field, additive/backward-compatible edit myself. Named here and in `docs/qa/requests.md` (marked done).

## Verified

- **Bridge**: `cd bridge && swift test` — 211/211 pass, including 8 new/changed in `UsageTests.swift` (`parsesCodexWithPiJoined`, `parsesClaudeWithPiJoined`, `openCodeGoProviderNilWhenPiNotReady`, `openCodeGoProviderShapeWhenPiReady`, `piJoinsUsedByWhenReady`, `openCodeGoCardAppearsOnlyWhenPiIsReady`, plus updated existing tests for the new label/`usedBy` fields). All `UsageMonitor`-constructing tests now pass an explicit stub `PiAuthProbe` so the suite never shells out to a real `pi` binary.
- **iOS**: `xcodebuild test -scheme Relay -destination 'id=<iPhone 17 Pro sim>'` — 95/95 unit tests pass, including 2 new in `UsageStoreTests.swift` (contract-fixture decode now checks all three providers + `usedBy`; a raw-JSON decode test confirms `usedBy` defaults to `[]` when the key is absent, i.e. an old bridge payload still decodes). Ran on the iPhone 17 Pro simulator (`iPhone 17 Pro` booted device, not the physical iPhone — no Xcode account configured for device deployment on this Mac).
- **Live check on this Mac**: built and ran a scratch bridge instance (`RELAY_HOME=/tmp/relay-usage-pi-scratch swift run relay serve --port 17878 --local-only`, torn down after) — did **not** touch the live LaunchAgent on 7878, so no coordination needed with bridge-harden's soak. `GET /usage` returned real, live data:
  - `claude`: `usedBy: ["claude"]` — pi's `anthropic` login is genuinely `invalid` on this Mac right now, correctly excluded rather than assumed.
  - `codex`: `usedBy: ["codex", "pi"]`, label "ChatGPT" — confirmed shared ChatGPT OAuth.
  - `opencode-go`: present, `windows: []`, `stale: false`, `unavailableReason: "Usage not available from OpenCode"`, `usedBy: ["pi"]`.
- **Screenshots**: `docs/tasks/round-5/screenshots/usage-pi-{light,dark}.png`, via `-mock -demo usage` on the iPhone 17 Pro simulator. Confirms the "Used by" line renders (glyph + name, joined with "·"), the OpenCode Go card reads calm and settled (not dimmed, no warning triangle) in both appearances.

## Left open

- pi's `anthropic` OAuth login is currently invalid on this Mac, so the "pi joins the Claude card" path is exercised by unit tests (`piJoinsUsedByWhenReady`) but not by the live check above — worth a manual `pi /login` + re-check if anyone wants to see it live before shipping wider.
- No live/documented OpenCode Go usage API was found in Phase 1; if OpenCode ever publishes one, `openCodeGoProvider` is the single place to extend into a real fetch.
