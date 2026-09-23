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

## Round 1 (fixes-1.md, iOS items 1-4)

### Fixed
1. **Keyboard over the approval sheet.**
   - The keyboard is dismissed whenever the sheet presents.
   - The sheet sizes to its content with a `.height` detent measured from the content, so the gap above the buttons is gone.
   - Inline code in the question is regular weight on a lighter fill.
   - Screenshot: `approval-sheet.png`.
2. **Chats opening scrolled up.** There were two causes.
   - The transcript was a `LazyVStack`. Lazy rows are measured with estimates, so on long real transcripts the first bottom offset landed short. Reproduced on a real long chat. It's now an eager `VStack`, rendered only once messages have loaded, so the first layout already starts at the bottom. `MarkdownView` parses in `body`, so unchanged rows are skipped.
   - "At bottom" was worked out from scroll geometry alone. A programmatic scroll animation passes through "not at bottom" offsets, and the approval card or keyboard growing the bottom inset mid-animation turned following off. Now only the user's own scrolling (scroll phase tracking, interacting or decelerating) changes `followsBottom`. Any change to content height or bottom inset re-pins while following.
   - Earlier pages now load by scroll visibility and keep the position when prepended.
   - The first launch shows a spinner instead of flashing "No agents running".
3. **Step header.** `Approval.step` decodes as optional; the sheet shows "Question 2 of 3 · Time". Claude's final Submit tab (`index == count`, title `Submit`) is labelled "Review answers". Screenshots: `approval-step.png`, `approval-review.png`.
4. **Free text.**
   - A "Type something." option (`freeText: true`, or that exact label from older bridges) swaps the buttons for a text field.
   - Send posts the option's `keys` (arrow moves, per herd-bridge), then `POST /agents/:id/text` with `submit: true`.
   - Contract agreed with herd-bridge through `docs/api.md`: 155c21a from me, ef60451 from them.
   - Screenshot: `approval-free-text.png`.

### Tests
- Unit tests: 25 pass. New ones cover:
  - `step`/`freeText` decoding, plus the label fallback
  - the store's free-text answer order (keys, then text, trimmed) against a recording fake backend
  - a plain answer sending keys only
- `MockUITests` (new, no bridge needed), all pass:
  - a long, slow-loading chat opens at the bottom, including after switching agents in the sidebar
  - the keyboard is gone when the sheet presents from the card, and the step header is shown
  - a free-text answer reaches the (mock) agent
- The mock never reproduced the lazy-stack bug, so the scroll test only guards against regressions. The live test catches the real thing.
- `LiveE2ETests` against the restarted 7878 bridge on `w14:p2`, all 3 pass:
  - opens with no "↓"
  - keyboard hidden when the sheet appears
  - no "↓" with the approval card up
  - step headers "Question 1 of 3" / "Question 2 of 3" / "Review answers"
  - new `testFreeTextAnswer`: types "Green", and the agent replies "Colour is Green"
  - `tearDown` now sends Esc if the agent is left at a question, so one failure can't break the next test
- The UI test classes are `@MainActor` with `async setUp`; the UI test target now builds with no Swift 6 warnings.

### Notes and questions
- I restarted the 7878 bridge from this session (`swift run -c release herd serve --local-only --port 7878`). It stops if this session's background job is killed.
- `POST /prompt` can now return `409 agent_blocked`. The app shows the bridge's error message in the banner. The composer isn't disabled while blocked, because the card and sheet are the intended path.
- After a stop, the transcript shows `[Request interrupted by user]` / `[Request interrupted by user for tool use]` as user bubbles. Should the bridge drop them, or turn them into a small "Stopped" marker? The app could also style them. Your call.
