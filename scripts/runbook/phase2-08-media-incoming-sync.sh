#!/bin/bash
# Additive, one-way sync of the local Sonarr/Radarr libraries into a
# remote media server's library, over NFS. Generic and safe to deploy
# as-is: every device-specific value (NFS host, export, library
# mappings) comes from $ENV_FILE, not from anything hardcoded here.
#
# Usage: media-incoming-sync.sh [--verify | --verify-all]
#   --verify      after transferring, re-compare by CHECKSUM only the
#                 files THIS PASS actually sent -- typically zero to a
#                 handful. Fast, and the check that actually matters day
#                 to day: confirms whatever just arrived isn't corrupt,
#                 without re-reading years of already-synced library on
#                 every single trigger.
#   --verify-all  the slow version: checksum EVERY file in every mapped
#                 library, transferred this pass or not. Real bytes read
#                 on both ends for the whole library -- expect minutes,
#                 not seconds, on anything but a small library. Use this
#                 rarely (e.g. after suspected corruption or a storage
#                 migration on the far end), not as routine practice.
#
# SAFETY -- invariants, all deliberate:
#   * NEVER deletes on the destination. There is no --delete flag, by
#     design. The destination is presumed to be a real, possibly
#     hand-curated library, not a scratch area; a --delete here would
#     propagate any local removal into it. Do not add it. Removing
#     something from the sync is a manual act on the source.
#   * NEVER rsyncs into a non-NFS directory. If the mount fails, the bare
#     mountpoint is a local dir on this box's own root filesystem; rsyncing
#     into it would silently fill the root fs and write to the wrong
#     place. The script hard-aborts unless it confirms an actual NFS mount
#     at $MOUNT first (findmnt fstype nfs*).
#   * NEVER leaves a partial file under its final name. --partial-dir (not
#     bare --partial) parks incomplete transfers in a dot-directory, so an
#     interrupted run cannot leave a truncated file that the media server
#     will scan and add as a playable-but-broken item. Resume still works:
#     the next run picks the partial back up.
#
# TRIGGERING -- Sonarr/Radarr's "on import" Custom Script hooks run as the
# radarr/sonarr users, which have no business mounting NFS or touching
# /run as root. Rather than sudo or a privileged wrapper on their side,
# each import just writes an empty marker file into $TRIGGER_DIR (a
# tmpfs directory, group-writable by "media" -- see
# /etc/tmpfiles.d/media-incoming-sync.conf). A systemd .path unit
# (media-incoming-sync.path) watches that directory and starts this
# script as root whenever it's non-empty.
#
# CONCURRENCY -- overlapping triggers are the normal case (imports arrive
# in bursts, e.g. a season pack), not an edge case:
#   * flock($LOCKFILE) still guards against two copies of this script
#     genuinely running at once. In practice systemd won't start a second
#     instance of the same unit while one is active, so this mainly
#     protects a manual/ad-hoc invocation running alongside a
#     systemd-triggered one -- if that happens, the second one just fails
#     loudly (die), which is fine for a human at a terminal to see and
#     retry, unlike a silently-dropped automatic trigger.
#   * The actual "don't drop a request that arrives mid-run" guarantee
#     comes from systemd itself, not from this script: a .path unit
#     rechecks its condition the instant the service it triggered goes
#     inactive, and re-starts it immediately if the directory is still
#     non-empty (see `man systemd.path`). So this script only needs to
#     consume ("empty") $TRIGGER_DIR once, near the top of its single
#     pass, NOT loop internally -- if a new marker lands anywhere from
#     that point until this process exits, the directory is non-empty
#     again when the service deactivates, and systemd starts another
#     pass on its own. That's a stronger guarantee than this script could
#     give itself with plain flock/mkdir/rmdir (no unclosable race
#     between "last check" and "lock release": systemd's recheck IS the
#     transition to inactive, not a separate step that can be missed).
#   * The markers this pass is responsible for are CLAIMED (captured into
#     a fixed list) near the top, before the mount/rsync work, but only
#     actually DELETED at the very end, and only once this pass has
#     fully succeeded -- see $claimed below. Deleting them upfront was
#     tried first and was wrong: a failed mount (server down, network
#     blip) still left the trigger directory empty, so systemd saw
#     "nothing pending" on exit and never retried a request that was
#     never actually satisfied. Claiming a fixed list rather than
#     re-globbing at delete time also matters on the success path:
#     a marker written *during* this pass must never be swept up by that
#     final cleanup, or a request that arrived mid-run would be lost the
#     same way -- it needs to still be sitting there, unclaimed, so the
#     directory reads non-empty and systemd starts another pass for it.
set -uo pipefail

