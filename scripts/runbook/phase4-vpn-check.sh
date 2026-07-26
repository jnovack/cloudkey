#!/bin/bash
# Exits 0 if the VPN tunnel is genuinely passing traffic right now,
# non-zero otherwise. Deliberately does a real connectivity check rather
# than trusting `wg show` handshake recency alone -- without
# PersistentKeepalive, a perfectly healthy but briefly idle tunnel can
# show a stale handshake with nothing actually wrong, since WireGuard
# only re-handshakes when there's traffic to send.
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
