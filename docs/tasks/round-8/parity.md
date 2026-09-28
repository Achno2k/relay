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
  - no token or a bad token (REST and `/ws`), an unknown route, a wrong method;
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
| error `message` text | warning only; status and `code` must match |
| attachment `id` (e2e upload) | 16 lowercase hex on both sides |

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

(pending: the Go bridge doesn't serve yet)
