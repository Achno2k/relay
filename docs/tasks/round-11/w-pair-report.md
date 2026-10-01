# w-pair report

## What changed
- **`internal/setup`** (new): the Linux setup behind `relay pair`.
  - `survey` reads the machine, `Decide(State, wantUnit, exe) Plan` is the pure step decision, and `Run` executes the plan in order.
  - sudo: uses `sudo -n true` when it works; otherwise one `sudo -v` prompt, then `sudo -n -v` every minute while it runs. Running as root skips sudo.
  - herdr: installs only if missing, to `/usr/local/bin/herdr`. Same manifest (`herdr.dev/latest.json`), targets and sha256 check as `bs_herdr`, done in Go so no `jq` is needed. `herdr-server.service` is written and (re)started only when nothing answers herdr's `ping` on the socket, then it waits 15 s for it (a warning, not fatal).
  - Tailscale: if missing, runs `curl -fsSL https://tailscale.com/install.sh | sh` and starts `tailscaled`. If `BackendState` isn't `Running`, it runs `sudo tailscale up` and shows the login URL from its output as an `internal/qr` QR code. `--authkey` is passed as `--auth-key=file:<0600 temp file>`, so it stays off the process list, and the file is removed afterwards.
  - relay.service: makes sure `~/.relay`, `relay.log` and the token exist as the user. It writes the unit only if its text differs, enables it if needed, and restarts it when it is inactive, the unit changed, a round-9 user unit was just removed, or the running binary changed. A changed binary means `/proc/<MainPID>/exe` ends in ` (deleted)` or points at another path. Then it waits up to 45 s for `GET /health` on the Tailscale IP and fails with `systemctl status` / log hints if nothing answers.
  - A round-9 `~/.config/systemd/user/relay.service` is disabled (best effort) and removed.
  - `Supported()`: Linux with `systemctl` and `/run/systemd/system`. Anywhere else, `pair` only prints, as before.
- **Unit templates** (`internal/setup/units/`, both owned here):
  - `herdr-server.service`: moved from bootstrap with history. ExecStart is now the herdr path pair found.
  - `relay.service`: new system unit. `User=`, `HOME`, the mise-shims PATH, `serve --port N --require-tailscale`, `Restart=always`, `RestartSec=5`, `After=herdr-server.service tailscaled.service`, output appended to `~/.relay/relay.log`, `WantedBy=multi-user.target`.
  - `HERDR_SOCKET_PATH` goes into both units when it is set.
- **`relay pair`**: new flags `--authkey`, `--yes`/`-y`. `--port`, `--host` and `--url` are unchanged. On Linux it runs setup, then prints the QR as today. Without `--yes`, it lists the plan and asks through `ui.Confirm`. With no terminal it stops before changing anything. Nothing is asked when the plan is empty.
- **`relay install-systemd`**: writes the system unit with sudo and doesn't enable it. `config.SystemdUnit`/`SystemdUnitPath` (the user unit) are deleted.
- **bootstrap**: `bootstrap.Unit` now renders through `setup.Unit`. `InstallUnits` / `EnableHerdrServer` are untouched for w-init to remove.
- **Docs**:
  - `docs/remote-setup.md` is rewritten around Path 1 and Path 2, with the two units, updating, uninstalling, troubleshooting and the test record.
  - `docs/api.md`: the Pairing section describes what `relay pair` does on Linux, and its flags.
  - AGENTS.md: the Linux line now names the system unit.

## Verification
- `gofmt -l .` is empty. `go vet ./...` and `GOOS=linux go vet ./...` are clean. `go test -race ./...` passes.
- New tests in `setup_test.go`:
  - unit text for both units, quoting, the socket line, and inputs that must be rejected;
  - `Decide` on a fresh machine, on a second run (empty plan, no sudo prompt), and for each single change;
  - `BinaryChanged`, the herdr manifest parser, the Tailscale state parser and the auth URL parser.
- podman, Ubuntu 24.04 arm64, systemd as PID 1, real herdr 0.9.3 from herdr.dev, and a stub `tailscale` that starts logged out:
  - Without a tty and without `--yes`, it lists 6 steps and exits before changing anything.
  - `--yes` on a fresh box installs herdr, starts `herdr-server.service`, shows the login QR, writes and starts `relay.service`, and `/health` answers. This took 21 s.
  - **Second run:** before/after snapshots of the unit and binary mtimes, the token, both MainPIDs, both ExecMainStartTimestamps and both UnitFileStates are identical. It asked nothing and printed the same link.
  - Replacing `/usr/local/bin/relay` with `install` restarted `relay.service` only.
  - `--authkey` while logged out: the stub saw `--auth-key=file:` and no temp file was left.
  - A fake round-9 user unit was removed and the bridge restarted.
  - A second user with password sudo, driven by `expect`: one prompt, then everything ran.
  - Container restart: both units came back with no one logged in, and `/health` reported `herdr: connected`. The bridge listens only on 127.0.0.1 and the "Tailscale" IP.
  - `kill -9` of the bridge: it was back in about 5 s (`NRestarts=1`).
  - Tailscale stopped: the bridge exited 75 and was in auto-restart. It came back once Tailscale was Running.
