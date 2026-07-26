# Tailscale on Synology DSM, enrolled against a self-hosted Headscale server

Sets up the community Tailscale package on a Synology NAS (network-attached
storage device) as a client of a self-hosted Headscale coordination server.

## Prerequisites

- A Synology NAS on DSM (Synology's operating system) with SSH (Terminal &
  SNMP) enabled and root access.
- The community Tailscale package installed via Package Center.
- A reachable, self-hosted Headscale server (`<hs-domain>`) and the ability
  to generate a pre-auth key for it — see [Phase 5](Phase-5-Headscale) if you
  haven't built one yet.

## Steps

### 1. Update the Tailscale package to a current release

Headscale rejects registration from old Tailscale clients outright during
the protocol upgrade (`unsupported client version`). Package Center's
"latest available" build of the community Tailscale package lags upstream
and is commonly still too old to satisfy this — updating from within
Package Center alone is not reliable.

Instead, download the current `.spk` directly from
[Tailscale's Synology download page](https://tailscale.com/download/synology)
and install it via Package Center's **Manual Install** (the dropdown next
to the package-search box, top-right of Package Center), pointing it at
the downloaded `.spk` file.

### 2. Fix `/dev/net/tun` permissions

The community package runs `tailscaled` as an unprivileged `tailscale`
system user, but `/dev/net/tun` defaults to `root:root 0700` on DSM, which
blocks that user from creating the tunnel interface at all.

```bash
chgrp tailscale /dev/net/tun
chmod 660 /dev/net/tun
```

### 3. Grant `tailscaled` the network capabilities it needs

Creating the `tailscale0` interface requires `CAP_NET_ADMIN`, which an
unprivileged user doesn't have by default. Grant it directly to the binary:

```bash
setcap cap_net_admin,cap_net_raw+eip /volume1/@appstore/Tailscale/bin/tailscaled
```

> [!IMPORTANT]
> Re-run this after every Tailscale package update — an update replaces
> the `tailscaled` binary, which wipes the capability along with it.

### 4. Fix the iptables lock file permissions

`tailscaled` configures routing via `iptables`/`ip6tables`, both of which
need write access to a shared lock file that's root-owned by default:

```bash
touch /run/xtables.lock
chown root:tailscale /run/xtables.lock
chmod 660 /run/xtables.lock
```

### 5. Persist the permission fixes across reboots

`/dev/net/tun` and `/run/xtables.lock` both live on kernel-managed
pseudo-filesystems that get rebuilt fresh on every boot, resetting their
ownership back to root. Add a DSM Task Scheduler job to reapply the fixes
before Tailscale starts on each boot:

- **Control Panel → Task Scheduler → Create → Triggered Task → User-defined
  script**
- Trigger: **Boot-up**
- Run as: **root**
- Script:

```bash
chgrp tailscale /dev/net/tun
chmod 660 /dev/net/tun
touch /run/xtables.lock
chown root:tailscale /run/xtables.lock
chmod 660 /run/xtables.lock
```

### 6. Restart the package and enroll

```bash
synopkg restart Tailscale
```

Generate a pre-auth key on the Headscale server, then enroll from the
Synology:

```bash
tailscale up --login-server=https://<hs-domain> --authkey=<key-from-headscale>
```

## Verification

```bash
tailscale status
ip addr show tailscale0
```

Expect the Synology to list itself with its actual assigned tailnet address
(not a `100.64.0.1`-style placeholder), and its peers listed as `online`.
Confirm data-plane connectivity, not just control-plane registration:

```bash
ping <peer-tailnet-ip>
```

## Known limitation: no MagicDNS on the Synology itself

The community package can't resolve other tailnet peers by name from the
Synology (e.g. `ping cloudkey` fails, `ping 100.64.0.3` works). DSM won't
let `tailscaled` touch `/etc/resolv.conf` to point the OS resolver at
Tailscale's MagicDNS proxy:

```text
ignoring SetDNS permission error on Synology (Issue 4017); was: rename /etc/resolv.conf /etc/resolv.pre-tailscale-backup.conf: permission denied
```

This is a logged-and-ignored condition, not a crash — `tailscaled` continues
normally without it. It has no effect on actual tailnet reachability, which
routes over the stable IPs Headscale assigns regardless of DNS; it only
matters if something *running on* the Synology needs to reach another peer
*by hostname* rather than by tailnet IP. For a Synology acting purely as an
inbound target for other tailnet devices, this is safe to leave as-is —
address it only if something on the Synology itself starts making outbound
tailnet connections by name.
