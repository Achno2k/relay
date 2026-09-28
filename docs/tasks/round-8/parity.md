# Round 8 parity: Go bridge vs Swift bridge

## Run it

```sh
bridge-go/parity/run.sh                 # REST + 20 s of passive WS, all agents
bridge-go/parity/run.sh -v              # also print passing checks
bridge-go/parity/run.sh -e2e w14:p2,w14:p4,w14:p5   # + prompts through both bridges (reserve the e2e agents with qa-bridge first)
```

- `run.sh` builds `bridge-go/cmd/relay`, starts it with `RELAY_HOME=$(mktemp -d)` on `GO_PORT` (default 7880, never 7878), runs the tool against it and the live Swift bridge (7878), then stops it. Exit code 1 means a mismatch; 2 means the run couldn't start.
- `run.sh` copies `~/.relay/uploads` into the temp home, because attachment history is matched against `uploads/sent.jsonl`. The live `~/.relay` is only read.
- Env vars:
  - `GO_PORT`: the Go bridge's port.
  - `RELAY_BIN`: a prebuilt binary to use instead of building one.
  - `SWIFT_URL`: the reference bridge.
  - `KEEP=1`: keep the temp home and the bridge log.
- Flags (after `run.sh`):
  - `-agents id,…`: limit the per-agent probes.
  - `-max-pages N`: history pages of 200 walked per agent (default 3).
  - `-ws-seconds N`: passive WS window (default 20; 0 skips it).
  - `-retries N`: attempts per GET before a mismatch counts (default 4, 1.5 s apart).
  - `-dump DIR`: write each WS window's raw frames to `DIR` (real transcript text: keep it local).
- Unit tests: `go test ./parity`.

## What it compares

Both bridges get every request at the same moment. Each pair is compared on:
- the status code;
- `Content-Type` and `Cache-Control`;
- the body. JSON is compared structurally, keeping number literals as written and telling null apart from a missing key. Anything else is compared byte for byte.

A GET that differs is retried, because agents keep changing between reads. It fails only if it still differs after the last attempt.

- **Global:** `/health` (with and without a token), `/machine`, `/controls`, `/controls?kind=claude|pi|codex|bogus|` (the last one empty), `/usage`, `/workspaces`, `/agents`.
- **Per agent** (every live agent, read-only):
  - `/agents/:id`, `/approval` and `/controls`;
  - `/messages`, plus history pages walked with `before=`;
  - every attachment referenced in history (up to 20);
  - odd `limit`/`before` values on one agent that has a transcript.
- **Errors:** these only ever target the nonexistent pane `w999:p999`, so nothing reaches a real agent:
  - no token or a bad token (REST and `/ws`), an unknown route, a wrong method, `HEAD`, `OPTIONS`;
  - trailing and doubled slashes, a query `token` off `/ws`, `bearer` in lower case with extra spaces, a non-bearer scheme;
  - a bad agent id (a control character, 200 characters);
  - bad JSON, a missing field, an invalid key, and a 2 MB+ body (`413`);
  - empty text or prompt, an unknown attachment;
  - control bodies with zero keys, two keys or a bad value;
  - `POST /agents` validation;
  - an empty upload, and an upload to a missing agent.
- **WS, passive:** both `/ws` streams are recorded while the REST pass runs. Frames are compared by what they converge to, not one by one:
  - The first frame is exactly `{"type":"hello"}`.
  - `message.upserted`: the last version of each (agent, message id) is equal.
  - `agent.updated`: the per-agent trail of distinct states (updatedAt aside) is equal. If one trail is a suffix of the other, that's a warning.
  - `reply.live`:
    - it only has the contract keys;
    - `seq` strictly increases;
    - `tool` has string `name`/`summary`/`state`;
    - the last frame clears when the other side's does;
    - both sides show the same non-generic tool summaries (collapsing half-rendered prefixes like `Ran cd brid`).
  - Frames that landed within 2 s of the window's edges on one side only are skipped.
- **WS + REST, e2e (`-e2e`):** each listed agent gets the following, all while both streams are watched:
  1. A prompt through Swift and then one through Go (`echo parity-…`, then a fixed reply). An approval is checked on both bridges, then answered with option 1 through the bridge under test. Both `POST /prompt` replies must match.
  2. A stop: a `sleep 25` prompt through Go, then `POST /keys ["esc"]` through Go.
  3. `/agents/:id` and `/messages?limit=10` compared after each step.
  4. The same file uploaded to each bridge. Ids differ, the rest must match, and each bridge serves its own file back.

  The tool refuses to run e2e on an agent whose `cwdName` isn't `e2e`. The live-tool comparison is strict here.

## Normalized fields

Only fields that legitimately differ between two bridges started at different times:

| Field | Rule |
|---|---|
| `/health` `uptimeSeconds` | any non-negative integer |
| `reply.live` `seq` | per-bridge counter; only monotonicity is checked |
| `Agent.updatedAt`, screen message `createdAt` | Accepted if swift is later (it saw a seq change the Go bridge didn't) or go is at/after the Go bridge's start (its first-seen time). Any other difference is a **warning** (see R8 finding below). The go value must always be `YYYY-MM-DDTHH:MM:SS+00:00`. |
| `/usage` and `usage.updated`: `updatedAt`, `resetsAt`, `usedPercent`, `stale`, `windows` length, `unavailableReason` null/absent | each bridge fetches on its own timer; types and formats must still match. Providers are aligned by id, and one fetched by one side only is a warning. |
| `/usage` `usedBy` | also per fetch (it waits for the first `pi auth check`) |
| `Agent.model`/`modelLabel`/`permissionMode`/`effort` | swift has a value and go has `null`: a warning. Swift keeps each pane's last known controls (`lastControls`, never pruned), so it can show a value the screen no longer does, even from the previous agent in that pane. The reverse (go knows, swift doesn't) is a diff. |
| error `message` text | warning only; status and `code` must match |
| attachment `id` (e2e upload) | 16 lowercase hex on both sides |

