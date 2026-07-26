# Fail-closed VPN egress for isolated background services

This builds on the Phase 2 apps if you decided at that point (see
[Phase 2](Phase-2-Apps)) that you want their outbound traffic — searches,
downloads, indexer lookups — to fail closed behind a VPN rather than
fall back to your normal connection if the tunnel drops. See Part 9 of
[Phase 2 Hardening](Phase-2-Hardening) for how the pieces that build on top of this
(the liveness probe) behave whether or not this phase is present.

This is a complete implementation reference, not just an overview — every
script and config below is the **exact, currently-deployed content**,
with anything environment-specific replaced by an explicit `<placeholder>`
token. A reader or their agent should be able to implement this fully
from this document alone, substituting their own values wherever a
placeholder appears.

**What this builds**: a set of background services run inside a network
namespace whose only route to the internet is a VPN tunnel. If the
tunnel is down, those services have no path out at all — not a firewall
rule that might be misconfigured, a structural guarantee. The services
stay reachable on their normal ports from the rest of the network
throughout; only their own *outbound* connections are affected. A
health-check + auto-heal system keeps the tunnel (and `systemctl status`
itself) honest, and everything logs in a form suitable for later
monitoring/alerting.

## Placeholders

| Placeholder | Meaning | Scales? |
| --- | --- | --- |
| `<vpn>` | your chosen name for the namespace/tunnel interface | No |
| `<service>` | a protected systemd service pinned in the namespace | **Yes** |
| `<port>` | a port one of those services listens on | **Yes** |
| `<uplink>` | the host's real network interface | No |
| `<provider-dns-ip>` | your VPN provider's internal DNS | No |
| `<provider-check-url>` | a URL that confirms you're on the VPN | No |
| `<provider-name>` | substring confirming a successful check | No |

**On `<service>` and `<port>` scaling**: this guide shows the pattern
with a couple of illustrative entries per list — extend or shrink every
list marked "repeat per service/port" to match however many you actually
have. Everywhere one of these lists appears, it needs to be the *same*
list, in the *same* order, across every file that mentions it (the
`Before=` line, the DNAT loop, the `SERVICES=` variable, etc.) — there's
no single place that defines it once, so treat find-and-replace across
the whole set of files as one pass, not per-file.

The tooling itself (scripts, systemd unit filenames) is named plainly
after what it does (`vpn-check.sh`, `wg-quick-vpn.service`, etc.) —
those names aren't placeholders, they're just what this guide calls its
own files. Only `<vpn>` — the namespace/interface *value* those files
operate on — is something you choose.

## Architecture

- **Protected services**: any number of existing systemd-managed
  services whose outbound traffic must never bypass the VPN. They keep
  running as normal services — no containers, no code changes — just
  pinned into an isolated network namespace.
- **Isolated namespace**: a dedicated network namespace containing only
  a VPN tunnel interface and a veth pair back to the host. It has no
  other route to the internet, so it has no fallback path if the tunnel
  drops.
- **veth pair** (virtual Ethernet pair): connects the isolated namespace
  to the host's normal namespace — used only for the services to remain
  *reachable* from the rest of the network, never as an alternate way
  *out*.
- **DNAT** (Destination NAT): keeps each protected service answering on
  its normal address/port from the outside, even though it now lives in
  a different namespace internally. Nothing external needs to know
  anything moved.
- **Health check + auto-heal**: a small script that verifies the tunnel
  is genuinely passing traffic (not just "the interface exists"), and a
  timer that restarts the tunnel and the protected services automatically
  if it isn't.

## Prerequisites

- Root access on the host.
- A WireGuard configuration file from your VPN provider (`.conf` in
  standard `wg-quick` format).
- systemd 245+ (for `NetworkNamespacePath=` support — check with
  `systemd --version`).
- The protected services already installed and running as normal systemd
  units.

**Before you start**: check whether your kernel actually has in-kernel
WireGuard support, and whether `nftables` has a working kernel backend.
Neither is guaranteed on older or vendor-customized kernels, and both
absences change how this needs to be built (this guide covers both
cases):

```bash
modprobe wireguard 2>&1                 # kernel WireGuard?
modprobe nf_tables 2>&1                 # nftables kernel backend?
```

> [!IMPORTANT]
> If either command above fails, don't skip ahead — read the matching
> notes before continuing. A missing kernel WireGuard module is covered
> in Part 1; missing `nftables` support is covered in Part 3. The
> default tooling doesn't fail loudly in either case — it just breaks
> silently instead of falling back cleanly.

## Part 1 — Install WireGuard tooling

```bash
apt-get install --no-install-recommends wireguard-tools wireguard-go openresolv
```

`--no-install-recommends` matters here: `wireguard-tools` *recommends* a
kernel module package, and on some systems apt will resolve that
recommendation by pulling in an entirely different kernel image to
satisfy it.

> [!WARNING]
> Don't let apt swap your kernel image out from under you. Run
> `apt-get install --simulate` (without `--no-install-recommends`) first
> to see exactly what a plain install would pull in, before running
> anything for real on a production host. Only drop
> `--no-install-recommends` if your kernel does have native WireGuard
> support and you'd rather use that.

If your kernel lacks native WireGuard, the userspace fallback needs one
extra nudge — `wg-quick`'s default fallback lookup expects a binary
literally named `wireguard-go`, but some distributions package it as
plain `wireguard`. Check which name you actually got:

```bash
dpkg -L wireguard-go | grep bin/
```

If it's `wireguard` rather than `wireguard-go`, you'll need
`WG_QUICK_USERSPACE_IMPLEMENTATION=wireguard` set wherever you invoke
`wg-quick` (already included in the systemd unit in Part 3).

## Part 2 — Create the isolated namespace and veth pair

`/usr/local/sbin/netns-vpn-up.sh` (idempotent — safe to re-run):

