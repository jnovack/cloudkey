#!/bin/bash
# Tears down the isolated namespace and its veth pair.
set -uo pipefail

NS="<vpn>"
ip link delete veth-host 2>/dev/null
ip netns delete "$NS" 2>/dev/null
exit 0
