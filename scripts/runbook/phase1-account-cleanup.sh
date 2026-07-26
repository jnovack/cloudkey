#!/bin/bash
# Phase 1 account cleanup: removes the orphaned UniFi appliance user
# accounts that phase1-purge.sh leaves behind.
#
# apt purge removes a package's files but DELIBERATELY never deletes the
# system users its postinst created -- dpkg can't know whether that UID
# owns files elsewhere on the box, so it leaves user removal to the
# operator. The result is ~two dozen unifi-* / appliance-plane accounts
# (unifi-drive*, unifi-protect*, unifi-talk, apollo, ds, ms, ucs-*,
# uos-go, fabric-agent, ...) lingering indefinitely after the UniFi stack
# is gone: nologin, password-locked, key-less, their packages no longer
# installed. Pure clutter. This removes them.
#
# Run ON THE BOX. Idempotent (accounts already gone are skipped) and safe
# to run on either Cloud Key model -- every removal is gated behind
# per-account guards, so an account unexpectedly still in use on a given
# box is skipped with a warning instead of deleted. That guard-first design
# is why one static candidate list works across differently-provisioned
# boxes: the list is the superset, the guards decide per box.
#
# What it will NOT remove, on purpose:
#   * ui  -- uid 0, but created by ck-ui.postinst, and ck-ui is a KEPT
#            package (phase1-purge.sh's FORBIDDEN list). Its postinst would
#            just recreate the account, so deleting it both fights that and
#            violates the purge's forbidden-package rule. A locked, key-less
#            root alias is inert; leave it.
#   * postgres -- owns a real data directory; a bundled database dealt with
#            by Step 4's database cleanup, not an orphan.
#   * anything that trips a guard (below): a running process, an unlocked
#     password, an authorized_keys file, files anywhere but /var/log, or
#     being (re)created by a still-installed package's postinst.
#
# Usage: phase1-account-cleanup.sh [-y] [--dry-run]
#   --dry-run  report what would be removed; change nothing.
#   -y         skip the confirmation prompt.

set -uo pipefail

DRY_RUN=0
ASSUME_YES=0
for a in "$@"; do
  case "$a" in
    --dry-run) DRY_RUN=1 ;;
    -y)        ASSUME_YES=1 ;;
    *) echo "usage: phase1-account-cleanup.sh [-y] [--dry-run]" >&2; exit 2 ;;
  esac
done

# The UniFi appliance-plane accounts the purge orphans. This is the union
# observed across the Cloud Key models built so far; an entry already gone
# on a given box is simply skipped. `ui` is deliberately absent -- see the
# header for why it stays.
CANDIDATES="
unifi-drive-ssh ds fabric-agent ucs-update ucs-agent
unifi-credential-server unifi-drive-nfs apollo uos-go unifi-protect-ai ms
unifi-drive-backup unifi-innerspace unifi-drive-no-samba unifi-drive-no-policy
unifi-drive-samba-admin unifi-drive unifi-connect unifi-homekit unifi-talk
unifi-base freeswitch unifi-access unifi-protect
"

log(){ printf '%s\n' "$*"; }

# Trees find() must never descend: bulk data we deliberately keep
# (/volume), pseudo-filesystems, and other mounts. Everything else --
# crucially including /var and /var/log even when they are SEPARATE
# filesystems on some models -- is scanned. This is a prune list, NOT
# `find -xdev`: -xdev refuses to cross a mount boundary, which on a box
# where /var is its own filesystem silently hides an account's log dir
# from the guard and would orphan those files on removal.
FIND_PRUNE=( -path /proc -o -path /sys -o -path /dev -o -path /run \
             -o -path /volume -o -path /mnt -o -path /media )

owned_files(){ find / \( "${FIND_PRUNE[@]}" \) -prune -o -user "$1" -print 2>/dev/null; }