```bash
#!/bin/bash
# Creates the isolated network namespace and the veth pair that lets the
# root namespace (and, via routing, anything else on the host) reach
# into it — without giving the namespace itself any route back out
# except the VPN tunnel that gets brought up separately.
#
# Also source-NATs the namespace's own control traffic (routed via the
# veth pair, not via the tunnel itself — see wg-quick-vpn.service) so it
# actually reaches the internet looking like it came from the host's own
# uplink address, the same way any other outbound traffic from this box
# would.
set -euo pipefail

NS="<vpn>"
HOST_IP=10.200.200.1/30
NS_IP=10.200.200.2/30
NS_ADDR=10.200.200.2
UPLINK="<uplink>"

if ! ip netns list | grep -qx "$NS"; then
  ip netns add "$NS"
fi
ip netns exec "$NS" ip link set lo up

if ! ip link show veth-host >/dev/null 2>&1; then
  ip link add veth-host type veth peer name veth-ns
  ip link set veth-ns netns "$NS"
fi

ip addr replace "$HOST_IP" dev veth-host
ip link set veth-host up
ip netns exec "$NS" ip addr replace "$NS_IP" dev veth-ns
ip netns exec "$NS" ip link set veth-ns up

# Ingress reply routing: DNAT'd connections land on the app's veth-ns
# address, so replies are sourced from it too — but the namespace's only
# *destination-based* default route is the tunnel (see
# wg-quick-vpn.service), which would send those replies into the VPN
# instead of back to whoever actually connected. This routes anything
# *sourced* from the veth address back out via the veth pair, regardless
# of destination, without touching the tunnel's own default route for
# actual app-initiated (outbound) traffic.
ip netns exec "$NS" ip route replace default \
  via 10.200.200.1 dev veth-ns table 200
ip netns exec "$NS" ip rule del from "$NS_ADDR" table 200 priority 200 \
  2>/dev/null || true
ip netns exec "$NS" ip rule add from "$NS_ADDR" table 200 priority 200

# DNS for processes running inside the namespace (systemd-resolved's
# 127.0.0.53 stub only listens on the *host's* loopback, invisible from
# inside an isolated namespace). Point at your VPN provider's own DNS if
# it has one — keeps DNS queries inside the tunnel too, rather than
# leaking query patterns to whatever the host normally uses.
mkdir -p -m 755 "/etc/netns/$NS"
echo 'nameserver <provider-dns-ip>' > "/etc/netns/$NS/resolv.conf"

sysctl -qw net.ipv4.ip_forward=1

# -w: wait for the xtables lock instead of failing immediately. At boot,
# anything else that also configures iptables at roughly the same time
# (Tailscale, Docker, ufw, fail2ban...) can hold this lock momentarily —
# without -w the loser of that race fails outright ("Another app is
# currently holding the xtables lock") instead of just waiting the
# split-second for the winner to finish.
grep -q "^${NS_ADDR}$" \
  <(iptables -w 5 -t nat -S POSTROUTING 2>/dev/null \
    | grep -oP '(?<=-s )[0-9.]+') \
  2>/dev/null || \
  iptables -w 5 -t nat -A POSTROUTING -s "$NS_ADDR" -o "$UPLINK" -j MASQUERADE

# Keep every protected service reachable at the host's normal
# address/port — nothing external needs to know it moved into an
# isolated namespace. Repeat per port: this example shows two, list as
# many as you actually have (e.g. `for PORT in 8080 8443 9000 9001; do`
# for four).
for PORT in <port> <port>; do
  iptables -w 5 -t nat -C PREROUTING -p tcp --dport "$PORT" \
    -j DNAT --to-destination "$NS_ADDR:$PORT" 2>/dev/null || \
    iptables -w 5 -t nat -A PREROUTING -p tcp --dport "$PORT" \
      -j DNAT --to-destination "$NS_ADDR:$PORT"
done
```

**Packaged version**: [`scripts/runbook/phase4-netns-vpn-up.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-netns-vpn-up.sh),
same content as above.

`/usr/local/sbin/netns-vpn-down.sh`:

```bash
#!/bin/bash
# Tears down the isolated namespace and its veth pair.
set -uo pipefail

NS="<vpn>"
ip link delete veth-host 2>/dev/null
ip netns delete "$NS" 2>/dev/null
exit 0
```

**Packaged version**: [`scripts/runbook/phase4-netns-vpn-down.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-netns-vpn-down.sh).

```bash
install -m 755 scripts/runbook/phase4-netns-vpn-up.sh /usr/local/sbin/netns-vpn-up.sh
install -m 755 scripts/runbook/phase4-netns-vpn-down.sh /usr/local/sbin/netns-vpn-down.sh
```

`/etc/systemd/system/netns-vpn.service`:

```ini
[Unit]
Description=Create isolated network namespace for VPN-only egress
After=network.target
Before=wg-quick-vpn.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/sbin/netns-vpn-up.sh
ExecStop=/usr/local/sbin/netns-vpn-down.sh

[Install]
WantedBy=multi-user.target
```

## Part 3 — Bring up the VPN tunnel inside the namespace

Place your provider's WireGuard config at `/etc/wireguard/<vpn>.conf`.

> [!IMPORTANT]
> This file contains a private key, so treat it like one: mode `600`,
> root-owned. If someone else (your VPN provider, a script) generated it
> for you, don't open it yourself to check it — verify only its ownership
> and permissions with `stat` or `ls`, never its contents.

**If your kernel has no nftables support** (see Prerequisites): `wg-quick`
decides whether to use `nft` or `iptables` purely by checking whether the
`nft` *binary* exists on `PATH` — not whether it actually works. If the
binary is present but the kernel backend isn't, `wg-quick up` will fail
and roll back entirely. Fix this by adding one line to your config's
`[Interface]` section:

```ini
[Interface]
Table = off
```

This skips `wg-quick`'s automatic routing setup, which you'll replace
with two manual pieces below.

`/usr/local/sbin/vpn-peer-route.sh` — keeps the tunnel's *own* raw
control traffic routed via the veth pair rather than into itself:

