#!/bin/bash
# Phase 1 security hardening: create the unprivileged admin user 'cloudkey'
# with passwordless sudo and key-only login, then hand off to
# security-lock.sh to lock the privileged accounts and restrict SSH to keys.
#
# End state:
#   - user 'cloudkey' exists, in the sudo group, with NOPASSWD sudo
#   - root / ubnt / cloudkey passwords are LOCKED (no password login anywhere)
#   - SSH is key-only; root may still log in by key as a fallback
#
# The reset-button handler (separate cloudkey app) can later call
# security-unlock.sh for emergency root password access; security-lock.sh
# returns to this safe state.
#
# Bootstrap safety: the safe state keeps root SSH-by-key working, and this
# script copies root's existing authorized_keys to the new cloudkey user if
# cloudkey has none yet -- so applying it can't strand you. Pass a public
# key as the first argument to install that instead of copying root's.
#
# Idempotent: re-running reconciles the user, sudoers drop-in and keys.
set -euo pipefail

USER_NAME=cloudkey
SUDOERS=/etc/sudoers.d/cloudkey
PUBKEY="${1:-}"

[[ $EUID -eq 0 ]] || { echo "must run as root" >&2; exit 1; }

# 1. Create the user (home + bash shell) if absent; ensure sudo group.
if ! id "$USER_NAME" >/dev/null 2>&1; then
  useradd -m -s /bin/bash "$USER_NAME"
  echo "created user: $USER_NAME"
fi
usermod -aG sudo "$USER_NAME"

# 2. Passwordless sudo via a drop-in, validated with visudo BEFORE install
#    so a syntax slip can never leave sudo unusable.
tmp="$(mktemp)"
echo "$USER_NAME ALL=(ALL) NOPASSWD:ALL" > "$tmp"
if visudo -cf "$tmp" >/dev/null; then
  install -m 0440 -o root -g root "$tmp" "$SUDOERS"
  echo "installed sudoers drop-in: $SUDOERS"
else
  echo "FATAL: generated sudoers file failed validation" >&2
  rm -f "$tmp"; exit 1
fi
rm -f "$tmp"

# 3. SSH key. The safe state is key-only, so cloudkey MUST have a key or the
#    box would accept only root logins. Prefer an explicitly supplied key;
#    otherwise bootstrap from root's authorized_keys (the key you're already
#    using to administer the box).
home="$(getent passwd "$USER_NAME" | cut -d: -f6)"
install -d -m 700 -o "$USER_NAME" -g "$USER_NAME" "$home/.ssh"
ak="$home/.ssh/authorized_keys"
if [[ -n "$PUBKEY" ]]; then
  echo "$PUBKEY" >> "$ak"
  echo "added supplied public key to $ak"
elif [[ ! -s "$ak" && -s /root/.ssh/authorized_keys ]]; then
  cat /root/.ssh/authorized_keys >> "$ak"
  echo "bootstrapped $ak from root's authorized_keys"
fi
touch "$ak"
chown "$USER_NAME:$USER_NAME" "$ak"; chmod 600 "$ak"

if [[ ! -s "$ak" ]]; then
  echo "REFUSING to lock down: $USER_NAME has no authorized_keys and none" >&2
  echo "was supplied. Re-run with a key:" >&2
  echo "    phase1-security-setup.sh 'ssh-ed25519 AAAA...'" >&2
  exit 1
fi

# 4. Apply the safe state (locks root/ubnt/cloudkey passwords, key-only SSH).
#    Both scripts deploy side by side in /usr/local/sbin.
LOCK="$(dirname "$0")/security-lock.sh"
[[ -x "$LOCK" ]] || LOCK=/usr/local/sbin/security-lock.sh
exec "$LOCK"
