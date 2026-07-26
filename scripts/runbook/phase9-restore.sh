#!/bin/bash
# Phase 9 -- restore a backup produced by phase9-backup.sh onto a box that
# has already had the base OS + phases 1-8 re-run (packages present, apps
# reinstalled to /opt, /volume mounted). It lays the saved custom configs,
# scripts, systemd units, keys and app databases back on top.
#
# This is DESTRUCTIVE to the current custom config, so it confirms first
# (or pass --yes). It never deletes anything outside the backed-up paths
# (no rsync --delete). After restoring files it recommends a reboot, which
# is the reliable way to bring the namespace -> tunnel -> pinned-app boot
# ordering back up in the right sequence; pass --reboot to do it here.
#
# Usage: phase9-restore.sh [--dry-run] [--yes] [--reboot] [/sdcard/cloudkey-backup/<stamp>]
#   No path given -> a promoted 'good' snapshot if one exists, else 'latest'.
#   --dry-run: show exactly what would change and touch nothing (no service
#              stop/start, no reboot) -- safe to run on a live box.
#   BACKUP_ROOT=/sdcard/cloudkey-backup overrides the default snapshot root.
set -uo pipefail

DEST_ROOT="${BACKUP_ROOT:-/sdcard/cloudkey-backup}"
APPS=(nzbget sonarr radarr prowlarr)
# Generic, deployment-agnostic services safe to stop before overwriting
# their files. The VPN/namespace/autossh units are intentionally NOT
# hardcoded here -- a reboot brings them (and the netns-pinned apps) back
# via their own enabled boot units, in the correct order.
STOP=("${APPS[@]}" tailscaled)

ASSUME_YES=0
DO_REBOOT=0
DRY_RUN=0
SRC=""

for a in "$@"; do
  case "$a" in
    --dry-run) DRY_RUN=1 ;;
    --yes|-y)  ASSUME_YES=1 ;;
    --reboot)  DO_REBOOT=1 ;;
    /*)        SRC="$a" ;;
    *) echo "usage: phase9-restore.sh [--dry-run] [--yes] [--reboot] [/sdcard/cloudkey-backup/<stamp>]" >&2; exit 2 ;;
  esac
done

log(){ printf '%s  %s\n' "$(date '+%F %T')" "$*"; }

# Default target: an explicitly promoted 'good' snapshot, else 'latest'.
if [[ -z "$SRC" ]]; then
  if [[ -e "$DEST_ROOT/good" ]]; then SRC="$DEST_ROOT/good"; else SRC="$DEST_ROOT/latest"; fi
fi
SRC="${SRC%/}"
[[ -d "$SRC" ]] || { echo "FATAL: backup dir '$SRC' not found" >&2; exit 1; }
# Sanity: a real phase9 backup has these top-level trees.
[[ -d "$SRC/usr/local" && -d "$SRC/etc/systemd" ]] \
  || { echo "FATAL: '$SRC' does not look like a phase9 backup (missing usr/local or etc/systemd)" >&2; exit 1; }

echo "About to RESTORE from: $(readlink -f "$SRC")"
[[ -f "$SRC/MANIFEST.txt" ]] && sed 's/^/    /' "$SRC/MANIFEST.txt"

if [[ "$DRY_RUN" == 1 ]]; then
  log "DRY RUN -- showing what would change; touching nothing"
  # -ni: itemize the changes rsync WOULD make, without doing them.
  rsync -aAXni --exclude '/MANIFEST.txt' "$SRC"/ / 2>&1
  rc=$?
  log "dry run complete (rc=$rc). Lines above are pending changes ('.' = in sync)."
  exit "$rc"
fi

echo "This overwrites the current custom config/scripts/units/app databases."
if [[ "$ASSUME_YES" != 1 ]]; then
  read -r -p "Type RESTORE to proceed: " ans
  [[ "$ans" == "RESTORE" ]] || { echo "aborted"; exit 1; }
fi

# Stop the services that own files we're about to overwrite so nothing
# holds a stale copy open or rewrites its config mid-restore.
for s in "${STOP[@]}"; do systemctl stop "$s" 2>/dev/null && log "stopped $s"; done

log "restoring files: $SRC/ -> /"
# Reverse of backup. -aAX restores perms/ownership/ACLs/xattrs exactly as
# captured. NO --delete: only the backed-up paths are added/overwritten;
# nothing else on the box is removed. MANIFEST.txt is metadata, not a file
# to lay down on /.
rsync -aAX --exclude '/MANIFEST.txt' "$SRC"/ / 2>&1
rc=$?

systemctl daemon-reload
log "daemon-reload done"

# Bring back the services that don't depend on the VPN namespace. The
# netns-pinned apps (nzbget/sonarr/radarr/prowlarr) need the namespace up
# first -- leave those to the reboot below rather than racing them here.
for s in tailscaled; do systemctl start "$s" 2>/dev/null && log "started $s"; done

case "$rc" in
  0) log "file restore OK" ;;
  *) log "NOTE: rsync returned rc=$rc -- review output above before trusting the restore" ;;
esac

echo
echo "A reboot is the reliable way to finish: it starts the namespace, the"
echo "VPN tunnel, the autossh tunnels and the netns-pinned apps in the right"
echo "order via their enabled boot units."
if [[ "$DO_REBOOT" == 1 ]]; then
  log "rebooting now (--reboot)"
  reboot
else
  echo "Reboot when ready:  reboot"
  echo
  echo "After it comes back, verify:"
  echo "  phase1-verify.sh          # base OS sanity"
  echo "  vpn-verify-netns.sh       # VPN kill switch + no-leak proof"
  echo "  tailscale status          # tunnel reconnected"
  echo "  curl -s -o /dev/null -w '%{http_code}\\n' http://localhost/   # dashboard"
fi
exit "$rc"