```bash
#!/bin/bash
# Keeps the WireGuard peer's own endpoint reachable via the veth pair
# (through the root namespace's real uplink) instead of via the tunnel's
# own default route — otherwise the tunnel's handshake/keepalive packets
# would try to route through themselves. Reads the peer IP live from
# `wg show`, never from the config file itself (which holds the private
# key and is deliberately never read by tooling here).
#
# A /32 host route always wins over the tunnel's 0.0.0.0/0 default route
# on prefix length alone, so this needs no fwmark trickery — which
# matters if you're on a userspace WireGuard implementation (used when
# the kernel has no native support): unlike kernel WireGuard, userspace
# implementations don't reliably apply `wg set <if> fwmark ...` to the
# actual outbound socket, so fwmark-based routing splits can silently
# fail (packets never leave the namespace, with no error). Don't reach
# for fwmark as a "more proper" alternative to this — it looks cleaner
# but doesn't actually work under wireguard-go.
set -euo pipefail

ACTION="${1:?usage: $0 add|del}"
NS="<vpn>"
GATEWAY=10.200.200.1

PEER_IP=$(ip netns exec "$NS" wg show "$NS" endpoints 2>/dev/null \
  | awk '{print $2}' | cut -d: -f1)

if [[ -z "$PEER_IP" ]]; then
  echo "vpn-peer-route: no peer endpoint found, nothing to $ACTION" >&2
  exit 0
fi

case "$ACTION" in
  add)
    ip netns exec "$NS" ip route replace "$PEER_IP/32" via "$GATEWAY" dev veth-ns
    ;;
  del)
    ip netns exec "$NS" ip route del "$PEER_IP/32" via "$GATEWAY" \
      dev veth-ns 2>/dev/null || true
    ;;
  *)
    echo "vpn-peer-route: unknown action $ACTION" >&2
    exit 1
    ;;
esac
```

**Packaged version**: [`scripts/runbook/phase4-vpn-peer-route.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-vpn-peer-route.sh):

```bash
install -m 755 scripts/runbook/phase4-vpn-peer-route.sh /usr/local/sbin/vpn-peer-route.sh
```

`/etc/systemd/system/wg-quick-vpn.service`:

```ini
[Unit]
Description=VPN tunnel (userspace, inside isolated netns)
After=netns-vpn.service
Requires=netns-vpn.service
Before=<service>.service

[Service]
Type=oneshot
RemainAfterExit=yes
NetworkNamespacePath=/var/run/netns/<vpn>
Environment=WG_QUICK_USERSPACE_IMPLEMENTATION=wireguard
ExecStart=/usr/bin/wg-quick up <vpn>
ExecStartPost=/sbin/ip route add default dev <vpn>
ExecStartPost=/usr/local/sbin/vpn-peer-route.sh add
ExecStop=/usr/bin/wg-quick down <vpn>
ExecStopPost=/usr/local/sbin/vpn-peer-route.sh del

[Install]
WantedBy=multi-user.target
```

`Before=<service>.service` needs **every** protected service name listed,
space-separated on that one line (e.g.
`Before=sync-agent.service backup-agent.service report-agent.service`
for three) — this is one of the lists described in the Placeholders
section that has to match across every file that mentions it.

(Drop the `Environment=WG_QUICK_USERSPACE_IMPLEMENTATION=wireguard` line
if your kernel has native WireGuard, or your userspace binary is already
named `wireguard-go`. Drop the `ip route add default dev <vpn>`
`ExecStartPost` line if you didn't set `Table = off`.)

```bash
systemctl daemon-reload
systemctl enable --now netns-vpn.service
systemctl start wg-quick-vpn.service
systemctl status wg-quick-vpn.service   # expect "active (running)"
wg show <vpn> latest-handshakes         # non-zero timestamp = handshake succeeded
```

## Part 4 — Pin the protected services into the namespace

For each protected service, add a systemd drop-in override rather than
editing the unit file directly (survives package updates, easy to
inspect/remove) — this exact content, unchanged, for every one of them:

```bash
mkdir -p /etc/systemd/system/<service>.service.d
cat > /etc/systemd/system/<service>.service.d/vpn.conf <<'EOF'
[Unit]
After=wg-quick-vpn.service
Requires=wg-quick-vpn.service

[Service]
NetworkNamespacePath=/var/run/netns/<vpn>

# DNS override, orphan-proof. Under systemd-resolved, /etc/resolv.conf
# is a symlink to /run/systemd/resolve/stub-resolv.conf, and resolved
# replaces that stub file (atomic rename) whenever its state changes —
# at boot, and on any link/DNS reconfiguration. A bind mounted directly
# onto the stub FILE is silently orphaned by every such rename (the
# mount stays on the old inode; path lookups find the new file),
# reverting this service to 127.0.0.53 — which does not exist inside
# the namespace. Instead: cover the whole directory with a private
# tmpfs and bind the namespace's resolver file onto the stub PATH
# inside it — resolved's renames then happen underneath the covered
# directory and can never touch what this service resolves. Do NOT
# "simplify" this back to a direct /etc/resolv.conf bind; that
# re-introduces the silent DNS breakage.
TemporaryFileSystem=/run/systemd/resolve:ro
BindReadOnlyPaths=/etc/netns/<vpn>/resolv.conf:/run/systemd/resolve/stub-resolv.conf

# Belt-and-braces: never let this service's mounts propagate back into
# the host mount namespace, while still receiving host mount events.
MountFlags=slave
EOF
systemctl daemon-reload
systemctl restart <service>
```

If your host does *not* run `systemd-resolved` and `/etc/resolv.conf` is
a plain file, the two DNS lines collapse to a single direct bind —
`BindReadOnlyPaths=/etc/netns/<vpn>/resolv.conf:/etc/resolv.conf` — but
only if nothing on the host ever replaces that file (anything that does,
`resolvconf`, a DHCP client hook, orphans the bind exactly as described
above).

`Requires=` here does real work beyond ordering: if the tunnel service
stops (deliberately, or because it fails), systemd cleanly stops every
service that requires it too — no service is ever left running with a
half-broken network. Confirmed by testing: stopping the tunnel produces a
clean "Application is shutting down" in each service's own log, not a
crash. The reverse isn't automatic, though — restarting the tunnel does
not automatically restart the services that require it, since `Requires=`
only prevents starting *without* the dependency, it doesn't imply
restarting *because of* it. Restart them explicitly after the tunnel
comes back (the health-check script in Part 6 does this for you).

## Part 5 — Verify external reachability

Already handled by the DNAT loop in Part 2's `netns-vpn-up.sh`. Just
verify it — repeat this check per port, same list as everywhere else:

```bash
for PORT in <port> <port>; do
  curl -s -o /dev/null -w "port ${PORT}: %{http_code}\n" --max-time 5 "http://<host-address>:${PORT}/"
done
```

There's a second routing wrinkle here, separate from the tunnel's own
traffic in Part 3: a DNAT'd connection's *replies* are sourced from the
namespace's veth address, and — like the tunnel's own traffic — that
would otherwise hit the namespace's default route (into the tunnel)
instead of going back to whoever actually connected. This is already
handled by the `table 200` rule in `netns-vpn-up.sh` (Part 2) — it only
affects traffic *sourced from* the namespace's veth address, so it has no
effect on the protected services' own outbound connections (which source
from the tunnel's own assigned address instead) and doesn't create any
bypass of the kill switch — the fail-closed guarantee described under
"What this builds" above, under its common VPN-client name.

## Part 6 — Health check and auto-healing

> [!IMPORTANT]
> The tunnel service's own status can lie. `wg-quick` launches the actual
> VPN process as a detached background process outside systemd's
> tracking (`MainPID` stays `0` even while it's genuinely running). If
> that process dies unexpectedly — not a deliberate stop, an actual crash
> or network-triggered death — systemd has no way to know, and
> `wg-quick-vpn.service` reports `active` indefinitely with zero real
> connectivity behind it.

Confirmed by directly killing the tunnel process: the namespace's
interface disappeared, every protected service lost all connectivity,
and `systemctl status` kept reporting everything as `active` throughout,
with no error anywhere in the journal.

A real health check has to verify actual traffic, not just process or
interface state. Handshake recency alone isn't reliable either, unless
your VPN config sets `PersistentKeepalive` — without it, a perfectly
healthy but briefly idle tunnel can show a stale handshake with nothing
actually wrong, since WireGuard only re-handshakes when there's traffic
to send.

`/usr/local/sbin/vpn-check.sh` — exit 0 only on genuine, verified
connectivity:

```bash
#!/bin/bash
# Exits 0 if the VPN tunnel is genuinely passing traffic right now,
# non-zero otherwise. Deliberately does a real connectivity check rather
# than trusting `wg show` handshake recency alone (see note above).
#
# Usage: vpn-check.sh [-q]   (-q: no stdout, exit code only)
set -uo pipefail

NS="<vpn>"
QUIET=0
[[ "${1:-}" == "-q" ]] && QUIET=1

log() { [[ "$QUIET" == "1" ]] || echo "$1" >&2; }

if ! ip netns list 2>/dev/null | grep -qx "$NS"; then
  log "FAIL: namespace '$NS' does not exist"
  exit 1
fi

if ! ip netns exec "$NS" ip link show "$NS" >/dev/null 2>&1; then
  log "FAIL: no tunnel interface inside the namespace"
  exit 2
fi

RESPONSE=$(ip netns exec "$NS" curl -s --max-time 6 <provider-check-url> 2>/dev/null)
if [[ -z "$RESPONSE" ]]; then
  log "FAIL: no response from connectivity check (interface up but not passing traffic)"
  exit 3
fi

if [[ "$RESPONSE" != *"<provider-name>"* ]]; then
  log "FAIL: connected to something, but not confirmed as the VPN: $RESPONSE"
  exit 4
fi

log "OK: $RESPONSE"
exit 0
```

**Packaged version**: [`scripts/runbook/phase4-vpn-check.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-vpn-check.sh):

