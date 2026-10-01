# Round 10: r10-qa report

## What changed
- `ios/RelayUITests/Round10UITests.swift`: 10 mock UI tests: B1 title gap, B2 Edit diff + File tab / Read image + Markdown / only file rows tappable, B3 "Running Bash…", B4/B9 Photos tile corners, B5 ↓ at rest, B7 plan in the sheet + plan card (`-mockPlan`), B8 busy right after send.
- `docs/qa/round10.md`: repro and verification per bug, QA bugs R10-1 to R10-5, methods.
- `ios/Relay.xcodeproj` regenerated for the new file. No app code.

## Results
- Every user bug reproduced on master except B4/B9 (sim clicks always hit). B6 needed a cold launch with `GET /agents` held by a proxy.
- All verified on r10/all. B1 is a looks call for the user. B6 and R10-1 are proven: master fails the recipe, r10/all passes.
- R10-1 (P1 crash in `WSClient.ping` on a socket drop while backgrounded) found and fixed (ecc3172).
- Tests on r10/all: Round10UITests 10/10; full mock RelayUITests 93 run, 66 pass, 27 skipped, 0 fail.

## Left
- R10-3, R10-4 (bridge live-preview noise around plans), R10-5 (PlanCard a11y id): open, minor.
- R10-2 not re-checked live. B5 mid-fling not re-run on r10/all (no OS input allowed now).
- Process: early on I drove the visible Simulator with cliclick, which typed into the user's window. Everything after 14:26 on 10-01 was headless.
