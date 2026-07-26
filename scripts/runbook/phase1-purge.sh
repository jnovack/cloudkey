#!/bin/bash
# Phase 1 purge: strips the UniFi application layer from a UCK-G2-Plus,
# in verified-safe batches with a real liveness check between each.
#
# Run this ON THE BOX ITSELF over an interactive SSH session (PuTTY,
# Windows Terminal, whatever) -- deliberately not orchestrated from the
# operator's own machine, since there's no guarantee that machine has
# bash/ssh-loop tooling available.
#
# Mirrors Phase-1-De-Ubiquitizing Steps 0, 1, 2, and 4 exactly -- read
# that doc for the full why behind each step, especially the danger-zone
# section this script's package list is built from. Step 3 (the optional
# LCD replacement) is unrelated to this purge -- it's a separate install,
# not a danger-zone package removal -- so it's scripted on its own as
# phase1-cloudkey-install.sh instead of bolted on here. Step 5 (the
# reboot checkpoint) is deliberately NOT run automatically either -- a
# script can't observe its own box failing to come back from a bad
# reboot, since it dies with it.
# This script stops cleanly right before that point; reboot by hand, then
# run phase1-verify.sh once reconnected.
#
# Usage: phase1-purge.sh [-y]
#   -y   skip the confirmation prompt before the real purge begins.
#        Step 0's simulate + exact-match check always runs regardless,
#        and always aborts loudly on any mismatch -- this flag only
#        skips the "are you sure" prompt once that's already passed.

set -uo pipefail   # deliberately NOT -e: a failed liveness check needs
                   # to be caught and reported, not just crash silently

ASSUME_YES="${1:-}"

# Never touch these two, no matter what -- see Phase-1-De-Ubiquitizing's
# danger-zone section for the exact dependency chain (both cascade into the
# board's base-files package -- cloudkey-plus-apq8053-base-files on the Gen2
# Plus, cloudkey-g2-apq8053-base-files on the plain Gen2 -- and this board's
# initramfs package, which is what actually bricked the device the first
# time this was attempted). This guard is on ck-ui/ubnt-tools by package
# name, so it holds on either model; the Step 0 simulate gate below also
# aborts on ANY unexpected cascade, catching either base-files variant.
FORBIDDEN="ck-ui ubnt-tools"

PACKAGES="unifi-assets-uckp unifi-assets-uckg2 unifi-email-templates-all python3-unifi-console-protos mongodb-server mongodb-clients mongodb-server-core unifi unifi-core unifi-directory unifi-identity-update uid-agent ucs-agent uos-agent uos-discovery-client uos ulp-go ustd ubnt-systemhub ubnt-unifi-setup ucore-setup-listener"

UNITS="ck-splash-reboot.service ck-splash-shutdown.service ck-ui.service infctld-emergency.service infctld.service ubnt-systemhub.service ubnt-unifi-setup.service ucore-setup-listener.service ucs-agent.service uhwd.service uid-agent.service ulp-go.service unifi-core.service unifi-directory.service unifi-identity-update.service unifi-sdcard@.service unifi.service uos-agent.service uos-discovery-client.service usd.service usdbd.service ubnt-dpkg-restore.service"

# Batches mirror the doc's manual batching -- small enough that a
# failure isolates to a handful of packages, not all 20 at once.
# Both unifi-assets-* names are listed: the asset package is named for the
# Cloud Key generation, and only one of them exists on any given box. Both
# must appear here as well as in PACKAGES -- a package that passes the
# Step 0 simulate gate but is in no batch is approved for removal and then
# never actually purged.
BATCH_1="unifi-assets-uckp unifi-assets-uckg2 unifi-email-templates-all python3-unifi-console-protos"
BATCH_2="mongodb-server mongodb-clients mongodb-server-core"
BATCH_3="unifi unifi-core"
BATCH_4="unifi-directory unifi-identity-update uid-agent ucs-agent uos-agent uos-discovery-client uos ulp-go"
BATCH_5="ustd ubnt-systemhub ubnt-unifi-setup ucore-setup-listener"

remaining_packages() {
  local p state
  for p in $PACKAGES; do
    state=$(dpkg-query -W -f='${db:Status-Abbrev}' "$p" 2>/dev/null || true)
    case "$state" in
      i*|rc*) printf '%s\n' "$p" ;;
    esac
  done
}

check_liveness() {
  # Proxies "would a NEW ssh connection succeed" from inside the box
  # itself -- this is the exact failure signature from the incident that
  # originally bricked this device (ping still worked, but new SSH
  # connections were refused). An already-open session (like the one
  # running this script) can survive that condition, so this check is
  # what actually catches it instead of just hoping the script keeps running.
  if ! systemctl is-active --quiet ssh; then
    echo "ABORT: sshd is not active -- stop here, do not continue." >&2
    exit 1
  fi
  if ! ss -tlnp 2>/dev/null | grep -q ':22\b'; then
    echo "ABORT: nothing listening on :22 -- stop here, do not continue." >&2
    exit 1
  fi
}