```bash
install -m 755 scripts/runbook/phase4-vpn-check.sh /usr/local/sbin/vpn-check.sh
```

**A second, independent failure mode — already prevented by Part 4's
drop-in shape, but worth understanding and guarding.** A DNS override
bound *directly* onto `/etc/resolv.conf` is silently orphaned by
`systemd-resolved`: the kernel follows the symlink to
`/run/systemd/resolve/stub-resolv.conf` before binding, and resolved
replaces that stub file (atomic rename, not an in-place edit) whenever
its state changes — at boot, and on any link or DNS reconfiguration
(a DHCP renew, another VPN client pushing DNS settings, and so on; how
often varies by host, but on the box this was built on it happened
within minutes). Each rename leaves the bind-mount attached to the old,
now-unreachable inode with no error anywhere. The service's own
outbound route is untouched (the kill switch still holds — no route out
still means no route out), but its only visible "DNS server" reverts to
`127.0.0.53`, which doesn't exist inside the isolated namespace at all,
and `systemctl status` reports nothing but "active" the whole time.
Confirmed by controlled test: after one forced stub rewrite
(`systemctl restart systemd-resolved`), every service still using the
direct bind had reverted instantly, while services on Part 4's
tmpfs-covered shape kept resolving through the tunnel's DNS unaffected.

The tunnel-level check above is naturally immune to this — `ip netns
exec` builds a fresh mount namespace (and a fresh resolv.conf bind) on
every single invocation, so it never goes stale. And with Part 4's
drop-in as written, long-running services are structurally immune too:
resolved's renames happen underneath the tmpfs covering
`/run/systemd/resolve`, where no pinned service can see them. The heal
script below still verifies each service's own effective resolver every
cycle anyway — it's a cheap regression guard whose main job is catching
a drop-in that gets "simplified" back to the direct bind. If it ever
fires, it heals by `systemctl restart` of just that service, which
reapplies its mounts fresh. That restart-to-heal is cheap and safe here
since every protected service in this guide persists its own
queue/state to disk and resumes cleanly after a restart — confirm
that's also true of your own protected services before relying on it.