ENV_FILE="/etc/media-incoming-sync.env"

log() { printf '%s  %s\n' "$(date '+%F %T')" "$*" | tee -a "$LOG"; }
die() { log "ABORT: $*"; exit 1; }

# Everything device-specific (host, export, library mappings) is
# required config, not optional -- a missing or incomplete env file
# should fail loudly here, not silently sync nothing or sync the wrong
# thing. See Phase-2-Hardening Part 11 for the file's expected shape.
[[ -f "$ENV_FILE" ]] || { echo "media-incoming-sync: $ENV_FILE missing" >&2; exit 1; }
# shellcheck source=/etc/media-incoming-sync.env
source "$ENV_FILE"

: "${NFS_HOST:?not set in $ENV_FILE}"
: "${NFS_EXPORT:?not set in $ENV_FILE}"
: "${SRC:?not set in $ENV_FILE}"
: "${LOG:?not set in $ENV_FILE}"

MOUNT="/mnt/media-incoming-sync"
LOCKFILE="/run/media-incoming-sync.lock"
changed_list=""   # assigned via mktemp before use; declared here (empty)
                  # so cleanup()'s `rm -f "$changed_list"` never hits an
                  # unbound variable under `set -u` if cleanup fires
                  # (e.g. a die() in wait_for_host) before that point.
TRIGGER_DIR="/run/media-incoming-sync"   # see TRIGGERING above; created by tmpfiles.d, not here
PARTIAL_DIR=".rsync-partial"             # per-destination; rsync excludes it from the transfer

# <source subdir>|<destination path under the export>, one pair per
# MAPPING_SRC_N/MAPPING_DST_N in $ENV_FILE, N starting at 1 with no
# gaps -- the loop stops at the first missing MAPPING_SRC_N. Numbered
# pairs rather than one delimited variable so a folder name is never at
# risk of colliding with whatever delimiter was chosen (a real
# possibility for user-chosen library names, where a plain comma- or
# colon-joined string would not be).
MAPPINGS=()
n=1
while true; do
  src_var="MAPPING_SRC_$n"
  dst_var="MAPPING_DST_$n"
  src="${!src_var:-}"
  [[ -z "$src" ]] && break
  dst="${!dst_var:-}"
  [[ -z "$dst" ]] && die "$src_var is set but $dst_var is not, in $ENV_FILE"
  MAPPINGS+=("$src|$dst")
  n=$((n + 1))
done
[[ ${#MAPPINGS[@]} -gt 0 ]] || die "no MAPPING_SRC_1/MAPPING_DST_1 pair defined in $ENV_FILE"

VERIFY=0
VERIFY_ALL=0
for a in "$@"; do
  case "$a" in
    --verify) VERIFY=1 ;;
    --verify-all) VERIFY=1; VERIFY_ALL=1 ;;
    *) echo "usage: media-incoming-sync.sh [--verify | --verify-all]" >&2; exit 2 ;;
  esac
done

# See CONCURRENCY above: this is a backstop for a manual run colliding
# with a systemd-triggered one, not the mechanism that makes triggers
# reliable -- that's the .path unit's own recheck-on-deactivate.
exec 9>"$LOCKFILE"
flock -n 9 || die "another sync is already running (holding $LOCKFILE)"

