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
- ~~After a stop, the transcript shows `[Request interrupted by user]` as user bubbles.~~ Decided: the app handles it, and the bridge stays faithful to the transcript. See below.

### Stop marker (follow-up)
- A user line that is exactly `[Request interrupted by user]` or `[Request interrupted by user for tool use]` becomes a `ChatItem.stopped` row: a small centred "Stopped" marker, not a bubble. Assistant text and user messages that only mention the phrase are unchanged.
- Markers never resolve a pending (optimistic) prompt.
- Tests:
  - unit tests for the mapping and the pending filter; 27 pass
  - the live `testPromptApproveAndStop` now asserts the marker appears after a stop and no `[Request interrupted…` text is shown
  - all 6 UI tests pass (3 live on `w14:p2`, 3 mock)
- Screenshot: `chat-stopped.png`.

## Controls (controls.md, iOS section)

### What's there
- **Title pill:** the agent title with a chevron, and a secondary line under it with the model and mode (e.g. "Opus 5.5 · Auto"). A small spinner shows while a change is in flight.
- **Pill menu:**
  - Model ▸, Mode ▸ and Effort ▸ submenus, each showing the current value, with a checkmark in the submenu. Options come from `GET /controls`.
  - Then **Compact context** and **Clear conversation** (destructive, with a confirmation dialog).
  - Then workspace, kind and status, plus Stop while working.
- **While working or blocked:** a section caption says "Agent is working" (working) or "Answer the agent's question first" (blocked), and the rows are disabled. SwiftUI's `.disabled` greys a menu's buttons but not its pickers, so busy agents get read-only rows instead of pickers.
- **Mode chip:** when the mode isn't `default`, a blue chip ("Plan", "Auto") sits inside the composer on the leading side, orange for bypass. Tapping it lists the modes inline.
- **Optimistic updates:**
  - `AppStore.pendingControls` overlays the requested value on the pill and menus until `POST /control` answers.
  - On success, the returned `Agent` replaces the local one.
  - On failure, the overlay goes away (so the pill reverts) and a toast reads "Couldn't switch to Plan: <bridge message>".
  - One change per agent at a time.
- **`/clear`:** a changed `sessionId` (from the control response, `agent.updated` or a refresh) drops the cached chat, and the open chat refetches. `/compact` refetches the open chat when it's done.
- **Timeouts:** `/compact` gets a 120 s request timeout; other controls get 20 s.
- **Non-Claude agents** (or a bridge without `/controls`) show no controls, only the old info section.
- **Mock:** serves `controls.json` and mirrors the bridge's errors (409 busy/blocked, 400 for non-Claude and for `bypassPermissions`), with a short confirm delay.

### Deviations
- "Compact conversation" wraps with a hyphen at the system menu width, so the row reads **Compact context**.
- The blocked caption differs from the brief's "Agent is working", because "working" would be wrong there.

### Tests
- Unit tests, 35 total. New ones cover:
  - decoding the new `Agent` fields and `controls.json`
  - one-key `ControlRequest` bodies
  - pill labels with and without a pending change, including matching a full model id to its alias
  - a new session dropping the cached chat, while the same or an unknown session keeps it
  - the store: success applies the server agent; failure reverts and explains; `/clear` refetches
- Mock UI tests, all pass:
  - `testControls`: Sonnet → Plan (chip appears) → Bypass refused (toast, pill reverts) → Default (chip gone) → Clear with confirmation (chat empties)
  - `testControlsDisabledWhileWorking`
- Screenshots: `controls-menu`, `-model`, `-plan-chip`, `-chip-menu`, `-error`, `-clear-confirm`, `-busy`.

### Live (bridge fef4d32 on 7878, agent `w14:p2`)
- `testControls` passes: Sonnet 5 → Plan (chip appears) → Auto, with the pill label following each confirmed change. It restores the original model afterwards, and `w14:p2` ended on Opus 5.5 · Auto. Screenshot: `controls-live-sonnet-plan.png`.
- The whole suite is green: 35 unit tests, 5 mock UI tests, 4 live UI tests.
- Two bridge issues showed up along the way, both reported to herd-bridge and fixed there:
  - `/model` on a long conversation opens a "Switch model?" dialog, which caused a 504 (10d4a1b).
  - A stale "Interrupted" line plus a numbered list was read as a dialog, which caused a false refusal (fef4d32).
  - Both times the app reverted the pill and showed the bridge's message, as intended.
- The bridge added `409 control_refused` ("Kept model as …"). No app change was needed; the toast shows the message.
- Live stop test fix:
  - The test used to stop 3 s after sending an essay prompt. Claude writes nothing to the transcript when stopped before its first block, so the marker check was timing-dependent.
  - It now stops during a long Bash call and waits for "Running Bash…" first.
  - It uses `ping -c 45`, because Claude Code blocks a foreground `sleep`.

## Round 2 (round-2.md, all iOS parts)