`/usr/local/sbin/vpn-heal.sh` — on failure, restores the tunnel and every
service pinned to it, then re-verifies before declaring success; also
independently checks and heals each service's own DNS override every
cycle, regardless of tunnel health:

```bash
#!/bin/bash
# Runs on a timer (see vpn-heal.timer). If the tunnel is healthy, does
# nothing further than check each service's own DNS. If not, restores it
# and the services pinned to it, then verifies the fix actually worked
# before declaring success.
#
# `wg-quick`/userspace WireGuard daemonize outside systemd's process
# tracking (Type=oneshot's MainPID is never set to the real tunnel
# process), so wg-quick-vpn.service can keep reporting "active"
# indefinitely after the underlying tunnel has actually died — this
# script is what makes `systemctl status` trustworthy again: an explicit
# stop+start on every healing pass means its state reflects reality as
# of the last check, not just the last time it happened to be started.
#
# Second, independent thing this watches: each service's DNS override
# (the tmpfs + bind drop-in from Part 4). That shape makes resolved's
# stub rewrites unable to orphan the override, so this per-service
# check should never fire under normal operation — it stays as a cheap
# regression guard against a drop-in "simplified" back to a direct
# /etc/resolv.conf bind, which IS silently orphaned by every stub
# rewrite (see Part 6's failure-mode discussion). If it fires, a
# `systemctl restart` on just that service reapplies its mounts fresh.
#
# Logging is deliberately transition-based, not per-check: this runs
# every 60s, and logging "still healthy" on every single pass would bury
# real events (disconnects, heals) in noise and eat into a limited
# journal budget on constrained hosts for nothing. `journalctl -u
# vpn-heal.service` already gets a timestamped entry every cycle for
# free via systemd itself (Starting.../Finished...), which doubles as a
# heartbeat signal for a future alerting pass — no need to duplicate
# that at the application-log level too.
set -uo pipefail

CHECK=/usr/local/sbin/vpn-check.sh
TUNNEL_SERVICE=wg-quick-vpn.service
# List every protected service, space-separated — same list as the
# Before= line in wg-quick-vpn.service (Part 3). Example with three:
# SERVICES="sync-agent backup-agent report-agent"
SERVICES="<service> <service>"
DNS_EXPECTED="nameserver <provider-dns-ip>"
RETRIES=3
RETRY_DELAY=5
STATE_FILE=/run/vpn-heal.state

log() { logger -t vpn-heal "$1"; echo "$1"; }
prev_state() { cat "$STATE_FILE" 2>/dev/null || echo "unknown"; }
set_state() { echo "$1" > "$STATE_FILE"; }

# Checks the DNS override actually pinned to a running service's process
# is intact. Reads via that process's own mount namespace (nsenter -m),
# not `ip netns exec` — this deliberately checks the same long-lived
# bind-mount the service itself is using, not a fresh one, since the
# whole point is to catch it going stale under the service.
service_dns_ok() {
  local pid
  pid=$(systemctl show -p MainPID --value "$1" 2>/dev/null)
  [[ -n "$pid" && "$pid" != "0" ]] || return 1
  nsenter -t "$pid" -m -- grep -qx "$DNS_EXPECTED" /etc/resolv.conf 2>/dev/null
}

heal_service_dns() {
  local svc_fail=0
  for svc in $SERVICES; do
    service_dns_ok "$svc" && continue
    log "$svc: DNS override not intact (unexpected with the tmpfs drop-in shape) — restarting to reapply"
    systemctl restart "$svc"
    sleep 2
    if service_dns_ok "$svc"; then
      log "$svc: DNS override reapplied"
    else
      log "$svc: DNS override still broken after restart"
      svc_fail=1
    fi
  done
  return "$svc_fail"
}

if "$CHECK" -q; then
  [[ "$(prev_state)" == "healthy" ]] || log "tunnel healthy (was: $(prev_state))"
  set_state healthy
  heal_service_dns
  exit $?
fi

set_state unhealthy
log "tunnel unhealthy — healing: restarting $TUNNEL_SERVICE and dependent services"

# Explicit stop+start (not `restart`) so an intermediate `systemctl
# status` mid-heal shows an honest "inactive", not a misleading "active"
# carried over from before the problem was detected.
systemctl stop "$TUNNEL_SERVICE"
systemctl start "$TUNNEL_SERVICE"

# Requires= only stops dependents when the tunnel stops — it does not
# auto-start them when the tunnel comes back, so that's done explicitly.
# shellcheck disable=SC2086
systemctl start $SERVICES

for ((i = 1; i <= RETRIES; i++)); do
  if "$CHECK" -q; then
    set_state healthy
    log "heal succeeded (attempt $i/$RETRIES)"
    heal_service_dns
    exit $?
  fi
  sleep "$RETRY_DELAY"
done

log "heal FAILED after $RETRIES attempts — tunnel still not passing traffic"
exit 1
```

**Packaged version**: [`scripts/runbook/phase4-vpn-heal.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-vpn-heal.sh):

```bash
install -m 755 scripts/runbook/phase4-vpn-heal.sh /usr/local/sbin/vpn-heal.sh
```

`/etc/systemd/system/vpn-heal.service`:

```ini
[Unit]
Description=Check VPN tunnel health and heal it if down
# Deliberately no After=/Requires= on wg-quick-vpn.service — this unit's
# whole job is to run independently of that service's (unreliable)
# reported state and check/fix the real thing.

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/vpn-heal.sh
```

`/etc/systemd/system/vpn-heal.timer`:

```ini
[Unit]
Description=Periodically check/heal the VPN tunnel

[Timer]
OnBootSec=60
OnUnitActiveSec=60
AccuracySec=5

[Install]
WantedBy=timers.target
```

```bash
systemctl daemon-reload
systemctl enable --now vpn-heal.timer
```

## Dashboard integration (optional, but recommended)

