#!/bin/bash
# Phase 9 -- back up every custom file this build adds, across all phases,
# to the SD card. /sdcard is a SEPARATE physical device (/dev/mmcblk1p1)
# from both the eMMC root and the /volume data drive, so a backup here
# survives a wipe/reflash of either.
#
# MODEL: this is a CONFIG/STATE backup, not a disk image. Restore assumes
# the base OS + phases 1-8 have already been re-run on a fresh box
# (packages installed, apps reinstalled to /opt, drive mounted); it then
# lays these custom configs, scripts, systemd units, keys and app
# databases back on top. Deliberately NOT captured: media under /volume
# (huge, not config) and the reinstalled app binaries under
# /opt/{Sonarr,Radarr,Prowlarr} (phase 2 puts them back).
#
# The file list is DIRECTORY-based wherever possible so the real,
# deployment-specific filenames (the VPN config, the per-relay autossh
# keys, etc.) are captured without this public script having to name them.
# Keep it in sync with the per-phase "File manifest" sections.
#
# Optional:
#   BACKUP_ROOT=/sdcard/cloudkey-backup   # default
#   --no-quiesce      don't stop the media apps first (slightly-stale DBs)
#   --allow-rootfs    permit writing to the root filesystem; only for
#                     throwaway validation runs (BACKUP_ROOT=/tmp/...)
set -uo pipefail

DEST_ROOT="${BACKUP_ROOT:-/sdcard/cloudkey-backup}"
STAMP="$(date +%Y%m%d-%H%M%S)"
DEST="$DEST_ROOT/$STAMP"
KEEP=7                                   # how many snapshots to retain
QUIESCE=1                                # stop live-DB apps for a consistent snapshot
ALLOW_ROOTFS=0                           # refuse to back up onto the eMMC root by default
APPS=(nzbget sonarr radarr prowlarr)     # these hold live SQLite databases

PATHS=(
  # --- scripts we added (everything in these two dirs is custom) ---------
  /usr/local/bin
  /usr/local/sbin
  # --- systemd units + drop-ins + enablement (whole tree; it's small) ----
  # plus the one custom unit that lives in /lib (LCD-panel replacement).
  /etc/systemd/system
  /lib/systemd/system/cloudkey.service
  # --- per-phase /etc config --------------------------------------------
  /etc/wireguard                                   # Phase 4 tunnel (private key)
  /etc/netns                                       # Phase 4 namespace DNS
  /etc/autossh                                     # Phase 3 tunnel keys + known_hosts
  /etc/profile.d/zz-cloudkey-dashboard.sh          # interactive-login dashboard loader
  /etc/cloudkey.env                                # Phase 1 Step 3 LCD-app config
  /etc/sudoers.d/cloudkey                          # Phase 1 passwordless-sudo drop-in
  /etc/systemd/journald.conf.d                     # Phase 1 persistent-journal drop-in
  /etc/ssh/sshd_config.d/10-security.conf          # Phase 1 SSH policy (safe/emergency)
  /etc/apt/keyrings/tailscale-archive-keyring.gpg  # Phase 5 client apt key
  /etc/apt/sources.list.d/tailscale.list           # Phase 5 client apt repo
  # --- app config + state (NOT the /opt binaries) -----------------------
  /var/lib/nzbget
  /var/lib/sonarr
  /var/lib/radarr
  /var/lib/prowlarr
  /var/lib/tailscale                               # node identity -> restore avoids re-auth
  # --- docs -------------------------------------------------------------
  /opt/docs
)

for a in "$@"; do
  case "$a" in
    --no-quiesce)   QUIESCE=0 ;;
    --allow-rootfs) ALLOW_ROOTFS=1 ;;
    *) echo "usage: phase9-backup.sh [--no-quiesce] [--allow-rootfs]" >&2; exit 2 ;;
  esac
done

log(){ printf '%s  %s\n' "$(date '+%F %T')" "$*"; }

