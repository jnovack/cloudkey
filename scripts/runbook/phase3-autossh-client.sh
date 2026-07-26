#!/bin/bash
# Phase 3 client setup: install autossh, generate per-relay tunnel keys,
# pin relay host keys, and write the client-side systemd units.
#
# Run this on the client device. It deliberately does not configure the
# relay/VPS, because that side needs a sudo-capable account on a different
# host. After generating keys, this script prints a copy/paste relay setup
# block that contains only public keys.
#
# Required:
#   RELAYS="relay1.example.com relay2.example.com"
#   RELAYS="relay1=relay1.example.com relay2=relay2.example.com"
#
# Optional:
#   CLIENT_NAME=cloudkey-1234
#   TIER1_PORT=2345
#   WEBUI_PORTS="8080:80 6789 8989 7878 9696"
#   START=0        # set START=1 only after the relay-side block is installed
#
# WEBUI_PORTS entries are either a bare port (same port both ends) or
# RELAYPORT:LOCALPORT when the two must differ -- needed when the client
# serves on a privileged port (a dashboard on :80 -> 8080:80), or when one
# relay carries several clients that all use the same local port.

set -euo pipefail

: "${RELAYS:?set RELAYS, e.g. RELAYS=\"relay1.example.com relay2.example.com\"}"

TIER1_PORT="${TIER1_PORT:-2345}"
WEBUI_PORTS="${WEBUI_PORTS:-}"
START="${START:-0}"
CLIENT_NAME="${CLIENT_NAME:-}"
KEY_DIR="/etc/autossh/keys"

unit_name() {
  printf '%s' "$1" | tr -c 'A-Za-z0-9_.@:-' '-'
}

relay_name() {
  case "$1" in
    *=*) printf '%s' "${1%%=*}" ;;
    *) unit_name "$1" ;;
  esac
}

relay_host() {
  case "$1" in
    *=*) printf '%s' "${1#*=}" ;;
    *) printf '%s' "$1" ;;
  esac
}

client_name() {
  if [ -n "$CLIENT_NAME" ]; then
    printf '%s' "$CLIENT_NAME"
    return
  fi
  hostname
}

# A bare port has no ':', so both expansions return it unchanged and the
# forward stays symmetric -- pre-existing WEBUI_PORTS values keep working.
relay_port() { printf '%s' "${1%%:*}"; }
local_port() { printf '%s' "${1##*:}"; }

# sshd binds the relay end of a -R forward as the unprivileged tunnel
# account, which cannot take a port below 1024. Because the units set
# ExitOnForwardFailure, one unbindable port kills the WHOLE tier-2 tunnel
# rather than just that forward -- so reject it here, where the cause is
# still visible, instead of leaving a dead unit to diagnose later.
for spec in $WEBUI_PORTS; do
  rport=$(relay_port "$spec")
  lport=$(local_port "$spec")
  # Check each half separately: concatenating them would let a half-empty
  # pair like '8080:' pass on the strength of the other side.
  case "$rport" in ''|*[!0-9]*) rport=invalid ;; esac
  case "$lport" in ''|*[!0-9]*) lport=invalid ;; esac
  if [ "$rport" = invalid ] || [ "$lport" = invalid ]; then
    echo "WEBUI_PORTS: '$spec' is not a port or a RELAYPORT:LOCALPORT pair" >&2
    exit 1
  fi
  if [ "$rport" -lt 1024 ]; then
    echo "WEBUI_PORTS: relay-side port $rport is privileged (<1024); the" >&2
    echo "  unprivileged tunnel account cannot bind it. Map it to a high" >&2
    echo "  relay-side port instead, e.g. 8080:${lport}" >&2
    exit 1
  fi
done

apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y autossh openssh-client

install -d -m 700 "$KEY_DIR"
touch "$KEY_DIR/known_hosts"
chmod 644 "$KEY_DIR/known_hosts"

