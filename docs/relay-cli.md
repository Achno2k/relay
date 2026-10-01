# relay CLI

One `relay` binary runs the bridge (on the Mac or a Linux box) and sets up a cloud box from your laptop. This page covers the setup side. The bridge API is in `api.md`, and the manual box setup is in `remote-setup.md`.

## Install
```sh
curl -fsSL https://raw.githubusercontent.com/Achno2k/relay/main/scripts/install.sh | sh
relay --version
```
- Builds exist for darwin/arm64, linux/amd64 and linux/arm64.
- The script checks the download against the release's `checksums.txt`.
- `RELAY_VERSION=0.2.0` installs that release instead of the latest one. `RELAY_INSTALL_DIR=~/bin` installs somewhere other than `/usr/local/bin`.
- From a checkout: `cd bridge && make install`, which puts it in `~/.local/bin`.

## Two ways to set up a box
**Path 1: on the box.** Install relay there, then:
```sh
relay pair
```
- It installs whatever is missing: herdr and its server, Tailscale (shows a login QR), and `relay.service`.
- It ends with the pairing QR. Scan it with the app.
- Install and log in to the agent CLIs (claude, codex, pi) yourself.

**Path 2: from the laptop.** Stand in the repo you want on the box, then:
```sh
relay init
```
1. Pick the AWS profile, region and an existing instance. `relay init` never creates instances.
2. Pick the harnesses: claude, codex, pi.
3. Box setup: base tools, GitHub CLI, mise + node 22, herdr, the harnesses, and `relay` in `/usr/local/bin`.
4. Sign in to each harness. You open the URL it prints and paste the code back. For pi, type `/login` in pi, then `/quit`.
5. GitHub auth if needed (a token is recommended), then clone the repo to `~/work/<repo>/main`. Claude Code and Codex are told to trust that folder, so an agent started there from the app doesn't stop at a trust prompt.
6. A harness writes the repo's environment plan, which then runs on the box.
7. `relay pair --yes` runs on the box in your terminal. Both QR codes (Tailscale login and pairing) show up here.
8. A herdr workspace named after the repo, with the checkout as its cwd. The app's "New chat" can start agents there.

- Run it again at any time. Every step checks first, and it offers to resume with the saved box.
- `relay init --fresh` picks the instance again.

### The box binary
`relay init` and `relay deploy` put a linux binary on the box:
- From a checkout (or with `RELAY_SRC=<checkout>/bridge`), they build one from source.
- A released relay on linux with the box's arch uploads itself.
- Any other release downloads the matching release asset and checks its checksum.

## Commands
Laptop:

| command | does |
|---|---|
| `relay init` | Path 2 above |
| `relay doctor` | checks the local tools, the config, the box (reachable, disk), its Tailscale, `relay.service` + `/health`, and that the box runs this relay version |
| `relay ssh [cmd…]` | shell on the box, or runs a command there (`relay ssh relay pair` reprints the QR) |
| `relay attach` | `herdr --remote <user>@<host>` |
| `relay deploy` | puts this relay on the box, syncs the config, restarts `relay.service` |
| `relay env setup\|verify\|show\|regen <repo>` | the repo's environment plan (runs on the box) |
| `relay reset` | wipes what relay installed on the box: both units, logins, runtimes, clones. `--local` also deletes the laptop config |

Box and Mac:

| command | does |
|---|---|
| `relay serve --port 7878 [--require-tailscale] [--local-only]` | the bridge |
| `relay pair [--host H] [--port P] [--url U]` | pairing QR + link. On Linux it also sets up the box (Path 1) |
| `relay token [--rotate]` | prints or rotates the bridge token |
| `relay install-launchd` / `relay install-systemd` | install the service |

## Files
- Laptop: `~/.relay/config.toml` holds the box, harnesses and repos. An old `~/.agents/config.toml` moves here on first run.
- Box: `~/.relay` (config, plans, the `on-box` marker), `RELAY_ON_BOX=1` in `~/.profile`. Units: `herdr-server.service` and `relay.service`.
- `docs/iam-policy.json` is the AWS policy `relay init` needs.
