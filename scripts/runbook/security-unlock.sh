#!/bin/bash
# EMERGENCY access. Meant to be invoked by the reset-button handler in the
# separate cloudkey front-panel app on a physical button hold, so someone
# with physical access who has lost their SSH key can still get back in.
#
# It re-enables root PASSWORD login and unlocks root's EXISTING password.
# It deliberately does NOT set a password -- it only strips the lock that
# security-lock.sh applied, restoring whatever password root had before. If
# root never had a password, this grants nothing; set one first.
#
# This is the LESS-safe state. Once back in, run security-lock.sh to return
# the box to key-only.
#
# Idempotent: safe to run repeatedly.
set -euo pipefail

DROPIN=/etc/ssh/sshd_config.d/10-security.conf

[[ $EUID -eq 0 ]] || { echo "must run as root" >&2; exit 1; }

# Unlock root's existing password hash (passwd -u strips the leading '!'
# that passwd -l added). Second field of `passwd -S` is the status: L=locked.
if passwd -S root | awk '{exit ($2=="L")?0:1}'; then
  passwd -u root >/dev/null && echo "unlocked root password"
else
  echo "root password already unlocked (or none set)"
fi

# Emergency SSH policy: same single managed drop-in, rewritten to allow
# root password login.
cat > "$DROPIN" <<'EOF'
# Managed by security-lock.sh / security-unlock.sh -- do not hand-edit.
# EMERGENCY STATE (reset-button unlock): root password login re-enabled.
# Re-secure with:  security-lock.sh
# Both option spellings set for the same reason as security-lock.sh: 8.4
# honours ChallengeResponse*, 8.7+ honours KbdInteractive*.
PasswordAuthentication yes
KbdInteractiveAuthentication yes
ChallengeResponseAuthentication yes
PermitRootLogin yes
EOF

if ! sshd -t 2>"/tmp/sshd-unlock-test.$$"; then
  echo "FATAL: sshd config invalid, not reloading:" >&2
  cat "/tmp/sshd-unlock-test.$$" >&2; rm -f "/tmp/sshd-unlock-test.$$"
  exit 1
fi
rm -f "/tmp/sshd-unlock-test.$$"
systemctl reload ssh 2>/dev/null || systemctl reload sshd
echo "EMERGENCY state applied: root password login enabled."
echo "Re-secure when done:  security-lock.sh"
