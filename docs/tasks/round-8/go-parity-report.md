# go-parity report (round 8)

## What changed
- `bridge-go/parity/`: a Go tool plus `run.sh`, run with one command: `bridge-go/parity/run.sh`.
  - `run.sh` builds and starts the Go bridge on a temp `RELAY_HOME` (port 7880, never 7878).
  - The tool diffs the Go bridge against the live Swift bridge (7878), prints each difference, and exits 1 on any mismatch.
  - What it covers:
    - every GET for every agent and workspace, `/machine`, `/usage`, controls, and history pages;
    - error shapes, from a nonexistent pane only;
    - both `/ws` streams;
    - with `-e2e`, prompts, a stop and uploads through both bridges on the e2e agents;
    - with `-e2e-controls`, effort through Go and back through Swift.
  - `-dump DIR` keeps each WS window's frames as local evidence.
- `docs/tasks/round-8/parity.md`: how to run it, what it compares, and each normalized field with the reason it legitimately differs. It also lists the known R8 fixes the harness applies to Swift's side, and the results.
- Tests: `go test ./parity` covers the differ, the rules, provider alignment, state trails, live-frame checks and each known fix. There were no Swift tests to port.

## How it was verified
- Self-test: I ran the frozen Swift binary as the "Go" side (temp home). REST + passive WS came out clean, and so did e2e on claude, pi and codex. That showed which differences are legitimate between two bridges started at different times, and every rule in `parity.md` comes from that run. Any diff left against Go is a real difference.
- Against Go: 0 failures on REST + 60 s of WS across all live agents, and on the e2e turns for all three kinds. Details and times are in `parity.md`.
- Final run (12:19 UTC): e2e on claude, pi and codex with `-e2e-controls` gave 170 checks and 0 failures. It covered invalid control bodies and effort changed through Go and restored through Swift on each kind, and all three agents were left at their original effort.
- iOS live UI tests: ios-bugs ran the 11 Live* tests on the simulator against the Go bridge on 7880, and all 11 passed on the first try. The same 11 passed against 7878. The table is in `parity.md`.

## Found
Filed by qa-bridge in `docs/qa/round8.md`:
- R8-13 (from parity): Swift's `Agent.updatedAt` goes stale, because `URL.resourceValues` caches the mtime.
- R8-12 (seen by parity, confirmed by qa-bridge): the Swift bridge ignores SIGTERM.
- R8-25 (code review): Go `/ws` never pinged, while Swift pings every 30 s. A vanished phone kept usage and live polling running. Fixed by go-server.

R8-26, a contract gap (found by ios-bugs, confirmed on both bridges, relay-lead to decide): `GET /controls?kind=claude` leaves out `defaultModel` when Claude has no saved model, but api.md says it's always sent.

Sent straight to the owner: the Claude logo showing as `reply.live` text in Go. go-live fixed it in aec80a5.

Explained, not bugs:
- Go sends about a third as many `reply.live` frames as Swift. The difference is Swift's junk frames that the R8-10/22/23 fixes drop.
- pi catalog diffs come from each bridge's 10 min `pi --list-models` cache.
- Swift keeps the controls of a previous agent in the same pane. That's R8-19; Go forgets them.

Reviewed against Swift and found faithful: server routing, auth, body decoding, paging, `POST /agents`, the agent monitor, and the app wiring (`run.go`). Probes also confirmed the edge behavior (HEAD, OPTIONS, slashes, auth header variants).

## What's left
- R8-26 needs a decision: change the api.md wording, or report Claude's built-in default model.
- At cutover the lead can run `bridge-go/parity/run.sh -e2e w14:p2,w14:p4,w14:p5` one last time (reserve the e2e agents first).
