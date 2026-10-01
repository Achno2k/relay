# w-init report

## What changed
- **init.** After the env plan:
  - It runs `relay pair --yes` on the box through `sshx.Interactive`, so the Tailscale login QR and the pairing QR show in the laptop terminal. If that fails, the error says to retry with `relay ssh relay pair`.
  - It creates a herdr workspace for the repo cloned in this run (`bs_workspace`). The workspace is labelled with the repo name and uses `~/work/<repo>/main` as its cwd. If a workspace with that label already exists, it is left alone.
  - The summary shows `relay ssh relay pair` (print the QR again), `relay attach` and `relay ssh`.
  - The Slack steps were already gone after w-merge.
- **bootstrap.**
  - The unit install is removed: `InstallUnits`, `EnableHerdrServer`, the embedded `herdr-server.service` and `Units`. What was left in `units.go` moved to `remote.go`.
  - The binary step is now "Installing relay to /usr/local/bin/relay".
  - New tasks: `bs_pi` and `WorkspaceTask`.
- **pi harness** (`harness/pi.go`, `headless_pi.go`):
  - Install: npm `@earendil-works/pi-coding-agent`.
  - Login check: `auth.json` has at least one provider. `pi auth check` needs a provider name, so it can't be used here.
  - Login: pi has no login subcommand, so this opens the `pi` TUI. A new optional `harness.LoginHinter` tells the user to type `/login` and then `/quit`.
  - Headless: `pi --print --no-session`, so `relay env --harness pi` works.
  - It shows up in the init picker automatically.
- **Box binary** (`bootstrap.BoxBinary`, `release.go`):
  - If the source tree can be found, it builds from source as before. `BuildForBox` now stamps `api.Version`, so doctor's version check matches in dev.
  - Otherwise, if the laptop binary is linux/<box arch>, it uploads itself.
  - Otherwise it downloads `relay_linux_<arch>.tar.gz` from release `v<Version>` and checks it against `checksums.txt`.
  - Dev, snapshot and dirty versions refuse to download and point to `RELAY_SRC`.
- **deploy** uses `BoxBinary`, then runs `RestartRelay`. That restarts `relay.service`. If the unit isn't installed, it fails and says to run `relay pair`.
- **doctor.** New checks:
  - `box tailscale`: `tailscale status --json` reports `BackendState`.
  - `box bridge`: `relay.service` is active, `/health` answers on 127.0.0.1:7878, and herdr shows as connected.
  - `box version`: the box binary's `--version` and the running bridge's `/health` version both equal this binary's version.
- **reset.**
  - Signs pi out (`auth.json`) and removes `~/.pi`.
  - `rs_profiles` now also strips the legacy `export AGENTS_ON_BOX=1`. The rename had turned that sed into a duplicate of the `RELAY_ON_BOX` one.
  - It already removed both units.
- **Release.**
  - `scripts/install.sh` checks the archive against `checksums.txt` (sha256sum or shasum).
  - New env vars: `RELAY_VERSION` pins a release, `RELAY_INSTALL_DIR` sets the install dir, and `RELAY_RELEASES` overrides the release URL for tests.
  - New `.github/workflows/release.yml` runs goreleaser on `v*` tags. CI already ran the bridge checks.
- **Docs.** New `docs/relay-cli.md`: install, both paths, the box binary, the command tables and files.

## Verification
- All four bridge checks pass: `gofmt -l .`, `go vet ./...`, `GOOS=linux go vet ./...` and `go test -race ./...`.
- New tests:
  - `release_test.go` (httptest release): checksum ok, checksum mismatch, missing asset, 404, and non-release versions.
  - `workspace_test.go`: runs `bs_workspace` twice against a fake `herdr`. It creates one workspace with the right cwd and is a no-op the second time. It also checks quoting.
  - `doctor_test.go`: the tailscale, health and version parsers.
  - `TestHarnessTasks` now covers pi.
- `bs_auto_claude` and `bs_auto_codex` were run twice in a temp HOME. The first run sets the keys and keeps other settings and TOML tables. The second run changes nothing.
- `install.sh` was run against a local fake release: a good archive installs and runs `--version`, and a tampered archive fails with a checksum mismatch.
- `goreleaser check` passes. `goreleaser release --snapshot` builds the three archives (each holds only `relay`) and `checksums.txt`. I deleted `dist/` afterwards.
- `relay doctor` with an empty `RELAY_HOME` lists the new checks as skipped.
- Not run: anything against a real box. The lead does the box tests.

## Known gaps
- **Auto mode is not wired in**, per the user. `bs_auto_claude` (`permissions.defaultMode: "auto"`) and `bs_auto_codex` (`approval_policy = "on-request"`, `sandbox_mode = "workspace-write"`) are in `bootstrap.sh` and tested by hand, but init doesn't call them. The classifier blocked the Go wiring (`AutoModeTasks` plus an init step).
- `relay pair --yes` only exists on w-pair's branch. On `r11-init` alone, init's pairing step fails with "unknown flag" until the branches are merged.
- Merge note: I deleted `bootstrap/scripts/herdr-server.service` and `bootstrap/units.go`. If w-pair moved or edited either one, take w-pair's version under `internal/setup` and keep my deletion here.
- Only the repo cloned in the current run gets a workspace. Repos cloned earlier get one the next time `relay init` runs from them.
- The pi login hint assumes the user picks a provider in the TUI. Credentials set only through env vars (API keys) read as "not signed in".
- A `git describe` version between tags, like `v0.1.0-3-gabc`, tries to download a release that doesn't exist. It fails with a 404 instead of the `RELAY_SRC` hint.
- doctor assumes the bridge port is 7878.
