#!/usr/bin/env bash
# Build the Go bridge for Linux (amd64 + arm64), static (CGO_ENABLED=0), version stamped.
#   scripts/build-linux.sh                  -> bridge-go/bin/relay-linux-{amd64,arm64} + SHA256SUMS
# Env:
#   OUT       output dir (default bridge-go/bin)
#   VERSION   version /health reports (default <api.Version>-<git short sha>[-dirty])
#   ARCHES    space-separated GOARCH list (default "amd64 arm64")
set -euo pipefail
cd "$(dirname "$0")/../bridge-go"
out=${OUT:-$PWD/bin}
mkdir -p "$out"
if [ -z "${VERSION:-}" ]; then
  base=$(sed -n 's/^var Version = "\(.*\)"$/\1/p' internal/api/json.go)
  [ -n "$base" ] || { echo "build-linux: can't read Version from internal/api/json.go" >&2; exit 1; }
  sha=$(git rev-parse --short HEAD)
  dirty=$(git status --porcelain -- . | grep -qv '^??' && echo -dirty || true)
  VERSION="$base-$sha$dirty"
fi
cd "$out"
rm -f SHA256SUMS
for arch in ${ARCHES:-amd64 arm64}; do
  bin=relay-linux-$arch
  (cd "$OLDPWD" && CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath \
    -ldflags "-s -w -X relay/internal/api.Version=$VERSION" -o "$out/$bin" ./cmd/relay)
  shasum -a 256 "$bin" >>SHA256SUMS
  echo "built $out/$bin ($VERSION)"
done
