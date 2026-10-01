#!/usr/bin/env bash
# bootstrap.sh — base toolchain for a relay box. Ubuntu 24.04, user `ubuntu`.
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
RELAY_DIR="${RELAY_HOME:-$HOME/.relay}"
export PATH="$MISE_SHIMS:$LOCAL_BIN:/usr/local/bin:$PATH"
export DEBIAN_FRONTEND=noninteractive

say()  { printf '  %s\n' "$*"; }
die()  { printf '  error: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# apt-get update at most once an hour, and at most once per bootstrap run.
apt_refresh() {
    local stamp=/var/lib/apt/periodic/relay-update-stamp
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
        die "passwordless sudo is required. Add to /etc/sudoers.d/90-relay:
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

# bs_pi installs the pi coding agent.
bs_pi() { npm_global @earendil-works/pi-coding-agent pi; }

# --------------------------------------------------------- 5b. auto mode -----

# bs_auto_claude makes Claude Code start in auto mode
# (permissions.defaultMode in ~/.claude/settings.json). Other settings stay.
bs_auto_claude() {
    local f="$HOME/.claude/settings.json" tmp
    mkdir -p "$HOME/.claude"
    [ -s "$f" ] || echo '{}' > "$f"
    if jq -e '.permissions.defaultMode == "auto"' "$f" >/dev/null 2>&1; then
        say "claude already starts in auto mode"
        return 0
    fi
    tmp="$(mktemp)"
    jq '.permissions = ((.permissions // {}) + {defaultMode: "auto"})' "$f" > "$tmp" \
        || { rm -f "$tmp"; die "$f is not valid JSON"; }
    mv "$tmp" "$f"
    say "claude: permissions.defaultMode = auto"
}

# bs_auto_codex sets Codex's closest equivalent: it works inside the workspace
# and asks only when it wants out. The two top-level keys in
# ~/.codex/config.toml are replaced, everything else is kept.
bs_auto_codex() {
    mkdir -p "$HOME/.codex"
    python3 - "$HOME/.codex/config.toml" <<'RELAY_PY'
import os, re, sys
path = sys.argv[1]
want = [("approval_policy", '"on-request"'), ("sandbox_mode", '"workspace-write"')]
old = open(path).read() if os.path.exists(path) else ""
lines = old.splitlines()
# Top-level keys must come before the first [table] header.
first = next((i for i, l in enumerate(lines) if l.lstrip().startswith("[")), len(lines))
head, rest = lines[:first], lines[first:]
keys = re.compile(r"^\s*(%s)\s*=" % "|".join(k for k, _ in want))
head = [l for l in head if not keys.match(l)]
new = "\n".join(["%s = %s" % kv for kv in want] + head + rest) + "\n"
if new == old:
    print("  codex already set to on-request + workspace-write")
else:
    with open(path, "w") as f:
        f.write(new)
    print("  codex: approval_policy = on-request, sandbox_mode = workspace-write")
RELAY_PY
}

# ------------------------------------------------------------ 6. profile -----

# bs_relay_home creates ~/.relay and marks this machine as the box so the CLI
# knows not to proxy itself over ssh.
bs_relay_home() {
    mkdir -p "$RELAY_DIR" "$RELAY_DIR/plans"
    chmod 0700 "$RELAY_DIR"
    touch "$RELAY_DIR/on-box"

    local line_env='export RELAY_ON_BOX=1'
    local line_path="export PATH=\"\$HOME/.local/share/mise/shims:\$HOME/.local/bin:/usr/local/bin:\$PATH\""
    local rc
    for rc in "$HOME/.profile" "$HOME/.bashrc"; do
        touch "$rc"
        grep -qxF "$line_env"  "$rc" || printf '\n# relay\n%s\n' "$line_env"  >> "$rc"
        grep -qxF "$line_path" "$rc" || printf '%s\n' "$line_path" >> "$rc"
    done
    say "RELAY_ON_BOX=1 written to ~/.profile and ~/.bashrc"
    say "$RELAY_DIR ready"
}

# ------------------------------------------------------ 7. herdr workspace ---

# bs_workspace <name> <dir> creates a herdr workspace labelled <name> with <dir>
# as its cwd, so the app's "New chat" can start agents there. A workspace with
# that label already there is left alone.
bs_workspace() {
    local name="$1" dir="$2" list
    have herdr || die "herdr is not installed"
    [ -d "$dir" ] || die "no checkout for $name"
    list="$(herdr workspace list 2>&1)" || die "herdr server is not answering: $list"
    if printf '%s' "$list" | jq -e --arg l "$name" '.result.workspaces[]? | select(.label == $l)' >/dev/null; then
        say "herdr workspace $name already exists"
        return 0
    fi
    herdr workspace create --cwd "$dir" --label "$name" --no-focus >/dev/null
    say "created herdr workspace $name"
}

# Allow `bash bootstrap.sh <fn>`; when piped on stdin the Go side appends the
# call itself.
if [ "$#" -gt 0 ]; then "$@"; fi