# Is $1 (re)created by the postinst of a package that is still installed?
# Guards against deleting a service account something on THIS box still
# owns. Matches only real user-creation lines (adduser/useradd), so a
# coincidental mention -- e.g. a Debian "+ds" version string inside an
# unrelated postinst -- does not count as a reference. Errs toward
# reporting a reference (i.e. skipping the removal): the safe direction.
recreated_by_installed_pkg(){
  local u="$1" pi pkg st
  for pi in $(grep -lE "(adduser|useradd)[^#]*\b$u\b" /var/lib/dpkg/info/*.postinst 2>/dev/null); do
    pkg=$(basename "$pi" .postinst)
    st=$(dpkg-query -W -f='${db:Status-Abbrev}' "$pkg" 2>/dev/null || true)
    case "$st" in ii*) printf '%s' "$pkg"; return 0 ;; esac
  done
  return 1
}

if [ "$DRY_RUN" != 1 ] && [ "$ASSUME_YES" != 1 ]; then
  echo "About to remove orphaned UniFi appliance accounts left by the purge."
  echo "In-use accounts are guarded and skipped; ui/postgres are never touched."
  read -rp "Proceed? [y/N] " reply
  [[ "$reply" =~ ^[Yy]$ ]] || { echo "aborted"; exit 1; }
fi

REMOVED=(); SKIPPED=(); GONE=0
for u in $CANDIDATES; do
  getent passwd "$u" >/dev/null 2>&1 || { GONE=$((GONE + 1)); continue; }

  # --- guards: any hit means "leave it alone" ---------------------------
  if pgrep -u "$u" >/dev/null 2>&1; then
    log "SKIP  $u -- has a running process"; SKIPPED+=("$u"); continue
  fi
  hash=$(getent shadow "$u" | cut -d: -f2)
  case "$hash" in
    ""|\*|\!*) : ;;   # empty / '*' / '!...' are all locked -- fine to remove
    *) log "SKIP  $u -- has an active (unlocked) password"; SKIPPED+=("$u"); continue ;;
  esac
  home=$(getent passwd "$u" | cut -d: -f6)
  if [ -f "$home/.ssh/authorized_keys" ]; then
    log "SKIP  $u -- has an authorized_keys file"; SKIPPED+=("$u"); continue
  fi
  if pkg=$(recreated_by_installed_pkg "$u"); then
    log "SKIP  $u -- (re)created by still-installed package '$pkg'"; SKIPPED+=("$u"); continue
  fi

  # Files: safe to delete only if every file this account owns lives under
  # /var/log -- i.e. orphaned UniFi logs (e.g. ucs-update's leftover
  # unifi_package-identity-update dir). Anything outside /var/log is a real
  # payload; skip and let a human decide, so the two boxes don't silently
  # diverge on a judgement call.
  mapfile -t files < <(owned_files "$u")
  if [ "${#files[@]}" -gt 0 ]; then
    outside=$(printf '%s\n' "${files[@]}" | grep -vE '^/var/log/' || true)
    if [ -n "$outside" ]; then
      log "SKIP  $u -- owns files outside /var/log:"
      printf '        %s\n' "$outside"
      SKIPPED+=("$u"); continue
    fi
  fi

  # --- remove -----------------------------------------------------------
  note=""; [ "${#files[@]}" -gt 0 ] && note=" (+ ${#files[@]} orphaned /var/log path(s))"
  if [ "$DRY_RUN" = 1 ]; then
    log "WOULD REMOVE  $u$note"; REMOVED+=("$u"); continue
  fi
  if [ "${#files[@]}" -gt 0 ]; then
    # rm -rf on a parent no-ops its (already-removed) children, so order
    # within the list doesn't matter -- all paths are under /var/log here.
    rm -rf "${files[@]}"
  fi
  deluser --quiet "$u" >/dev/null 2>&1
  # deluser leaves the matching usergroup behind; drop it only if now empty.
  getent group "$u" >/dev/null 2>&1 && delgroup --quiet --only-if-empty "$u" >/dev/null 2>&1
  log "REMOVED  $u$note"; REMOVED+=("$u")
done

# Sweep orphaned appliance GROUPS. deluser leaves a removed user's private
# group behind, and some groups (e.g. unifi-streaming) never had a user at
# all -- so this is a separate pass. Guarded: a group is removed only if it
# has no members AND is no existing user's primary group.
GROUP_REMOVED=()
for g in $(getent group | awk -F: '$1 ~ /^(unifi|ucs|uos|apollo|fabric-agent|ds|ms)([-_]|$)/{print $1}'); do
  members=$(getent group "$g" | cut -d: -f4)
  if [ -n "$members" ]; then
    log "SKIP group  $g -- has members: $members"; continue
  fi
  gid=$(getent group "$g" | cut -d: -f3)
  if getent passwd | awk -F: -v g="$gid" '$4==g{found=1} END{exit !found}'; then
    log "SKIP group  $g -- primary group of an existing user"; continue
  fi
  if [ "$DRY_RUN" = 1 ]; then
    log "WOULD REMOVE group  $g"; GROUP_REMOVED+=("$g"); continue
  fi
  if delgroup --quiet "$g" >/dev/null 2>&1; then
    log "REMOVED group  $g"; GROUP_REMOVED+=("$g")
  fi
done

echo
echo "=== summary ==="
printf 'accounts removed: %d   skipped (in use): %d   already gone: %d\n' \
  "${#REMOVED[@]}" "${#SKIPPED[@]}" "$GONE"
printf 'groups removed:   %d\n' "${#GROUP_REMOVED[@]}"
[ "$DRY_RUN" = 1 ] && echo "(dry run -- nothing was changed)"
exit 0
