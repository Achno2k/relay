# Round 6: loose ends + live tool-call glitch

Read first: `../../../AGENTS.md`, `../../api.md` (especially "Live reply"), `../round-5/live-typing-report.md`, `../round-5/bridge-harden-report.md`.

| Session | Owns |
|---|---|
| tests-cleanup | `bridge/Tests/**` (test support: FakeHerdr etc.), plus the attachment-name sanitising code in the bridge for QA-2 |
| live-tool-bridge | `bridge/Sources/RelayCore/LiveReply*.swift`, its tests, api.md "Live reply" |
| live-tool-ios | `ios/Relay/Features/Chat/` (LiveReplyView, the tool rows), `ios/Relay/Stores/` live-reply handling, their tests |

Rules (same as round 5):
- Stay in your own paths and commit only them (never `-a`/`-A`). Coordinate through herdr (`herdr agent prompt <name> …`). The lead is `herd-native`.
- The bridge is LaunchAgent `com.relay.bridge` on 7878. Only live-tool-bridge rebuilds and kickstarts it; announce that first. tests-cleanup runs tests only.
- Test agents are in herdr workspace `herd-e2e` (cwd `~/.relay/e2e`): w14:p2 claude, w14:p4 pi, w14:p5 codex. Never touch the user's other agents.
- The iPhone is 00008110-000414D91422801E (`TEAM=TC56945264`, `Relay/Relay-FreeTeam.entitlements`). **Ask herd-native before taking it over**: the user may be using it.
- Write a report at the end to `docs/tasks/round-6/<session>-report.md`: what changed, how it was verified, what's left.

Commit trailer:
```
Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Q4qo2QabVUAvi2YgczSwpD
```