### 1. Scroll-to-bottom button
- **Cause:** there were two.
  - The button was pushed out of its container with `.offset(y: -52)`, so taps could land outside its hit area.
  - It scrolled by assigning `ScrollPosition(edge: .bottom)`, which is a no-op when the value doesn't change.
- **Fix:**
  - The button is now an overlay on the chat area, inside the composer's safe-area inset, so no offset is needed.
  - Every programmatic scroll (the button, following new content, the approval card or keyboard, prepending a page) goes through `ScrollViewReader.scrollTo` to a bottom sentinel placed after the working indicator.
- **Test:** `Round2UITests.testScrollButtonReturnsToBottom` swipes up until ↓ shows, taps it, then asserts the button is gone and the last code block is hittable.

### 2. Attachments
- **`+` menu:** Photos (multi-select, up to the remaining slots), Camera (disabled without a camera), Files (`fileImporter`: images, PDF, text, source, JSON, any data), then "New chat in <folder>".
- **Preparing:** images go through ImageIO (HEIC and PNG included), are downscaled to at most 2048 px (never upscaled) and re-encoded as JPEG at 0.85. Other files go as they are, with a MIME type from their UTType. Limits: 20 MB per file and 10 per message, with a toast when either is hit.
- **Tray:** each file uploads as soon as it's picked, and progress shows on its tray item (a ring on thumbnails, a bar on file chips). The tray sits inside the glass field above the text, and each item has a remove button. Send is enabled once every upload has finished and there's text or at least one file. Text may be empty.
- **Bubbles:**
  - Image thumbnails (116 pt, tap for full screen with pinch and double-tap zoom) and file chips (tap for QuickLook) sit above the text. Messages with only attachments render too.
  - Files are fetched from `GET …/attachments/:id` and cached for the session. Our own uploads are pre-seeded in the cache, so there's no refetch.
  - An expired file (404) shows "Expired".
