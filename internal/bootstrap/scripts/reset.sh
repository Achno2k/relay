#!/usr/bin/env bash
# Undo everything agents bootstrap installed on this box. Run as the box user
# with passwordless sudo. Leaves the OS, ssh access and the user account alone.
set -u
step() { printf '%s\n' "-- $*"; }

step "stopping units"
sudo -n systemctl disable --now agents-bot.service herdr-server.service 2>/dev/null || true
sudo -n rm -f /etc/systemd/system/agents-bot.service /etc/systemd/system/herdr-server.service
sudo -n systemctl daemon-reload

step "signing out of harnesses and github"
gh auth logout --hostname github.com 2>/dev/null || true
codex logout 2>/dev/null || true
claude auth logout 2>/dev/null || true

step "removing binaries"
sudo -n rm -f /usr/local/bin/agents /usr/local/bin/herdr
sudo -n apt-get remove -y -qq gh >/dev/null 2>&1 || true
sudo -n rm -f /etc/apt/sources.list.d/github-cli.list /etc/apt/keyrings/githubcli-archive-keyring.gpg

step "removing runtimes, caches and state"
rm -rf "$HOME/.local/share/mise" "$HOME/.local/bin/mise" "$HOME/.config/mise" \
       "$HOME/.npm" "$HOME/.cache/go-build" "$HOME/go" \
       "$HOME/.claude" "$HOME/.claude.json" "$HOME/.codex" "$HOME/.config/gh" \
       "$HOME/.config/herdr" "$HOME/.agents" "$HOME/work" \
       "$HOME/.gitconfig"

step "cleaning shell profiles"
for rc in "$HOME/.profile" "$HOME/.bashrc"; do
    [ -f "$rc" ] || continue
    sed -i '/^# agents$/d; /^export AGENTS_ON_BOX=1$/d; /mise\/shims/d' "$rc"
done

step "apt cache"
sudo -n apt-get clean >/dev/null 2>&1 || true

step "done"
df -h / | tail -1
