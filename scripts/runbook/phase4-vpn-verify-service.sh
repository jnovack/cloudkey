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
