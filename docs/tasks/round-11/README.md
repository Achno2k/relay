# Round 11: relay-cli (agents-cli moves into relay)

Cloud VM setup is manual today (`docs/remote-setup.md`). `~/projects/agents-cli` already sets up an EC2 box: base tools, herdr, agent CLIs and their logins, GitHub auth, clone, and a Claude-generated environment plan. It drives the box from Slack. We fold it into this repo so one `relay` binary does both jobs, and drop Slack.

Read first: `../../../AGENTS.md`, `../../api.md`, `../../remote-setup.md`, and in `~/projects/agents-cli`: `PLAN.md`, `CLAUDE.md`, `README.md`.

## The two ways to set up a VM
- **Path 1, on the VM.** The user installs relay (`curl …/install.sh | sh`) and runs `relay pair`. That one command installs whatever is missing (herdr + its server, Tailscale + login QR, the bridge service) and ends with the pairing QR. The user does nothing else.
- **Path 2, from the laptop.** `relay init` is agents-cli's `agents init` flow (AWS profile, region, existing instance, SSH, harnesses, bootstrap, logins, GitHub, clone, env plan). At the end it runs `relay pair` on the box over an interactive SSH session, so both QR codes show in the laptop terminal.

## Decisions (final, from the user)
- One Go module: `bridge/` (module `relay`). The directory keeps its name so the Mac LaunchAgent path doesn't change. Go 1.25.
- One binary, `relay`, built from `bridge/cmd/relay`.
  - Laptop commands: `init`, `doctor`, `ssh`, `attach`, `deploy`, `reset`, `env`.
  - Box and Mac commands: `serve`, `pair`, `token`, `install-launchd`, `install-systemd`.
- cobra for every command. Existing flag names stay (`serve --port --require-tailscale --local-only`, `pair --port --host --url`, `token --rotate`). The LaunchAgent plist passes `serve --port 7878 --require-tailscale`, so that must keep working.
- agents-cli packages go to `bridge/internal/`: `cli`, `ui`, `awsx`, `sshx`, `bootstrap`, `envplan`, `harness`.
  - agents-cli's `config` (laptop `config.toml`) merges into relay's `internal/config`.
  - agents-cli's `herdr` client is deleted, since only Slack used it. Relay's `internal/herdr` stays.
- **Slack is gone:** `slackbot`, `state` (sqlite), `worktree`, the `sessions` and `bot` commands, `agents-bot.service`, the Slack init steps, `agent-instructions.md` / `instructions.go` (the reply.md contract), `HarnessArgs`, `docs/slack-manifest.yaml`, and the slack-go + sqlite deps.
- `attach` becomes plain `herdr --remote <user>@<host>`, with no session ids.
- Home directory: `~/.agents` becomes `~/.relay`.
  - Laptop: `~/.relay/config.toml`. Move an existing `~/.agents/config.toml` over on first run, like the `~/.herd` move in `config.home`.
  - Box: the relay home is also `~/.relay`. `AGENTS_ON_BOX` / `AGENTS_HOME` become `RELAY_ON_BOX` / `RELAY_HOME`.
- **Linux services are system units with `User=`**, as agents-cli does, not user units, so no linger is needed. Two units:
  - `herdr-server.service`: `herdr server`.
  - `relay.service`: `relay serve --port 7878 --require-tailscale`, `Restart=always`, after `herdr-server` and `tailscaled`.
  - The Mac keeps its LaunchAgent. `install-systemd` writes the system unit (needs sudo).
- **Agents on the box start in auto mode**, set in each CLI's own config during `relay init`, after login:
  - Claude: `permissions.defaultMode: "auto"` in `~/.claude/settings.json`.
  - Codex: the closest equivalent in `~/.codex/config.toml` (`approval_policy = "on-request"`, `sandbox_mode = "workspace-write"`).
  - pi: nothing to set.
  - No `--dangerously-skip-permissions` anywhere.
