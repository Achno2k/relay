# Round 10: r10-chat report

Branch `r10/chat`. Commits in pick order:

| sha | what |
|---|---|
| 46d5ebd | B3/B8 "Thinking…" shimmer, B5 ↓ mid-fling |
| 1ddc19d | RelayKit: `ToolCall.path/edit/plan`, `ToolEdit`, `ToolEditChange`, `Approval.plan` |
| 115b1ad | `ToolStep` carries `path`, `edit`, `plan` (r10-files uses it) |
| 2587144 | B7 plan card + plan in the approval sheet; unit tests |
| d4a4403 | xcodeproj regen (PlanCard.swift, Round10ChatTests.swift) |
| 8570f7d | B7 sheet: plan in its own box |
| 02149c0 | doc comment |
| 25b2fcb | B6 resync race |
| fd29da8 | B3: "Thinking…" under live text that stopped growing |
| ecc3172 | R10-1 crash: the pong handler resumes once |

## B3 / B8: shimmer bubble
- `AppStore.awaitingReply`: set on send/retry. Cleared by `working`/`blocked`, a new assistant message, a failed send, stop, agent closed, or 30 s.
- The chat is "busy" when working or awaiting. It shows a shimmering "Thinking…" (`WorkingBubble`, id `workingIndicator`) unless the last row already shows progress: streaming live text, or a tool group with an unfinished step ("Running Bash…").
- Landed text or finished tools while working show "Thinking…" below them, so the chat never looks idle.
- Live text that hasn't grown for 1.5 s (Claude thinking mid-turn) also gets "Thinking…" under it (r10-qa's B3 note).
- A tool group is live only while a step is unfinished, so only one shimmer shows at a time.
- The pulsing dot is gone from the chat. `PulsingDot` itself stays in DesignSystem, unused.

## B5: ↓ while the list coasts
- Cause: an animated `scrollTo` loses to fling momentum, and the `.decelerating` geometry updates flipped `followsBottom` back to false.
- Fix: mid-fling the button does a plain jump, which stops the fling. `jumpingToBottom` keeps the fling's tail from turning following off. When the list settles, it scrolls to the bottom again if it isn't there. A new touch cancels the jump.
- Not verified mid-fling: XCUITest waits for idle before a tap, so it can't tap while the list coasts. r10-qa's repro method is needed.

## B6: Stop stuck after the reply
- r10-bridge found the root cause. `refresh()` fetches `/agents` while the socket keeps delivering. `merge()` then replaced every agent with that older snapshot, so a `done` that landed mid-resync (on every foreground and reconnect) went back to `working`. The bridge never resends an unchanged status.
- Fix: socket agent events get a stamp. `merge()` keeps the socket's state for agents it updated, created or closed after the fetch started.

### B6 audit (lead's ask)
- Only two paths write an agent's status: socket `agent.updated`, and `refresh()` → `merge()`. The `control()` and `createAgent` responses also upsert, but neither can bring back `working` (the bridge refuses controls while working). Messages, live reply, pending sends and awaiting-reply never touch status.
- Foreground can't hit the race. On `.connected`, `handle()` awaits the resync inside the socket's event loop, so later socket events wait for its merge. `refreshGeneration` drops the other, slower resync. That's why r10-qa couldn't reproduce it on master.
- Cold launch can. `start()` resyncs without holding socket events (`resyncOnConnect` is false), so a `done` arriving during the first `/agents` is overwritten. Pull to refresh on the Usage page is the same, but rarer. `25b2fcb` covers both.
- Status: fix in, reachable path identified; end-to-end proof pending r10-qa's cold-launch repro.

## R10-1: crash when the socket dies (r10-qa, P1)
- `sendPing` can call its pong handler twice when the socket is cancelled with a ping in flight. The checked continuation resumed twice and trapped (it hit on bridge restarts).
- `WSClient.pingResult` resumes once, guarded by a lock. `PingTests` call the handler twice in both orders, plus 200 two-thread races.

## B7: plan
- `ChatItem.plan` goes after the tool group holding `ExitPlanMode`. It never splits the group, so the call's result still lands.
- `PlanCard` shows about 180 pt with a fade and "Show full plan" (ids `planCard`, `planToggle`). `-demo plan` opens it expanded.
- Approval sheet: with `plan` it opens at `.large`. The plan scrolls in a box (`approvalPlan`) and the options stay pinned below. The card above the composer says "Review the plan".
- Checked on the mock and against r10-bridge's 7883 (w14:p2's real plan).

## Tests
- `RelayTests/Round10ChatTests.swift`: awaiting-reply lifecycle, the resync race (done/closed/created mid-resync, snapshot still wins otherwise), toolCall field decoding (including an unknown edit kind), `Approval.plan`, and plan placement.
- Unit tests 185/185 pass on iPhone 17. With the `25b2fcb` check turned off, 2 of the 3 resync tests fail.

## Open
- r10-qa's B8 note on codex: the typed text stays in the field with a grey arrow, and the bubble shows at about +3 s. I couldn't reproduce it on the mock with 10 Hz `agent.updated` plus a 2 s prompt delay; the field clears and the pending bubble shows at once. It needs a video on a real codex agent.
- r10-polish asked about a B4 composer change (the `+` menu becomes a sheet). ComposerView is mine; I'll take their patch once the lead decides.