## Known fixes (R8 bugs the port fixes on purpose)

`parity/fixes.go` applies each one to Swift's side, or accepts exactly that change, and labels the warning with the bug id:

| Bug | How parity handles it |
|---|---|
| R8-8 `file://` and `host:` paths scrubbed | A string differs only in words where swift has `file://…`, `host:/…` or `host:~/…` and go's word has no `/Users/` or `/home/`. A `…`-cut summary may end on a different word. |
| R8-9 injected user lines | Swift's messages get the api.md rule first: drop `<task-notification`/`<bash-stdout>`/`<bash-stderr>` texts, `<bash-input>x</bash-input>` → `! x`, drop emptied user messages, and merge the assistant messages that become adjacent. Pages are then trimmed to their first shared id, and `hasMore` isn't compared. It also applies to `message.upserted`. |
| R8-13 stale `updatedAt` | See the clock rule above; the remaining differences are labelled R8-13. |
| R8-18 WS bad token | Swift's bodiless `400` must be go's `401 {"error":{"code":"unauthorized"}}`. |

Other R8 fixes don't show on the wire in a read-only run (R8-11, R8-17 and the others), or only as `reply.live` junk that Swift sends and Go doesn't (R8-10, R8-22, R8-23). The live comparison is by tool set and clearing, so those don't fail.

Warnings that are expected on every run:
- `reply.live` final text: the text differs by screen timing.
- `usage.updated only go sent it`: the fresh bridge polls on its first WS client.
- `updatedAt`: Swift's stale mtime.

## Self-test

The harness was checked by running the frozen Swift binary as the "Go" side:

```sh
RELAY_BIN=<copy of bridge/.build/release/relay> GO_PORT=7883 bridge-go/parity/run.sh
```

A clean Swift-vs-Swift run means every remaining diff against Go is a real difference.

- REST + passive WS on all agents: 166 checks, 0 failures after the rules above. It takes about 1 min.
- Findings from the self-test, filed by qa-bridge:
  - The Swift `Agent.updatedAt` goes stale: `URL.resourceValues` caches the transcript mtime per URL.
  - The Swift bridge ignores SIGTERM, so `run.sh` escalates to SIGKILL after 5 s.

## Results against the Go bridge

All runs are against the live Swift bridge (7878).

| When (2026-09-28, UTC) | Run | Result |
|---|---|---|
| 11:41 | first run, all agents | 19 failed. The failures were R8-8, R8-9 and the pi catalog (pi's model list changed inside Swift's 10 min cache). This led to the known-fixes layer. |
| 11:47 | all agents + 40 s WS | 0 failed |
| 11:51 | e2e claude, pi, codex | 0 real diffs. The one failure was a harness bug: the repeat check compared truncated text. Fixed. |
| 11:56 | all agents + 60 s WS | 0 failed |
| 11:57 | edge probes (HEAD, OPTIONS, slashes, auth variants) | 0 failed |
| 12:10 | iOS Live* UI suite on the Go bridge (7880) | 11/11 pass |

iOS live UI tests (ios-bugs, simulator, e2e agents, app at master 3391a48). The same 11 tests ran against each bridge:

| Test | Swift 7878 | Go 7880 |
|---|---|---|
| LiveE2ETests testControls, testFreeTextAnswer, testImageAttachment, testMultipleQuestions, testPDFAttachment, testPromptApproveAndStop | pass | pass |
| LiveKindControlsTests testCodexApproval, testCodexModelAndEffort, testPiModelAndEffort | pass | pass |
| LiveNewChatTests testCreateClaudeWithModelAndEffortOpensEmptyChat | pass on rerun (menu taps didn't land the first time) | pass |
| LiveReplyUITests testGrowingTextBeforeTranscriptMessageLands | pass | pass |
| LiveToolE2ETests testSlowBashShowsRunningRowWithoutToolText | pass | pass |

Totals: 11/11 on Swift and 11/11 on Go, all first try on Go.

ios-bugs also noticed that `GET /controls?kind=claude` has no `defaultModel` when `~/.claude/settings.json` has no `model`. Go matches Swift (the key is absent on both), so it's a contract gap with api.md's "always sent" wording, not a regression. Sent to qa-bridge to file.

What the e2e turns showed:
- The claude turn's `reply.live` frames are identical in content and order on both bridges: generic `Ran a command`, then `Ran echo parity-…`, then cleared. `message.upserted` and the status trails match for all three kinds, and so do the stop via Go and the uploads.
- Go sends fewer `reply.live` frames than Swift (about 1/3 in busy windows). The difference is junk that Swift sends and Go correctly drops: a per-second `(1m 5s · 5 lines) (ctrl+b to run in background)` from scrolled-off tool output (R8-22), the logo, `(click)` overlays (R8-23) and dialog footers (R8-10). No real text or tool frames are missing.
- For codex, Go's single text frame can come ~100 ms before Swift's, mid-stream (`parity done` vs `parity done 96351-swift`), right before the transcript lands. That's timing.

Found by parity and the code review, and filed in `docs/qa/round8.md`:
- R8-13: stale `updatedAt` (Swift).
- R8-12: SIGTERM ignored (Swift, confirmed by qa-bridge).
- R8-25: Go `/ws` never pings (Swift pings every 30 s), so a vanished phone keeps usage and live polling running.
- A Claude logo leaking into `reply.live` in Go: reported to go-live, fixed in aec80a5.
