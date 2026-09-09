# agents-cli

`agents` runs coding agents (Claude Code, Codex) on a single EC2 box, drives
them from Slack, and lets you attach your laptop terminal to the same panes.

One binary, two sides:

- laptop: `agents init`, `agents attach`, `agents ssh`, `agents doctor`
- box (under systemd): `agents sessions`, `agents env`, `agents bot`

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/Achno2k/agents-cli/main/scripts/install.sh | sh
```

Or download a release tarball (darwin/arm64, linux/amd64, linux/arm64) and put
`agents` on your PATH. The install script prefers `/usr/local/bin` and falls
back to `~/.local/bin`.

Local dependencies: `ssh`; `aws` CLI for setup; `herdr` for attach;
`session-manager-plugin` only if the box uses SSM transport. `agents doctor`
checks all of these plus connectivity to the box.

## First run

```sh
cd your-repo
agents init
```

Picks an AWS profile/region/instance, saves `~/.agents/config.toml`, installs
base tools, herdr, and the harnesses on the box, clones the repo, and runs the
generated environment plan (you approve it first). Run `agents doctor`
afterwards to verify the setup.

## Slack

`agents init` optionally writes Slack tokens and enables the bot on the box.
Mention the bot in a new thread to create a session for the channel's default
repo; replies in that thread are forwarded to the agent and the bot posts
status updates plus the agent's final reply. `@bot attach <name>` and friends
override the defaults. There is a Slack user allowlist.

## Attach

```sh
agents attach <session-id|agent-name>
```

Marks the session as attached by `<user>@<hostname>` on the box
(`agents sessions mark-attached <id> --by <user>@<host>` over ssh), then runs
`herdr --remote <user>@<host>` so your terminal mirrors the agent's pane.
Detach by exiting herdr. The box must be reachable with plain ssh from your
laptop for the herdr leg.
