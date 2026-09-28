# go-core report

## What changed
- `bridge-go/go.mod`: module `relay`, Go 1.24. Deps: `coder/websocket`, `fsnotify`, `rsc.io/qr`, `golang.org/x/sys` (sysctl, uname, fcntl in tests). `tools.go` pins deps that land later.
- `internal/api`: every wire type in `api.md`, with Swift's exact JSON shapes:
  - `Agent` optionals and `reply.live` `text`/`tool` are `null`. Optionals Swift synthesized are omitted (`omitzero`).
  - Non-optional arrays are always `[]`, never `null`.
  - `api.Marshal` doesn't HTML-escape (Swift doesn't). Use it for every body and frame.
  - `Block` and `ServerEvent` are tagged unions with constructors plus Marshal/Unmarshal.
  - `api.Error`, `FromError` (maps `herdr.Error` through `APIError()`), `FormatTime`/`ParseTimestamp`/`NormalizeTimestamp`, `UsageProvider.MarkedStale`/`SameDataExcludingFreshness`.
- `internal/herdr`:
  - `Client` interface and `SocketClient` (one connection per call, read deadline, ctx cancel).
  - `Error` with Swift's HTTP mapping.
  - `EventStream` (subscribe, per-pane watch, resync, backoff 0.5 to 5 s, keeps the newest 256 events).
  - `IsValidKey`/`FirstInvalidKey`.
  - `herdrtest`: a socket-level fake (Swift `FakeHerdr`) plus `World` helpers.
- `internal/config`: `Home`/`LegacyHome` (RELAY_HOME), token load/rotate (0600, base64url), `~/.herd` migration, `LogRotator`/`RunLogRotator`, `PairingURL`, Tailscale lookup (Linux: `$PATH`, `/usr/bin/tailscale`), the LaunchAgent plist, and the **new** systemd user unit.
- `internal/machine`: darwin (sysctl `hw.model`, IOPlatformUUID, ComputerName, `pmset` battery) and **new** linux (`/etc/machine-id`, hostname, `BAT*`, DMI product name or `uname -m`, os-release `PRETTY_NAME`). On this Mac the output matches the Swift `/machine` byte for byte.
- `internal/qr`: terminal QR with half-block characters (`rsc.io/qr`, level M, 2-module quiet zone).
- `cmd/relay`: `serve` (`--port`, `--local-only`, `--require-tailscale`; exits 75), `pair`, `token [--rotate]`, `install-launchd`, `install-systemd`, `--version`. `serve` cancels on SIGINT/SIGTERM and calls `service.Run`.
- `docs/api.md`: the Machine section now covers Linux.
- QA fixes (`docs/qa/round8.md`), commit ab7a06c:
  - R8-5: `unsupported_*` → `400 unsupported`, `*_taken` → `409` (herdr's code kept). go-server owns the retry loop.
  - R8-6: EOF or reset before an answer → `503 herdr_unavailable`.
  - R8-7: 3 s deadline on reads and lists, 10 s on actions, `agent.start` gets its own timeout + 10 s. The deadline covers the whole call. A hung herdr gives `504` in about 3 s, and 100 requests in flight don't slow the first one after recovery.
  - R8-12: SIGINT/SIGTERM cancel the context, and a second signal kills. Exit takes 0.1 s on macOS and 2 ms on Linux, once go-server's 728a367 landed.
  - R8-20: `serve` exits 78 with a clear message when the socket path is too long for `sun_path`.
  - R8-21: on start the home becomes 0700 and the log 0600.
  - For go-server's R8-4: `herdr.Client.CloseTab`, plus `herdrtest.Hangup{}`.

## Tests ported (Swift vs Go)
| Swift suite | Swift | Go (here) | Notes |
|---|---|---|---|
| KeyNamesTests | 4 | 4 | same names |
| LogRotatorTests | 2 | 2 | |
| MigrationTests | 4 | 3 (+3 Go-only: RELAY_HOME skip, token 0600, R8-21 home 0700) | `oldUploadMarkersStillResolve` is uploads logic, handed to go-drivers |
| FdHygieneTests | 2 | 2 | pipe watcher via `unix.FcntlInt` |
| SocketResilienceTests | 4 | 2 (+4 Go-only: timeout, ctx cancel, Watch resubscribe, Stop before Start; +5 QA: R8-5/6/7/20, CloseTab) | `restRouteAnswers503FastWhenHerdrDies` and `healthReflectsHerdrReachabilityAcrossARestart` need the router/monitor, handed to go-server |
| UploadsTests.machineKind | 1 | 1 | + fake-root Linux test, darwin shape test |
| Support/FakeHerdr | – | `herdrtest` | |
| (new) api contract | – | fixture round-trip over every `docs/fixtures` file, Swift shape table, timestamps, usage freshness | |

## Verification
- `gofmt -l`, `go vet`, `GOOS=linux go vet`, `go test -race` on my packages.
- Mutation check: renaming a JSON key makes the fixture test fail.
- `/machine` checked against the live Swift bridge.
- `serve` smoke on 7893 with a temp home: `/health`, `/machine` (same bytes as the Swift one), `/workspaces`. SIGTERM exit timed.
- Linux (podman `golang:1.24`, arm64):
  - `gofmt`, `go vet ./...` and `go test -race ./...` on the **whole module** pass. The one transcript failure is fixed by go-transcripts in 7e19ad1.
  - Runtime: `relay --version`, `install-systemd` (unit printed and checked), `serve`: `/health` gives `herdr: unavailable`, `/machine` reads os-release and DMI, SIGTERM exits in 2 ms, home 0700, token 0600.
  - The podman machine `podman-machine-default` is left running for others. Stop it with `podman machine stop`.

## Left
- Nothing open on my side.
- Cutover (lead): point the LaunchAgent at the Go binary with `relay install-launchd`. The plist has the same label, args and PATH as Swift's.
- Not verified: a real systemd host (`systemctl --user enable --now`); the container has no systemd.
- `herdr.Agent` keeps Swift's optional strings as plain strings (`""` = absent). Only `name` stays a pointer, because it goes out as `null`.
- Unknown `agent_status` values no longer fail the whole `agent.list` decode, as they did in Swift.
