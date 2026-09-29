# Remote setup: a Linux VM

Run Relay's bridge on a Linux machine (e.g. a cloud VM) so the app can drive the agents there. Each machine runs its own bridge next to its own herdr and is paired separately. See "Multiple machines" in `api.md`.

Setup is manual for now (P1). Examples use a VM called `vm`, a user `you` and the tailnet IP `100.101.102.103`.

## What you need
- A Linux VM with systemd (Ubuntu 22.04+, Debian 12+, Fedora 39+), amd64 or arm64.
- SSH access as a normal user (not root). The bridge runs as that user.
- herdr and the agent CLIs (claude, codex, pi) installed and logged in on the VM, as that user.
- The Mac with this repo and Go, to build the binary.
- The iPhone on the same tailnet (Tailscale iOS app).

## 1. Tailscale on the VM
```sh
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
tailscale ip -4          # e.g. 100.101.102.103
```
- The bridge finds `tailscale` on `PATH`, then in `/usr/bin` and `/usr/local/bin`.
- Nothing to open in the cloud firewall. Tailscale only needs outbound traffic.

## 2. Build and copy the binary
On the Mac:
```sh
scripts/build-linux.sh
# -> bridge-go/bin/relay-linux-amd64, relay-linux-arm64, SHA256SUMS
ssh vm uname -m          # x86_64 -> amd64, aarch64 -> arm64
ssh vm mkdir -p .local/bin
scp bridge-go/bin/relay-linux-amd64 vm:.local/bin/relay
ssh vm 'chmod +x ~/.local/bin/relay && ~/.local/bin/relay --version'
```
- Static (`CGO_ENABLED=0`), no libc dependency.
- The version is stamped as `<version>-<git sha>`, with `-dirty` if `bridge-go/` had uncommitted changes. `/health` reports it, and so does the app's Machines screen.
- `VERSION=… scripts/build-linux.sh` sets it by hand. `ARCHES=arm64` builds one arch.

## 3. herdr
- Start herdr on the VM as the same user (`ssh vm`, then `herdr`, then detach), or from the Mac with `herdr --remote vm`.
- The bridge talks to herdr's socket. Default: `~/.config/herdr/herdr.sock`. Check it with:
  ```sh
  herdr status server      # the "socket:" line
  ```
- If yours is elsewhere, set `HERDR_SOCKET_PATH` for the service (step 4). On Linux it must be at most 107 bytes, or `relay serve` exits 78.
- The bridge starts fine without herdr. `/health` then says `"herdr": "unavailable"` and the app shows the machine with no agents until herdr is up.

## 4. Run it as a systemd user service
On the VM:
```sh
relay install-systemd
systemctl --user daemon-reload
systemctl --user enable --now relay.service
sudo loginctl enable-linger $USER
```
- `install-systemd` writes `~/.config/systemd/user/relay.service` (`--port` to change 7878). It doesn't enable it.
- `enable-linger` starts your user services at boot and keeps them running with nobody logged in. Without it the bridge stops when your last SSH session ends.
- The unit runs `relay serve --require-tailscale` with `Restart=always`, `RestartSec=5`. If Tailscale isn't up yet the bridge exits 75 and systemd retries every 5 s until it is. A crash restarts it the same way.
- The unit's `PATH` is `~/.local/bin:~/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin`. The bridge runs `claude`, `codex` and `pi` itself for usage and controls. If they live elsewhere (npm prefix, nvm), or you need `HERDR_SOCKET_PATH`:
  ```sh
  systemctl --user edit relay.service
  # [Service]
  # Environment=PATH=/home/you/.npm-global/bin:/home/you/.local/bin:/usr/local/bin:/usr/bin:/bin
  # Environment=HERDR_SOCKET_PATH=/home/you/.config/herdr/herdr.sock
  systemctl --user restart relay.service
  ```
- Check it:
  ```sh
  systemctl --user status relay.service
  curl -s http://127.0.0.1:7878/health
  ```

## 5. Pair
```sh
relay pair
```
- Prints a QR code and the `relay://pair?url=…&token=…` link.
- In the app: machine menu > Add machine…, then scan the QR or paste the link.
- The URL uses `tailscale ip -4`. If that's wrong (NAT, several tailnet IPs, or you want the MagicDNS name):
  ```sh
  relay pair --url http://vm.tail1234.ts.net:7878
  ```
- Pairing the same machine again (new IP, new token) replaces its entry in the app. Nothing else changes.

## Network and firewall
- The bridge listens on the Tailscale IPv4 and `127.0.0.1` only, never `0.0.0.0`. The VM's public interface doesn't expose it, so no firewall rule is needed.
- Anyone on your tailnet who can reach the VM still needs the token. Tailscale ACLs can narrow it further (e.g. only your phone to `vm:7878`).
- If you run `ufw` with a default deny on incoming traffic, allow the port on the Tailscale interface only:
  ```sh
  sudo ufw allow in on tailscale0 to any port 7878 proto tcp
  ```

## Updating
On the Mac, rebuild and copy to a temp name, then swap and restart:
```sh
scripts/build-linux.sh
scp bridge-go/bin/relay-linux-amd64 vm:.local/bin/relay.new
ssh vm 'mv ~/.local/bin/relay.new ~/.local/bin/relay && systemctl --user restart relay.service'
ssh vm curl -s http://127.0.0.1:7878/health     # new "version"
```
- The token lives in `~/.relay/token`, so the app stays paired.
- `mv` replaces the file atomically. Copying straight over the running binary fails with "text file busy".

## Logs
- Bridge output: `~/.relay/relay.log`. The bridge rotates it at 10 MB.
  ```sh
  tail -f ~/.relay/relay.log
  ```
- Starts, stops, crashes and restarts: `journalctl --user -u relay.service`.

## Token
- `relay token` prints it. `relay token --rotate` replaces it. Then restart the service and pair again; the app replaces the old entry.

## Uninstalling
On the VM:
```sh
systemctl --user disable --now relay.service
rm ~/.config/systemd/user/relay.service
systemctl --user daemon-reload
rm ~/.local/bin/relay
rm -rf ~/.relay                       # token, uploads, logs
sudo loginctl disable-linger $USER    # only if nothing else needs it
```
Then remove the machine in the app (Manage machines…).

## Troubleshooting
| Symptom | Check |
|---|---|
| App shows the machine offline | `systemctl --user status relay.service`; `tailscale status` on the VM and the phone |
| Service keeps restarting, log says `tailscale ip -4 unavailable` | `sudo tailscale up`; `tailscale ip -4` |
| Machine online but no agents | herdr isn't running or the socket path is wrong: `herdr status server`, `curl -s 127.0.0.1:7878/health` |
| App says re-pair needed | the token changed or the URL now reaches another machine: `relay pair` and scan again |
| Usage cards say the CLI isn't found | the unit's `PATH` doesn't include it: `systemctl --user edit relay.service` |
| Bridge stops when you log out | `loginctl show-user $USER -p Linger` should say `yes` |

## Tested
Verified in round 9 on an Ubuntu 24.04 arm64 container with systemd as PID 1 (podman), with a stub `tailscale` that printed the container IP:
- without Tailscale the unit exits 75 and systemd retries every 5 s;
- with it, the unit serves `/health` on `127.0.0.1` and the "Tailscale" IP only;
- `kill -9` of the bridge brings it back in about 5 s;
- after a container restart with no one logged in (linger) it comes back up by itself;
- `disable --now` stops it;
- the amd64 binary runs `--version` under emulation.
