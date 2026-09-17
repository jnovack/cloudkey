#!/bin/bash
# Phase 1: keep enough journal on disk to diagnose a box that died. The
# stock UniFi image already sets Storage=persistent, but caps it at
# SystemMaxUse=80M -- small enough that a single daemon stuck in a
# logging loop (seen: tailscaled at ~150 lines/min) rotates the whole
# previous boot out within a few hours, so `journalctl -b -1` is empty
# by the time anyone looks.
#
# Written as a drop-in rather than an edit to /etc/systemd/journald.conf:
# that file is a dpkg conffile the UniFi image already modifies, so an
# edit there can be lost to a systemd upgrade's conffile handling or a
# vendor rewrite. A drop-in overrides the main file whatever it says, and
# the `zz-` prefix sorts it after any vendor drop-in so it wins those too.
# Storage=persistent is restated here so this file alone guarantees it,
# rather than relying on the vendor file keeping it.
#
# SystemMaxUse=500M: on the Plus, /var/log shares the ~6G overlay
# partition with every other rootfs change, so the cap stays well short
# of that; at normal volume it still holds well over a week. Noisy
# daemons are rate-limited per unit (e.g. Phase 5's tailscaled drop-in)
# so one of them can't eat the budget on its own.
#
# Safe to re-run: the drop-in is overwritten in full each time.
set -euo pipefail

DROPIN_DIR=/etc/systemd/journald.conf.d
DROPIN="$DROPIN_DIR/zz-persistent.conf"

install -d -m 755 "$DROPIN_DIR"
cat > "$DROPIN" <<'EOF'
# Managed by phase1-persistent-journal.sh (Phase 1, Before you start).
[Journal]
Storage=persistent
SystemMaxUse=500M
EOF
chmod 644 "$DROPIN"

# journald only reads its config at startup. The restart creates
# /var/log/journal/<machine-id> if it's missing; the flush then moves any
# of this boot's messages still in /run onto disk now.
systemctl restart systemd-journald
journalctl --flush

echo "== verify =="
# Last value across main file + drop-ins is the one journald uses.
effective() {
  systemd-analyze cat-config systemd/journald.conf | sed -n "s/^$1=//p" | tail -n1
}
storage="$(effective Storage)"
maxuse="$(effective SystemMaxUse)"
if [[ "$storage" != persistent || "$maxuse" != 500M ]]; then
  echo "FAIL: effective Storage=${storage:-<unset>} SystemMaxUse=${maxuse:-<unset>} (want persistent, 500M)" >&2
  exit 1
fi
journal_dir="/var/log/journal/$(cat /etc/machine-id)"
if [[ ! -f "$journal_dir/system.journal" ]]; then
  echo "FAIL: no $journal_dir/system.journal after flush" >&2
  exit 1
fi
journalctl --disk-usage
echo "persistent journal active (Storage=$storage, SystemMaxUse=$maxuse)"
