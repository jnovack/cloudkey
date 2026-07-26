#!/bin/bash
# Phase 2, Part 1 (script 00 -- run this before any of the four
# numbered app installers): creates the shared 'media' group and the
# directory structure on /volume that NZBGet/Sonarr/Radarr share.
#
# Required by phase2-01-nzbget.sh, phase2-02-sonarr.sh, and
# phase2-03-radarr.sh -- all three either join or assign their user to
# the 'media' group, which has to exist first. Not required by
# phase2-04-prowlarr.sh (its own dedicated group). Also the first thing
# to re-run after replacing the drive (Phase-1-De-Ubiquitizing Step 7)
# -- safe to re-run any time, it's idempotent.
#
# The three .keep files aren't optional -- see Phase-2-Overview for
# why (a still-active board-level tool deletes empty directories under
# /volume on every boot; a placeholder file neutralizes it).

set -euo pipefail

if [[ ! -d /volume ]]; then
  echo "error: /volume doesn't exist -- run Phase-1-De-Ubiquitizing Step 7 (or phase1-format-mount-volume.sh) first" >&2
  exit 1
fi

groupadd media 2>/dev/null || true

# Libraries live under one parent (media/) so they have a common root for
# copying to a NAS / backing up / exporting -- one path, not a list that
# drifts as libraries are added. downloads/ stays outside it: it's
# transient import scratch, not library content.
mkdir -p /volume/downloads/completed /volume/media/tv /volume/media/movies
touch /volume/media/.keep /volume/media/tv/.keep /volume/media/movies/.keep \
  /volume/downloads/completed/.keep
chown root:media /volume /volume/media /volume/media/tv /volume/media/movies
chmod -R 2775 /volume

echo "Done:"
find /volume -maxdepth 2