for relay_spec in $RELAYS; do
  relay=$(relay_host "$relay_spec")
  unit=$(relay_name "$relay_spec")
  client=$(client_name)
  relay_user="tunnel-${client}"
  relay_webui_user="tunnel-${client}-webui"
  tier1_key="$KEY_DIR/${unit}_tunnel"
  tier2_key="$KEY_DIR/${unit}_tunnel_webui"

  if [ ! -f "$tier1_key" ]; then
    ssh-keygen -t ed25519 -N '' -f "$tier1_key" \
      -C "client-autossh-tier1@$client"
  fi

  if [ -n "$WEBUI_PORTS" ] && [ ! -f "$tier2_key" ]; then
    ssh-keygen -t ed25519 -N '' -f "$tier2_key" \
      -C "client-autossh-tier2@$client"
  fi

  chmod 600 "$tier1_key" "$tier2_key" 2>/dev/null || true
  chmod 644 "$tier1_key.pub" "$tier2_key.pub" 2>/dev/null || true
  printf '%s %s\n' "$(ssh-keygen -y -f "$tier1_key" | awk '{print $1 " " $2}')" \
    "client-autossh-tier1@$client" > "$tier1_key.pub"
  if [ -n "$WEBUI_PORTS" ]; then
    printf '%s %s\n' "$(ssh-keygen -y -f "$tier2_key" | awk '{print $1 " " $2}')" \
      "client-autossh-tier2@$client" > "$tier2_key.pub"
  fi

  ssh-keygen -R "$relay" -f "$KEY_DIR/known_hosts" >/dev/null 2>&1 || true
  ssh-keyscan -t ed25519 "$relay" >> "$KEY_DIR/known_hosts"

  cat > "/etc/systemd/system/autossh-tunnel-${unit}.service" <<EOF
[Unit]
Description=Autossh Tier-1 rescue tunnel to ${relay}
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
Type=simple
User=root
Environment=AUTOSSH_GATETIME=0
Environment=AUTOSSH_POLL=30
ExecStart=/usr/bin/autossh -M 0 -N \\
  -o "ServerAliveInterval 30" \\
  -o "ServerAliveCountMax 3" \\
  -o "ExitOnForwardFailure yes" \\
  -o "StrictHostKeyChecking yes" \\
  -o "UserKnownHostsFile=${KEY_DIR}/known_hosts" \\
  -o "IdentitiesOnly yes" \\
  -i "${tier1_key}" \\
  -R 127.0.0.1:${TIER1_PORT}:127.0.0.1:22 \\
  ${relay_user}@${relay}
Restart=always
RestartSec=15
# autossh traps SIGTERM and exits 1, so a plain 'systemctl stop' would
# otherwise leave the unit in 'failed'. That is noise for Tier 1 and
# actively misleading for Tier 2, whose normal workflow IS start-then-stop.
# Restart=always still covers real crashes, so nothing is masked by this.
SuccessExitStatus=0 1

[Install]
WantedBy=multi-user.target
EOF

  if [ -n "$WEBUI_PORTS" ]; then
    forwards=""
    for spec in $WEBUI_PORTS; do
      forwards="${forwards}  -R 127.0.0.1:$(relay_port "$spec"):127.0.0.1:$(local_port "$spec") \\
"
    done

    cat > "/etc/systemd/system/autossh-tunnel-${unit}-webui.service" <<EOF
[Unit]
Description=Autossh Tier-2 web-UI tunnel to ${relay} (manual start only)
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
Type=simple
User=root
Environment=AUTOSSH_GATETIME=0
Environment=AUTOSSH_POLL=30
ExecStart=/usr/bin/autossh -M 0 -N \\
  -o "ServerAliveInterval 30" \\
  -o "ServerAliveCountMax 3" \\
  -o "ExitOnForwardFailure yes" \\
  -o "StrictHostKeyChecking yes" \\
  -o "UserKnownHostsFile=${KEY_DIR}/known_hosts" \\
  -o "IdentitiesOnly yes" \\
  -i "${tier2_key}" \\
