#!/bin/bash
# Keeps the WireGuard peer's own endpoint reachable via the veth pair
# (through the root namespace's real uplink) instead of via the tunnel's
# own default route -- otherwise the tunnel's handshake/keepalive packets
# would try to route through themselves. Reads the peer IP live from
# `wg show`, never from the config file itself (which holds the private
# key and is deliberately never read by tooling here).
#
# A /32 host route always wins over the tunnel's 0.0.0.0/0 default route
# on prefix length alone, so this needs no fwmark trickery -- which
# matters if you're on a userspace WireGuard implementation (used when
# the kernel has no native support): unlike kernel WireGuard, userspace
# implementations don't reliably apply `wg set <if> fwmark ...` to the
# actual outbound socket, so fwmark-based routing splits can silently
# fail (packets never leave the namespace, with no error). Don't reach
# for fwmark as a "more proper" alternative to this -- it looks cleaner
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
