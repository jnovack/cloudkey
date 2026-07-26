#!/bin/bash
# Creates the isolated network namespace and the veth pair that lets the
# root namespace (and, via routing, anything else on the host) reach
# into it -- without giving the namespace itself any route back out
# except the VPN tunnel that gets brought up separately.
#
# Also source-NATs the namespace's own control traffic (routed via the
# veth pair, not via the tunnel itself -- see wg-quick-vpn.service) so it
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
# address, so replies are sourced from it too -- but the namespace's only
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
# it has one -- keeps DNS queries inside the tunnel too, rather than
# leaking query patterns to whatever the host normally uses.
mkdir -p -m 755 "/etc/netns/$NS"
echo 'nameserver <provider-dns-ip>' > "/etc/netns/$NS/resolv.conf"

sysctl -qw net.ipv4.ip_forward=1

# -w: wait for the xtables lock instead of failing immediately. At boot,
# anything else that also configures iptables at roughly the same time
# (Tailscale, Docker, ufw, fail2ban...) can hold this lock momentarily --
# without -w the loser of that race fails outright ("Another app is
# currently holding the xtables lock") instead of just waiting the
# split-second for the winner to finish.
grep -q "^${NS_ADDR}$" \
  <(iptables -w 5 -t nat -S POSTROUTING 2>/dev/null \
    | grep -oP '(?<=-s )[0-9.]+') \
  2>/dev/null || \
  iptables -w 5 -t nat -A POSTROUTING -s "$NS_ADDR" -o "$UPLINK" -j MASQUERADE

# Keep every protected service reachable at the host's normal
# address/port -- nothing external needs to know it moved into an
# isolated namespace. Repeat per port: this example shows two, list as
# many as you actually have (e.g. `for PORT in 8080 8443 9000 9001; do`
# for four).
for PORT in <port> <port>; do
  iptables -w 5 -t nat -C PREROUTING -p tcp --dport "$PORT" \
    -j DNAT --to-destination "$NS_ADDR:$PORT" 2>/dev/null || \
    iptables -w 5 -t nat -A PREROUTING -p tcp --dport "$PORT" \
      -j DNAT --to-destination "$NS_ADDR:$PORT"
done