If you have (or want) an on-login status summary, call the real health
check rather than trusting `systemctl is-active wg-quick-vpn.service` —
that unit's `Active` state can go stale exactly as described in Part 6.
This is the actual function used (bash, assumes `$GREEN`/`$RED`/etc. are
already set up as ANSI color codes elsewhere in the script, and a
`svc_line` helper exists for plain systemd-unit status lines):

```bash
vpn_line() {
  # Calls the real health-check script rather than trusting
  # `systemctl is-active wg-quick-vpn.service` — that unit's Active
  # state can go stale if the tunnel process dies unannounced.
  local color text
  if /usr/local/sbin/vpn-check.sh -q; then
    color="$GREEN"; text="live"
  else
    color="$RED"; text="DOWN (auto-heal checks every 60s)"
  fi
  printf "  %-28s ${color}%s${RESET}\n" "Tunnel (live check)" "$text"
}
```

Called alongside a plain unit-status line for the heal timer itself:

```bash
vpn_line
svc_line vpn-heal.timer "Auto-heal timer"
```

[CloudKey Admin Tools](CloudKey-Admin-Tools) packages exactly this
logic as [`scripts/runbook/cloudkey-dashboard.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/cloudkey-dashboard.sh)
— the snippet above is what it implements, kept here for the rationale.
It gates the whole section on `/usr/local/sbin/vpn-check.sh` being
present rather than on any unit's `LoadState`, since `wg-quick-vpn.service`
exists as soon as you follow Part 3 — its mere presence can't tell "Phase
4 built" from "Phase 4 not built" the way an absent unit does for the
other optional phases.

## Monitoring and alerting readiness

Everything needed for a future alerting pass already exists; this
section is what that pass would hook into — nothing further to build
until you actually want alerting active.

**Event log** (connects/disconnects/self-heals — nothing else):

```bash
journalctl -t vpn-heal -f
```

This is deliberately transition-based, not per-check (see the comment
block at the top of `vpn-heal.sh`) — a line appears only when the
tunnel actually goes down, gets healed, or fails to heal. An alerting
rule would watch for `unhealthy` or `FAILED` appearing in this stream.

**Heartbeat, for detecting the monitoring itself dying** (not just the
tunnel — a silent event log is ambiguous between "everything's fine" and
"the timer got disabled"):

```bash
journalctl -u vpn-heal.service -f
```

This gets a systemd-generated `Starting.../Finished...` pair every timer
cycle *regardless of outcome*, for free — no extra logging code needed.
An alerting rule of "no entry in longer than N cycles" is a complete
dead-man's-switch on the whole health-check system without touching
`vpn-heal.sh` itself.

**Log durability**: confirm `journald` is actually persistent before
relying on any of this surviving a reboot — some minimal/embedded
systems default to volatile (in-memory only) journal storage:

```bash
grep '^Storage=' /etc/systemd/journald.conf   # want: Storage=persistent
journalctl --disk-usage                # check against your disk budget
```

If it's not already `persistent`, set it and restart `systemd-journald`.
Also check `SystemMaxUse=` is set to something sane for your disk size —
constrained/embedded hosts especially, where an unbounded journal could
compete with actual application storage.

## Verification

Confirm each protected service's *own* egress, not just the namespace in
general (run inside each service's actual process namespace, not just
`ip netns exec`, for the strongest proof) — repeat per service:

```bash
pid=$(systemctl show -p MainPID --value <service>)
nsenter -t "$pid" -n -m -- curl -s <provider-check-url>
```

Two easy-to-miss details in that one line, both confirmed by testing:

- **`-m` (join the mount namespace too), not just `-n`.** The DNS
  override for the namespace (Part 4) is a bind-mount that's only
  visible inside that service's own *mount* namespace — `-n` alone
  reaches the tunnel's network correctly but still resolves hostnames
  via the host's own (unreachable-from-in-here) resolver, which fails
  every lookup and would misreport a working service as broken.
- **The `--` before `curl`.** Without it, `nsenter`'s own flag parsing
  can silently swallow one of `curl`'s short flags (e.g. `-s`) instead
  of passing it through, and the check fails with no explanation.

**Packaged version**: [`scripts/runbook/phase4-vpn-verify-service.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-vpn-verify-service.sh),
deployed at `/usr/local/sbin/vpn-verify-service.sh` (repeat
per service, or run with no arguments to check every pinned service in
one pass) does exactly this, plus the namespace-identity check the raw
one-liner skips:

```bash
#!/bin/bash
# Confirms each protected service's OWN process is actually egressing
# through the tunnel -- not just that the namespace/tunnel exists in the
# abstract. Joins each service's real PID's net+mount namespace (nsenter
# -n -m) rather than `ip netns exec`, since the DNS override for the
# namespace is a bind-mount (Part 4) that only that process's own mount
# namespace sees -- `ip netns exec` or `nsenter -n` alone silently falls
# back to the host's own resolver (unreachable from inside the
# namespace) instead of failing loudly, which would misreport a working
# service as broken.
#
# Usage: vpn-verify-service.sh [service ...]   (default: every pinned service)
set -uo pipefail

NS="<vpn>"
CHECK_URL="<provider-check-url>"
CONFIRM="<provider-name>"
# Same list as SERVICES= in vpn-heal.sh / the Before= line in
# wg-quick-vpn.service (Part 3).
DEFAULT_SERVICES=(<service> <service>)
SERVICES=("$@")
[[ ${#SERVICES[@]} -eq 0 ]] && SERVICES=("${DEFAULT_SERVICES[@]}")

fail=0
for svc in "${SERVICES[@]}"; do
  pid=$(systemctl show -p MainPID --value "$svc" 2>/dev/null)
  if [[ -z "$pid" || "$pid" == "0" ]]; then
    echo "FAIL  $svc: not running"
    fail=1
    continue
  fi

  actual_ns=$(ip netns identify "$pid" 2>/dev/null)
  if [[ "$actual_ns" != "$NS" ]]; then
    echo "FAIL  $svc (pid $pid): in namespace '${actual_ns:-none}', expected '$NS'"
    fail=1
    continue
  fi

  # The -- separates nsenter's own flags from curl's -- without it,
  # nsenter's getopt parsing can swallow curl's short flags (e.g. -s)
  # and fail silently instead of running the check.
  response=$(nsenter -t "$pid" -n -m -- curl -s --max-time 6 "$CHECK_URL" 2>/dev/null)
  if [[ -z "$response" ]]; then
    echo "FAIL  $svc (pid $pid): no response -- DNS or tunnel not reachable from its own namespace"
    fail=1
  elif [[ "$response" != *"$CONFIRM"* ]]; then
    echo "FAIL  $svc (pid $pid): connected, but not confirmed on the VPN: $response"
    fail=1
  else
    echo "OK    $svc (pid $pid): $response"
  fi
done

exit "$fail"
```