- macOS: `relay pair --host …` still only prints the link.

## Known gaps
- The real Tailscale installer and the real `tailscale up` login were not run (stub only). They are left for the lead's box test, along with `relay init` → `relay pair --yes` over SSH.
- One box, one user. If a different user runs `relay pair` on the same machine, it rewrites both units for that user, and the old user's herdr server stops.
- A herdr the user runs by hand counts as "a server answers", so no `herdr-server.service` is installed. If that herdr isn't running after a reboot, the bridge waits with `herdr: unavailable`.
- `tailscale up` with no flags fails on a box whose Tailscale was set up earlier with non-default flags ("requires mentioning all non-default flags"). The error is shown as is.
- herdr is installed only when missing and never upgraded. That is still bootstrap's job (`bs_herdr`).
- `relay pair` doesn't write `~/.relay/on-box`. Only bootstrap marks a box.
- The confirmation and `ui` output print terminal-query escapes under `expect`/`script`. This is the existing `ui` behaviour, not new.
- For w-init:
  - `bootstrap.InstallUnits` / `EnableHerdrServer` and the `Units` list still exist; remove them when init switches to `relay pair --yes`.
  - `reset.sh` already removes both units from `/etc/systemd/system`.

## Box test fixes (2026-10-01)
### What changed
- **herdr integrations.**
  - `relay pair` looks for `claude`, `codex` and `pi` on `PATH`, then in `~/.local/share/mise/shims`, `~/.local/bin`, `~/.npm-global/bin` and `$(npm prefix -g)/bin`. npm itself is found the same way.
  - For each one found, it reads `herdr integration status` and runs `herdr integration install <kind>` unless the status starts with `current`. So "not installed", "outdated" or a missing line all install.
  - This is the `Plan.Integrations` field. It needs no sudo.
  - A failed install is a warning, not fatal. The bridge still works; only that agent's chat stays empty. herdr refuses when the CLI's config dir (`~/.claude`, `~/.codex`) doesn't exist yet, so the warning says to start that CLI once and run `pair` again.
  - Path 2 gets this too, since `relay init` runs `relay pair --yes` after the harnesses are installed.
- **Quiet Tailscale install.** `tailscale.com/install.sh` now runs as root behind one `ui.Spinner` line, with its output appended to `~/.relay/setup.log` (0600). On failure, the last 20 lines and the log path are printed. Integration installs use the same runner.
- `setup.UI` gained `Muted` and `Spinner`. `Plan` has a slice now, so `Empty` uses `reflect.DeepEqual`.
- Docs: `remote-setup.md` has the new step, troubleshooting rows and a test record. The `api.md` pairing steps are updated.

### Verification
- All four checks pass.
- New tests:
  - `Decide` for missing, outdated and current integrations, for no harnesses, and for a herdr status that failed to read;
  - integrations alone don't ask for sudo;
  - `ParseIntegrationStatus` against the real output format;
  - `lastLines`.
- podman (Ubuntu 24.04 arm64, systemd, real herdr 0.9.3), run under `expect` so the spinner is live. Stub `claude` was in `~/.local/bin` and stub `codex` in the mise shims, neither on the exec `PATH`. Stub `pi` was under a fake `npm prefix -g`.
  - Without `~/.claude` and `~/.codex`: herdr's message was shown, then a warning, and setup finished with `/health` answering.
  - With the dirs: all three integrations were installed and `herdr integration status` said `current` for each.
  - The next run printed "herdr integrations are current". Hook file mtimes, unit mtimes and service PIDs/start times were identical, and `setup.log` was untouched.
  - The real Tailscale installer ran with only `[✓] install Tailscale` on screen. The apt lines, `+ set +x` and "Installation complete!" are in `setup.log` only.

### Known gaps
- In the container, `tailscaled` can't start because there is no tun device, so the run after the install stopped at "tailscaled doesn't answer". On the box this works, as the box test showed.
- The integration check only runs on harnesses that are already installed. A CLI installed after `pair` needs another `relay pair`.
- Every run with a CLI that has never been started repeats the warning until that CLI has its config dir.

## Bug: agents start for CLIs that aren't signed in (iOS, 2026-10-01)
Built on w-init's contract (`011ccba`: `GET /kinds`, `409 not_installed` / `not_signed_in`, `docs/fixtures/kinds.json`).

