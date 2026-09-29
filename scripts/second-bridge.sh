#!/usr/bin/env bash
# A second Go bridge on this Mac that the app sees as another machine ("Test VM"), for testing
# multiple machines in the simulator. Same herdr, temp RELAY_HOME (never ~/.relay), 127.0.0.1 only.
# Runs in the foreground and prints a pair link; Ctrl-C stops it and deletes the temp home.
#   scripts/second-bridge.sh
# Env:
#   PORT                 default 7881 (7878 is the live bridge and is refused)
#   RELAY_MACHINE_ID     default test-vm
#   RELAY_MACHINE_NAME   default "Test VM"
#   RELAY_BIN            run this binary instead of building bridge-go
#   KEEP=1               keep the temp home (token, relay.log) afterwards
set -euo pipefail
cd "$(dirname "$0")/../bridge-go"
port=${PORT:-7881}
if [ "$port" = 7878 ]; then
  echo "second-bridge: 7878 is the live bridge; pick another PORT" >&2
  exit 2
fi
if curl -s -o /dev/null "http://127.0.0.1:$port/health"; then
  echo "second-bridge: something already listens on $port" >&2
  exit 2
fi
export RELAY_MACHINE_ID=${RELAY_MACHINE_ID:-test-vm}
export RELAY_MACHINE_NAME=${RELAY_MACHINE_NAME:-Test VM}
tmp=${TMPDIR:-/tmp}
home=$(mktemp -d "${tmp%/}/relay-second.XXXXXX")
export RELAY_HOME=$home
pid=
cleanup() {
  if [ -n "$pid" ]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  if [ "${KEEP:-}" = 1 ]; then echo "second-bridge: kept $home"; else rm -rf "$home"; fi
}
trap cleanup EXIT INT TERM

bin=${RELAY_BIN:-}
if [ -z "$bin" ]; then
  bin="$home/relay"
  go build -o "$bin" ./cmd/relay
fi
"$bin" serve --port "$port" --local-only >"$home/relay.log" 2>&1 &
pid=$!
i=0
until curl -s -o /dev/null "http://127.0.0.1:$port/health"; do
  i=$((i + 1))
  if [ $i -gt 100 ] || ! kill -0 "$pid" 2>/dev/null; then
    echo "second-bridge: bridge didn't come up on $port; log:" >&2
    tail -n 30 "$home/relay.log" >&2
    exit 2
  fi
  sleep 0.2
done
echo "second-bridge: \"$RELAY_MACHINE_NAME\" ($RELAY_MACHINE_ID) on http://127.0.0.1:$port, home $home"
echo "  log: $home/relay.log"
"$bin" pair --url "http://127.0.0.1:$port" | tail -n 1
echo "Ctrl-C to stop."
wait "$pid"
