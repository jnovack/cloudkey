#!/bin/bash
# Colored one-screen status summary for a de-Ubiquitized Cloud Key: the
# Phase 2 media-stack apps, the Phase 3 autossh rescue tunnels, and Phase 5
# Tailscale. Meant to fire automatically on interactive SSH logins (see
# zz-cloudkey-dashboard.sh), but safe to run by hand any time from any
# session for a quick check -- every check here is read-only
# (systemctl/tailscale status), never starts, stops, or reconfigures
# anything.
#
# Every phase past 1 is optional, and there's no fixed order past that --
# a box might have the media stack but not remote access, or Tailscale but
# no media stack. Each section only prints at all if at least one of its
# sub-checks is actually installed (present, whether or not it's running);
# an entirely-unbuilt phase produces no header and no lines, not a wall of
# "not installed". Within a section that IS printing, individual
# not-installed lines still show, since a partially-built phase (e.g. only
# 2 of the 4 media apps) is worth calling out.
set -uo pipefail

if [ -t 1 ] && [ "$(tput colors 2>/dev/null || echo 0)" -ge 8 ]; then
  RED=$'\e[31m'; GREEN=$'\e[32m'; YELLOW=$'\e[33m'; CYAN=$'\e[36m'
  BOLD=$'\e[1m'; RESET=$'\e[0m'
else
  RED=''; GREEN=''; YELLOW=''; CYAN=''; BOLD=''; RESET=''
fi

section() { printf '\n%s%s%s\n' "$BOLD" "$1" "$RESET"; }

# unit_installed <unit> -- true if the unit file exists, regardless of
# whether it's enabled or currently running. Used to gate whole sections
# on the sub-check that's true: "did the build add this at all," not
# "is it up right now."
unit_installed() {
  [ "$(systemctl show -p LoadState --value "$1" 2>/dev/null)" = "loaded" ]
}

# svc_line <unit> <label>
# Classifies a systemd unit for the dashboard:
#   not installed -- unit file doesn't exist (LoadState != loaded)
#   standby       -- loaded but disabled (expected for an on-demand unit,
#                    e.g. a Tier-2 autossh web-UI tunnel)
#   running       -- loaded, active
#   DOWN          -- loaded, enabled, but not active
svc_line() {
  local unit="$1" label="$2"
  local loadstate enabled active color text
  loadstate=$(systemctl show -p LoadState --value "$unit" 2>/dev/null)
  enabled=$(systemctl is-enabled "$unit" 2>/dev/null)
  active=$(systemctl is-active "$unit" 2>/dev/null)

  if [ "$loadstate" != "loaded" ]; then
    color="$YELLOW"; text="not installed"
  elif [ "$enabled" = "disabled" ]; then
    color="$CYAN"; text="standby, manual start"
  elif [ "$active" = "active" ]; then
    color="$GREEN"; text="running"
  else
    color="$RED"; text="DOWN (${active})"
  fi
  printf "  %-30s ${color}%s${RESET}\n" "$label" "$text"
}

printed_any=0

# unit:label -- see phase2-apps.md for why these four and these ports.
APPS=(
  "nzbget:NZBGet (port 6789)"
  "sonarr:Sonarr (port 8989)"
  "radarr:Radarr (port 7878)"
  "prowlarr:Prowlarr (port 9696)"
)
media_installed=0
for entry in "${APPS[@]}"; do
  unit_installed "${entry%%:*}" && { media_installed=1; break; }
done
if [ "$media_installed" -eq 1 ]; then
  section "Media stack"
  for entry in "${APPS[@]}"; do
    svc_line "${entry%%:*}" "${entry#*:}"
  done
  printed_any=1
fi

# Discover configured relays instead of hardcoding one -- phase3-autossh.md
# supports one Tier-1 + optional Tier-2 pair per <vps>, and there's no fixed
# count of relays across builds. An empty glob here already means "Phase 3
# not built," so it doubles as this section's install check.
shopt -s nullglob
tier1_units=(/etc/systemd/system/autossh-tunnel-*.service)
shopt -u nullglob
if [ "${#tier1_units[@]}" -gt 0 ]; then
  section "Rescue tunnels (autossh)"
  for unit_path in "${tier1_units[@]}"; do
    unit="$(basename "$unit_path" .service)"
    [ "${unit%-webui}" != "$unit" ] && continue  # -webui is Tier 2, paired in below
    vps="${unit#autossh-tunnel-}"
    svc_line "$unit" "$vps (Tier 1, rescue SSH)"
    svc_line "${unit}-webui" "$vps (Tier 2, on-demand web UI)"
  done
  printed_any=1
fi

if command -v tailscale >/dev/null 2>&1; then
  section "Tailscale"
  if ts_ip="$(tailscale ip -4 2>/dev/null)" && [ -n "$ts_ip" ]; then
    printf "  %-30s ${GREEN}%s${RESET}\n" "tailscale" "up ($ts_ip)"
  else
    printf "  %-30s ${CYAN}%s${RESET}\n" "tailscale" "installed, not connected"
  fi
  printed_any=1
fi

# Can be used as an optional fallback so something prints on yet-to-be-configured devices.
# [ "$printed_any" -eq 1 ] || printf '\ncloudkey-dashboard: nothing optional built yet (Phase 1 only)\n'

printf '\n'