### What changed
- **RelayKit**
  - New `KindStatus` model (`kind`, `installed`, `signedIn`, `signInHint`, and `canStart`).
  - `APIClient.kinds()` and a `Backend.kinds()` requirement. The default implementation throws 404, so older bridges and test doubles read as "unknown".
  - `LiveBackend`, `NamespacedBackend` and `RoutingBackend` forward the call.
  - `RelayError.kindNotReady` is the hint for a `409 not_installed` / `not_signed_in`, with a fallback text when the bridge sends no message.
- **AppStore**
  - `kinds(machineId:)` fetches fresh every time; the app never caches it, because the bridge re-derives it. It returns nil on 404 or when the machine is offline.
  - `createAgent` now returns `CreateOutcome` (`.created`, `.kindNotReady(hint)`, `.failed`). A 409 for a kind doesn't go to the error banner. Other failures still do.
- **New chat sheet**
  - The kind picker is now three rows instead of a segmented control, because a segment can't be greyed out with text under it.
  - A kind that can't start is disabled and greyed, with the bridge's hint under it (backticks render as code). It can't be picked.
  - `/kinds` is fetched when the sheet opens and again whenever the machine changes. If the current pick can't start, the first kind that can is selected (`pickKind`).
  - When nothing is known (an older bridge, or the machine is offline), every kind stays pickable and the bridge has the last word.
  - When the picked kind can't start, the Model/Effort section is hidden and Create is disabled.
  - A 409 from Create (a race with a stale `/kinds`) shows an alert, "Can't start <Kind>", with the hint. The sheet stays open, and `/kinds` is refetched.
- **Mock**
  - `-mockSignedOut <kinds>` and `-mockNotInstalled <kinds>` drive both the mock `/kinds` and the 409s, using the same hint strings as the bridge.
  - `-mockKindsStale` makes `/kinds` claim every kind can start, while Create still refuses (the race case).

### Verification
All runs headless: Simulator app closed, iPhone 17 Pro on iOS 26.4, xcodebuild only.
- `RelayTests`: 211 tests, 0 failures. New tests in `NewChatTests.swift`:
  - decoding the `kinds.json` fixture;
  - `kindNotReady` is set only for 409s with those two codes;
  - `pickKind`: a signed-out pick moves to the first kind that can start; unknown keeps the pick; nothing startable keeps the pick; a kind the bridge doesn't list counts as startable;
  - hint markdown renders as code;
  - the store: `/kinds` is fetched fresh, a missing route is nil with no banner, a 409 gives `.kindNotReady` with no banner and no chat opened, and a 500 still shows the banner.
- `RelayUITests/NewChatKindsUITests` (new, mock):
  - a signed-out codex and a missing pi are disabled with their hints; tapping codex does nothing; Claude creates fine;
  - with only codex signed in, codex is picked for you and its models load;
  - a stale `/kinds` plus a refused Create shows the alert with the hint, the sheet stays open, and no chat starts.
- `RelayUITests/NewChatUITests` (existing) still passes with the rows.
- Regression run of the other UI suites that open the sheet or the sidebar (`A11yAuditUITests`, `EmptyStateUITests`, `Round9UITests`, `Round2UITests`, `SidebarShellUITests`, `SidebarListsUITests`): 43 tests, 0 failures. One was skipped: `testAuditScreens` skips itself when `RELAY_SHOTS` isn't set.

### Known gaps
- Not tried against a real bridge or a box. The live `/kinds` and the 409s are w-init's code, and the app side ran against the mock only.
- The alert's message is plain text, because alerts don't render markdown, so the backticks are dropped there. The rows show them as code.
- After a 409 race, the refetch only greys the row if the bridge's `/kinds` has caught up; it caches for up to 15 s.
- There is no live UI test for this, since it would need a CLI that is signed out on the test machine.

### Lead note: older bridges answer `/kinds` with 404
- This was already the behaviour: `store.kinds` uses `try?`, so a 404 or any other failure comes back as nil, and nil means "unknown".
- The sheet's rule is now one helper, `NewChatSheet.canStart(kind, statuses:)`. The rows, Create and `pickKind` all use it. Unknown, or a kind the bridge didn't list, counts as startable.
- New unit test `olderBridgeOrFailedKindsLeavesEveryKindStartable`:
  - covers 404, 500, unreachable and a bad response;
  - each one gives nil from the store, every kind startable, the selection unchanged, and no banner;
  - after that, Create still gets the 409 as `.kindNotReady(hint)`.
- Also new: `canStartFollowsTheStatus`.
- RelayTests: 212 pass. `NewChatKindsUITests` and `NewChatUITests` pass again, headless.