```bash
install -m 755 scripts/runbook/phase4-vpn-verify-service.sh /usr/local/sbin/vpn-verify-service.sh
```

### Namespace-level checks (structural / fail-closed)

The per-service check above proves each service *reaches* the VPN. These
two prove the properties that make that safe — that the namespace has no
*other* way out, and that each service is genuinely inside it — and they
lean on `ip netns exec` for the strongest, most direct read of the
namespace's own routing table.

**Fail-closed routing** — arbitrary internet traffic must be routed out
the tunnel interface, never the veth back to the host. Ask the kernel
which device it would use to reach a public address, from inside the
namespace:

```bash
ip netns exec <vpn> ip route get 1.1.1.1
# expect: ... dev <vpn>      (the tunnel)
# a LEAK would read: ... dev veth-ns   (the host-facing veth)
```

If the tunnel is down this route doesn't resolve at all — there is no
fallback, which is the whole point.

**Namespace identity by inode** — `ip netns identify <pid>` is the
friendly form, but the ground truth is that two processes share a network
namespace *iff* the inode of `/proc/<pid>/ns/net` equals the inode of the
named namespace `/var/run/netns/<vpn>`. Compare them directly, so the
proof doesn't depend on any tool interpreting it for you:

```bash
pid=$(systemctl show -p MainPID --value <service>)
readlink /proc/$pid/ns/net          # -> net:[4026533075]
stat -Lc 'net:[%i]' /var/run/netns/<vpn>   # -> net:[4026533075], must match
```

