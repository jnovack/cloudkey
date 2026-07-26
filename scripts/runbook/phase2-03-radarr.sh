#!/bin/bash
# Phase 2, Part 4 (script 03): installs Radarr (movies).
#
# Requires phase2-00-provision-drive.sh to have run first -- adduser
# --ingroup media below requires that group to already exist.
#
# Radarr bundles a database helper built against a newer GLIBC than
# Debian 11 ships -- see Phase-2-Overview for the full story. This
# checks for that specific failure after starting the service and
# applies the documented fix automatically if it's actually present,
# rather than assuming either way.

set -euo pipefail

if ! getent group media >/dev/null; then
  echo "error: 'media' group doesn't exist -- run phase2-00-provision-drive.sh first" >&2
  exit 1
fi

apt-get install -y curl sqlite3
id radarr >/dev/null 2>&1 || adduser --system --no-create-home --ingroup media radarr

cd /root
wget --content-disposition \
  'https://radarr.servarr.com/v1/update/master/updatefile?os=linux&runtime=netcore&arch=arm64'
tar -xzf Radarr.*.linux-core-arm64.tar.gz
rm -rf /opt/Radarr
mv Radarr /opt/
chown radarr:media -R /opt/Radarr

mkdir -p /var/lib/radarr
chown radarr:media /var/lib/radarr
chmod 775 /var/lib/radarr

cat << 'EOF' > /lib/systemd/system/radarr.service
[Unit]
Description=Radarr Daemon
After=syslog.target network.target
[Service]
User=radarr
Group=media
Type=simple
ExecStart=/opt/Radarr/Radarr -nobrowser -data=/var/lib/radarr/
TimeoutStopSec=20
KillMode=process
Restart=on-failure
[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now radarr
sleep 5

if journalctl -u radarr -n 30 --no-pager | grep -q 'GLIBC_2.33.*libe_sqlite3\|libe_sqlite3.*GLIBC_2.33'; then
  echo "GLIBC/SQLite mismatch detected -- applying the documented fix."
  apt-get install -y libsqlite3-0
  cd /opt/Radarr
  [ -f libe_sqlite3.so.backup ] || mv libe_sqlite3.so libe_sqlite3.so.backup
  ln -sf /usr/lib/aarch64-linux-gnu/libsqlite3.so.0 libe_sqlite3.so
  systemctl restart radarr
  sleep 5
fi

mkdir -p /etc/systemd/system/radarr.service.d
cat > /etc/systemd/system/radarr.service.d/volume-mount.conf <<'EOF'
[Unit]
RequiresMountsFor=/volume
EOF

# The symlink above does not survive an in-app upgrade -- those replace
# /opt/Radarr wholesale. Reapply it before every start so an upgrade
# can't leave Radarr hung at "Press enter to exit..." (which systemd
# reports as "active (running)"). See phase2-05-servarr-sqlite-heal.sh.
install -m 755 "$(dirname "$0")/phase2-05-servarr-sqlite-heal.sh" \
  /usr/local/sbin/servarr-sqlite-heal.sh
cat > /etc/systemd/system/radarr.service.d/sqlite-glibc.conf <<'EOF'
[Service]
ExecStartPre=/usr/local/sbin/servarr-sqlite-heal.sh Radarr
EOF

systemctl daemon-reload
systemctl restart radarr

echo
echo "Radarr installed. In-app upgrades are self-repairing -- see Phase-2-Apps Part 8."
systemctl is-active radarr
