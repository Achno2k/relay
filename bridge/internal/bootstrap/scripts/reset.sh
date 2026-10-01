#!/usr/bin/env bash
# Undo everything agents bootstrap installed on this box. Run as the box user
# with passwordless sudo. Leaves the OS, ssh access and the user account alone.
#
# One function per phase so the Go side can run them one at a time and render
# each as its own ui.Step:
#
#     bash reset.sh rs_units
#
# A phase exits non-zero only when something that is actually there refuses to
# go away. Things that were never installed are reported and skipped.
set -u

export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:/usr/local/bin:$PATH"
export DEBIAN_FRONTEND=noninteractive

fail=0
note() { printf '  %s\n' "$*"; }
die()  { printf '  error: %s\n' "$*" >&2; fail=1; }
have() { command -v "$1" >/dev/null 2>&1; }
finish() { return "$fail"; }

# rm_user removes a file or tree owned by this user. The Go module cache under
# ~/go is written read-only, so without the chmod every single file under it
# fails with "Permission denied" — thousands of lines and nothing deleted.
rm_user() {
    local p="$1"
    [ -e "$p" ] || [ -L "$p" ] || return 0
    chmod -R u+w "$p" 2>/dev/null || true
    rm -rf "$p" 2>/dev/null || true
    if [ -e "$p" ] || [ -L "$p" ]; then
        sudo -n chmod -R u+w "$p" >/dev/null 2>&1 || true
        sudo -n rm -rf "$p" >/dev/null 2>&1 || true
    fi
    if [ -e "$p" ] || [ -L "$p" ]; then
        die "could not remove $p"
        return 1
    fi
    note "removed $p"
}

# rm_root removes a path that belongs to root.
rm_root() {
    local p="$1"
    [ -e "$p" ] || [ -L "$p" ] || return 0
    sudo -n rm -rf "$p" >/dev/null 2>&1 || true
    if [ -e "$p" ] || [ -L "$p" ]; then
        die "could not remove $p"
        return 1
    fi
    note "removed $p"
}

# ------------------------------------------------------------- 1. units ------

# rs_units stops and removes the systemd units bootstrap installed.
rs_units() {
    if ! have systemctl; then
        note "no systemd here, nothing to stop"
        finish
        return
    fi
    for unit in agents-bot.service herdr-server.service; do
        if systemctl cat "$unit" >/dev/null 2>&1; then
            if sudo -n systemctl disable --now "$unit" >/dev/null 2>&1; then
                note "stopped $unit"
            else
                die "could not stop $unit"
            fi
        else
            note "$unit not installed"
        fi
    done
    rm_root /etc/systemd/system/agents-bot.service
    rm_root /etc/systemd/system/herdr-server.service
    sudo -n systemctl daemon-reload >/dev/null 2>&1 || die "systemctl daemon-reload failed"

    # A herdr server started by hand outlives the unit.
    if have herdr; then
        herdr server stop >/dev/null 2>&1 || true
    fi
    finish
}

# ----------------------------------------------------------- 2. sign out -----

# rs_signout drops the credentials the box holds. Each tool is checked after the
# logout rather than trusting its exit code, so "was never signed in" and "would
# not sign out" stay distinguishable.
rs_signout() {
    if have gh; then
        gh auth logout --hostname github.com >/dev/null 2>&1 || true
        if gh auth status >/dev/null 2>&1; then
            die "gh is still signed in"
        else
            note "gh signed out"
        fi
    else
        note "gh not installed"
    fi

    if have claude; then
        claude auth logout >/dev/null 2>&1 || true
        if claude auth status --json 2>/dev/null | grep -q '"loggedIn"[[:space:]]*:[[:space:]]*true'; then
            die "claude is still signed in"
        else
            note "claude signed out"
        fi
    else
        note "claude not installed"
    fi

    if have codex; then
        codex logout >/dev/null 2>&1 || true
        if codex login status >/dev/null 2>&1; then
            die "codex is still signed in"
        else
            note "codex signed out"
        fi
    else
        note "codex not installed"
    fi
    finish
}

# ---------------------------------------------------------- 3. binaries ------

# rs_binaries removes what bootstrap put in system paths.
rs_binaries() {
    rm_root /usr/local/bin/agents
    rm_root /usr/local/bin/herdr

    if dpkg-query -W -f='${Status}' gh 2>/dev/null | grep -q '^install ok installed$'; then
        if sudo -n apt-get remove -y -qq gh >/dev/null 2>&1; then
            note "removed the gh package"
        else
            die "apt-get remove gh failed"
        fi
    else
        note "gh package not installed"
    fi

    rm_root /etc/apt/sources.list.d/github-cli.list
    rm_root /usr/share/keyrings/githubcli-archive-keyring.gpg
    rm_root /etc/apt/keyrings/githubcli-archive-keyring.gpg
    sudo -n apt-get clean >/dev/null 2>&1 || true
    finish
}

# ---------------------------------------------- 4. runtimes, caches, state ---

# rs_runtimes removes mise, the language caches, every harness config and all
# the clones and worktrees under ~/work.
rs_runtimes() {
    # The Go module cache is deliberately read-only. Let go clean handle it when
    # go is around, and fall back to making the tree writable by hand.
    if [ -d "$HOME/go" ]; then
        if have go; then
            note "go clean -modcache"
            go clean -modcache >/dev/null 2>&1 || true
        fi
        chmod -R u+w "$HOME/go" 2>/dev/null || true
    fi

    rm_user "$HOME/.local/share/mise"
    rm_user "$HOME/.local/bin/mise"
    rm_user "$HOME/.config/mise"
    rm_user "$HOME/.npm"
    rm_user "$HOME/.cache/go-build"
    rm_user "$HOME/go"
    rm_user "$HOME/.claude"
    rm_user "$HOME/.claude.json"
    rm_user "$HOME/.codex"
    rm_user "$HOME/.config/gh"
    rm_user "$HOME/.config/herdr"
    rm_user "$HOME/.agents"
    rm_user "$HOME/work"
    rm_user "$HOME/.gitconfig"
    finish
}

# ----------------------------------------------------------- 5. profiles -----

# rs_profiles takes the bootstrap lines back out of the shell rc files and
# verifies they are gone.
rs_profiles() {
    for rc in "$HOME/.profile" "$HOME/.bashrc"; do
        if [ ! -f "$rc" ]; then
            note "$rc absent"
            continue
        fi
        if ! sed -i '/^# agents$/d; /^export AGENTS_ON_BOX=1$/d; /mise\/shims/d' "$rc"; then
            die "could not edit $rc"
            continue
        fi
        if grep -q 'AGENTS_ON_BOX\|mise/shims' "$rc"; then
            die "$rc still mentions agents"
        else
            note "cleaned $rc"
        fi
    done
    finish
}

# Allow `bash reset.sh <phase>`; when piped on stdin the Go side appends the
# call itself.
if [ "$#" -gt 0 ]; then "$@"; fi