**Packaged version**: [`scripts/runbook/phase4-vpn-verify-netns.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-vpn-verify-netns.sh),
deployed at `/usr/local/sbin/vpn-verify-netns.sh`, runs both of
the above for the whole namespace in one pass — the fail-closed route
assertion, a live namespace-egress-vs-host-egress contrast (namespace on
the VPN, host not, and different IPs), and the inode-equality check for
every pinned service — exiting non-zero on any failure so it can gate a
health check or CI step:

```bash
#!/bin/bash
# Structural / fail-closed proof for the isolated namespace itself --
# the complement to vpn-verify-service.sh (which proves each *service's*
# own process egresses via the tunnel). This script proves two things the
# per-service check takes on faith:
#
#   1. The namespace has NO path out except the tunnel. Proven at the
#      routing layer with `ip route get <public-ip>`: the chosen route to
#      an arbitrary internet address MUST leave via the tunnel interface,
#      never the veth back to the host. If the tunnel were down the route
#      would either not resolve or fall to the veth -- either way this
#      fails loudly. This is the kill switch stated as an assertion.
#
#   2. Every pinned service is REALLY in the namespace, by inode. Instead
#      of trusting `ip netns identify` (the friendly form of the same
#      lookup), this compares the raw values directly: the inode of
#      /proc/<pid>/ns/net must equal the inode of /var/run/netns/<vpn>.
#      Two processes share a network namespace iff those inodes are equal
#      -- that is the ground truth `ip netns identify` is built on.
#
# It also confirms the namespace's public egress is on the VPN AND that
# the host's own egress is NOT -- a live side-by-side that catches a
# tunnel silently carrying host-identical traffic.
#
# Usage: vpn-verify-netns.sh [service ...]   (default: every pinned service)
set -uo pipefail

NS="<vpn>"          # namespace name
TUN_IF="<vpn>"      # tunnel interface inside the namespace
VETH=veth-ns        # host-facing veth inside the namespace (the NON-exit path)
TEST_DST=1.1.1.1    # any routable public address; used only to read the route
CHECK_URL="<provider-check-url>"
CONFIRM="<provider-name>"
# Same list as SERVICES= in vpn-heal.sh / the Before= line (Part 3).
SERVICES=("$@")
[[ ${#SERVICES[@]} -eq 0 ]] && SERVICES=(<service> <service>)

fail=0
note() { printf '%-6s %s\n' "$1" "$2"; }

# --- 0. Namespace exists at all -------------------------------------------
# 2>/dev/null: `ip netns list` prints a harmless "RTNETLINK ... Operation
# not supported" to stderr on some kernels; it doesn't affect the listing.
if ! ip netns list 2>/dev/null | grep -qw "$NS"; then
  note FAIL "namespace '$NS' does not exist"
  exit 1
fi

# --- 1. Fail-closed routing: arbitrary internet must exit via the tunnel ---
route=$(ip netns exec "$NS" ip -o route get "$TEST_DST" 2>/dev/null)
route_dev=$(awk '{for(i=1;i<NF;i++) if($i=="dev") print $(i+1)}' <<<"$route")
if [[ "$route_dev" == "$TUN_IF" ]]; then
  note OK "fail-closed: route to $TEST_DST exits via '$TUN_IF' ($route)"
elif [[ "$route_dev" == "$VETH" ]]; then
  note FAIL "LEAK: route to $TEST_DST exits via veth '$VETH' -- traffic can bypass the tunnel ($route)"
  fail=1
else
  note FAIL "no tunnel route to $TEST_DST (dev='${route_dev:-none}') -- tunnel likely down ($route)"
  fail=1
fi

# --- 2. Namespace public egress is on the VPN, host's is NOT --------------
ns_resp=$(ip netns exec "$NS" curl -s --max-time 8 "$CHECK_URL" 2>/dev/null)
host_resp=$(curl -s --max-time 8 "$CHECK_URL" 2>/dev/null)
ns_ip=$(grep -oE '([0-9]{1,3}\.){3}[0-9]{1,3}' <<<"$ns_resp" | head -1)
host_ip=$(grep -oE '([0-9]{1,3}\.){3}[0-9]{1,3}' <<<"$host_resp" | head -1)

if [[ -z "$ns_resp" ]]; then
  note FAIL "namespace egress: no response from $CHECK_URL (DNS or tunnel unreachable)"
  fail=1
elif [[ "$ns_resp" != *"$CONFIRM"* ]]; then
  note FAIL "namespace egress: NOT confirmed on the VPN -- $ns_resp"
  fail=1
else
  note OK "namespace egress on VPN (${ns_ip:-?}) -- $ns_resp"
fi

if [[ "$host_resp" == *"$CONFIRM"* ]]; then
  note WARN "host egress also reports on the VPN -- cannot prove the namespace is distinct from the host"
elif [[ -n "$ns_ip" && "$ns_ip" == "$host_ip" ]]; then
  note FAIL "namespace and host share egress IP $ns_ip -- traffic is NOT tunneled"
  fail=1
elif [[ -n "$ns_ip" && -n "$host_ip" ]]; then
  note OK "host egress is off-VPN and distinct (host=$host_ip vs ns=$ns_ip)"
fi

# --- 3. Ground-truth inode equality: each service's net ns == the netns ---
netns_ino=$(stat -Lc %i "/var/run/netns/$NS" 2>/dev/null)
if [[ -z "$netns_ino" ]]; then
  note FAIL "cannot stat /var/run/netns/$NS to read its inode"
  fail=1
else
  for svc in "${SERVICES[@]}"; do
    pid=$(systemctl show -p MainPID --value "$svc" 2>/dev/null)
    if [[ -z "$pid" || "$pid" == "0" ]]; then
      note FAIL "$svc: not running"
      fail=1
      continue
    fi
    # readlink gives "net:[NNNN]"; strip to the bare inode number.
    link=$(readlink "/proc/$pid/ns/net" 2>/dev/null)
    proc_ino=${link#net:[}; proc_ino=${proc_ino%]}
    if [[ "$proc_ino" == "$netns_ino" ]]; then
      note OK "$svc (pid $pid): net ns inode $proc_ino == netns '$NS' inode"
    else
      note FAIL "$svc (pid $pid): net ns inode ${proc_ino:-none} != netns '$NS' inode $netns_ino"
      fail=1
    fi
  done
fi

exit "$fail"
```

```bash
install -m 755 scripts/runbook/phase4-vpn-verify-netns.sh /usr/local/sbin/vpn-verify-netns.sh
```

Confirm reachability from outside is unaffected — repeat per port:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://<host-address>:<port>/
```

Confirm the kill switch actually holds under an unannounced failure —
this is the test that matters most, since a deliberate `systemctl stop`
proves less than an actual crash does:

> [!NOTE]
> This forcibly kills the live tunnel process, so expect a real (if
> brief) connectivity gap for every protected service. That's the point
> of the test — Part 6's auto-heal should recover it within one
> health-check interval with no manual action.

```bash
pkill -9 -f "wireguard <vpn>"   # or your kernel-WireGuard equivalent
# wait up to one health-check interval, then:
/usr/local/sbin/vpn-check.sh
journalctl -t vpn-heal -n 10
```

Expect: the check fails immediately after the kill, the timer detects and
heals it within one interval with no manual action, and the journal shows
an honest, complete account of the outage and recovery.

## File manifest

Everything created by this guide, for a final checklist. Anywhere
`<service>` appears, there's one such entry per protected service, not
just one:

| Path | Purpose |
| --- | --- |
| `/etc/wireguard/<vpn>.conf` | VPN provider config (private key — mode 600) |
| `/usr/local/sbin/netns-vpn-up.sh` | namespace + veth + NAT/DNAT (idempotent); source of truth [`scripts/runbook/phase4-netns-vpn-up.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-netns-vpn-up.sh) |
| `/usr/local/sbin/netns-vpn-down.sh` | namespace teardown; source of truth [`scripts/runbook/phase4-netns-vpn-down.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-netns-vpn-down.sh) |
| `/usr/local/sbin/vpn-peer-route.sh` | keeps traffic off the default route; source of truth [`scripts/runbook/phase4-vpn-peer-route.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-vpn-peer-route.sh) |
| `/usr/local/sbin/vpn-check.sh` | health check, exit 0 = genuinely live; source of truth [`scripts/runbook/phase4-vpn-check.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-vpn-check.sh) |
| `/usr/local/sbin/vpn-heal.sh` | auto-heal (tunnel + per-service DNS drift), transition-based logging; source of truth [`scripts/runbook/phase4-vpn-heal.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-vpn-heal.sh) |
| `/usr/local/sbin/vpn-verify-service.sh` | on-demand per-service verification (Verification section); source of truth [`scripts/runbook/phase4-vpn-verify-service.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-vpn-verify-service.sh) |
| `/usr/local/sbin/vpn-verify-netns.sh` | on-demand namespace-level fail-closed + inode-equality proof (Verification section); source of truth [`scripts/runbook/phase4-vpn-verify-netns.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase4-vpn-verify-netns.sh) |
| `/etc/systemd/system/netns-vpn.service` | brings up the namespace at boot |
| `/etc/systemd/system/wg-quick-vpn.service` | brings up the tunnel |
| `/etc/systemd/system/vpn-heal.service` + `.timer` | health check/heal |
| `/etc/systemd/system/<service>.service.d/vpn.conf` | pins one service in |
| `/etc/netns/<vpn>/resolv.conf` | namespace-local DNS |
