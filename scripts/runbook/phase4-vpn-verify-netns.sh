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
