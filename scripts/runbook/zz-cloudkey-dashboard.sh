#!/bin/bash
# Sourced by /etc/profile.d on every login shell -- including the
# non-interactive cases (`ssh host 'command'`, scp, rsync, cron), which all
# source /etc/profile too since OpenSSH runs the shell as a login shell
# either way. The three checks below are what actually tells those cases
# apart from a real interactive login, in order:
#   1. $-  contains 'i' only for a genuinely interactive shell.
#   2. [ -t 1 ]  stdout is a real terminal, not a pipe/redirect/file.
#   3. tput colors -ge 8   the terminal actually supports color, so this
#      never dumps a wall of raw escape codes on a dumb one.
# Confirmed by test: a plain `ssh <host> 'echo test'` prints only its own
# output, nothing from the dashboard, even though /etc/profile is sourced
# in both cases -- check 1 is what draws the actual line.
#
# The "zz-" prefix keeps this last among /etc/profile.d scripts (run in
# lexical order), so it fires after anything else in there has finished
# setting up the environment.

case "$-" in
  *i*) ;;
  *) return 2>/dev/null || exit 0 ;;
esac

[ -t 1 ] || return 2>/dev/null || exit 0

colors="$(tput colors 2>/dev/null || echo 0)"
[ "$colors" -ge 8 ] || return 2>/dev/null || exit 0

command -v cloudkey-dashboard >/dev/null 2>&1 && cloudkey-dashboard
