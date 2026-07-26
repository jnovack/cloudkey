#!/bin/bash
# Run this after rebooting (following phase1-purge.sh), once reconnected.
# Scripts Step 5's verification checklist from Phase-1-De-Ubiquitizing
# so nothing gets skipped by accident -- but the reboot itself, and
# reconnecting afterward, is still on you: this can't run itself.
set -uo pipefail

echo "=== uptime (confirms it actually rebooted, not just stayed up) ==="
uptime
echo

echo "=== DHCP renewed? ==="
ip -4 addr show eth0
echo

echo "=== failed units ==="
echo "(nginx, or a UI/telemetry daemon crash-looping once its target is"
echo "gone, are expected here and harmless -- see Step 5. Anything else"
echo "is worth investigating before calling this done.)"
systemctl --failed
echo

echo "=== residual UniFi packages (only the deliberately-kept ones should remain) ==="
dpkg -l | grep -iE 'unifi|ubnt|uos-|ucs-|uid-agent|ulp-go' || echo "(none found)"
echo

echo "=== apt upgradable ==="
apt list --upgradable 2>/dev/null
echo

echo "If nginx/a telemetry unit are the only failures and expected, clear them:"
echo "    systemctl disable --now nginx <telemetry-daemon>"
echo "    systemctl reset-failed"
