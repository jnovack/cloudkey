#!/bin/bash
# Fires media-incoming-sync.sh without needing root or sudo from the
# caller. Meant as Sonarr/Radarr's Custom Script (Settings -> Connect,
# On Import/On Upgrade) -- those run as the sonarr/radarr users, which
# have no business mounting NFS. Writing a marker file into a
# group-writable tmpfs directory needs neither: the systemd .path unit
# watching that directory (root) does the actual triggering.
#
# mktemp, not a hand-rolled name: guarantees a unique, atomically-created
# file even if two imports land in the same second, which a bare
# `touch pending.$(date +%s)` would collide on and silently overwrite.
set -uo pipefail

TRIGGER_DIR="/run/media-incoming-sync"

[[ -d "$TRIGGER_DIR" ]] || {
  echo "media-incoming-sync-trigger: $TRIGGER_DIR missing -- is the" \
       "tmpfiles.d rule installed and has systemd-tmpfiles run?" >&2
  exit 1
}

mktemp -q "$TRIGGER_DIR/pending.$(date +%Y%m%dT%H%M%S).XXXXXX" >/dev/null
