# usage: subscription usage limits screen — report

## What changed

**Research** (`docs/tasks/round-5/usage-research.md`, sent to the lead before building):
- Claude: `claude -p "/usage" --output-format stream-json --verbose` — a free local command with structured `usage_report.rate_limits.limits[]` on the assistant message. `/status` doesn't work in print mode; `/usage` does.
- Codex/pi: `codex app-server`, JSON-RPC `account/rateLimits/read`. pi shares the same `openai-codex` OAuth account (confirmed via `pi auth check`), so one call covers both — no separate pi card.

**Bridge** (`bridge/Sources/RelayCore/Usage*.swift`, new):
- `UsageModels.swift`: `UsageWindow`/`UsageProvider`/`UsageSnapshot`.
- `UsageSources.swift`: the two process probes.
  - `CodexUsageProbe`: spawns `codex app-server`, sends `initialize` + `account/rateLimits/read` over stdio, reads until the matching response line or a watchdog timeout, always reaps (SIGTERM → wait → SIGKILL → `waitUntilExit`).
  - `ClaudeUsageProbe`: runs `claude -p "/usage" --output-format stream-json --verbose --no-session-persistence` from a dedicated `~/.relay/usage-probe` cwd, plus `claude auth status --json` for the plan name. Same watchdog/reap pattern.
- `UsageParsers.swift`: pure parsing (no I/O) of both raw payloads into `UsageProvider`.
- `UsageMonitor.swift`: actor `Service`. Seeds the cache once at startup regardless of subscribers; afterwards polls only while `hub.count > 0`. Backoff per provider: 60s base, doubles to a 600s cap when a poll's result is unchanged, resets on change. `requestRefresh()` (manual pull-to-refresh) bypasses backoff but is throttled to once per 15s. `snapshot()` re-derives `stale` (>15 min old) at read time; `GET /usage` only ever reads this cache, never fetches live.
- `Server.swift`: `GET /usage`, `POST /usage/refresh` (`429 rate_limited` when throttled).
- `Models.swift`: `ServerEvent.usageUpdated`, broadcast once per provider whose snapshot actually changed.
- `RelayApp.swift`: wires `UsageMonitor` into the service group.
- `api.md`: "Usage" section, `UsageProvider`/`UsageWindow` models, REST rows, WS event row. `docs/fixtures/usage.json`.

**iOS**:
- `RelayKit/Models.swift`: `UsageWindow`/`UsageProvider`/`UsageSnapshot`, `ServerEvent.usageUpdated`.
- `RelayKit/APIClient.swift` + `Backend.swift`: `usage()` / `refreshUsage()`.
- `Relay/Stores/UsageStore.swift` (new): `load()` (`GET /usage`), `refresh()` (pull-to-refresh; treats `429` as "already fresh," not an error), `apply(_:)` for pushed updates. **No socket of its own** — per the lead's direction, `AppStore` forwards `.usageUpdated` from its one `/ws` connection into `usage.apply(_:)`, and calls `usage.load()` on every `refresh()` (app foreground, reconnect). `RelayState`'s reducer and `MockBackend`'s replay both fold `.usageUpdated` into their existing no-op case — usage state deliberately isn't part of `RelayState`.
- `Relay/Features/Usage/UsageView.swift` (new): one glass card per provider — label, plan, a ring + "Resets in Xh/Xd" + percent per window, "Updated x ago", dimmed + a reason string when stale/unavailable. Pull-to-refresh.
- `Relay/Features/Sidebar/SidebarView.swift`: a "Usage" row (`gauge` icon) in the existing "···" menu, next to Connected/Unpair.
- `Relay/App/MainView.swift`: sheet presentation; `-demo usage` launch flag for screenshots, alongside the existing `-demo newChat` etc.
- Mock: `MockBackend` serves `docs/fixtures/usage.json` and fakes a refresh (bumps percentages, broadcasts `usage.updated`).

## Shared files touched outside my paths

Per README's rule ("minimal hooks elsewhere, tell the owner") and confirmed live with both owners before committing:
- `bridge/Sources/RelayCore/{Server,RelayApp,Models}.swift` — bridge-harden (flagged mid-session; agreed I'd commit these myself and name them).
- `ios/Relay/Stores/{AppStore,RelayState}.swift`, `ios/Relay/Mock/MockBackend.swift`, `ios/Relay/App/{MainView,AppModel}.swift`, `ios/Relay/Features/Sidebar/SidebarView.swift`, `ios/RelayTests/{StoreTests,ConnectionHardeningTests}.swift` — ios-harden/ios-polish (both idle/finished for round 5; lead confirmed their paths were free and I should commit + name them). Also settled the design question with the lead: `UsageStore` has no socket of its own — `AppStore` forwards `usage.updated` from its existing connection instead of opening a second one.

## Verified

- **Bridge**: `cd bridge && swift test` — 205/205 pass, including 15 new (`UsageParsersTests`, `UsageMonitorTests`, `UsageRoutesTests` in `UsageTests.swift`): parsing both payload shapes, missing/malformed data, gating on subscriber count, exponential backoff, manual-refresh throttling, stale re-derivation, `GET /usage`/`POST /usage/refresh` routes and auth.
- **No session pollution**: ran `claude -p "/usage" --no-session-persistence` repeatedly from `~/.relay/usage-probe`; `~/.claude/projects/-Users-you--relay-usage-probe/` stays empty (no transcript files) across polls.
- **Process reaping**: `ps aux | grep -E "codex app-server|claude -p"` empty after every poll cycle, including after a timeout-forced kill.
- **Live check on this Mac**: ran the debug bridge on a scratch port/`RELAY_HOME` (never touched the real `com.relay.bridge` on 7878). `GET /usage` returned real Claude (11%/11%, plan Max) and Codex (39%, plan Free) data; `POST /usage/refresh` returned `202` then `429` on an immediate second call; a `/ws` client received real `reply.live` traffic confirming the socket path works.
- **iOS**: `xcodebuild test` on iPhone 17 Pro simulator — 90/90 `RelayTests` pass, including 6 new `UsageStoreTests` (load, error mapping, refresh + 429 handling, `apply` upsert, fixture decode).
- **Screenshots**: `-mock -demo usage -agent w2:p1`, light and dark, both attached to this session — two glass cards (Claude, Codex / pi), rings colored green at these percentages, "Resets in 1d"/"in 5d", "Updated now".

## Left open

- No UI test drives the sidebar "Usage" menu item specifically (covered by code review + the screenshot flow instead); RelayUITests wasn't touched.
- `UsageWindow.windowMinutes` is populated for Codex but always `nil` for Claude (the source doesn't report a window length) — the countdown UI works fine off `resetsAt` alone, but a future Claude API change that adds it would need no UI change.
- iOS device run (physical iPhone) not done — simulator only, per the round-5 rule's fallback ("otherwise use the simulator and say so").
