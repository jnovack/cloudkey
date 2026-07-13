# cloudkey

**cloudkey** is a replacement for `/usr/bin/ck-ui` on your Ubiquiti Cloud Key
Generation 2 device: a small Go daemon that drives the front-panel OLED
display and status LEDs directly, without the stock UI.

## Screens

The display cycles through these screens, fading in and holding each one lit
before fading to a genuinely black, held-blank state (real rest time for the
OLED panel) between screens.

| Screen | Shows |
| --- | --- |
| ![host](docs/screenshots/screen-host.png) | Hostname and current date/time |
| ![network](docs/screenshots/screen-network.png) | LAN and WAN IP addresses |
| ![storage](docs/screenshots/screen-storage.png) | Used space and percent-used for the SD card (`/sdcard`) and the internal volume (`/volume`), with a warning icon past 90% used |
| ![system](docs/screenshots/screen-system.png) | CPU and memory utilization, plus used space and percent-used for the root filesystem |
| ![speedtest](docs/screenshots/screen-speedtest.png) | Hourly download/upload speed test results (opt-in, see `-speedtest` below) |
| ![autossh](docs/screenshots/screen-autossh.png) | Up/down status for one or two autossh tunnels (opt-in, see `-autossh-tunnel1-name` below) |
| ![wireguard](docs/screenshots/screen-wireguard.png) | WireGuard tunnel connection status (opt-in, see `-wireguard-iface` below) |
| ![tailscale](docs/screenshots/screen-tailscale.png) | Tailscale connection status (opt-in, see `-tailscale` below) |

WireGuard and Tailscale check actual tunnel liveness rather than the
systemd unit state of the underlying service: `wg-quick@<iface>` is
typically a one-shot unit that stays "active" forever once `wg-quick up`
succeeds, and `tailscaled`'s unit reports "active" whenever the daemon
process is running, neither of which reflects the peer/tailnet actually
being reachable. So WireGuard status comes from a recent handshake
(`wg show <iface> latest-handshakes`), and Tailscale status comes from
`tailscale status --json`'s `BackendState`.

autossh is the exception: `-R` (remote) forwards bind no local port to
probe, so without a `-M` monitor port there's no local traffic signal to
check at all. autossh status therefore falls back to `systemctl is-active
<unit>` — which only proves the autossh/ssh process is still running, not
that the tunnel is passing traffic. Pair each tunnel's ssh config with
`ServerAliveInterval`/`ServerAliveCountMax` so a genuinely dead connection
makes the process exit (and the unit go inactive) instead of hanging open
indefinitely; without that, a stale tunnel can still show as "up".

After boot, the status LED is blue while the daemon is idle. With `-speedtest`
enabled, it blinks blue while a speed test is running. When
`-reset-button-cmd` is configured, a physical reset-button press blinks it
white (a 300ms fade up, 300ms fade down) as acknowledgment, then returns to
blue.

## Installation

### Quick Start

