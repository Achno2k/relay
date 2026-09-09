#!/bin/sh
# Install agents from the latest GitHub release.
# Usage: curl -fsSL https://raw.githubusercontent.com/Achno2k/agents-cli/main/scripts/install.sh | sh
set -eu

REPO="Achno2k/agents-cli"
BASE="https://github.com/${REPO}/releases"

say() { printf '%s\n' "$*"; }
die() { say "install: $*" >&2; exit 1; }

detect_os() {
	case "$(uname -s)" in
	Darwin) echo darwin ;;
	Linux) echo linux ;;
	*) die "unsupported OS: $(uname -s). Download a release from ${BASE}" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) die "unsupported architecture: $(uname -m)" ;;
	esac
}

install_bin() {
	src=$1
	if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
		mv "$src" /usr/local/bin/agents
		echo /usr/local/bin
	elif command -v sudo >/dev/null 2>&1; then
		sudo mv "$src" /usr/local/bin/agents
		echo /usr/local/bin
	else
		dest="${HOME}/.local/bin"
		mkdir -p "$dest"
		mv "$src" "$dest/agents"
		echo "$dest"
	fi
}

main() {
	os=$(detect_os)
	arch=$(detect_arch)
	case "${os}/${arch}" in
	darwin/arm64 | linux/amd64 | linux/arm64) ;;
	*) die "no release for ${os}/${arch}; darwin/arm64, linux/amd64 and linux/arm64 are built" ;;
	esac

	url="${BASE}/latest/download/agents_${os}_${arch}.tar.gz"
	say "Downloading ${url}"
	tmp=$(mktemp -d) || die "mktemp failed"
	trap 'rm -rf "$tmp"' EXIT
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$url" -o "$tmp/agents.tgz" || die "download failed"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$tmp/agents.tgz" "$url" || die "download failed"
	else
		die "need curl or wget to download"
	fi
	tar -xzf "$tmp/agents.tgz" -C "$tmp" || die "extract failed"
	[ -f "$tmp/agents" ] || die "archive did not contain an agents binary"
	chmod +x "$tmp/agents"

	dest=$(install_bin "$tmp/agents")
	say "Installed agents to ${dest}/agents"
	case ":${PATH}:" in
	*":${dest}:"*) ;;
	*) say "warning: ${dest} is not on your PATH; add it to use \`agents\`" ;;
	esac
	"${dest}/agents" --version || true
}

main "$@"
