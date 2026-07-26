#!/bin/bash
# Phase 5 (script 01): installs the Tailscale client.
#
# Run this ON THE CLIENT DEVICE being enrolled (e.g. the Cloud Key) --
# NOT the VPS running the Headscale server (that's script 00). Enables
# and starts tailscaled, but does not run `tailscale up` -- joining the
# tailnet is a separate, deliberate step using a pre-auth key generated
# on the Headscale server (see Phase-5-Headscale Part 9).
#
# Safe to re-run: apt-get install is a no-op if already installed.
#
# Assumes Debian bullseye (matching this repo's Cloud Key). Substitute
# your own distro codename in both URLs below if different.

set -euo pipefail

DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl gnupg
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://pkgs.tailscale.com/stable/debian/bullseye.noarmor.gpg \
  -o /etc/apt/keyrings/tailscale-archive-keyring.gpg
chmod a+r /etc/apt/keyrings/tailscale-archive-keyring.gpg

repo="deb [signed-by=/etc/apt/keyrings/tailscale-archive-keyring.gpg]"
repo="$repo https://pkgs.tailscale.com/stable/debian bullseye main"
echo "$repo" > /etc/apt/sources.list.d/tailscale.list

apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y tailscale

echo
echo "tailscaled installed and running. Not joined to a tailnet yet."
systemctl is-enabled tailscaled
systemctl is-active tailscaled
echo
echo "Next: generate a pre-auth key on the Headscale server (make preauthkey"
echo "USER_ID=<id> EXPIRATION=1h), then on this device:"
echo "  tailscale up --login-server=https://<hs-domain> --authkey=<key>"
