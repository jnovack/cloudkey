#!/bin/bash
# Runs on a timer (see vpn-heal.timer). If the tunnel is healthy, does
# nothing further than check each service's own DNS. If not, restores it
# and the services pinned to it, then verifies the fix actually worked
# before declaring success.
#
# `wg-quick`/userspace WireGuard daemonize outside systemd's process
# tracking (Type=oneshot's MainPID is never set to the real tunnel
# process), so wg-quick-vpn.service can keep reporting "active"
# indefinitely after the underlying tunnel has actually died -- this
# script is what makes `systemctl status` trustworthy again: an explicit
# stop+start on every healing pass means its state reflects reality as
# of the last check, not just the last time it happened to be started.
#
# Second, independent thing this watches: each service's DNS override
# (the tmpfs + bind drop-in from Part 4). That shape makes resolved's
# stub rewrites unable to orphan the override, so this per-service
# check should never fire under normal operation -- it stays as a cheap
# regression guard against a drop-in "simplified" back to a direct
# /etc/resolv.conf bind, which IS silently orphaned by every stub
# rewrite (see Part 6's failure-mode discussion). If it fires, a
# `systemctl restart` on just that service reapplies its mounts fresh.
#
# Logging is deliberately transition-based, not per-check: this runs
# every 60s, and logging "still healthy" on every single pass would bury
# real events (disconnects, heals) in noise and eat into a limited
# journal budget on constrained hosts for nothing. `journalctl -u
# vpn-heal.service` already gets a timestamped entry every cycle for
# free via systemd itself (Starting.../Finished...), which doubles as a
# heartbeat signal for a future alerting pass -- no need to duplicate
# that at the application-log level too.
set -uo pipefail

CHECK=/usr/local/sbin/vpn-check.sh
TUNNEL_SERVICE=wg-quick-vpn.service
# List every protected service, space-separated -- same list as the
# Before= line in wg-quick-vpn.service (Part 3). Example with three:
# SERVICES="sync-agent backup-agent report-agent"
SERVICES="<service> <service>"
DNS_EXPECTED="nameserver <provider-dns-ip>"
RETRIES=3
RETRY_DELAY=5
STATE_FILE=/run/vpn-heal.state

log() { logger -t vpn-heal "$1"; echo "$1"; }
prev_state() { cat "$STATE_FILE" 2>/dev/null || echo "unknown"; }
set_state() { echo "$1" > "$STATE_FILE"; }

# Checks the DNS override actually pinned to a running service's process
# is intact. Reads via that process's own mount namespace (nsenter -m),
# not `ip netns exec` -- this deliberately checks the same long-lived
# bind-mount the service itself is using, not a fresh one, since the
# whole point is to catch it going stale under the service.
service_dns_ok() {
  local pid
  pid=$(systemctl show -p MainPID --value "$1" 2>/dev/null)
  [[ -n "$pid" && "$pid" != "0" ]] || return 1
  nsenter -t "$pid" -m -- grep -qx "$DNS_EXPECTED" /etc/resolv.conf 2>/dev/null
}

heal_service_dns() {
  local svc_fail=0
  for svc in $SERVICES; do
    service_dns_ok "$svc" && continue
    log "$svc: DNS override not intact (unexpected with the tmpfs drop-in shape) -- restarting to reapply"
    systemctl restart "$svc"
    sleep 2
    if service_dns_ok "$svc"; then
      log "$svc: DNS override reapplied"
    else
      log "$svc: DNS override still broken after restart"
      svc_fail=1
    fi
  done
  return "$svc_fail"
}

if "$CHECK" -q; then
  [[ "$(prev_state)" == "healthy" ]] || log "tunnel healthy (was: $(prev_state))"
  set_state healthy
  heal_service_dns
  exit $?
fi

set_state unhealthy
log "tunnel unhealthy -- healing: restarting $TUNNEL_SERVICE and dependent services"

# Explicit stop+start (not `restart`) so an intermediate `systemctl
# status` mid-heal shows an honest "inactive", not a misleading "active"
# carried over from before the problem was detected.
systemctl stop "$TUNNEL_SERVICE"
systemctl start "$TUNNEL_SERVICE"

# Requires= only stops dependents when the tunnel stops -- it does not
# auto-start them when the tunnel comes back, so that's done explicitly.
# shellcheck disable=SC2086
systemctl start $SERVICES

for ((i = 1; i <= RETRIES; i++)); do
  if "$CHECK" -q; then
    set_state healthy
    log "heal succeeded (attempt $i/$RETRIES)"
    heal_service_dns
    exit $?
  fi
  sleep "$RETRY_DELAY"
done

log "heal FAILED after $RETRIES attempts -- tunnel still not passing traffic"
exit 1
