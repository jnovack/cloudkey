#!/bin/bash
# Formats and mounts a bulk-storage drive at /volume: whole-disk ext4, no
# partition table (the "superfloppy" pattern this box has used for every
# drive so far -- see phase1-de-ubiquitizing.md Step 7 for why).
#
# Wipes the target device unconditionally, with no attempt to detect or
# preserve an existing filesystem/partition table -- a drive being
# (re)installed here is never assumed to carry data worth keeping, per
# project decision. Don't run this against a drive you haven't confirmed
# is the one you mean to wipe.
#
# Persists the mount via a systemd .mount unit, NOT /etc/fstab. Confirmed
# by direct testing (2026-07-13): this board still runs a live UniFi
# bootup-hook framework (owned by the load-bearing base-files package --
# cloudkey-plus-apq8053-base-files / cloudkey-g2-apq8053-base-files by model
# -- and ubnt-tools, kept installed on purpose, see the danger-zone section)
# that resets /etc/fstab to a minimal template on every boot,
# silently dropping any manually-added line. Plain systemd unit files
# under /etc/systemd/system/ are untouched by this and have survived
# multiple real reboots, so that's what this script writes instead.
#
# Usage: phase1-format-mount-volume.sh <device> [-y]
#   <device>  e.g. /dev/sda -- required, no default, so a typo in the
#             calling context can't silently target the wrong disk.
#   -y        skip the confirmation prompt (for non-interactive runs).
#
# Safe to re-run against the same already-formatted device: writing the
# same unit file content twice and re-enabling it is a no-op.

set -euo pipefail

DEVICE="${1:?Usage: $0 <device> [-y]}"
ASSUME_YES="${2:-}"

if [[ ! -b "$DEVICE" ]]; then
  echo "error: $DEVICE is not a block device" >&2
  exit 1
fi

lsblk "$DEVICE"

if [[ "$ASSUME_YES" != "-y" ]]; then
  read -rp "This will WIPE $DEVICE and reformat it as /volume. Continue? [y/N] " reply
  [[ "$reply" =~ ^[Yy]$ ]] || { echo "aborted"; exit 1; }
fi

wipefs -a "$DEVICE"
mkfs.ext4 -F -L volume "$DEVICE"

UUID="$(blkid -s UUID -o value "$DEVICE")"

cat > /etc/systemd/system/volume.mount <<EOF
[Unit]
Description=Bulk storage drive (whole-disk ext4, no partition table, see phase1-de-ubiquitizing.md Step 7)

[Mount]
What=/dev/disk/by-uuid/$UUID
Where=/volume
Type=ext4
Options=defaults,noatime

[Install]
WantedBy=local-fs.target
EOF

systemctl daemon-reload
systemctl enable --now volume.mount

echo "Done. /volume is mounted:"
lsblk "$DEVICE"
mount | grep /volume
