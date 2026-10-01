# w-merge report

## What changed
- **Import.** `import/agents-cli` has all 55 local agents-cli commits, including the 7 that were never pushed. One move commit (`3e189fb`) puts `internal/<pkg>` under `bridge/internal/`. `config` and `herdr` go to `agentsconfig` / `agentsherdr` for that commit only. It also moves `scripts/install.sh`, `.goreleaser.yaml`, `.github/workflows/ci.yml` and `docs/iam-policy.json`, and `Makefile` goes to `bridge/Makefile`. It drops `cmd/agents`, `go.mod`, `PLAN.md`, `CLAUDE.md`, `README.md` and the Slack manifest (`.claude/` and `bin/` were never tracked). Merged with `--allow-unrelated-histories` in `496d044`.
- **Module.** `bridge/go.mod` is the only go.mod (`relay`, Go 1.25.0). The slack-go and sqlite deps are gone.
- **Config.** `internal/config/config.go` (git-moved from agentsconfig):
  - `Config{AWS, Box, Harness, Repos}` in `~/.relay/config.toml` (`ConfigPath`).
  - `Load` moves `~/.agents/config.toml` over once (`MigrateAgentsConfig`, skipped when `RELAY_HOME` is set).
  - `OnBox` reads `RELAY_ON_BOX` or `~/.relay/on-box`.
  - `Slack`, `SessionPolicy` and `HarnessArgs` are removed.
- **Slack removed.** Deleted: `slackbot`, `state`, `worktree`, `agentsherdr`, `cli/bot.go`, `cli/sessions.go`, `bootstrap/slackcheck*`, `bootstrap/instructions*`, `agent-instructions.md`, `agents-bot.service`, the Slack init steps and `--skip-slack`. Also removed: the `bypassPermissionsModeAccepted` write, which only existed for unattended bot mode.
- **cobra.** `internal/cli/{serve,pair,token,install_launchd,install_systemd}.go` hold the bridge commands, and `main.go` is just `cli.Execute()`.
  - Flag names are unchanged. `serve` still exits 75 and 78 (78 checked by hand).
  - `--version` prints only the version, as before. `relay version` prints `relay <v>` through `ui`.
  - `cli.Version` is `api.Version`, so `/health` and the CLI report the same version.
- **Renames.** `~/.agents` → `~/.relay`. `AGENTS_ON_BOX` / `AGENTS_HOME` / `AGENTS_SRC` / `AGENTS_UI_PLAIN` → `RELAY_*`. Box binary `/usr/local/bin/agents` → `/usr/local/bin/relay`, built from `./cmd/relay`, and `ModulePath` is `relay`. The banner spells RELAY (new R, L, Y glyphs).
- **Commands without the bot:**
  - `attach` takes no argument and runs `herdr --remote <user>@<host>`.
  - `deploy` builds, uploads and syncs config, then restarts `relay.service` if it is active.
  - `reset` removes `relay.service`, the legacy `agents-bot.service`, `herdr-server.service`, both binaries and both homes on the box.
  - `reset --local` deletes only `config.toml` and `cm/`, because the Mac's `~/.relay` also holds the bridge token.
  - `init` still installs `herdr-server.service` through `bootstrap.InstallUnits`, which w-init removes.
- **Build files.** `.goreleaser.yaml` builds `bridge/cmd/relay` (`dir: bridge`) to `Achno2k/relay`. CI runs the four bridge checks. `install.sh` is renamed to relay. `bridge/Makefile` stamps `api.Version` and `BuildSourceDir`. `dist/` is in the root `.gitignore`.
- AGENTS.md has one new line about the laptop CLI.
- Scripts and docs already used double-dash flags, so nothing needed changing there.

## Verification
- `gofmt -l .` is empty, and `go vet ./...` and `GOOS=linux go vet ./...` are clean. `go test -race ./...` passes, including new `config_test.go` tests: the agents config move, an existing relay config is never overwritten, Load/Save round trip at 0600, and the on-box marker.
- I built to the scratchpad and ran `relay serve --port 7999 --local-only` with a temp `RELAY_HOME`. `/health` returned `{"ok":true,"name":"relay","version":"0.1.0",...}`.
- `relay --version` printed `0.1.0`. `relay pair --host` printed the QR and link.
- A socket path that is too long made `serve` exit 78.
- `bash -n` passes on bootstrap.sh and reset.sh, and `sh -n` on install.sh.
- `git log --follow bridge/internal/cli/init.go` shows the agents-cli history. `config.go` was mostly rewritten, so it needs `git log --follow -M30%`.
- Not touched: the LaunchAgent, `bridge/bin/relay`, `~/.relay`, and any box.

## Known gaps (for w-pair / w-init)
- Running `relay` with no arguments now prints help. It used to run `serve`. Every unit and script passes `serve`, so nothing breaks.
- `install-systemd` still writes the user unit. w-pair owns the system units.
- `init` still installs `herdr-server.service` and has no auto mode, pi, `relay pair` or workspace step. `deploy` doesn't fall back to a release binary. `doctor` has no bridge, Tailscale or version checks. All of that is w-init's.
- `install.sh` doesn't verify checksums yet (w-init).
- A box set up by agents-cli keeps `~/.agents/on-box` and `AGENTS_ON_BOX` until it is bootstrapped again. `OnBox` reads only the new names.
- The imported history still contains agents-cli's old demo strings (a real AWS profile name and an example EC2 hostname). The current files use synthetic values.
