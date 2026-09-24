# Round 5: polish, hardening, live typing

The goal is to make what exists solid. **No new features** beyond live typing.

Read first: `../../../AGENTS.md`, `../../api.md`, `../ios-report.md`, `../bridge-report.md`.

## Sessions and file ownership
Stick to your own paths. If you need a change in someone else's paths, write it into `docs/qa/requests.md` addressed to the owner, or ask them through herdr.

| Session | Owns |
|---|---|
| live-typing | `bridge/Sources/RelayCore/LiveReply*.swift` (new), `ios/Relay/Features/Chat/LiveReply*.swift` (new), minimal hooks elsewhere (tell the owner), api.md "Live reply" section |
| bridge-harden | everything in `bridge/` except live-typing's files; api.md error sections |
| ios-harden | `ios/Relay/Stores/`, `ios/RelayKit/`, `ios/Relay/App/` |
| ios-polish | `ios/Relay/Features/` (except live-typing's files), `ios/Relay/DesignSystem/`, assets |
| qa | none; read-only on code. Writes `docs/qa/` |

## Shared rules
- The bridge is the LaunchAgent `com.relay.bridge` on 7878. After a release build: `launchctl kickstart -k gui/$(id -u)/com.relay.bridge`. Only bridge-harden and live-typing rebuild it; say so in herdr before kickstarting, because it drops live clients for a second.
- Test agents live in herdr workspace `herd-e2e` (cwd `~/.relay/e2e`): w14:p2 claude, w14:p4 pi, w14:p5 codex. Create extra panes there only, and close them after. Never touch the user's other agents.
- Codex's default model is rejected with the ChatGPT login; keep w14:p5 on gpt-6-luna.
- Commit only your own paths (`git add <paths>`; never `-a`/`-A`). End every commit message with the trailer lines below.
- Tests must stay green: `cd bridge && swift test`, plus the iOS unit and mock UI tests on the iPhone 17 Pro simulator. The final UI run goes on the iPhone (id 00008110-000414D91422801E) if Xcode has an account; otherwise use the simulator and say so.
- Finish with a short report at `docs/tasks/round-5/<session>-report.md`: what changed, what was verified and how, and anything left open.

Commit trailer:
```
Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Q4qo2QabVUAvi2YgczSwpD
```
