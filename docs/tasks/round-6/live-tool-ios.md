# live-tool-ios: show "Running Bash…" immediately, with no text glitch

The user bug and its cause are described in `live-tool-bridge.md`; read it. The bridge will add an optional `tool` object to `reply.live` (see api.md "Live reply" once live-tool-bridge updates it; build against a mock until then).

- While `reply.live.tool` is set, show the standard live tool row (the same "Running Bash…" shimmer row and icon the transcript toolCall uses) at the tail of the chat, **immediately**.
- When the transcript's toolCall arrives, swap in place with no jump and no duplicate row. Match on name plus summary; fall back to the latest unmatched call of the same name.
- Live prose text (`reply.live.text`) keeps its current rendering, but make sure it never shows tool/command text. Defensively drop lines that look like `⏺ Name(`, `⎿`, `• Ran`, in case an older bridge sends them.
- No flicker: coalesce updates, animate only on real state changes, and make sure the pending-bubble and live row ordering is stable.
- Tests: reducer/unit tests for the live tool lifecycle (start → transcript lands → cleared; stop mid-tool; switching agents mid-tool) and a mock UI test.
- Live check on the simulator against w14:p2 with a slow Bash command, with a screen recording (`xcrun simctl io booted recordVideo`) at `docs/tasks/round-6/live-tool.mov`.
- Ask herd-native before a device run.
