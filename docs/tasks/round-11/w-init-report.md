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
- **Codex auto mode is not wired in**, per the user. `bs_auto_codex` (`approval_policy = "on-request"`, `sandbox_mode = "workspace-write"`) is in `bootstrap.sh` and tested by hand, but init doesn't call it. Claude Code needs nothing, since auto mode is already its default.
- `relay pair --yes` only exists on w-pair's branch. On `r11-init` alone, init's pairing step fails with "unknown flag" until the branches are merged.
- Merge note: I deleted `bootstrap/scripts/herdr-server.service` and `bootstrap/units.go`. If w-pair moved or edited either one, take w-pair's version under `internal/setup` and keep my deletion here.
- Only the repo cloned in the current run gets a workspace. Repos cloned earlier get one the next time `relay init` runs from them.
- The pi login hint assumes the user picks a provider in the TUI. Credentials set only through env vars (API keys) read as "not signed in".
- A `git describe` version between tags, like `v0.1.0-3-gabc`, tries to download a release that doesn't exist. It fails with a 404 instead of the `RELAY_SRC` hint.
- doctor assumes the bridge port is 7878.

## Box test fixes (2026-10-01)
- **Trust after clone.** `init` adds a "Trust" step right after the clone, for each picked harness that asks (`bootstrap.TrustTasks`).
  - `bs_trust_claude` sets `projects["<abs path>"].hasTrustDialogAccepted = true` in `~/.claude.json` with jq and keeps every other key.
  - `bs_trust_codex` sets `trust_level = "trusted"` under `[projects."<abs path>"]` in `~/.codex/config.toml`, which is what Codex's own trust prompt writes. An existing table for that path keeps its other keys, and a missing one is appended.
  - The path is the checkout's physical path (`pwd -P`). pi is left alone. Its `--approve` / `trust.json` covers project-local pi files, not the folder, and I haven't checked whether it prompts on a fresh box.
- **SSH key answer line.** New `ui.InputDefault` returns the placeholder when nothing is typed and shows the value actually used on the collapsed line. `awsx.askKeyPath` uses it. `ui.Input` is unchanged, because `reset` must not treat its placeholder ("Delete") as a typed confirmation.
- **Auto mode.** `bs_auto_claude` is gone and the Claude gap note is dropped. `bs_auto_codex` stays, still not wired in.
- **Verification.**
  - `trust_test.go` runs both trust functions twice in a temp HOME. The first run writes the trust and keeps the other JSON keys and TOML keys, the second run is a no-op, and a missing Codex table gets appended.
  - `TestInputDefaultShowsTheValueUsed` covers the default and a typed path.
  - All four bridge checks pass.

## Bug: agents start for CLIs that aren't signed in (2026-10-01)
- **Contract** (`011ccba`):
  - `docs/api.md` has the new `GET /kinds` row, an "Agent kinds" section (KindStatus, the sign-in checks, exact hints, ≤ 15 s cache), and the two `409`s under "Starting an agent".
  - Fixture `docs/fixtures/kinds.json`, with `api.KindStatus` covered by the fixture round-trip test.
- **Bridge.** New package `internal/kinds` with a `Checker`:
  - **Installed:** the CLI is on `agentcli.LookPath`, the same lookup the usage code uses.
  - **claude:** signed in if `$CLAUDE_CONFIG_DIR` or `~/.claude/.credentials.json` exists. Otherwise it asks `claude auth status --json` (the macOS keychain case).
  - **codex:** signed in if `CODEX_API_KEY` or `OPENAI_API_KEY` is set, or `$CODEX_HOME` or `~/.codex/auth.json` exists.
  - **pi:** signed in if `$PI_CODING_AGENT_DIR` or `~/.pi/agent/auth.json` holds at least one provider.
  - The `internal/harness` checks are bash run over ssh on the box, so they don't fit the bridge. The logic is the same.
  - `All` serves `/kinds` with a 15 s cache. `Startable` re-checks without the cache and returns the 409.
- **Server.** `Options.Kinds` serves `GET /kinds`, and `create` calls `Startable` after body validation and before `Backend.Create`, so herdr is never touched for a blocked kind. Kinds outside claude, codex and pi aren't gated. `service.Run` wires `kinds.New`.
- **Verification.**
  - `internal/kinds` tests cover:
    - nothing installed, and installed but signed out (each hint);
    - each sign-in source: credentials file, `claude auth status`, API key env, `CODEX_HOME`, `PI_CODING_AGENT_DIR`;
    - the cache: still answers at TTL−1 s and refreshes at the TTL;
    - `Startable` ignoring the cache, and an unknown kind not being gated.
  - `internal/server` tests: `/kinds` needs the token, and `POST /agents` returns `409 not_signed_in` / `not_installed` with the hint, without calling `Backend.Create`.
  - Live on the Mac: a bridge on 7999 reported all three kinds signed in, with claude going through the keychain path. With an empty `CODEX_HOME` and no API keys, codex showed `signedIn:false` with the hint, and `POST /agents` for codex answered `409 not_signed_in`.
  - All four bridge checks pass.
- **Gaps.**
  - If the claude credentials file is missing, `claude auth status` runs. It can take up to 8 s and holds the `/kinds` cache lock while it runs.
  - Credentials that the CLIs read from env vars this check doesn't look at (e.g. `ANTHROPIC_API_KEY` without `claude auth status` agreeing) read as signed out.
