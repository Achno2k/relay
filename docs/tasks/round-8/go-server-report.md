# go-server report (round 8)

## What changed
- `bridge-go/internal/server`: routing, bearer auth (`?token=` on `/ws`), the JSON error shape, the 2 MB JSON body cap (`413 too_large`), 20 MB uploads, the WebSocket (`hello` first, then hub events) and the event hub (1000 events per client, oldest dropped).
  - It routes by hand, not with `http.ServeMux`, to keep Hummingbird's behavior, all probed on the live Swift bridge: auth runs before routing (an unknown path without a token is 401), an unknown path or method is `404 {"code":"not_found","message":"Not Found"}`, empty path segments are ignored (`/agents/` works), HEAD is 404, and a plain GET on `/ws` is an empty 200.
  - JSON bodies decode like Swift's `Decodable`: keys are case-sensitive, required keys must be present and non-null, and anything else is `400 invalid JSON body`. Validation order matches Swift: body, then body checks, then the agent id.
  - The server declares a `Backend` interface; `internal/service` implements it, so the server never imports the service.
- `bridge-go/internal/service`: AgentService (snapshots, messages, approval, prompt/keys/text, input clearing, uploads, controls pass-through, `POST /agents`), the monitor (coalesced refresh, deltas, one tailer per agent, `/health` reachability) and `Run` (wiring plus one HTTP server per host).
- Interfaces agreed with the peers: `docs/tasks/round-8/go-server-interfaces.md`.
- QA fixes (docs/qa/round8.md):
  - R8-3: the server scrubs error messages it didn't write itself (from herdr, internal errors, `control_failed`).
  - R8-4: a failed `agent.start` closes the tab it opened, except on `agent_not_ready`.
  - R8-5: only `agent_pane_busy` is retried (a code probed on live herdr).
  - R8-12: `Run` returns about 0.1 s after cancel, with a 1.5 s cap on background work.
  - R8-13: `os.Stat` on every snapshot.
  - R8-18: a bad WS token gets 401 with the JSON body before the upgrade.
  - R8-19: per-pane state is dropped on `agent.closed`.
  - R8-11: `clearInput` now handles claude, pi and codex, using go-live's `approval.InputFor` (codex is read with ANSI so its dim placeholder counts as empty). go-drivers clears before pi/codex slash commands. Checked live on a scratch pi through the Go bridge: text left in the box was gone after `/keys esc`, and nothing was submitted.

## Tests ported
| Swift suite | Swift | Go | Notes |
|---|---|---|---|
| RoutesTests | 19 | 19 | `service/routes_test.go` (real service, fake herdr, httptest) |
| AuthTests | 1 | 1 | |
| NewAgentTests | 9 | 5 | launchFlags, codexConfigTopLevelOnly and settingsLock are in controls (go-drivers); codexFallback is in transcript |
| InputClearingTests | 5 | 5 | plus 2 Go-only tests for pi/codex (R8-11) |
| MiscTests | 8 | 1 | agentNames here; the rest are with go-core and go-transcripts |
| UsageRoutesTests (asked by go-drivers) | 4 | 4 | |
| ControlsTests.routes (asked by go-drivers) | 1 | 1 | |
| SocketResilience 503 + health (asked by go-core) | 2 | 2 | |

Go-only tests pin the probed Hummingbird edge behavior, hub overflow, WS unsubscribe, monitor created/closed/tailing, the scrubbing and the QA fixes.

## How it was verified
- `gofmt -l`, `go vet`, `GOOS=linux go vet` and `go test -race` (5 runs, no flakes) on both packages.
- Live smoke: the Go bridge on 7882 (temp `RELAY_HOME`) against the Swift one on 7878, GETs only. Every GET matched after normalizing key order, `updatedAt` and `uptimeSeconds`: health, workspaces, agents, machine, controls ×4, the e2e agents, their messages, controls, approval, and the 404s. The one exception was `/usage`, whose live numbers differ because the Swift cache was stale.
- SIGTERM to exit: 0.1 s.

## What's left
- qa-bridge re-verifies the R8 fixes on 7880.
- Full WS parity and every-agent diffs are go-parity's harness.
- A note on probing: my first scratch split landed in w13 (a wrong param name). I closed it at once. It only held my own scratch codex, and no one else's pane was touched.
