#!/usr/bin/env bash
# bootstrap.sh — base toolchain for an agents box. Ubuntu 24.04, user `ubuntu`.
#
# Every function is idempotent and safe to re-run. The Go side sends this file
# on stdin with a single function call appended, so each install shows up as its
# own ui.Step. Run it by hand the same way:
#
#     bash bootstrap.sh bs_git
#
set -euo pipefail

MISE_SHIMS="$HOME/.local/share/mise/shims"
LOCAL_BIN="$HOME/.local/bin"
AGENTS_DIR="${AGENTS_HOME:-$HOME/.agents}"
export PATH="$MISE_SHIMS:$LOCAL_BIN:/usr/local/bin:$PATH"
export DEBIAN_FRONTEND=noninteractive

say()  { printf '  %s\n' "$*"; }
die()  { printf '  error: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# apt-get update at most once an hour, and at most once per bootstrap run.
apt_refresh() {
    local stamp=/var/lib/apt/periodic/agents-update-stamp
    if [ -f "$stamp" ] && [ -z "$(find "$stamp" -mmin +60 2>/dev/null)" ]; then
        return 0
    fi
    say "apt-get update"
    sudo -n apt-get update -qq
    sudo -n mkdir -p "$(dirname "$stamp")"
    sudo -n touch "$stamp"
}

apt_install() {
    local missing=()
    for pkg in "$@"; do
        dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null | grep -q '^install ok installed$' || missing+=("$pkg")
    done
    if [ ${#missing[@]} -eq 0 ]; then
        say "already installed: $*"
        return 0
    fi
    apt_refresh
    say "installing: ${missing[*]}"
    sudo -n apt-get install -y -qq --no-install-recommends "${missing[@]}"
}

# ---------------------------------------------------------------- 1. sudo ----

# bs_sudo_check fails loudly unless this user has passwordless sudo, which every
# later step depends on.
bs_sudo_check() {
    if ! have sudo; then
        die "sudo is not installed on this box"
    fi
    if ! sudo -n true 2>/dev/null; then
        die "passwordless sudo is required. Add to /etc/sudoers.d/90-agents:
    $(id -un) ALL=(ALL) NOPASSWD:ALL"
    fi
    say "passwordless sudo ok for $(id -un)"
    . /etc/os-release 2>/dev/null || true
    say "host: ${PRETTY_NAME:-unknown} ($(uname -m))"
}

# ------------------------------------------------------------ 2. base apt ----

# bs_apt_basics installs the packages every other step assumes.
bs_apt_basics() {
    apt_install ca-certificates gnupg jq build-essential pkg-config
}

# bs_curl installs curl.
bs_curl() { apt_install curl; }

# bs_unzip installs unzip and zip.
bs_unzip() { apt_install unzip zip; }

# bs_git installs git.
bs_git() { apt_install git; git --version; }

# bs_gh installs the GitHub CLI from GitHub's own apt repository.
bs_gh() {
    if have gh; then
        say "gh already installed: $(gh --version | head -1)"
        return 0
    fi
    local keyring=/usr/share/keyrings/githubcli-archive-keyring.gpg
    if [ ! -s "$keyring" ]; then
        say "adding cli.github.com apt repository"
        curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
            | sudo -n dd of="$keyring" status=none
        sudo -n chmod go+r "$keyring"
    fi
    echo "deb [arch=$(dpkg --print-architecture) signed-by=$keyring] https://cli.github.com/packages stable main" \
        | sudo -n tee /etc/apt/sources.list.d/github-cli.list >/dev/null
    sudo -n apt-get update -qq
    sudo -n apt-get install -y -qq gh
    gh --version | head -1
}

# ------------------------------------------------------- 3. mise + node 22 ----

# bs_mise installs mise to ~/.local/bin and pins node 22 globally.
bs_mise() {
    if ! have mise; then
        say "installing mise from https://mise.run"
        curl -fsSL https://mise.run | MISE_QUIET=1 sh
    fi
    have mise || die "mise install did not put mise on PATH ($LOCAL_BIN)"
    say "mise $(mise --version)"

    if mise ls --global node 2>/dev/null | grep -q '^node *22'; then
        say "node 22 already pinned"
    else
        say "mise use -g node@22"
        mise use -g node@22
    fi
    mise reshim >/dev/null 2>&1 || true
    say "node $(node --version 2>/dev/null || echo missing), npm $(npm --version 2>/dev/null || echo missing)"
}

# ------------------------------------------------------------- 4. herdr ------

# bs_herdr installs the herdr release binary to /usr/local/bin. It mirrors
# https://herdr.dev/install.sh: same latest.json manifest, same sha256 check,
# but into a system path so systemd and every user can reach it.
bs_herdr() {
    local manifest_url=https://herdr.dev/latest.json target arch manifest version url want got tmp
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64)  target=linux-x86_64 ;;
        aarch64|arm64) target=linux-aarch64 ;;
        *)             die "no herdr release for $arch" ;;
    esac

    manifest="$(curl -fsSL --retry 3 --max-time 30 "$manifest_url")" \
        || die "cannot reach $manifest_url"
    version="$(printf '%s' "$manifest" | jq -r '.version')"
    url="$(printf '%s' "$manifest" | jq -r --arg t "$target" '.assets[$t] // empty')"
    want="$(printf '%s' "$manifest" | jq -r --arg t "$target" '.sha256[$t] // empty')"
    [ -n "$url" ] && [ ${#want} -eq 64 ] || die "manifest has no $target asset"

    if have herdr && herdr --version 2>/dev/null | grep -qw "$version"; then
        say "herdr $version already installed"
        return 0
    fi

    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' RETURN
    say "downloading herdr $version ($target)"
    curl -fsSL --retry 3 --max-time 180 "$url" -o "$tmp/herdr"
    got="$(sha256sum < "$tmp/herdr" | awk '{print $1}')"
    [ "$got" = "$want" ] || die "herdr checksum mismatch (want $want, got $got)"
    sudo -n install -m 0755 "$tmp/herdr" /usr/local/bin/herdr
    say "herdr $(/usr/local/bin/herdr --version)"
}

# ---------------------------------------------------------- 5. harnesses -----

npm_global() {
    local pkg="$1" bin="$2"
    have npm || die "npm missing; run the mise step first"
    if have "$bin"; then
        say "$bin already installed: $("$bin" --version 2>/dev/null | head -1)"
        return 0
    fi
    say "npm install -g $pkg"
    npm install -g --no-fund --no-audit "$pkg"
    mise reshim >/dev/null 2>&1 || true
    have "$bin" || die "$pkg installed but $bin is not on PATH"
    say "$bin $("$bin" --version 2>/dev/null | head -1)"
}

# bs_claude installs Claude Code.
bs_claude() { npm_global @anthropic-ai/claude-code claude; }

# bs_codex installs the OpenAI Codex CLI.
bs_codex() { npm_global @openai/codex codex; }

# ------------------------------------------------------------ 6. profile -----

# bs_agents_home creates ~/.agents and marks this machine as the box so the CLI
# knows not to proxy itself over ssh.
bs_agents_home() {
    mkdir -p "$AGENTS_DIR" "$AGENTS_DIR/plans"
    chmod 0700 "$AGENTS_DIR"
    touch "$AGENTS_DIR/on-box"

    local line_env='export AGENTS_ON_BOX=1'
    local line_path="export PATH=\"\$HOME/.local/share/mise/shims:\$HOME/.local/bin:/usr/local/bin:\$PATH\""
    local rc
    for rc in "$HOME/.profile" "$HOME/.bashrc"; do
        touch "$rc"
        grep -qxF "$line_env"  "$rc" || printf '\n# agents\n%s\n' "$line_env"  >> "$rc"
        grep -qxF "$line_path" "$rc" || printf '%s\n' "$line_path" >> "$rc"
    done
    say "AGENTS_ON_BOX=1 written to ~/.profile and ~/.bashrc"
    say "$AGENTS_DIR ready"
}

# Allow `bash bootstrap.sh <fn>`; when piped on stdin the Go side appends the
# call itself.
if [ "$#" -gt 0 ]; then "$@"; fi