- Harnesses: claude, codex, and pi (npm `@earendil-works/pi-coding-agent`, binary `pi`).
- After cloning a repo, `relay init` creates a herdr workspace with that repo as its cwd, so the app's "New chat" can start agents there (`POST /agents` uses a workspace's cwd).
- `relay init` never creates instances. The user picks an existing one.
- Releases: goreleaser builds `relay` for darwin/arm64, linux/amd64 and linux/arm64. The public GitHub repo will be `Achno2k/relay`. The lead creates it and pushes; workers never push.

## Workers
| name | worktree | branch (base) | owns |
|---|---|---|---|
| w-merge | `~/projects/relay-r11-merge` | `r11-merge` (master) | the import, layout, cobra port, Slack removal, `~/.relay` move |
| w-pair | `~/projects/relay-r11-pair` | `r11-pair` (r11-merge) | `relay pair` on Linux, `internal/setup`, both unit templates, `docs/remote-setup.md`, the pairing section of `docs/api.md` |
| w-init | `~/projects/relay-r11-init` | `r11-init` (r11-merge) | `init`/`deploy`/`doctor`/`reset`/`attach`, bootstrap.sh, auto mode, pi, workspace per repo, goreleaser + `scripts/install.sh`, `docs/relay-cli.md` |

w-merge runs alone first. w-pair and w-init start from its final commit.

Stay inside your own worktree. Never touch the main checkout (`~/projects/relay`) or `~/projects/agents-cli`. Reading agents-cli and fetching from it is fine.

## w-merge
1. **Bring in agents-cli with its history.**
   - `git fetch ~/projects/agents-cli main:import/agents-cli`. Its local `main` has 7 commits that were never pushed; you need those too.
   - In a temporary worktree of *this* repo on that branch, make one commit that moves its files to their new homes:
     - `internal/<pkg>` goes to `bridge/internal/<pkg>`.
     - The colliding packages (`config`, `herdr`) go to a temporary name.
     - `scripts/install.sh` goes to `scripts/install.sh`.
     - `.goreleaser.yaml` goes to the repo root.
     - `.github/workflows/ci.yml` goes to `.github/workflows/ci.yml`.
     - Fold the docs in, or drop them.
   - Merge it into `r11-merge` with `--allow-unrelated-histories`. `git log --follow` on a moved file must show its agents-cli history.
   - Drop `.claude/`, `bin/` and `cmd/agents`.
2. Module paths become `relay/internal/...` and go.mod is one file at Go 1.25. Merge the configs, delete agents-cli's herdr client, delete the Slack pieces (list above).
3. Port the bridge subcommands in `bridge/cmd/relay/main.go` to cobra in `internal/cli`, one file per command. `main.go` becomes a thin `cli.Execute()`. Keep the exit codes `serve` uses (75, 78). Fix any scripts or docs that pass single-dash flags (`-port`).
4. `~/.agents` → `~/.relay`, and the env var renames.
5. Make the rest compile and work with Slack gone. Keep `init`, `deploy`, `doctor` and `reset` doing what they did, minus the bot. Deeper changes are w-init's.
6. Done when all checks pass, `go build -o /tmp/relay ./cmd/relay && /tmp/relay serve --port 7999 --local-only` serves `/health`, and `relay --version` works. Don't touch the running LaunchAgent or `bridge/bin/relay`.

## w-pair
- `relay pair` on Linux. Each step checks first and is safe to run again:
  1. sudo: passwordless, or prompt once.
  2. herdr: install it if missing, using the same manifest and checksum logic as agents-cli's `bs_herdr`. Install `herdr-server.service` only if no herdr server already answers on the socket.
  3. Tailscale: install it with `https://tailscale.com/install.sh` if missing. If it's logged out, run `tailscale up` and show the login URL as a terminal QR (`internal/qr`). `--authkey <key>` skips the login.
  4. Write and enable `relay.service` (system unit). Restart it if the binary changed. Wait for `/health` on the Tailscale IP (timeout with a clear error).
  5. Print the pairing QR + link, as today.