1. Turn on SSH from the Unifi Console
2. `ssh root@UniFi-CloudKeyG2`
3. Download the latest `cloudkey` binary from the
   [Releases page](https://github.com/jnovack/cloudkey/releases) and copy it
   to `/usr/local/bin/cloudkey`.
4. Continue with [Using the `systemd` Service](#using-the-systemd-service)
   below. It disables the stock service without removing its binary, so you
   can restore it if needed.

#### Using the `systemd` Service

Disable the old service first.

1. `systemctl disable ck-ui`
2. `systemctl stop ck-ui`

Install this one.

1. Copy `cloudkey.service` to `/lib/systemd/system/` and ensure the
   downloaded or built binary is at `/usr/local/bin/cloudkey`.
2. `touch /etc/cloudkey.env` and set any flags you need as environment
   variables (see Configuration below).
3. `systemctl daemon-reload`
4. `systemctl enable cloudkey`
5. `systemctl start cloudkey`

### Developers

1. Have a working Go environment (see `go.mod` for the minimum version).
2. `make build` — cross-compiles for the Cloud Key's `linux/arm` target and
   writes the binary to `.local/bin/cloudkey`.
3. SCP the file over to your Cloud Key, or use `make deploy` if you've set
   `DEPLOY_HOST`/`DEPLOY_KEY` in the `Makefile` for your device.

At this point, you can choose to back up and overwrite the `/usr/bin/ck-ui`
file or install the systemd service above, depending on your Linux
experience.

## Configuration

Every flag can also be set via an environment variable — useful for
`/etc/cloudkey.env` under systemd. Env vars are the flag name uppercased,
with dashes replaced by underscores, prefixed with `CLOUDKEY_`.

| Flag | Env var | Default | Description |
| --- | --- | --- | --- |
| `-delay` | `CLOUDKEY_DELAY` | `5000` | Milliseconds each screen stays lit |
| `-blank-delay` | `CLOUDKEY_BLANK_DELAY` | `3000` | Milliseconds screens stay blanked between screens |
| `-demo` | `CLOUDKEY_DEMO` | `false` | Use fake screen data instead of network, storage, CPU/memory, and speed-test collection; the framebuffer and LEDs still require target hardware |
| `-speedtest` | `CLOUDKEY_SPEEDTEST` | `false` | Enable and display the speedtest screen |
| `-reset-button-cmd` | `CLOUDKEY_RESET_BUTTON_CMD` | `""` | Shell command to run on a single physical reset-button press (empty disables the watcher) |
| `-pidfile` | `CLOUDKEY_PIDFILE` | `/var/run/cloudkey.pid` | Pidfile path |
| `-reset` | `CLOUDKEY_RESET` | `false` | Clear the screen and exit, instead of running normally |
| `-version` | `CLOUDKEY_VERSION` | `false` | Print version and exit |
| `-autossh-tunnel1-name` | `CLOUDKEY_AUTOSSH_TUNNEL1_NAME` | `""` | Label for the first autossh tunnel; empty disables the autossh screen entirely |
| `-autossh-tunnel1-service` | `CLOUDKEY_AUTOSSH_TUNNEL1_SERVICE` | `""` | systemd unit name to check for the first tunnel's liveness (e.g. `autossh-tunnel1`); empty always shows down |
| `-autossh-tunnel2-name` | `CLOUDKEY_AUTOSSH_TUNNEL2_NAME` | `""` | Label for the second autossh tunnel; empty hides the second row |
| `-autossh-tunnel2-service` | `CLOUDKEY_AUTOSSH_TUNNEL2_SERVICE` | `""` | systemd unit name to check for the second tunnel's liveness |
| `-wireguard-name` | `CLOUDKEY_WIREGUARD_NAME` | `WireGuard` | Display name for the WireGuard screen |
| `-wireguard-iface` | `CLOUDKEY_WIREGUARD_IFACE` | `""` | WireGuard interface to check, e.g. `wg0`; empty disables the wireguard screen |
| `-wg-cmd` | `CLOUDKEY_WG_CMD` | `wg` | `wg` binary to run for WireGuard status checks; override if it's not on `PATH` |
| `-tailscale` | `CLOUDKEY_TAILSCALE` | `false` | Enable and display the Tailscale screen |
| `-tailscale-name` | `CLOUDKEY_TAILSCALE_NAME` | `TailScale` | Display name for the Tailscale screen |
| `-tailscale-cmd` | `CLOUDKEY_TAILSCALE_CMD` | `tailscale` | `tailscale` binary to run for Tailscale status checks; override if it's not on `PATH` |

If the wireguard or tailscale screen is enabled but its binary can't be
found, cloudkey exits at startup with an error rather than silently showing
"disconnected" forever — a missing dependency is a configuration error to
fix, not a display state.

### Finding your WireGuard name and interface

`CLOUDKEY_WIREGUARD_NAME` is just the label shown as the screen's title —
pick anything (it defaults to `WireGuard`). `CLOUDKEY_WIREGUARD_IFACE`
is not free text: it has to match the real interface name on the device
running cloudkey. To find it, run one of these on that device:

```bash
wg show interfaces        # lists every active WireGuard interface
ip link show type wireguard
```

If the tunnel is managed by `wg-quick` (as `wg-quick@<iface>` under
systemd), the interface name is also the config file's name minus
`.conf` — `/etc/wireguard/wg0.conf` means `wg0` — or you can read it
straight off the unit:

```bash
systemctl list-units 'wg-quick@*'
```

See [`cloudkey.env.example`](cloudkey.env.example) for a starter `/etc/cloudkey.env`. Example:

```text
CLOUDKEY_SPEEDTEST=true
CLOUDKEY_RESET_BUTTON_CMD=systemctl restart unifi
```

## Why?

I am an edge case.  I do not use my Cloud Key device for Unifi.  I think it is
a great sexy little hardware device, but to manage a network off of what is
essentially a POE SDCard, you are insane.

Issues with stability are [very well documented](https://help.ubnt.com/hc/en-us/articles/360000128688-UniFi-Troubleshooting-Offline-Cloud-Key-and-Other-Stability-Issues#4).
Using mongodb on an sdcard (limited write cycles) without *automatically*
reparing has lead me to have to recover 4 times in 2 years even with the
secondary USB power from the UPS. That is NOT remotely production stable.
Run Unifi on a server, not a "raspberry pi".

With that said, I am sure you are asking yourself *"Why do you have it all?"*
The Ubiquity Cloud Key Gen2 is a POE, ARMv7, Single-Board-Computer with
on-board battery backup and a 160x64 framebuffer display built-in.  It is
sexy, for under $200. It looks like an iDevice.

Sure, you can buy a $35 Raspberry Pi, add a case, with a touchscreen, with
a power-supply, and blah blah, but I'll pay for quality and craftmanship so
it does not look like another Frankenstein project around my house.

I can ship it to my parents, tell them to plug one cable into the new-fangled
doo-hickey and tell them to call their ISP when it has a sad face on it
(feature not developed yet).
