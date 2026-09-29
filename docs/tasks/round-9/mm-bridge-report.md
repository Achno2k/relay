# Round 9: mm-bridge report

## What changed
- `docs/api.md`, new "Multiple machines" section (faff791):
  - one bridge per machine, no proxy, no route or payload changes;
  - pairings keyed by `/machine` `id`; re-pairing a known id replaces url + token and keeps label and per-agent state;
  - `401`, or a changed `/machine` id on a stored pairing, means `needsRePair`, for that machine only;
  - ids are per machine; the app keys by machine id + id;
  - machine ids match `[A-Za-z0-9._-]{1,64}`, so `|` and `/` are safe key separators;
  - per-machine status `connecting` / `online` / `offline` / `needsRePair`; herdr unavailable counts as online with no agents;
  - test-only env overrides, `second-bridge.sh`, `relay pair --url`.
- `bridge-go` (b37a023, 156d28c):
  - `RELAY_MACHINE_ID` / `RELAY_MACHINE_NAME` replace `/machine` `id` / `name`. An invalid id makes `serve` exit 78.
  - `relay pair --url <base>` puts the base in the link verbatim, http(s) with a host and no path. It wins over `--host`/`--port`.
  - `api.Version` is now a `var` so builds can stamp it. Default builds still report `0.1.0`.
  - The Tailscale retry log line now says "launchd or systemd".
  - `bridge-go/.gitignore` ignores `bin/`.
- `scripts/build-linux.sh`: amd64 + arm64, `CGO_ENABLED=0`, `-trimpath -s -w`, version `<api.Version>-<sha>[-dirty]`, plus `SHA256SUMS`. Output goes to `bridge-go/bin/`.
- `scripts/second-bridge.sh`: Go bridge on 7881 (refuses 7878), temp `RELAY_HOME`, `test-vm` / "Test VM", same herdr, 127.0.0.1 only. It runs in the foreground, prints the pair link, and Ctrl-C stops it and deletes the temp home.
- `docs/remote-setup.md`: Linux VM steps. Covers Tailscale, build and copy, the herdr socket, systemd and linger, pairing, firewall, updating, logs, token, uninstalling, troubleshooting.

## Tests
- `go test ./...` in `bridge-go`: all pass. `GOOS=linux go vet ./...` is clean.
- New: `TestOverrides`, `TestRealIDIsValid` (machine), `TestParseBaseURL` (config).

## How verified
- `second-bridge.sh`: `/machine` answered `{"id":"test-vm","name":"Test VM",…}`, `/health` had herdr connected, and `/agents` listed the same herdr's agents. Ctrl-C stopped the bridge and removed the temp home.
- `RELAY_MACHINE_ID='a/b'` made `serve` exit 78. `pair --url http://h/x` was rejected.
- `install-systemd`, run for real in an Ubuntu 24.04 arm64 podman container with systemd as PID 1, as a normal user with linger:
  - without `tailscale` the unit exits 75 and systemd restarts it every 5 s;
  - with a stub `tailscale` that prints the container IP, it listens on `127.0.0.1` + that IP only and serves `/health`; `/machine` needs the token; the token file is 0600;
  - `kill -9` of the main PID brought it back about 5 s later with a new PID;
  - after a container restart, with 0 login sessions, it came back up (linger);
  - update flow: `cp` over the running binary fails with "text file busy"; `mv` + restart works, and `/health` showed the new version;
  - `disable --now` stops it.
- The amd64 binary ran `--version` under podman amd64 emulation.
- Container and image removed afterwards. The live 7878 bridge was not touched.

## What's left
- No real cloud VM or real Tailscale on Linux yet. The systemd test used a stub `tailscale`.
- `second-bridge.sh` shares this Mac's CLIs, so while the app has its WS open it polls claude/codex usage too. That's twice the probes against the same subscriptions. Fine for testing.
- The Swift `bridge/` has no machine overrides. By the brief, those go in the Go bridge only.