BACKUP_MOUNT="$(dirname "$DEST_ROOT")"
[[ -d "$BACKUP_MOUNT" ]] \
  || { echo "FATAL: backup parent '$BACKUP_MOUNT' not present" >&2; exit 1; }

# Resolve which filesystem the destination actually lands on, rather than
# asking whether the parent dir happens to be a mountpoint. The dangerous
# case is the quiet one: /sdcard exists as an empty mountpoint directory but
# the card is not mounted, so the whole backup silently fills the eMMC root
# instead. --target answers "what fs would this path be written to", which
# catches that, an unmounted /mnt/..., and BACKUP_ROOT=/sdcard (whose parent
# is '/', and so would pass any parent-directory test) with one check.
BACKUP_FS="$(findmnt -no TARGET --target "$BACKUP_MOUNT" 2>/dev/null || echo /)"
if [[ "$BACKUP_FS" == "/" && "$ALLOW_ROOTFS" != 1 ]]; then
  echo "FATAL: '$BACKUP_MOUNT' is on the root filesystem, not separate backup media." >&2
  echo "       Mount the SD card/backup disk first, or set BACKUP_ROOT to a path on it." >&2
  echo "       To write to the rootfs anyway (throwaway validation runs), pass --allow-rootfs." >&2
  exit 1
fi
mkdir -p "$DEST"

# Quiesce apps with live databases so their SQLite files are captured in a
# consistent state; restart them however this script exits.
stopped=()
restart_apps(){ local s; for s in "${stopped[@]:-}"; do [[ -n "$s" ]] && systemctl start "$s" 2>/dev/null && log "restarted $s"; done; }
trap restart_apps EXIT
if [[ "$QUIESCE" == 1 ]]; then
  for s in "${APPS[@]}"; do
    if systemctl is-active --quiet "$s"; then
      systemctl stop "$s" && stopped+=("$s") && log "stopped $s for a consistent snapshot"
    fi
  done
fi

log "backing up ${#PATHS[@]} source paths -> $DEST"
EXISTING_PATHS=()
MISSING_PATHS=()
for p in "${PATHS[@]}"; do
  if [[ -e "$p" ]]; then
    EXISTING_PATHS+=("$p")
  else
    MISSING_PATHS+=("$p")
    log "skipping missing path: $p"
  fi
done

if [[ "${#EXISTING_PATHS[@]}" -eq 0 ]]; then
  echo "FATAL: none of the configured backup paths exist" >&2
  exit 1
fi

# -aAX: perms + ACLs + xattrs + ownership (ext4 SD card preserves them all).
# -R : recreate each full source path under $DEST, so restore is a clean
#      reverse (rsync $DEST/ back to /). NO --delete anywhere.
rsync -aAXR --info=stats1 "${EXISTING_PATHS[@]}" "$DEST/" 2>&1
rc=$?

{
  echo "backup_time: $STAMP"
  echo "hostname:    $(hostname)"
  echo "rsync_rc:    $rc"
  echo "quiesced:    ${stopped[*]:-none}"
  echo "paths:"
  printf '  %s\n' "${EXISTING_PATHS[@]}"
  echo "missing_paths:"
  printf '  %s\n' "${MISSING_PATHS[@]:-none}"
} > "$DEST/MANIFEST.txt"

ln -sfn "$STAMP" "$DEST_ROOT/latest"
log "updated $DEST_ROOT/latest -> $STAMP"

# Retain only the last $KEEP timestamped snapshots (never touches 'latest'
# or a promoted 'good' symlink -- those don't start with '20').
ls -1dt "$DEST_ROOT"/20*/ 2>/dev/null | tail -n "+$((KEEP + 1))" | while read -r old; do
  rm -rf "$old" && log "pruned old snapshot $old"
done

case "$rc" in
  0)     log "backup OK -> $DEST" ;;
  23|24) log "backup completed with partial-transfer warnings (rc=$rc) -- a source path may be missing; review above" ;;
  *)     log "backup FAILED (rc=$rc)" ;;
esac
exit "$rc"
