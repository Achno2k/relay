# Fix round 1 (found in the live E2E run)

Context: I ran the app end to end against the real bridge. The live UI test is at `ios/HerdUITests/LiveE2ETests.swift`. The throwaway agent is `w14:p2` in herdr workspace `herd-e2e`, cwd `.../scratchpad/herd-e2e`. It's yours to prompt, stop and break. Don't touch any other agent.
Pull first: I committed a few fixes (re-pairing store start, the `-agent` option in live mode, multi-question follow-up in `AppStore.answer`).

## Bridge (herd-bridge)
1. **Stop leaves the prompt behind.** When Claude Code is interrupted with Esc, it puts the interrupted prompt back into its input box. The next `POST /prompt` then types after it, so two prompts get glued together. Fix it so that after a stop the input is empty, and a prompt always goes into an empty input. Find the right herdr key names in `docs/herdr-schema.json` (`C-u` is rejected as `invalid_key`). Verify live on w14:p2: prompt, stop, prompt again, then check the transcript user message is exactly the second prompt.
2. **Unnumbered menus** (Claude's folder-trust prompt: "Quick safety check… ❯ No, exit / Yes, I trust this folder", "Enter to confirm · Esc to cancel"). Right now `/approval` returns `options: []` and the question "Enter to confirm · Esc to cancel". Parse cursor-style menus. Options are the menu lines, and keys are arrow moves relative to the `❯` line followed by Enter. Pick a sensible question ("Trust this folder?" plus cwdName, or the menu's heading line). Add synthetic tests. Reproduce it live by creating a new agent in a fresh, untrusted empty dir under the scratchpad via `POST /agents`, then close that pane/workspace when done.
3. While you're in the parser: include the multi-question progress when the screen shows tabs (`←  ☒ Delivery  ☐ Focus  ✔ Submit  →`). Add an optional `step: {"index": 2, "count": 3, "title": "Focus"}` to `Approval` and document it in api.md.

Run your bridge on port 7979 (I have one on 7878). `swift test` must be clean. Commit, then append a short "Round 1" section to `docs/tasks/bridge-report.md`.

## iOS (herd-ios)
1. **The keyboard covers the approval sheet.** If the composer is focused when the sheet appears, the keyboard hides the options. Dismiss focus/keyboard when the sheet presents. The sheet should also size to its content: there's a big empty gap between the question and the buttons in `docs/screenshots/approval-sheet.png`. Inline code in the question should be lighter.
2. **Chats open scrolled up** (the ↓ button is visible) instead of at the latest message. This happens with a long, real transcript. Open at the bottom with no visible jump, and also when switching agents from the sidebar.
3. Show the optional `approval.step` ("Question 2 of 3 · Focus") in the sheet header once the bridge adds it. Decode it as optional.
4. "Type something." option: when an option's label is "Type something." (Claude's free-text choice), tapping it shows a text field in the sheet. Send the option's keys, then the text via `POST /agents/:id/keys` or a prompt. Check with herd-bridge how to type text into that field; coordinate through `docs/api.md`.

Verify with the unit tests plus the live UI test:
```
TEST_RUNNER_HERD_E2E_LINK='herd://pair?url=http%3A%2F%2F127.0.0.1%3A7878&token='$(cat ~/.herd/token) TEST_RUNNER_HERD_E2E_AGENT=w14:p2 xcodebuild test -scheme Herd -destination 'platform=iOS Simulator,name=iPhone 17 Pro' -only-testing:HerdUITests
```
Extend the UI test for any fix that can be tested there. Commit, then append "Round 1" to `docs/tasks/ios-report.md`.

Both of you: same repo. Commit only your own paths (`git add <paths>`, never `-A`).