# Claim (but do NOT yet delete) whatever's in the trigger directory
# right now -- these are the markers this pass is committing to fully
# account for. See CONCURRENCY above for why deletion is deferred to
# the very end, gated on success.
shopt -s nullglob
claimed=("$TRIGGER_DIR"/pending.*)
shopt -u nullglob

[[ -d "$SRC" ]] || die "source $SRC does not exist"
mkdir -p "$MOUNT"

# The media server may live somewhere only reachable over a VPN/tailnet
# that isn't necessarily up yet -- at boot, or right after a network
# blip, that networking can lag behind this script. Wait for the host
# to actually answer before mounting; abort rather than hang forever.
wait_for_host() {
  local n
  for ((n = 0; n < 24; n++)); do   # up to ~2 min
    if ping -c1 -W2 "$NFS_HOST" >/dev/null 2>&1; then
      log "$NFS_HOST reachable"
      return 0
    fi
    sleep 5
  done
  die "$NFS_HOST unreachable after ~2 min -- not mounting"
}
wait_for_host

# Mount only if not already mounted; remember whether WE mounted it so we
# only unmount what we brought up (leave a pre-existing mount in place).
we_mounted=0
if ! findmnt -rno FSTYPE "$MOUNT" >/dev/null 2>&1; then
  log "mounting ${NFS_HOST}:${NFS_EXPORT} -> $MOUNT"
  mount -t nfs4 "${NFS_HOST}:${NFS_EXPORT}" "$MOUNT" \
    || die "mount failed -- refusing to rsync into a local directory"
  we_mounted=1
fi
cleanup() {
  [[ "$we_mounted" == 1 ]] && umount "$MOUNT" 2>/dev/null && log "unmounted $MOUNT"
  rm -f "$changed_list" "$changed_list.raw"
}
# INT/TERM as well as EXIT: a bare EXIT trap does not fire when bash is
# killed by a signal, so an interrupted run (timeout, Ctrl-C, systemd stop)
# would leave the NFS mount behind. A stale mount is not merely untidy --
# if the remote host goes away, anything touching $MOUNT can hang on it.
# One trap for everything this script needs cleaned up on exit -- a
# second `trap ... EXIT` elsewhere would silently replace this one
# instead of adding to it, which is exactly the bug this comment is
# here to stop someone from reintroducing.
trap cleanup EXIT INT TERM

# Fail-closed guard: prove $MOUNT is a real NFS mount before writing a byte.
fstype=$(findmnt -rno FSTYPE "$MOUNT" 2>/dev/null)
case "$fstype" in
  nfs|nfs4) : ;;
  *) die "$MOUNT is not an NFS mount (fstype='${fstype:-none}') -- refusing to rsync" ;;
esac

# -rlt: recurse, keep symlinks + mtimes. --no-owner/--no-group: don't fight
# the server's own uid mapping / share ACL (chown would fail as a mapped
# user anyway). --partial-dir: resume big interrupted media files without
# ever exposing a truncated file under its final name -- see SAFETY above.
# --timeout: a wedged NFS mount must fail the run, not hang it forever.
# NO --delete -- see the SAFETY note at the top.
# --exclude .keep: a common convention for placeholder files some setups
# use to stop empty-directory cleanup from removing a still-empty library
# folder. Harmless to exclude even if you don't use that convention.
RSYNC_OPTS=(-rlt --no-owner --no-group --partial-dir="$PARTIAL_DIR"
            --timeout=600 --exclude='.keep')

# Validate every destination BEFORE transferring any of them, so a typo or a
# renamed share folder aborts the run rather than half-syncing and creating a
# stray directory inside the destination library.
for m in "${MAPPINGS[@]}"; do
  d="${MOUNT}/${m#*|}"
  [[ -d "$d" ]] || die "destination $d missing on the share"
  [[ -d "${SRC}/${m%|*}" ]] || die "source ${SRC}/${m%|*} does not exist"
done

changed_list=$(mktemp)

