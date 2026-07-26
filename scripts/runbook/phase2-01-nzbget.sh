#!/bin/bash
# Phase 2, Part 2 (script 01): installs NZBGet (the downloader).
#
# Requires phase2-00-provision-drive.sh to have run first -- this joins
# nzbget to the 'media' group it creates. Safe to re-run: apt-get
# install is a no-op if already installed, and the MainDir/password
# changes are idempotent (re-running just re-sets them to the same or a
# fresh password).

set -euo pipefail

if ! getent group media >/dev/null; then
  echo "error: 'media' group doesn't exist -- run phase2-00-provision-drive.sh first" >&2
  exit 1
fi

apt-get install -y ca-certificates curl gnupg
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://nzbgetcom.github.io/nzbgetcom.asc \
  -o /etc/apt/keyrings/nzbgetcom.asc
chmod a+r /etc/apt/keyrings/nzbgetcom.asc

repo="deb [arch=all signed-by=/etc/apt/keyrings/nzbgetcom.asc]"
repo="$repo https://nzbgetcom.github.io/deb stable main"
echo "$repo" > /etc/apt/sources.list.d/nzbgetcom.list

apt-get update
apt-get install -y nzbget

usermod -a -G media nzbget
chown -R nzbget:media /volume/downloads

sed -i 's|^MainDir=.*|MainDir=/volume/downloads|' /var/lib/nzbget/nzbget.conf

newpass=$(openssl rand -base64 24 | tr -dc 'A-Za-z0-9' | head -c 24)
sed -i "s|^ControlPassword=.*|ControlPassword=$newpass|" /var/lib/nzbget/nzbget.conf

mkdir -p /etc/systemd/system/nzbget.service.d
cat > /etc/systemd/system/nzbget.service.d/volume-mount.conf <<'EOF'
[Unit]
RequiresMountsFor=/volume
EOF

systemctl daemon-reload
systemctl restart nzbget

echo
echo "NZBGet installed. Control password (write this down, you'll need it in Part 6): $newpass"
systemctl is-active nzbget
