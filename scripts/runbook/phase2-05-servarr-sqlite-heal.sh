#!/bin/bash
# Runs as ExecStartPre for Radarr and Prowlarr (see the sqlite-glibc.conf
# drop-in in Phase-2-Apps). Takes the app's install directory name --
# "Radarr" or "Prowlarr" -- and repairs its bundled SQLite helper if that
# helper can't actually load on this OS.
#
# Why this exists: these apps bundle libe_sqlite3.so built against
# GLIBC_2.33, and Debian 11 on this box ships 2.31. The install-time fix
# is to point that filename at the system's own SQLite library instead.
# But an in-app upgrade replaces the whole /opt/<App> tree, symlink
# included, so the fix is undone by every successful update -- and the
# next start crashes on the database layer.
#
# That crash is deceptively quiet: on a startup failure these apps print
# "Press enter to exit..." and then never exit. The process stays alive,
# so systemd reports "active (running)" and Restart=on-failure never
# fires -- there is no non-zero exit for it to react to. The service
# looks healthy while doing nothing at all. Running this before every
# start is what keeps an in-app upgrade from landing in that state
# unattended.
#
# Note the message is misleading: the process is NOT waiting for a
# keypress, so redirecting stdin does not help. systemd already sets
# StandardInput=null, ReadLine() returns EOF immediately, and during a
# real hang the main thread sits in futex_wait_queue_me with nothing
# reading fd 0. It survives because .NET won't exit while foreground
# threads (Kestrel, thread pool) started before the failure are still
# running. See Phase-2-Overview before trying to solve this with
# Restart= or StandardInput= settings -- they cannot work.
#
# Deliberately advisory, never fatal: if the repair can't be made, this
# still exits 0 and lets the app start. A start that might have worked
# should not be blocked by this script's own inability to fix something
# -- the app's log stays the source of truth for a real failure.

set -uo pipefail

APP="${1:-}"
SYSTEM_LIB=/usr/lib/aarch64-linux-gnu/libsqlite3.so.0

if [ -z "$APP" ]; then
  echo "servarr-sqlite-heal: no app name given, nothing to do" >&2
  exit 0
fi

BUNDLED="/opt/$APP/libe_sqlite3.so"

if [ ! -e "$BUNDLED" ]; then
  # Sonarr v4 doesn't ship this file at all -- not an error, just not
  # an app this applies to.
  exit 0
fi

# Already pointed at a working system library. The common case on every
# start that isn't the one right after an upgrade.
if [ -L "$BUNDLED" ] && [ -e "$BUNDLED" ]; then
  exit 0
fi

# Test the real thing rather than assuming a bundled copy is broken:
# upstream builds have shipped against an older GLIBC before (Prowlarr's
# pre-upgrade build loaded fine here), and replacing a library that
# works would be a regression, not a fix.
if ! ldd "$BUNDLED" 2>&1 | grep -q 'not found'; then
  exit 0
fi

if [ ! -e "$SYSTEM_LIB" ]; then
  echo "servarr-sqlite-heal: $APP needs the system SQLite library but" \
       "$SYSTEM_LIB is missing -- install libsqlite3-0. Letting" \
       "$APP start anyway; check its log if it fails." >&2
  exit 0
fi

# Keep the first bundled copy we displace, for reference. Never
# overwrite an existing backup with a later broken build.
if [ ! -e "$BUNDLED.backup" ]; then
  mv "$BUNDLED" "$BUNDLED.backup"
else
  rm -f "$BUNDLED"
fi

ln -sf "$SYSTEM_LIB" "$BUNDLED"
echo "servarr-sqlite-heal: repaired $APP's SQLite library after an upgrade" \
     "(bundled copy needs a newer GLIBC than this OS ships)"
