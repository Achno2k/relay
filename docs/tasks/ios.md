# Task: build `ios/` (the Herd app)

Read first: `../AGENTS.md`, `docs/api.md` (the contract), `docs/fixtures/*` (build against these; a bridge is being built in parallel by another agent).

## Goal
A native iOS 26 app for driving coding agents. It should feel like the **ChatGPT iOS app** rebuilt on **Liquid Glass**. No terminal views, and nothing that looks like a web view.

## Project
- `ios/Herd.xcodeproj` generated with **XcodeGen** (`ios/project.yml`; `brew install xcodegen` if missing). Scheme `Herd`, bundle id `dev.amansingh.herd`, iOS 26.0 deployment, Swift 6 strict concurrency.
- Pure SwiftUI + Observation. No third-party packages except, optionally, `swift-markdown-ui`, and only if AttributedString markdown + custom code blocks turn out to be insufficient. Prefer none.
- Folders: `App/`, `API/` (models, APIClient, WSClient), `Stores/`, `Features/{Sidebar,Chat,Composer,Approval,NewChat,Pairing}/`, `DesignSystem/`.
- A `MockBackend` that serves `docs/fixtures` (copied into the bundle for the Debug config), selected with a launch argument `-mock`, so the app and every preview run without a bridge. The mock replays `ws-events.jsonl` on a timer.

## UX spec (mirror ChatGPT iOS)
- **Root**: a chat screen with a **sidebar drawer** that slides in from the left: the edge-swipe gesture plus a top-left button. The chat content shifts right and dims, as in ChatGPT.
- **Sidebar**: a glass search field at the top, a "New chat" row, then agents grouped by workspace (section headers). Each row shows the title, a secondary line (agent kind · relative time), and a status indicator: working = animated pulse, blocked = orange "Needs you" pill, done = blue dot (unseen). Blocked agents float to the top of their section. Long-press opens a context menu (Stop, Rename later).
- **Chat top bar**: a glass capsule title in the centre showing the agent title with a chevron (tap shows a menu with workspace, kind and status). Sidebar button on the left, new-chat button on the right. Use a system toolbar so iOS 26 renders the glass itself. Add `.scrollEdgeEffectStyle(.soft)`.
- **Messages**:
  - User messages: right-aligned rounded bubbles in secondary system fill, max 80% width.
  - Assistant messages: no bubble, full-width markdown text (headings, lists, bold, inline code). Code blocks sit in a rounded container with a language label and a copy button, and scroll horizontally.
  - Tool activity: consecutive `toolCall`/`toolResult` blocks collapse into one row, "Worked for 42s ›" or, while live, "Running Bash…" with a shimmer. Tap to expand into a vertical list of steps (icon per tool: pencil for Edit/Write, magnifier for Grep/Glob, terminal for Bash, doc for Read). Errored steps are tinted red.
  - Thinking: a collapsed "Thought for a few seconds" row, like ChatGPT's reasoning row.
  - While the agent is `working` and the last message is not assistant text: the ChatGPT pulsing-dot indicator.
  - Smooth autoscroll to the bottom on new content unless the user has scrolled up, with a floating glass "↓" button in that case.
- **Composer**: a floating glass capsule pinned above the keyboard, the ChatGPT layout:
  - `+` on the left (menu: New chat in this workspace).
  - A growing text field, "Message <agent name>".
  - On the right, a circular send button (arrow.up). It **morphs into a stop button** (square) while working, via `GlassEffectContainer` + `glassEffectID`. Stop sends `["esc"]`.
  - Haptics on send.
- **Blocked**: when the open agent is blocked, fetch `/approval` and present a glass bottom sheet (medium detent) with the question and one full-width glass button per option (the first uses `.glassProminent`). Also show an inline "Needs your approval" card above the composer that reopens the sheet.
- **New chat sheet**: workspace picker (list), agent kind segmented (claude / codex / pi, default claude), optional first message. Create, then navigate to it.
- **Pairing** (first launch / no token): a clean onboarding screen with "Scan pairing code" (VisionKit `DataScannerViewController` QR) and "Enter manually" (URL + token). Parse `herd://pair?url=&token=`. Store the token in the Keychain. Register the `herd` URL scheme too.
- **Connection**: a small glass banner "Reconnecting…" when the WS is down. The WS reconnects with exponential backoff. Refetch `/agents` and the open chat on reconnect and on scenePhase active.
- Dark and light mode, Dynamic Type, SF Symbols, `.sensoryFeedback`. Use `.glassEffect(.regular.interactive())` for tappable glass. Let system components (toolbar, sheets, menus, search) provide glass rather than faking it with materials.

## Verify
- `xcodebuild -scheme Herd -destination 'platform=iOS Simulator,name=iPhone 17 Pro' build` is clean with no warnings. Add unit tests for model decoding against the fixtures, plus the store reducer (`message.upserted` merge, `agent.updated`, `agent.closed`).
- Run in the simulator with `-mock` and take screenshots (`xcrun simctl io booted screenshot`) of: the sidebar open, a chat with tool rows expanded and collapsed, the composer in the working (stop) state, the approval sheet, the new-chat sheet, and pairing. Save them in `docs/screenshots/` and look at them critically. Iterate until it genuinely feels like ChatGPT on iOS 26 and not a generic SwiftUI demo.
- Commit in small logical commits (end each message with the trailer lines below). Leave `docs/tasks/ios-report.md` covering what works, known gaps, and any contract questions.

Commit trailer:
```
Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Q4qo2QabVUAvi2YgczSwpD
```
