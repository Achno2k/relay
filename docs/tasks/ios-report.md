# iOS report

## Layout
- `ios/project.yml` → `Herd.xcodeproj` (XcodeGen). Scheme `Herd`, bundle `dev.amansingh.herd`, iOS 26, Swift 6 strict concurrency.
- `ios/HerdKit/` is a local Swift package with the UI-free code: models, `APIClient`, `WSClient`, `Backend`/`LiveBackend`, `PairingStore` + Keychain. Future widget and Live Activity targets can import it.
- `ios/Herd/`: `App/`, `Stores/` (`HerdState` reducer, `AppStore`), `Features/{Sidebar,Chat,Composer,Approval,NewChat,Pairing}`, `DesignSystem/` (markdown, shimmer, indicators), `Mock/`.
- No third-party packages. The markdown renderer is a small block parser on top of `AttributedString`'s inline parsing.

## Verified
- `xcodebuild … build` from clean: succeeds with no compiler or asset warnings. The only `warning:` line is Xcode's stock `appintentsmetadataprocessor … No AppIntents.framework dependency found` log.
- `xcodebuild … test`: 22 Swift Testing tests pass. They cover fixture decoding (all five files), unknown block/status/event types, pairing-link parsing, REST/WS URL building and backoff, the reducer (`message.upserted` replace/insert/ignore, `agent.updated`, `agent.created`, `agent.closed`), sidebar grouping (blocked first), tool-row grouping and markdown blocks. The tests read `docs/fixtures` straight from the repo.
- Simulator with `-mock` (fixtures copied into Debug builds only, `ws-events.jsonl` replayed every 4s). Screenshots are in `docs/screenshots/`: sidebar, chat with tools collapsed/expanded, working state (stop button), approval sheet, approval card, new chat, pairing, dark mode.
- Live path, tested against a throwaway local HTTP stub serving the fixtures (not committed, since the bridge isn't finished):
  - pairing through a `herd://pair` link
  - token kept in the Keychain, URL in App Group defaults, both still there after relaunch
  - `Authorization: Bearer` on every REST call; ids encoded as `w1%3Ap2`
  - `/ws?token=` retried with backoff and the "Reconnecting…" banner showed (`live-reconnecting.png`)

## Debug launch args
- `-mock`: fixture backend.
- `-agent <id>`: open that agent.
- `-replay off`: don't replay events.
- `-demo sidebar|tools|card|top|newChat|pairing`: set up a screen for screenshots.
- `-pair <herd:// link>`: pair at launch. I added this because `simctl openurl` shows an "Open in Herd?" prompt I couldn't tap.
- All of these are DEBUG only.

## Known gaps
- Not run against the real bridge yet (the bridge is still in progress), so live WebSocket frames are untested end to end. The decoding is covered by tests.
- The QR scanner (`DataScannerViewController`) needs a device camera. On the simulator it shows the "Camera unavailable" state.
- Keychain/App Group: the entitlements list `group.dev.amansingh.herd` as an app group and keychain access group. Unsigned simulator builds lack the entitlement, so `Keychain` falls back to the default group on `errSecMissingEntitlement`. On a device you need a team and the App Group registered.
- I couldn't drive touch in the simulator, so these were checked by reading the code, not by gesture:
  - edge-swipe/drag of the drawer
  - "↓" button appearing when scrolled up (only the "hidden at bottom" case was checked)
  - send → stop morph animation
  - haptics
- "Worked for 42s" only appears for messages the app watched grow over the socket this session. Older tool runs say "Used 3 tools", because the contract has no timings (see Q1).
- "Thought for a few seconds" is fixed text for the same reason.
- The pulsing dot is hidden when the last row is a live "Running X…" tool row, so two "working" indicators don't stack.
- Prompts show as a dimmed optimistic bubble until a transcript user message with the same text arrives (see Q3).
- Rename in the sidebar context menu is disabled (as the spec says, "later").
- ATS: `NSAllowsArbitraryLoads` is on, because the bridge is plain HTTP over Tailscale.
- "Unseen" (blue dot) is tracked on the phone per agent (`updatedAt` of the last view).

## Contract questions (api.md unchanged)
1. Tool timing: could `toolCall`/`toolResult` carry `createdAt` (the transcript has per-line timestamps)? Then "Worked for Ns" works for history too. The same goes for thinking duration.
2. `hasTranscript: false` fallback messages: are the synthetic ids stable across reads? If they change on every screen read, every `message.upserted` adds a new message instead of replacing one.
3. `POST /prompt`: can the `202` body return anything that ties the prompt to the transcript message it becomes? The app matches on exact text now, which breaks if Claude Code rewrites the prompt (for example pasted images or `@file` expansion).
4. `/approval` when an agent is blocked but the screen doesn't parse: 204, or 200 with `options: []` and the raw question? The app treats 204 as "nothing to show" and falls back to the composer.
5. WebSocket: does the bridge always send `hello` first? The app counts any frame as "connected". It also sends a ping every 15s; please make sure the server answers pings (Hummingbird does by default).
6. `POST /agents` takes `name`, but the new-chat sheet doesn't expose it. Should it?
7. Should `GET /agents` include agents with `status: unknown`? The app shows them without an indicator.
