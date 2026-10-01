# Round 10 QA

Simulator iPhone 17 Pro (in front). Test bridge: Go, built from each branch, on 127.0.0.1:7884 with a temp `RELAY_HOME`, paired as machine `qa-r10`. e2e agents only: `w14:p2` claude, `w14:p4` pi, `w14:p5` codex (started as `e2e-codex`; the pane was a bare shell).

## Bugs from the user

| id | repro on master (11ccd71) | fix | verified |
|---|---|---|---|
| B1 title padding | yes (visual; title baseline to first card ~26 pt, card to Now ~33 pt) | r10-polish 65caad1 | gap now ~19 pt; the user should eyeball it. UI test guards gap <= 24 pt |
| B2 file viewer | yes (tapping "Wrote x.md" only toggles the preview) | r10-bridge 1b1c546, dad1086; r10-files 21cf298 | live on claude: Edit diff, File tab, Write all-green, missing file "File not found". Endpoint: `../x` 403, absolute 400, missing 404 |
| B3 working feedback | yes (codex: lone dot ~15 s, no reply.live) | r10-chat 46d5ebd, fd29da8 | live: Thinking… then "Running a tool…" |
| B4/B9 Photos hitbox | no, 6/6 mouse clicks at the label and all row edges open the picker | r10-polish attachment sheet (in r10/all) | not yet on r10/all |
| B5 ↓ mid-fling | yes (video: tap ignored, list coasts ~6 s) | r10-chat 46d5ebd | video: jumps to bottom while coasting |
| B6 Stop stuck | yes, with a cold launch: app killed, pi turn over REST, launch 5 s later with every `GET /agents` held 4 s by a proxy. Stop stays 40 s+ after the reply | r10-chat 25b2fcb | same recipe: send arrow back |
| B7 plan missing | yes, exact match of the screenshot | r10-bridge dad1086; r10-chat 2587144, 8570f7d | live: plan in the sheet, Plan card in history |
| B8 lone dot | yes (codex: text stays in the field ~1 s, then a dot) | r10-chat 46d5ebd | video on claude and codex: field clears, Thinking… within ~250 ms |

## Found in QA

| id | sev | owner | status | title |
|---|---|---|---|---|
| R10-1 | P1 | r10-chat | verified ecc3172 | Crash: `WSClient.ping` resumes its continuation twice when the socket dies with a ping in flight. Repro: background the app 12-24 s, restart the bridge, foreground. Master crashed in round 2; fixed build 8/8 clean |
| R10-2 | P3 | r10-bridge | fixed d375606, not re-checked | codex's title spinner leaked into `agent.title` on a fresh session's first turn (`⁚ \| e2e`), ~10 agent.updated/s |
| R10-3 | P3 | r10-bridge | open | Claude chrome in `reply.live` text before a plan approval: `ctrl+g to edit in VS Code · <plan>.md` |
| R10-4 | P3 | r10-bridge | open | A plan-file Write streams as `{name: "Tool", summary: "Updated plan"}`, so the shimmer says "Running a tool…" |

## Methods (for re-runs)
- Taps and swipes on the Simulator window with `cliclick` (device points mapped to the window); videos with `simctl io recordVideo`, frames with ffmpeg.
- B6 proxy: 127.0.0.1:7885 -> 7884, holds `GET /agents` responses N s (all, or only the second of two back-to-back calls). Foreground-only races don't reproduce; a cold launch does.
- Never restart a bridge or proxy under a live app without expecting R10-1 on old builds.

## UI tests
`ios/RelayUITests/Round10UITests.swift` (mock): B8 busy right after send, B3 "Running Bash…", B5 ↓ at rest, B2 Edit diff + File tab, Read image/Markdown, only file rows tappable, B7 plan in sheet and plan card (`-mockPlan`, r10-chat caf79a5), B1 title gap.
- Run on the integration build (not master: the ids come from the worker branches).
- First run on r10-qa-int: B1, B2 edit, B2 tappable, B5 passed. B8/B3 failed on a test bug (mock's Read replaces Thinking… at 1.2 s; fixed to accept either). B2 Read failed (README row needed `-demo tools`; fixed). B7 not run yet.
