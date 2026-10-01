#!/bin/sh
# Parity check: a Go bridge built from this tree against a reference bridge (default the live one on 7878). One command:
#
#   bridge/parity/run.sh [parity flags]        e.g. -v, -agents w14:p2, -e2e w14:p2,w14:p4,w14:p5
#
# Builds bridge/cmd/relay, starts it on a temp RELAY_HOME (never ~/.relay) on GO_PORT (7880),
# runs the parity tool against both, then stops it. Exits non-zero on any mismatch.
#
# Env:
#   GO_PORT     port for the Go bridge (default 7880; 7878 is refused)
#   RELAY_BIN   run this bridge binary instead of building one (e.g. an older build, to self-test)
#   SWIFT_URL   the reference bridge (default http://127.0.0.1:7878)
#   KEEP=1      keep the temp home (logs, token) afterwards
set -eu

cd "$(dirname "$0")/.."
port=${GO_PORT:-7880}
if [ "$port" = 7878 ]; then
  echo "parity: 7878 is the live bridge; pick another GO_PORT" >&2
  exit 2
fi
if curl -s -o /dev/null "http://127.0.0.1:$port/health"; then
  echo "parity: something already listens on $port; stop it or set GO_PORT" >&2
  exit 2
fi

home=$(mktemp -d "${TMPDIR:-/tmp}/relay-parity.XXXXXX")
pid=
cleanup() {
  if [ -n "$pid" ]; then
    kill "$pid" 2>/dev/null || true
    # The Swift bridge ignores SIGTERM; don't hang on it.
    n=0
    while kill -0 "$pid" 2>/dev/null && [ $n -lt 25 ]; do sleep 0.2; n=$((n + 1)); done
    { kill -9 "$pid" && wait "$pid"; } 2>/dev/null || true
  fi
  if [ "${KEEP:-}" = 1 ]; then echo "parity: kept $home"; else rm -rf "$home"; fi
}
trap cleanup EXIT INT TERM

# Attachment history is matched against the sent log in uploads/; give the Go bridge a copy.
if [ -d "$HOME/.relay/uploads" ]; then cp -Rp "$HOME/.relay/uploads" "$home/uploads"; fi

bin=${RELAY_BIN:-}
if [ -z "$bin" ]; then
  bin="$home/relay"
  go build -o "$bin" ./cmd/relay
fi
go build -o "$home/parity" ./parity

RELAY_HOME="$home" "$bin" serve --port "$port" --local-only >"$home/bridge.log" 2>&1 &
pid=$!
i=0
until curl -s -o /dev/null "http://127.0.0.1:$port/health"; do
  i=$((i + 1))
  if [ $i -gt 100 ] || ! kill -0 "$pid" 2>/dev/null; then
    echo "parity: bridge didn't come up on $port; log:" >&2
    tail -n 30 "$home/bridge.log" >&2
    exit 2
  fi
  sleep 0.2
done

status=0
"$home/parity" -swift "${SWIFT_URL:-http://127.0.0.1:7878}" -go "http://127.0.0.1:$port" \
  -go-token "$home/token" "$@" || status=$?
if [ $status -ne 0 ]; then
  echo "parity: bridge log tail ($home/bridge.log):"
  tail -n 15 "$home/bridge.log" | sed 's/^/  /'
fi
exit $status
