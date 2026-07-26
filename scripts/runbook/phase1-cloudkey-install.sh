#!/bin/bash
# Phase 1, Step 3 (optional): replace the front-panel LCD app (ck-ui) with
# github.com/jnovack/cloudkey, pulling the pre-built binary straight from
# its latest GitHub release -- no cross-compile toolchain needed anywhere.
#
# Mirrors phase1-de-ubiquitizing.md Step 3 -- read that doc for the full
# why (the ck-ui purge prohibition, the systemd unit's hardening choices).
#
# Run this ON THE BOX ITSELF, any time after phase1-purge.sh's Step 1 has
# stopped/disabled ck-ui.service. Skip this script entirely if you don't
# want the LCD replaced.

set -euo pipefail

REPO="jnovack/cloudkey"

# curl isn't in the stock image; later phases install it too, but Step 3
# can run before those, so don't assume it's already there.
command -v curl >/dev/null 2>&1 || apt-get install -y curl

echo "=== Downloading latest cloudkey release ==="
curl -fsSL -o /root/cloudkey \
  "https://github.com/${REPO}/releases/latest/download/cloudkey-linux-arm"
chmod 755 /root/cloudkey

# The systemd unit + env template ship in the repo source, not as a
# release asset -- pull them from the tag the binary above actually came
# from, so the unit always matches the binary rather than whatever HEAD
# of the default branch happens to be.
TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep -m1 '"tag_name"' | cut -d'"' -f4)
if [ -z "$TAG" ]; then
  echo "ABORT: couldn't determine the latest release tag from the GitHub API -- stop here." >&2
  exit 1
fi
curl -fsSL -o /root/cloudkey.service \
  "https://raw.githubusercontent.com/${REPO}/${TAG}/cloudkey.service"
curl -fsSL -o /root/cloudkey.env.example \
  "https://raw.githubusercontent.com/${REPO}/${TAG}/cloudkey.env.example"

echo "=== Installing (tag $TAG) ==="
install -m 755 /root/cloudkey /usr/local/bin/cloudkey
install -m 644 /root/cloudkey.service /lib/systemd/system/cloudkey.service
# Don't clobber hand-edited config on a re-run of this script.
[ -f /etc/cloudkey.env ] || install -m 644 /root/cloudkey.env.example /etc/cloudkey.env
systemctl daemon-reload
systemctl enable --now cloudkey

echo
echo "=== Verify it actually opened the framebuffer ==="
sleep 2
journalctl -u cloudkey -n 20 --no-pager
echo
echo "Expect the correct resolution reported above (160x60 on this hardware),"
echo "not an error opening /dev/fb0. Edit /etc/cloudkey.env for tunnel/app"
echo "status rows, then: systemctl restart cloudkey"