- On macOS, `pair` stays as it is: it just prints.
- Flags: `--authkey`, `--port`, `--yes` (no confirmations, for `relay init`), plus the existing `--host` / `--url`. Put the logic in a new `internal/setup` package with tests for the pure parts (unit text, step decisions).
- You own both unit templates. w-init calls `relay pair --yes` and doesn't install units itself.
- Test it in a podman Ubuntu 24.04 container with systemd as PID 1, as round 9 did (`docs/remote-setup.md` "Tested"). Use a stub `tailscale` there. Running it twice must change nothing the second time.
- Rewrite `docs/remote-setup.md` around Path 1 and Path 2, and update the pairing section of `docs/api.md`.

## w-init
- `init`: drop the Slack steps. Then, after the env plan:
  1. Write the auto-mode settings.
  2. Run `relay pair --yes` interactively over SSH.
  3. Create the herdr workspace per repo.
  - The summary shows how to pair and attach.
- bootstrap: install `relay` to `/usr/local/bin/relay`. Add pi to the harnesses (install, login check, login command; see `internal/harness`). Remove unit installation (w-pair owns the units).
- The binary for the box:
  - Built from source when running from a checkout (`BuildSourceDir`, as now).
  - A released binary uploads itself when the box's OS/arch match. Otherwise it downloads the matching release asset and checks its checksum.
- `deploy` restarts `relay.service`. `doctor` adds checks for the bridge, Tailscale and version match. `reset` removes `relay.service` and `herdr-server.service`.
- goreleaser at the repo root builds `bridge/cmd/relay`. `scripts/install.sh` downloads from `Achno2k/relay` releases and checks `checksums.txt`. CI runs the bridge checks.
- `docs/relay-cli.md`: a short user doc covering both paths and the commands.

## Repo rules
- Checks, in `bridge/`: `gofmt -l .`, `go vet ./...`, `GOOS=linux go vet ./...`, `go test -race ./...`.
- No real hostnames, IPs, key paths, transcripts or chat titles in commits. Fixtures are synthetic.
- Full paths never cross the wire.
- All terminal output for the laptop commands goes through `internal/ui`. Keep agents-cli's look.
- Commit on your branch only, with `git add <paths>`, never `-a`/`-A`. Never push, never merge into master. The lead merges.
- Never touch the user's agents, herdr panes, the Mac LaunchAgent, the iPhone, or any EC2 box. The lead runs the box tests.

## Reporting
Append one time-prefixed line per milestone to `/private/tmp/claude-504/-Users-you-projects-relay/aa6e06bd-1c65-4a90-92b1-bd6f6916ce71/scratchpad/r11/status-<name>.md`:
`plan ready` · `<step> done` · `checks pass` · `COMMITTED <sha>` · `DONE <sha>`.
Questions for the lead start with `QUESTION:`; then wait. Never type into the lead's pane. When done, write `docs/tasks/round-11/<name>-report.md` (what changed, how it was verified, known gaps), commit it, and write `DONE <sha>`.

## 2026-10-01 box test fixes
Box test on a real EC2 box (Ubuntu 26.04, x86_64) passed Path 1 and Path 2. Fix these, each on your branch after `git merge --ff-only master` (master = r11 with both your branches merged):
- **w-pair**
  1. `relay pair` must run `herdr integration install <kind>` for each of claude, codex, pi that is on PATH (include the mise shims dir, `~/.local/bin`, npm global bin), only if not installed yet. Without it herdr reports `agent_session: null`, the bridge never finds the transcript and the app shows an empty chat. This covers both paths, since `relay init` runs `relay pair --yes` after the harnesses are installed.
  2. The Tailscale install script's output (apt lines, `+ set +x`, "Installation complete!") spills into the terminal. Run it behind a spinner step and keep the output in a log; print it only on failure.
- **w-init**
  1. After cloning, pre-trust the repo folder for Claude (`~/.claude.json` → `projects["<abs path>"].hasTrustDialogAccepted = true`, keep other keys), so a new agent from the app doesn't stop at "Trust this folder?". Same idea for Codex if it has a trust prompt.
  2. UI: after the "SSH private key" input, the collapsed answer line is blank. It should show the path.
  3. Auto mode: Claude Code now defaults to auto mode on its own (seen on the box: "Auto mode is now Claude Code's default permission mode"). Leave Claude alone. Drop the gap note for Claude; Codex stays as is.