overall=0
for m in "${MAPPINGS[@]}"; do
  s="${SRC}/${m%|*}/"                 # trailing slash = copy CONTENTS, not the dir itself
  d="${MOUNT}/${m#*|}/"
  log "rsync start: $s -> ${NFS_HOST}:${NFS_EXPORT}/${m#*|}/ (additive, no delete)"
  # -i/--itemize-changes: needed even when not verifying, cheap to always
  # collect, and it's the only way to know exactly which files THIS pass
  # transferred -- see the fast-verify path below, which checks only
  # those rather than re-reading the whole library. Itemize lines look
  # like ">f+++++++++ path/to/file" -- an 11-char opcode field, one
  # space, then the path, so the path starts at column 13. Confirmed by
  # testing the actual output rather than assumed: an earlier draft used
  # column 14 and silently truncated every filename's first character.
  rsync "${RSYNC_OPTS[@]}" -i --stats "$s" "$d" 2>&1 | tee -a "$LOG" | tee "$changed_list.raw" >/dev/null
  rc=${PIPESTATUS[0]}
  grep '^>f' "$changed_list.raw" 2>/dev/null | cut -c13- > "$changed_list"
  rm -f "$changed_list.raw"
  if [[ "$rc" -eq 0 ]]; then
    log "rsync OK: ${m#*|}"
  else
    log "rsync FAILED: ${m#*|} (exit $rc)"
    overall=1
    continue
  fi

  [[ "$VERIFY" == 1 ]] || continue

  # Checksum verification. The transfer above compares size+mtime only --
  # fast, but it will happily consider a file "already there" that was
  # corrupted in flight or truncated by an earlier interrupted run.
  # --checksum re-reads both sides and compares content hashes, so a
  # clean pass is real evidence the destination matches. Done as a dry
  # run: it reports differences, never fixes them, so verification can
  # never itself alter the library.
  #
  # Default (--verify): only checksum files THIS PASS actually sent
  # (from $changed_list, captured above), via --files-from -- typically
  # zero to a handful of files, so this stays fast regardless of how
  # large the overall library is. Already-synced files from previous
  # passes are not re-read; they were verified when they arrived.
  #
  # --verify-all: the full-tree checksum dry-run, unrestricted -- reads
  # every file on both ends. Genuinely slow on a real library (minutes,
  # not seconds); this exists for the rare full-integrity check, not
  # routine use.
  if [[ "$VERIFY_ALL" != 1 ]] && [[ ! -s "$changed_list" ]]; then
    log "VERIFIED: ${m#*|} -- nothing transferred this pass, nothing to check"
    continue
  fi

  log "verify (checksum) start: ${m#*|}"
  if [[ "$VERIFY_ALL" == 1 ]]; then
    verify_out=$(rsync "${RSYNC_OPTS[@]}" --checksum --dry-run \
      --itemize-changes "$s" "$d" 2>/dev/null)
  else
    verify_out=$(rsync "${RSYNC_OPTS[@]}" --checksum --dry-run \
      --itemize-changes --files-from="$changed_list" "$s" "$d" 2>/dev/null)
  fi
  mismatches=$(grep -c '^>f' <<<"$verify_out")
  if [[ "$mismatches" -eq 0 ]]; then
    log "VERIFIED: ${m#*|} -- every checked file matches by checksum"
  else
    log "VERIFY FAILED: ${m#*|} -- $mismatches file(s) differ; re-run to repair"
    grep '^>f' <<<"$verify_out" | tee -a "$LOG"
    overall=1
  fi
done

# Only consume the claimed markers on a fully successful pass -- see the
# comment where $claimed was captured. A failed pass (mount, rsync, or
# verify) leaves them in place, so the trigger directory is still
# non-empty when this service exits and systemd.path's own recheck
# retries the whole thing automatically -- no Restart= needed, and
# confirmed this applies regardless of exit status, not just success
# (see `man systemd.path`, and Phase-2-Hardening Part 11).
if [[ "$overall" -eq 0 ]]; then
  rm -f "${claimed[@]}" 2>/dev/null || true
fi

exit "$overall"
