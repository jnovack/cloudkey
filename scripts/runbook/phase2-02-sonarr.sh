#!/bin/bash
# Phase 2, Part 3 (script 02): installs Sonarr (TV shows).
#
# Requires phase2-00-provision-drive.sh to have run first -- Sonarr's
# installer assigns its new user to the 'media' group at creation time.
#
# Sonarr's official installer reads its user/group prompts from
# /dev/tty directly, bypassing stdin -- see Phase-2-Overview for why
# piping answers in doesn't work here. This patches those two `read`
# lines out of the downloaded script before running it, replacing them
# with fixed values, rather than trying to answer them interactively.

set -euo pipefail

if ! getent group media >/dev/null; then
  echo "error: 'media' group doesn't exist -- run phase2-00-provision-drive.sh first" >&2
  exit 1
fi

SCRIPT=/tmp/install-sonarr.sh
curl -fsSL -o "$SCRIPT" \
  https://raw.githubusercontent.com/Sonarr/Sonarr/develop/distribution/debian/install.sh

sed -i '/read.*-p.*app_uid/d; /read.*-p.*app_guid/d' "$SCRIPT"
sed -i '1a app_uid="sonarr"\napp_guid="media"' "$SCRIPT"

if grep -q 'read.*-p.*app_u\|read.*-p.*app_g' "$SCRIPT"; then
  echo "error: patching the Sonarr installer didn't remove all interactive prompts -- upstream script may have changed, check $SCRIPT by hand before running it" >&2
  exit 1
fi

bash "$SCRIPT"

mkdir -p /etc/systemd/system/sonarr.service.d
cat > /etc/systemd/system/sonarr.service.d/volume-mount.conf <<'EOF'
[Unit]
RequiresMountsFor=/volume
EOF

systemctl daemon-reload
systemctl restart sonarr

echo
echo "Sonarr installed."
systemctl is-active sonarr
