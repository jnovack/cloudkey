#!/bin/bash
#
# Fix obfuscated no-extension video files for NZBGet.
#
# Deployed to /var/lib/nzbget/scripts/fix-obfuscated.sh and enabled via
# Extensions=fix-obfuscated.sh in nzbget.conf (see Phase-2-Apps
# Part 10). The banner and OPTIONS section below aren't decoration --
# NZBGet's scanner only registers a file in ScriptDir as a
# post-processing script if it finds this exact structure (confirmed by
# testing: a version with only a plain comment, or with the banner
# doubled instead of matching this single-banner-plus-OPTIONS shape,
# both logged "doesn't exist" and were silently never run).
#
# The OPTIONS section needs its OWN closing "### NZBGET ... SCRIPT ###"
# banner below (not just the opening one above) -- confirmed against
# nzbget's own ExtensionLoader.cpp: once it enters the OPTIONS section it
# keeps consuming every subsequent line as a bogus config option until it
# finds a line containing " SCRIPT" to end on. Without that terminator it
# silently swallows the rest of this file -- `set -uo pipefail`,
# `POSTPROCESS_SUCCESS=93`, every function below -- as garbage options
# (confirmed live: nzbget's loadextensions API returned corrupted entries
# like "OSTPROCESS_SUCCESS"="93", the leading letter eaten by a parser
# that assumes every line is `#Name=value`). The script still ran despite
# this -- POST-PROCESSING kind is decided from the opening banner, before
# OPTIONS parsing starts -- but it left every discovered "option" garbage
# in the Settings UI. Do not remove the closing banner as redundant.
#
##############################################################################
### NZBGET POST-PROCESSING SCRIPT                                          ###

# Rename obfuscated video files; fail genuinely junk downloads.
#
# Why this exists: some Usenet reposts ("-xpost" groups especially) ship
# with every file deliberately renamed to garbage
# ("[PRiVATE]_xxx...yEnc  1547065448 (1_2159)") and no par2 recovery
# files at all. NZBGet's own ParRename/DirectRename can only recover a
# real filename by reading it back out of a matching par2 -- with none
# present, it has nothing to check against, and moves the file through
# to completed/ exactly as obfuscated as it arrived. Sonarr/Radarr only
# recognize video files by extension, so the download silently never
# gets imported, even though the actual video content is often fine.
#
# This script runs after every download and, if NZBGet didn't already
# see a recognizable video file, sniffs the first bytes of each
# sufficiently large file for a known container signature (MP4/MOV,
# Matroska/WebM, AVI, ASF/WMV, raw MPEG-TS) and renames it accordingly --
# the same fix as doing it by hand, just automatic.
#
# Deliberately narrow: it never touches a download that already
# contains a recognized video file -- the normal case, which needs no
# help -- and only considers files at or above a real-video size floor,
# so small extras (samples, subs, nfos) are never misidentified. It
# renames, never deletes -- any leftover small junk files stay in the
# download folder rather than risk removing something legitimate based
# on a heuristic.
#
# Only when NOTHING in the folder can be identified as video -- a
# genuinely junk/fake release -- does it mark the download failed
# (`[NZB] MARK=BAD`, exit POSTPROCESS_ERROR) so Sonarr/Radarr's own
# failed-download handling can blocklist it and search again. That
# requires "Failed Download Handling" enabled in Sonarr/Radarr (the
# default) -- otherwise the download just sits marked FAILURE in
# NZBGet's history for you to deal with by hand. This is the only case
# that costs a re-download, and it's the only case where the content
# was never recoverable in the first place.

##############################################################################
### OPTIONS                                                                ###

# (no configurable options -- thresholds are fixed in the script body)

### NZBGET POST-PROCESSING SCRIPT                                          ###
##############################################################################

set -uo pipefail

POSTPROCESS_SUCCESS=93
POSTPROCESS_NONE=95
POSTPROCESS_ERROR=94

DIR="${NZBPP_DIRECTORY:-}"
NAME="${NZBPP_NZBNAME:-unknown}"
STATUS="${NZBPP_STATUS:-}"

log() { echo "[$1] fix-obfuscated: $2"; }

if [ -z "$DIR" ] || [ ! -d "$DIR" ]; then
  log ERROR "no NZBPP_DIRECTORY to process"
  exit "$POSTPROCESS_NONE"
fi

# Only look at downloads NZBGet itself thinks succeeded. A real
# download failure (missing articles, failed par repair) is NZBGet's
# and Sonarr/Radarr's problem to handle, not this script's.
case "$STATUS" in
  SUCCESS*) ;;
  *) exit "$POSTPROCESS_NONE" ;;
esac

MIN_VIDEO_BYTES=$((20 * 1024 * 1024))
VIDEO_EXTS="mp4 mkv avi ts m2ts mov wmv webm m4v"

has_video=0
for f in "$DIR"/*; do
  [ -f "$f" ] || continue
  ext="${f##*.}"
  ext="${ext,,}"
  for v in $VIDEO_EXTS; do
    [ "$ext" = "$v" ] && has_video=1
  done
done

if [ "$has_video" = 1 ]; then
  # Normal case: Sonarr/Radarr can already see a video file.
  exit "$POSTPROCESS_NONE"
fi

sniff_extension() {
  local path="$1" head12 byte0 byte188
  head12=$(head -c 12 "$path" | xxd -p | tr -d '\n')
  case "$head12" in
    ????????66747970*) echo mp4; return ;;              # 'ftyp' at offset 4 (MP4/MOV)
    1a45dfa3*) echo mkv; return ;;                        # EBML header (Matroska/WebM)
    52494646????????41564920*) echo avi; return ;;        # 'RIFF'....'AVI '
    3026b275*) echo wmv; return ;;                        # ASF/WMV GUID
  esac
  byte0=$(head -c 1 "$path" | xxd -p)
  if [ "$byte0" = "47" ]; then
    byte188=$(dd if="$path" bs=1 skip=188 count=1 2>/dev/null | xxd -p)
    [ "$byte188" = "47" ] && { echo ts; return; }         # MPEG-TS sync byte, confirmed at packet 2
  fi
  echo ""
}

renamed=0
for f in "$DIR"/*; do
  [ -f "$f" ] || continue

  size=$(stat -c %s "$f" 2>/dev/null || echo 0)
  [ "$size" -lt "$MIN_VIDEO_BYTES" ] && continue

  ext=$(sniff_extension "$f")
  if [ -n "$ext" ]; then
    # Check mv's own exit status rather than assuming it worked --
    # an unchecked failure here (permissions, disk full, cross-device
    # weirdness) would otherwise still log "renamed" and let the loop
    # report overall success below, leaving Sonarr/Radarr with the same
    # unimportable file and no signal anything went wrong.
    if mv -- "$f" "$f.$ext"; then
      log INFO "renamed '$(basename -- "$f")' -> '$(basename -- "$f").$ext' (detected $ext by file signature)"
      renamed=1
    else
      log WARNING "detected $ext for '$(basename -- "$f")' but rename failed -- leaving it in place"
    fi
  fi
done

if [ "$renamed" = 1 ]; then
  exit "$POSTPROCESS_SUCCESS"
fi

log WARNING "no recognized video signature found anywhere in '$NAME' -- marking failed for blocklist+retry"
echo "[NZB] MARK=BAD"
exit "$POSTPROCESS_ERROR"