run_batch() {
  local batch="$1"
  local remaining
  remaining=$(for p in $batch; do
    if echo "$REMAINING_PACKAGES" | grep -qx "$p"; then
      printf '%s\n' "$p"
    fi
  done | tr '\n' ' ')

  if [ -z "$remaining" ]; then
    echo "--- skipping batch; none of these packages remain: $batch ---"
    return
  fi

  echo "--- purging: $remaining ---"
  # shellcheck disable=SC2086
  if ! apt-get purge -y $remaining; then
    echo "ABORT: apt-get purge failed for batch [$remaining] -- stop here." >&2
    exit 1
  fi
  check_liveness
  echo "--- batch OK, sshd still live ---"
}

echo "=== Step 0: simulate, verify the EXACT package list before touching anything ==="
REMAINING_PACKAGES=$(remaining_packages | sort -u)
if [ -z "$REMAINING_PACKAGES" ]; then
  echo "No target packages are currently installed or left in config-only state."
  echo "Continuing with unit-disable, data cleanup, and residual checks."
else
  echo "Remaining target packages on this box:"
  echo "$REMAINING_PACKAGES"
fi

# shellcheck disable=SC2086
RAW_SIMULATE=$(apt-get purge --simulate $REMAINING_PACKAGES)
echo "$RAW_SIMULATE"
SIMULATED=$(echo "$RAW_SIMULATE" | grep -E '^(Remv|Purg) ' | awk '{print $2}' | sort -u)
EXPECTED="$REMAINING_PACKAGES"

for f in $FORBIDDEN; do
  if echo "$SIMULATED" | grep -qx "$f"; then
    echo "ABORT: simulate wants to remove forbidden package [$f] -- stop, do not proceed." >&2
    exit 1
  fi
done

if [ "$SIMULATED" != "$EXPECTED" ]; then
  echo "ABORT: simulated removal list does not exactly match the remaining target packages." >&2
  echo "--- expected ---" >&2
  echo "$EXPECTED" >&2
  echo "--- simulated (actual) ---" >&2
  echo "$SIMULATED" >&2
  echo "Run 'apt-cache depends <package>' on anything unexpected before proceeding by hand -- do not force past this." >&2
  exit 1
fi
echo "Simulate matches exactly the remaining target packages. Safe to proceed."
echo

if [ "$ASSUME_YES" != "-y" ]; then
  read -rp "Proceed with the real purge + unit-disable + cleanup? [y/N] " reply
  [[ "$reply" =~ ^[Yy]$ ]] || { echo "aborted"; exit 1; }
fi

echo "=== Step 1: disabling UniFi systemd units ==="
for u in $UNITS; do
  systemctl disable --now "$u" 2>&1 | grep -v '^Removed\|^Synchronizing' || true
done
echo

echo "=== Step 2: purging in verified batches, liveness check after each ==="
run_batch "$BATCH_1"
run_batch "$BATCH_2"
run_batch "$BATCH_3"
run_batch "$BATCH_4"
run_batch "$BATCH_5"

echo "--- autoremove ---"
apt-get --purge autoremove -y
check_liveness
echo

echo "=== Step 4: cleaning up leftover application data ==="
echo "Contents before removal (check this looks empty/unwanted before continuing):"
ls -la /data/unifi /data/uos /data/autobackup 2>/dev/null
rm -rf /data/unifi /data/uos /data/autobackup

if command -v psql >/dev/null 2>&1; then
  EXISTING_DBS=$(sudo -u postgres psql -lqt 2>/dev/null | cut -d'|' -f1 | tr -d ' ')
  for db in unifi-core unifi-identity-update; do
    if echo "$EXISTING_DBS" | grep -qx "$db"; then
      sudo -u postgres dropdb "$db" 2>/dev/null || true
      sudo -u postgres dropuser "$db" 2>/dev/null || true
      echo "dropped $db"
    fi
  done
fi
echo

echo "=== Steps 0, 1, 2, 4 complete. Residual-package check: ==="
dpkg -l | grep -iE 'unifi|ubnt|uos-|ucs-|uid-agent|ulp-go' || echo "(only the deliberately-kept packages should remain -- see Step 2's verify note in the doc)"
echo
echo "=== NEXT: reboot by hand, then run phase1-verify.sh once reconnected ==="
echo "This script stops here on purpose -- Step 5's reboot checkpoint can't"
echo "be observed from inside a box that's mid-reboot. Run:"
echo "    reboot"
echo "then reconnect and run: phase1-verify.sh"