${forwards}  ${relay_webui_user}@${relay}
Restart=always
RestartSec=15
# autossh traps SIGTERM and exits 1, so a plain 'systemctl stop' would
# otherwise leave the unit in 'failed'. That is noise for Tier 1 and
# actively misleading for Tier 2, whose normal workflow IS start-then-stop.
# Restart=always still covers real crashes, so nothing is masked by this.
SuccessExitStatus=0 1

[Install]
WantedBy=multi-user.target
EOF
  fi
done

systemctl daemon-reload

for relay_spec in $RELAYS; do
  unit=$(relay_name "$relay_spec")
  if [ "$START" = "1" ]; then
    systemctl enable "autossh-tunnel-${unit}.service"
    systemctl start "autossh-tunnel-${unit}.service"
    systemctl status "autossh-tunnel-${unit}.service" --no-pager -n 5
  else
    echo "installed autossh-tunnel-${unit}.service but left it disabled"
    echo "after installing the relay-side block, run:"
    echo "  systemctl enable --now autossh-tunnel-${unit}.service"
  fi
done

cat <<'EOF'

Relay-side setup follows. Run the matching block on each relay before
starting Tier 1. It contains public keys only.
EOF

for relay_spec in $RELAYS; do
  relay=$(relay_host "$relay_spec")
  unit=$(relay_name "$relay_spec")
  client=$(client_name)
  relay_user="tunnel-${client}"
  relay_webui_user="tunnel-${client}-webui"
  tier1_pub=$(cat "$KEY_DIR/${unit}_tunnel.pub")
  tier2_pub=""
  permits=""

  if [ -n "$WEBUI_PORTS" ]; then
    tier2_pub=$(cat "$KEY_DIR/${unit}_tunnel_webui.pub")
    # permitlisten must name the RELAY-side port -- that is what sshd
    # actually binds. Using the local port here silently revokes the
    # forward the unit is asking for.
    for spec in $WEBUI_PORTS; do
      permits="${permits},permitlisten=\"127.0.0.1:$(relay_port "$spec")\""
    done
  fi

  cat <<EOF

# Relay: ${relay}
sudo useradd -r -m -d /home/${relay_user} -s /usr/sbin/nologin ${relay_user} 2>/dev/null || true
sudo passwd -l ${relay_user}
sudo -u ${relay_user} mkdir -p -m 700 /home/${relay_user}/.ssh
sudo -u ${relay_user} touch /home/${relay_user}/.ssh/authorized_keys
key_line='command="/bin/false",no-agent-forwarding,no-X11-forwarding,no-pty,no-user-rc,permitlisten="127.0.0.1:${TIER1_PORT}" ${tier1_pub}'
sudo -u ${relay_user} grep -qxF "\$key_line" /home/${relay_user}/.ssh/authorized_keys || printf '%s\n' "\$key_line" | sudo -u ${relay_user} tee -a /home/${relay_user}/.ssh/authorized_keys >/dev/null
sudo chmod 600 /home/${relay_user}/.ssh/authorized_keys
EOF

  if [ -n "$WEBUI_PORTS" ]; then
    cat <<EOF
sudo useradd -r -m -d /home/${relay_webui_user} -s /usr/sbin/nologin ${relay_webui_user} 2>/dev/null || true
sudo passwd -l ${relay_webui_user}
sudo -u ${relay_webui_user} mkdir -p -m 700 /home/${relay_webui_user}/.ssh
sudo -u ${relay_webui_user} touch /home/${relay_webui_user}/.ssh/authorized_keys
key_line='command="/bin/false",no-agent-forwarding,no-X11-forwarding,no-pty,no-user-rc${permits} ${tier2_pub}'
sudo -u ${relay_webui_user} grep -qxF "\$key_line" /home/${relay_webui_user}/.ssh/authorized_keys || printf '%s\n' "\$key_line" | sudo -u ${relay_webui_user} tee -a /home/${relay_webui_user}/.ssh/authorized_keys >/dev/null
sudo chmod 600 /home/${relay_webui_user}/.ssh/authorized_keys
EOF
  fi
done
