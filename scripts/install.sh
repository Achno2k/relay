#!/bin/sh
# Install relay from a GitHub release and check it against checksums.txt.
# Usage: curl -fsSL https://raw.githubusercontent.com/Achno2k/relay/main/scripts/install.sh | sh
# RELAY_VERSION=0.2.0 picks a release; the default is the latest.
# RELAY_INSTALL_DIR=<dir> installs there instead of /usr/local/bin.
# RELAY_RELEASES=<url> replaces the GitHub releases URL, for tests.
set -eu

REPO="Achno2k/relay"
BASE="${RELAY_RELEASES:-https://github.com/${REPO}/releases}"

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

# download <url> <file>
download() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		die "need curl or wget to download"
	fi
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		die "need sha256sum or shasum to check the download"
	fi
}

install_bin() {
	src=$1
	if [ -n "${RELAY_INSTALL_DIR:-}" ]; then
		mkdir -p "$RELAY_INSTALL_DIR"
		mv "$src" "$RELAY_INSTALL_DIR/relay"
		echo "$RELAY_INSTALL_DIR"
	elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
		mv "$src" /usr/local/bin/relay
		echo /usr/local/bin
	elif command -v sudo >/dev/null 2>&1; then
		sudo mv "$src" /usr/local/bin/relay
		echo /usr/local/bin
	else
		dest="${HOME}/.local/bin"
		mkdir -p "$dest"
		mv "$src" "$dest/relay"
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

	if [ -n "${RELAY_VERSION:-}" ]; then
		dl="${BASE}/download/v${RELAY_VERSION#v}"
	else
		dl="${BASE}/latest/download"
	fi
	asset="relay_${os}_${arch}.tar.gz"
	say "Downloading ${dl}/${asset}"
	tmp=$(mktemp -d) || die "mktemp failed"
	trap 'rm -rf "$tmp"' EXIT
	download "${dl}/${asset}" "$tmp/$asset" || die "download failed"
	download "${dl}/checksums.txt" "$tmp/checksums.txt" || die "checksums.txt download failed"

	want=$(awk -v a="$asset" '$2 == a { print $1 }' "$tmp/checksums.txt")
	[ -n "$want" ] || die "checksums.txt has no entry for $asset"
	got=$(sha256 "$tmp/$asset")
	[ "$got" = "$want" ] || die "checksum mismatch for $asset (want $want, got $got)"
	say "Checksum ok"

	tar -xzf "$tmp/$asset" -C "$tmp" || die "extract failed"
	[ -f "$tmp/relay" ] || die "archive did not contain a relay binary"
	chmod +x "$tmp/relay"

	dest=$(install_bin "$tmp/relay")
	say "Installed relay to ${dest}/relay"
	case ":${PATH}:" in
	*":${dest}:"*) ;;
	*) say "warning: ${dest} is not on your PATH; add it to use \`relay\`" ;;
	esac
	"${dest}/relay" --version || true
}

main "$@"
