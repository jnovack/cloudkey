#!/bin/bash
# Phase 2, Part 5 (script 04): installs Prowlarr (the indexer manager).
#
# Does NOT require phase2-00-provision-drive.sh -- Prowlarr never
# touches /volume directly, only talks HTTP to the other apps, so it
# gets its own dedicated group instead of joining 'media'.
#
# Same GLIBC/SQLite risk as Radarr, per Prowlarr's own docs for this
# Debian version -- checked and fixed automatically if actually present,
# same pattern as phase2-03-radarr.sh.

set -euo pipefail

apt-get install -y curl sqlite3
id prowlarr >/dev/null 2>&1 || adduser --system --group --no-create-home prowlarr

cd /root
wget --content-disposition \
  'https://prowlarr.servarr.com/v1/update/master/updatefile?os=linux&runtime=netcore&arch=arm64'
tar -xzf Prowlarr.*.linux-core-arm64.tar.gz
rm -rf /opt/Prowlarr
mv Prowlarr /opt/
chown prowlarr:prowlarr -R /opt/Prowlarr

mkdir -p /var/lib/prowlarr
chown prowlarr:prowlarr /var/lib/prowlarr
chmod 775 /var/lib/prowlarr

cat << 'EOF' > /lib/systemd/system/prowlarr.service
[Unit]
Description=Prowlarr Daemon
After=syslog.target network.target
[Service]
User=prowlarr
Group=prowlarr
Type=simple
ExecStart=/opt/Prowlarr/Prowlarr -nobrowser -data=/var/lib/prowlarr/
TimeoutStopSec=20
KillMode=process
Restart=on-failure
[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now prowlarr
sleep 5

# Prowlarr's bundled SQLite helper needs a newer GLIBC than this OS
# ships. Do NOT gate this on a GLIBC error appearing in the log: unlike
# Radarr, Prowlarr starts cleanly with an unloadable bundled copy and
# logs nothing, so a log check silently skips the fix and leaves the
# problem to surface on some later upgrade instead. The heal script
# tests the library itself with `ldd`, which catches it either way.
apt-get install -y libsqlite3-0
install -m 755 "$(dirname "$0")/phase2-05-servarr-sqlite-heal.sh" \
  /usr/local/sbin/servarr-sqlite-heal.sh
/usr/local/sbin/servarr-sqlite-heal.sh Prowlarr

# Reapply on every start -- an in-app upgrade replaces /opt/Prowlarr
# wholesale and takes the symlink with it.
mkdir -p /etc/systemd/system/prowlarr.service.d
cat > /etc/systemd/system/prowlarr.service.d/sqlite-glibc.conf <<'EOF'
[Service]
ExecStartPre=/usr/local/sbin/servarr-sqlite-heal.sh Prowlarr
EOF

systemctl daemon-reload
systemctl restart prowlarr
sleep 5

echo
echo "Prowlarr installed."
systemctl is-active prowlarr
