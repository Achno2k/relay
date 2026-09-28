# go-drivers report

## What changed
- `bridge-go/internal/controls`: port of AgentControls, ClaudeControls, ControlDrivers, CodexDriver, PiDriver, ModelCatalogs, SettingsGuard.
  - `Controls` holds the per-pane state Swift kept on AgentService: confirmed hold, last known values, codex sticky mode, transcript control cache, settings lock.
  - API agreed with go-server (`docs/tasks/round-8/go-server-interfaces.md`): `New(Deps)`, `State`, `Label`, `ForAgent`, `Apply` (returns the pane id), `ForKind`, `Catalog`, `HasDriver`, `LaunchArgs`, `AgentModel`, `LabelForKind`, `Hold`, `Forget`.
  - Picker, InputBox and ApprovalParser come from go-live's `internal/approval`.
- `bridge-go/internal/controls/agentcli`: runs `claude`, `codex` and `pi`.
  - PATH: Homebrew and /usr/local first (as Swift did), then the inherited PATH, then `~/.local/bin`, `~/.claude/local`, npm/bun/volta globals, `~/bin` and Linuxbrew. Works under launchd and systemd.
  - Every child gets SIGTERM on timeout or cancel, then SIGKILL 2 s later.
- `bridge-go/internal/usage`: port of UsageModels logic, UsageParsers, UsageMonitor, UsageSources.
  - The cache is behind a mutex and probes run outside it.
  - Probes take the monitor's context, so shutdown kills a running probe.
- `bridge-go/internal/uploads`: port of UploadStore, sent log, marker, PastedContent use and UploadCleaner.
  - Kind and Content-Type come from an extension table dumped from UTType on macOS 26, so Linux gives the same answers.
  - `sent.jsonl` keeps Swift's `.iso8601` date format, so the log carries over at cutover.

## QA bugs fixed (docs/qa/round8.md)
- R8-1: the claude usage probe adds `--strict-mcp-config`, streams stdout and stops at the first `usage_report` line, with a 45 s backstop. A live probe takes 4.1 s.
  - The command line in api.md "Usage" doesn't mention the flag yet. That's a docs line for relay-lead; the wire shape is unchanged.
- R8-2: `Snapshot()` never waits on a probe (`TestSnapshotNeverWaitsOnAProbe`).
- R8-11 (driver side): pi and codex slash commands call `Deps.ClearInput` before `agent.prompt`. go-server makes `clearInput` kind-aware.
- R8-17: SettingsGuard remembers that the file was absent and removes the settings file Claude created.
- R8-3 (driver side): `control_failed` quotes are scrubbed with the agent's cwd.
- R8-19 (driver side): `Controls.Forget(paneID)`; the control cache is capped at 256 files.
- go-server found that usage shutdown hung on a running probe. Fixed with ctx through probes (`TestRunStopsDuringAProbe`).

## Tests (Swift → Go)
| Swift file | Swift | Go here | Elsewhere |
|---|---|---|---|
| ControlsTests | 12 | 11 | `routes` → go-server |
| ClaudeControlsTests | 11 | 11 | |
| AgentDriversTests | 11 | 11 | |
| AgentDriversFlowTests | 8 | 8 | |
| UsageTests | 21 | 17 | 4 UsageRoutesTests → go-server |
| UploadsTests (+SentLog) | 14 | 13 | `machineKind` → go-core |
| From other files | | 2 | FuzzTests `uploadNameSanitizeIsAlwaysSafe`, MigrationTests `oldUploadMarkersStillResolve` |
| New | | 16 | R8 fixes, JSON shape, sent-log line shape, broadcast-only-on-change, `ForKind`, agentcli (4) |

Totals: controls 47, agentcli 4, usage 22, uploads 16. Go test names match the Swift names (`modelSwitchKeepsSavedDefault` → `TestModelSwitchKeepsSavedDefault`).

## Verification
- `gofmt -l` is clean. `go vet` and `GOOS=linux go vet` pass. `go test -race` passes for all four packages, also in a clean worktree at HEAD.
- Live read-only parity against Swift on 7878:
  - `GET /controls` and `/controls?kind=claude|codex` are identical.
  - `/agents/:id/controls` and model/modelLabel/effort/permissionMode for w14:p2, w14:p4 and w14:p5 are identical.
  - The only difference is one pi model that `pi --list-models` started listing after Swift's 10-minute cache filled. It isn't a code difference.
- `RELAY_LIVE_USAGE=1 go test -run TestLiveClaudeUsageProbe ./internal/usage` runs the real probe.

## What's left
- R8-11 needs go-server's kind-aware `clearInput` before qa-bridge can re-verify it on 7880.
- The api.md "Usage" command line should mention `--strict-mcp-config` (relay-lead).
- `pi --list-models` and `codex debug models` now have a 30 s timeout. Swift had none.
