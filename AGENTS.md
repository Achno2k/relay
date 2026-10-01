# Relay

Personal iOS app that drives the coding agents in a herdr session from the phone.

- `bridge/`: Go module `relay` (`cmd/relay`, packages in `internal/`). The `relay` binary runs on the Mac as the LaunchAgent `com.relay.bridge` (built to `bridge/bin/relay`, gitignored) or on Linux as the system unit `relay.service`, set up by `relay pair` (`docs/remote-setup.md`), keeps its data in `~/.relay`, talks to herdr's unix socket (`$HERDR_SOCKET_PATH`, default `~/.config/herdr/herdr.sock`) and tails agent transcripts. Checks: `gofmt -l .`, `go vet ./...`, `GOOS=linux go vet ./...`, `go test -race ./...`.
- The same `relay` binary is the laptop CLI that sets up an EC2 box (`init`, `doctor`, `ssh`, `attach`, `deploy`, `reset`, `env`; was agents-cli). cobra, one file per command in `internal/cli`. Laptop config is `~/.relay/config.toml`; on the box `RELAY_ON_BOX=1` / `~/.relay/on-box`. Laptop terminal output goes through `internal/ui`.
- `ios/`: the Relay app. SwiftUI, iOS 26+, Swift 6, Liquid Glass, modelled on the ChatGPT iOS app. XcodeGen (`ios/project.yml` → `Relay.xcodeproj`, scheme `Relay`), shared code in the local `RelayKit` package. It keeps the bundle id `dev.amansingh.herd` and App Group `group.dev.amansingh.herd` so installs update in place. It pairs via `relay://` and still accepts old `herd://` links.
- `docs/api.md` is the contract between them. Change it first. Fixtures live in `docs/fixtures/`.
- `docs/herdr-schema.json` is herdr's socket API schema (protocol 22).

Rules
- Never commit real transcripts, agent lists or paths from this machine. Fixtures are synthetic.
- Full filesystem paths never cross the wire (`cwdName` only; tool summaries are cwd-relative).
- Status is re-derived from herdr, never cached as truth.
- No third-party UI libraries in the app.
