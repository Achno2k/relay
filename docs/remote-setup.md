# Remote setup: a Linux VM

Run Relay's bridge on a Linux machine (e.g. a cloud VM) so the app can drive the agents there. Each machine runs its own bridge next to its own herdr and is paired separately. See "Multiple machines" in `api.md`.

There are two ways to do it. Both end with `relay pair` on the VM, which sets up what's missing and prints the pairing QR code. Examples use a VM called `vm`, a user `you` and the tailnet IP `100.101.102.103`.

## What you need
- A Linux VM with systemd (Ubuntu 22.04+, Debian 12+, Fedora 39+), amd64 or arm64.
- SSH access as a normal user with sudo (passwordless, or you type the password once). The bridge and herdr run as that user.
- The iPhone on your tailnet (Tailscale iOS app).
- Path 2 only: an existing EC2 instance and an AWS profile on the laptop. `relay init` never creates instances.

## Path 1: on the VM
```sh
ssh vm
curl -fsSL https://raw.githubusercontent.com/Achno2k/relay/main/scripts/install.sh | sh
relay pair
```
- `install.sh` puts `relay` in `/usr/local/bin` (or `~/.local/bin` without sudo).
- `relay pair` lists what it is about to do and asks once. Then, skipping whatever is already there:
  1. **sudo**: checks `sudo -n true`; otherwise asks for your password once and keeps it fresh while it runs.
  2. **herdr**: installs it to `/usr/local/bin/herdr` if it isn't on `PATH`, from the herdr.dev manifest, checking its sha256. If no herdr server answers on the socket, it writes and starts `herdr-server.service`. A herdr you already run is left alone.
  3. **herdr integrations**: for each of `claude`, `codex` and `pi` it finds (on `PATH`, in the mise shims, `~/.local/bin` or npm's global bin), runs `herdr integration install <kind>` unless `herdr integration status` already says `current`. Without it herdr never reports the agent's session, the bridge can't find its transcript, and the app shows an empty chat. herdr refuses while the CLI's own config dir doesn't exist yet (installed, never started); `pair` then warns and carries on, so start or log in to that CLI and run `relay pair` again.
  4. **Tailscale**: installs it with `https://tailscale.com/install.sh` if missing (behind a spinner; the installer's output goes to `~/.relay/setup.log` and only shows if it fails) and starts `tailscaled`. If it's logged out, it runs `tailscale up` and shows the login link as a QR code. Scan it with the phone (or open the link anywhere) and it carries on. `relay pair --authkey tskey-…` logs in without it.
  5. **relay.service**: writes `/etc/systemd/system/relay.service`, enables it, and starts it. It restarts it only when the unit or the `relay` binary changed. Then it waits up to 45 s for `/health` on the Tailscale IP.
  6. Prints the pairing QR code and the `relay://pair?url=…&token=…` link.
- In the app: machine menu > Add machine…, then scan the QR or paste the link.
- Run it again any time. On a set-up machine it changes nothing and just prints the QR again.
- `--yes` skips the question (needed without a terminal). `--port` changes 7878.

## Path 2: from the laptop
```sh
relay init
```
- `relay init` (see `relay-cli.md`) picks the AWS profile, region and an existing instance, sets up SSH, installs the harnesses and logs them in, sets up GitHub, clones your repos and writes the environment plan.
- At the end it runs `relay pair --yes` on the box over an interactive SSH session. The Tailscale login QR and the pairing QR both show in the laptop terminal.
- `relay pair` runs after the harnesses are installed and logged in, so it also installs their herdr integrations.
- It also gives each cloned repo a herdr workspace, so the app's "New chat" can start agents there.
- `relay attach` opens the box's herdr on the laptop (`herdr --remote <user>@<host>`).

## The two units
Both are system units in `/etc/systemd/system` with `User=` set to you, so they start at boot with nobody logged in. No linger needed.

| unit | runs | notes |
|---|---|---|
| `herdr-server.service` | `herdr server` | `Restart=always`. Only written when no herdr server already answers. |
| `relay.service` | `relay serve --port 7878 --require-tailscale` | `Restart=always`, `RestartSec=5`, after `herdr-server` and `tailscaled`. |

- If Tailscale isn't up yet, the bridge exits 75 and systemd retries every 5 s until it is. A crash restarts it the same way.
- Both units' `PATH` is `~/.local/share/mise/shims:~/.local/bin:~/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin`. The bridge runs `claude`, `codex` and `pi` itself for usage and controls. If they live elsewhere (npm prefix, nvm):
  ```sh
  sudo systemctl edit relay.service
  # [Service]
  # Environment=PATH=/home/you/.npm-global/bin:/home/you/.local/bin:/usr/local/bin:/usr/bin:/bin
  sudo systemctl restart relay.service
  ```
  `relay pair` rewrites only the unit file, so the drop-in survives it.
- A `HERDR_SOCKET_PATH` set when you run `relay pair` goes into both units. On Linux it must be at most 107 bytes, or `relay serve` exits 78.
- The bridge starts fine without herdr. `/health` then says `"herdr": "unavailable"` and the app shows the machine with no agents until herdr is up.
- `relay install-systemd` writes `relay.service` alone (with sudo) without enabling it, for doing the rest by hand.
- A round-9 user unit (`~/.config/systemd/user/relay.service`) is stopped and removed by `relay pair`.

## Pairing URL
- The URL uses `tailscale ip -4`. If that's wrong (NAT, several tailnet IPs, or you want the MagicDNS name):
  ```sh
  relay pair --url http://vm.tail1234.ts.net:7878
  ```
- Pairing the same machine again (new IP, new token) replaces its entry in the app. Nothing else changes.

## Network and firewall
- The bridge listens on the Tailscale IPv4 and `127.0.0.1` only, never `0.0.0.0`. The VM's public interface doesn't expose it, so no firewall rule is needed. Tailscale only needs outbound traffic.
- Anyone on your tailnet who can reach the VM still needs the token. Tailscale ACLs can narrow it further (e.g. only your phone to `vm:7878`).
- If you run `ufw` with a default deny on incoming traffic, allow the port on the Tailscale interface only:
  ```sh
  sudo ufw allow in on tailscale0 to any port 7878 proto tcp
  ```

## Updating
- Path 1: run `install.sh` again, then `relay pair`. It sees the running bridge is the old binary and restarts it.
- Path 2: `relay deploy` from the laptop.
- The token lives in `~/.relay/token`, so the app stays paired.
- Building by hand on the Mac still works: `scripts/build-linux.sh` writes `bridge/bin/relay-linux-{amd64,arm64}`. Copy it to a temp name and `mv` it over `/usr/local/bin/relay` (copying straight over the running binary fails with "text file busy"), then `relay pair`.

## Logs
- Bridge output: `~/.relay/relay.log`. The bridge truncates it at 10 MB.
  ```sh
  tail -f ~/.relay/relay.log
  ```
- Starts, stops, crashes and restarts: `journalctl -u relay.service`, `journalctl -u herdr-server.service`.

## Token
- `relay token` prints it. `relay token --rotate` replaces it. Then `sudo systemctl restart relay.service` and pair again; the app replaces the old entry.

## Uninstalling
- Path 2: `relay reset` from the laptop.
- By hand on the VM:
  ```sh
  sudo systemctl disable --now relay.service herdr-server.service
  sudo rm /etc/systemd/system/relay.service /etc/systemd/system/herdr-server.service
  sudo systemctl daemon-reload
  sudo rm /usr/local/bin/relay
  rm -rf ~/.relay                       # token, uploads, logs
  ```
- Then remove the machine in the app (Manage machines…). herdr and Tailscale stay installed.

## Troubleshooting
| Symptom | Check |
|---|---|
| `relay pair` says `relay.service doesn't answer` | `systemctl status relay.service`; `tail ~/.relay/relay.log` |
| App shows the machine offline | `systemctl status relay.service`; `tailscale status` on the VM and the phone |
| Service keeps restarting, log says `tailscale ip -4 unavailable` | `relay pair` (logs Tailscale in); `tailscale ip -4` |
| Machine online but no agents | herdr isn't running or the socket path is wrong: `systemctl status herdr-server.service`, `herdr status server`, `curl -s 127.0.0.1:7878/health` |
| App says re-pair needed | the token changed or the URL now reaches another machine: `relay pair` and scan again |
| Usage cards say the CLI isn't found | the unit's `PATH` doesn't include it: `sudo systemctl edit relay.service` |
| `relay pair` stops with "no terminal to confirm on" | run it in a terminal, or pass `--yes` |
| New chats in the app stay empty | the herdr integration is missing: `herdr integration status`, then `relay pair` |
| `relay pair` failed on a step behind a spinner | `~/.relay/setup.log` has the full output |

## Tested
Round 11, `relay pair` on Ubuntu 24.04 arm64 in a podman container with systemd as PID 1, real herdr from herdr.dev, and a stub `tailscale` (logged out at first, prints a login URL from `up`, then reports the container IP):
- a fresh run installs herdr, starts `herdr-server.service`, shows the login QR, writes and starts `relay.service`, and `/health` answers with `"herdr": "connected"`;
- a second run changes nothing: same unit files and mtimes, same PIDs and start times, no question asked;
- replacing `/usr/local/bin/relay` and running it again restarts only `relay.service`;
- `--authkey` logs in through `--auth-key=file:` and leaves no key file behind;
- a round-9 user unit is removed;
- a user without passwordless sudo is asked once (driven with `expect`);
- without a terminal and without `--yes` it lists the plan and stops;
- after a container restart both units come back with no one logged in; `kill -9` of the bridge brings it back in about 5 s; with Tailscale stopped the bridge exits 75 and comes back once it's up.

Box test fixes (same container setup, stub `claude`/`codex` in `~/.local/bin` and the mise shims, stub `pi` under a fake `npm prefix -g`, none of them on the exec `PATH`):
- before the CLIs' config dirs exist, `herdr integration install` fails; `pair` shows its message, warns and finishes;
- once they exist, `pair` installs all three integrations (`herdr integration status` says `current`), and the next run changes nothing;
- the real `tailscale.com/install.sh` runs behind one spinner line; its apt output, `+ set +x` and "Installation complete!" land only in `~/.relay/setup.log`.

Not tested here: a real `tailscaled` (the container has no tun device) and the real login. The EC2 box test covers them.
