#!/bin/bash
# Return the Cloud Key to its SAFE state: password login locked on every
# privileged account and SSH restricted to keys only. This is the normal
# steady state after Phase 1's security hardening -- run it any time to
# reassert it.
#
# Paired with security-unlock.sh, which the reset-button handler in the
# separate cloudkey front-panel app calls to grant emergency root password
# access. After getting in that way, run THIS to re-secure the box.
#
# Why locking still leaves key access working: passwd -l only prepends '!'
# to the stored hash, which blocks PASSWORD auth -- SSH public-key auth
# doesn't consult the hash, so root and cloudkey still log in by key. That
# is deliberate: it's the fallback that keeps the key-only safe state from
# being a lockout.
#
# Idempotent: safe to run repeatedly.
set -euo pipefail

DROPIN=/etc/ssh/sshd_config.d/10-security.conf
ACCOUNTS=(root ubnt cloudkey)

[[ $EUID -eq 0 ]] || { echo "must run as root" >&2; exit 1; }

# 1. Lock the password on every listed account that actually exists. 'ubnt'
#    is included for the general case; it simply doesn't exist on every box.
for u in "${ACCOUNTS[@]}"; do
  if id "$u" >/dev/null 2>&1; then
    passwd -l "$u" >/dev/null && echo "locked password: $u"
  fi
done

# 2. SSH policy drop-in: key-only, root by key only. A SINGLE managed file
#    that lock/unlock overwrite in full -- not an override layered on top --
#    so the current mode is whatever this file says, with no sshd
#    first-match-wins ordering to reason about.
cat > "$DROPIN" <<'EOF'
# Managed by security-lock.sh / security-unlock.sh -- do not hand-edit.
# SAFE STATE: key-only SSH; password login disabled; root by key only
# (prohibit-password still permits root's key login as a fallback).
#
# Both KbdInteractive* and ChallengeResponse* are set: OpenSSH renamed the
# option in 8.7, and bullseye's 8.4 only honours the OLD name. Leaving it
# out on 8.4 is not cosmetic -- with UsePAM yes, keyboard-interactive can
# tunnel a password login even while PasswordAuthentication is no.
PasswordAuthentication no
KbdInteractiveAuthentication no
ChallengeResponseAuthentication no
PermitRootLogin prohibit-password
EOF

# Validate before reloading so a bad drop-in can never wedge sshd.
if ! sshd -t 2>"/tmp/sshd-lock-test.$$"; then
  echo "FATAL: sshd config invalid, not reloading:" >&2
  cat "/tmp/sshd-lock-test.$$" >&2; rm -f "/tmp/sshd-lock-test.$$"
  exit 1
fi
rm -f "/tmp/sshd-lock-test.$$"
systemctl reload ssh 2>/dev/null || systemctl reload sshd
echo "SAFE state applied: key-only SSH ($DROPIN)"
