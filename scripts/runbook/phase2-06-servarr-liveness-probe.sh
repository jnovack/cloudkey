#!/bin/bash
# Runs on a timer (servarr-liveness-probe.timer). Catches the failure
# mode systemd structurally cannot: an app that is running but not
# serving.
#
# Why this is needed at all: on a startup failure these apps print
# "Press enter to exit..." and then never terminate -- .NET keeps the
# process resident while foreground threads (Kestrel, thread pool)
# started before the failure are still alive. systemd sees a live
# MainPID, reports "active (running)", and Restart=on-failure never
# fires because there is no exit code to react to. Confirmed by test:
# in that state `systemctl is-active` says "active" while nothing is
# listening on the app's port at all. A liveness probe is the only
# thing that can tell those two states apart.
#
# The known trigger (an in-app upgrade discarding the SQLite symlink) is
# already prevented by servarr-sqlite-heal.sh running as ExecStartPre.
# This is the backstop for the general case -- any cause, including ones
# not yet seen.
#
# Deliberately conservative, because a probe that restarts things
# incorrectly is worse than no probe:
#
#   * Only probes units that are currently "active". A unit an admin
#     stopped stays stopped; a unit that genuinely failed is systemd's
#     to restart, not ours.
#   * Ignores apps for GRACE seconds after they start, so normal
#     startup time (~15s here) never reads as a hang.
#   * Requires FAIL_THRESHOLD consecutive failures before acting, so a
#     single slow or dropped response doesn't bounce a healthy app.
#   * Caps restarts per app per hour. If restarting isn't fixing it,
#     the problem isn't one a restart solves, and hammering it just
#     buries the evidence.
#
# NETNS is empty by default because Phase 4 (the VPN killswitch) is
# OPTIONAL -- most of this script's usefulness has nothing to do with
# it. Leave NETNS empty and the four apps are probed directly on the
# host's own loopback, exactly as they run without Phase 4. Only set it
# if Phase 4 was built AND the apps were pinned into that namespace --
# use the same name substituted for <vpn> throughout Phase-4-WireGuard.
# If NETNS is set but that namespace doesn't exist, this skips the pass
# and says so rather than silently falling back to direct probing --
# that mismatch (configured but missing) is worth surfacing, not papering
# over. An earlier version hardcoded a namespace name unconditionally and
# exited immediately if it was missing, which meant on any setup that
# skipped Phase 4 this probe silently never ran at all, forever.
#
# When probing IS done inside a namespace, this does NOT couple the probe
# to tunnel health: the apps listen on loopback *inside* the namespace,
# which keeps working whether or not the tunnel itself is up, so a VPN
# outage can't cause a false restart here. Tunnel health is
# vpn-heal.sh's job (Phase 4 Part 6), not this script's.
#
# Logging is transition-based: this runs every 60s and logging "still
# healthy" every pass would bury real events and eat the box's 500MB
# persistent journal budget. systemd already records a per-cycle
# heartbeat (Starting.../Finished...) for free.

set -uo pipefail

NETNS=""   # e.g. NETNS="<vpn>" -- only if Phase 4 was built; see above
STATE_DIR=/run/servarr-probe
GRACE=90            # seconds after start before an app is probed
FAIL_THRESHOLD=3    # consecutive failures before restarting
MAX_RESTARTS=3      # per app, per hour
CURL_TIMEOUT=10

# unit:port:path -- /ping is unauthenticated on the *arr apps, and
# NZBGet's jsonrpc/version answers without credentials from loopback,
# so none of this needs API keys that would rot when they're rotated.
APPS=(
  "nzbget:6789:/jsonrpc/version"
  "sonarr:8989:/ping"
  "radarr:7878:/ping"
  "prowlarr:9696:/ping"
)

log(){ printf '%s\n' "$*"; }

# Only skip the whole pass if a namespace was actually CONFIGURED and
# it's missing (a real mismatch worth surfacing). Empty NETNS (Phase 4
# never built, the normal case) is not a skip condition at all -- see
# probe_url() below, which branches on whether NETNS is set.
if [ -n "$NETNS" ] && [ ! -e "/var/run/netns/$NETNS" ]; then
  log "servarr-probe: netns '$NETNS' configured but missing -- skipping this pass" >&2
  exit 0
fi

probe_url() {
  if [ -n "$NETNS" ]; then
    ip netns exec "$NETNS" curl -sf -m "$CURL_TIMEOUT" -o /dev/null "$1"
  else
    curl -sf -m "$CURL_TIMEOUT" -o /dev/null "$1"
  fi
}

mkdir -p "$STATE_DIR"

now=$(date +%s)

for entry in "${APPS[@]}"; do
  unit="${entry%%:*}"
  rest="${entry#*:}"
  port="${rest%%:*}"
  path="${rest#*:}"

  [ "$(systemctl is-active "$unit")" = "active" ] || continue

  # Skip freshly started apps. ActiveEnterTimestamp is empty in some
  # transitional states -- treat unknown as "too new to judge".
  started="$(systemctl show -p ActiveEnterTimestamp --value "$unit")"
  if [ -n "$started" ]; then
    started_epoch=$(date -d "$started" +%s 2>/dev/null || echo 0)
  else
    started_epoch=$now
  fi
  [ $((now - started_epoch)) -lt "$GRACE" ] && continue

  fail_file="$STATE_DIR/$unit.fails"
  fails=$(cat "$fail_file" 2>/dev/null || echo 0)

  if probe_url "http://127.0.0.1:$port$path"; then
    # Only log the recovery edge, not every healthy pass.
    if [ "$fails" -gt 0 ]; then
      log "servarr-probe: $unit responding again after $fails failed probe(s)"
    fi
    echo 0 > "$fail_file"
    continue
  fi

  fails=$((fails + 1))
  echo "$fails" > "$fail_file"

  if [ "$fails" -lt "$FAIL_THRESHOLD" ]; then
    log "servarr-probe: $unit not responding on port $port ($fails/$FAIL_THRESHOLD)"
    continue
  fi

  # Threshold reached. Check the hourly restart budget before acting.
  restart_file="$STATE_DIR/$unit.restarts"
  recent=""
  while read -r ts; do
    [ -n "$ts" ] && [ $((now - ts)) -lt 3600 ] && recent+="$ts"$'\n'
  done < <(cat "$restart_file" 2>/dev/null)

  count=$(printf '%s' "$recent" | grep -c . || true)
  if [ "$count" -ge "$MAX_RESTARTS" ]; then
    log "servarr-probe: $unit still not responding, but already restarted" \
        "$count time(s) this hour -- not restarting again. Restarting is not" \
        "fixing this; check 'journalctl -u $unit'." >&2
    continue
  fi

  log "servarr-probe: $unit alive to systemd but not serving on port $port" \
      "after $fails probes -- restarting"
  printf '%s%s\n' "$recent" "$now" > "$restart_file"
  systemctl restart "$unit"
  echo 0 > "$fail_file"
done