- **Optimistic bubble:** it includes the attachments, and resolves when the transcript shows the same text *and* the same attachment ids.
- **Live on `w14:p2`:** both tests generate their file in the app (DEBUG `-uitestAttachments` adds "Test image" / "Test PDF" to `+`, because UI tests can't drive the Photos picker). Each run draws a fresh random word, so replies from earlier runs can't match.
  - `testImageAttachment`: a PNG with the word; the reply contains it.
  - `testPDFAttachment`: a PDF with the word as its code word; the reply contains it.

### 3. Sidebar rework
- **Layout (Codex home):**
  - A glass filter button (orange dot when anything needs input) and a glass `⋯` menu with machine name, OS, bridge host, connection state and Unpair.
  - A large "Herd" title.
  - Device chips: All, plus one chip per machine from `GET /machine` (green dot when connected, laptop/desktop symbol, name). It's a list, so more bridges can plug in later.
- **Projects:** one row per herdr workspace, empty ones included, so you can start a chat in any folder. Each row has a folder icon, the name, a rotating chevron, an orange hand badge with a count when chats need input, and a compose button that opens New chat for that folder.
  - Folders start collapsed. Expansion is remembered per folder.
  - Expanded chats are indented, blocked first, then newest.
- **Rows:**
  - Status glyph: spinner (working), orange hand (needs input), blue eye (ready for review), check (completed), hollow circle (idle).
  - Subtitle: "Waiting for you · 10m" / "Working · 2m", with the folder name added in flat lists.
- **Search:** a glass circle bottom-left that grows into a field (glass morph). The "New chat" capsule sits bottom-right, as in `filter-menu.png`. Reasons:
  - The top stays clean for the title and chips, and both actions are in thumb reach on a 6.1" phone.
  - The list keeps its full height.
  - Results are a flat list with folder names.
- **Drawer change:** the sidebar no longer has its own drag-to-close, because a left swipe there now archives. The dimmed chat still closes the drawer on tap or drag.
- The chat's top-left button also gets an orange dot when a *different* chat needs input.

### 4. Filter menu and archive
- The filter button opens a glass menu: All / Needs input / Ready for review / Working / Completed / Archived, each with its icon, and a checkmark on the current one. Any filter other than All replaces Projects with a flat Sessions list (newest first, folder in the subtitle).
- Completed means done and seen, or idle. Ready for review means done and not yet opened.
- The filter is remembered across launches.
- **Archive:** swipe left (full swipe allowed) or the context menu, with Unarchive under Archived.
  - It's client-side, stored in the App Group defaults, and never closes the agent.
  - Archived chats are hidden everywhere except Archived. A blocked agent un-archives itself, whether it arrives by `agent.updated`, `agent.created` or a refresh.

### Test isolation on the user's phone
- All app UI state goes through `AppDefaults`: selected chat, seen times, filter, expanded folders, device chip and archive.
- Under `-uitest` (every UI test passes it), or when XCTest hosts the app, it uses the separate suite `dev.amansingh.herd.uitest`. `-resetSidebar` clears only that suite and does nothing without `-uitest`.
- A unit test checks that archiving and filtering in tests leave the user's real keys alone.
- Live tests pair with the Tailscale link and the user's own token, so the app stays paired as before.

### Tests
- 46 unit tests, including: attachment and machine decoding, attachment-only messages, JPEG downscaling, file pass-through and the 20 MB limit, every filter predicate, archive hiding, projects with badges, search, auto-unarchive, pending prompts matched by attachment ids, and defaults isolation.
- Mock UI:
  - `Round2UITests` (5): scroll button, projects tree plus new chat in a folder, filter menu plus persistence, archive and unarchive, attachments.
  - `MockUITests` (5, updated for collapsed folders).
- Live UI (6): stop, controls, free text, multiple questions, image, PDF.
- Screenshots: `docs/screenshots/round2-*-{dark,light}.png` (home, expanded tree, filter menu, needs-input sessions, archived, composer tray, bubble, image viewer).

### Device run: blocked on signing (needs the user)
- Ran `xcodebuild test -destination 'id=00008110-000414D91422801E' … DEVELOPMENT_TEAM=TC56945264 CODE_SIGN_ENTITLEMENTS=Herd/Herd-FreeTeam.entitlements -allowProvisioningUpdates -only-testing:HerdUITests`, with the Tailscale pair link.
- It failed before any test ran, and nothing was installed on the phone:
  - `No Accounts: Add a new account in Accounts settings`
  - `No profiles for 'dev.amansingh.herd.uitests.xctrunner' were found`
- The only local profile for team TC56945264 is `dev.amansingh.herd`. The UI test runner needs its own profile, and command-line provisioning can't create one without an Apple ID signed into Xcode.
- Every result above is from the iPhone 17 Pro simulator. The live tests on the simulator used the real bridge on 7878 via 127.0.0.1.

## Round 3 (round-3-controls-per-agent.md, iOS)

### What changed
- **Per-agent controls:** the title menu reads `GET /agents/:id/controls` for the open agent.
  - It's cached per agent under a key of kind, `sessionId` and `model`, so a model change refetches: effort levels depend on the model.
  - After every control call the app takes the `Agent` from the `202` (pi resets effort on a model switch) and refetches the lists.
  - On an older bridge (404) Claude agents fall back to `GET /controls`, and other kinds show no controls.
- **Only supported sections appear:** Model, Mode, Effort, Compact, Clear, each gated by `supports`, with that agent's own lists and labels. pi has no Mode. Codex's modes are Ask for approval / Approve for me / Full Access.
- **Long model lists (pi offers 57 here):** the Model submenu shows the current model (checked), then the next ten (the bridge floats scoped models to the top), then "All models (N)…". That opens a searchable sheet with the label, the id in mono underneath, and a checkmark on the current model.
- **Model matching:** exact id first (pi's `provider/id`, codex's slug), then substring for Claude's aliases. A plain substring match would pick `gpt-5.1` for `gpt-5.1-mini`.
- **Pill line:** model plus mode ("GPT-5.6-Terra · Approve for me"). Kinds without modes show effort instead ("gpt-5.6-sol · High").
- **Mode chip:** shows whenever the mode isn't the kind's first one (Claude `default`, codex `ask`). Full Access / Bypass are orange.
- **Mock:** Claude and codex use `docs/fixtures/agent-controls-*.json`. pi keeps a synthetic 23-model list so the search sheet gets exercised (the pi fixture has 6). The codex agent from `agents.json` gets the `agents-multi.json` fields, a pi agent is added, and the mock's `control` enforces each kind's `supports` and lists.

### Tests (simulator; the device is still signed out, see below)
- 53 unit tests. New ones cover: all four contract fixtures, lists missing for a kind, exact-before-alias matching, codex and pi pill lines and mode-chip rules, the per-agent cache (refetch on session or model change), and the 404 fallback.
- Mock UI:
  - `Round3UITests.testPiModelSearchAndEffort`: long list, search sheet, "grok" filter, pi effort levels, no Mode.
  - `testCodexModelEffortAndMode`: short list, efforts up to Ultra, mode chip and switching back to Ask.
- Live UI (`LiveKindControlsTests`, targets read from the bridge, originals restored in teardown):
  - **codex `w14:p5`: passed.** Model then effort through the menu; `GET /agents/:id` confirms both; the pane was left on gpt-5.6-terra.
  - **pi `w14:p4`: model passed; effort failed on the bridge.** The model switch through "All models…" and search worked. Setting effort to Off gave `504 "/thinking off sent, but no confirmation"`, the footer still said `high`, but `…/controls` lists `off` for that model. I reported it to herd-bridge and restored pi (gpt-5.6-sol, high). Rerun pending their fix.
- All earlier live tests (stop, controls, free text, multiple questions, image, PDF) and all mock UI tests passed in the same run.
- **Physical device:** `build-for-testing` for the phone still fails with "No Accounts" / no profile for `dev.amansingh.herd.uitests.xctrunner`, so everything above is on the iPhone 17 Pro simulator, as the brief allows.
- Screenshots: `round3-pi-model-menu`, `round3-pi-model-search`, `round3-codex-menu` (mock), plus live `round3-live-*`.
