#!/bin/bash
# Phase 1 base tooling: packages later phases depend on but that aren't in
# the stock UniFi OS image. Installed here, as part of the baseline, so a
# freshly de-Ubiquitized box is ready for every later phase in one place --
# rather than each phase apt-installing its own prerequisites ad hoc.
#
#   rsync       -- file sync (e.g. Phase-2 completed-download offload to an
#                  NFS share); pulls nothing surprising.
#   nfs-common  -- NFS client (mount.nfs / mount.nfs4). Pulls rpcbind,
#                  keyutils, libnfsidmap2.
#
# Safe to re-run: apt-get install is idempotent, so this doubles as a
# "confirm the baseline is present" check.
set -euo pipefail

PACKAGES="rsync nfs-common"

apt-get update

# Same simulate-first habit as the rest of Phase 1: a base-release install
# removes nothing, but confirm that before committing.
echo "== simulate (expect '0 to remove') =="
apt-get install --simulate $PACKAGES

echo "== install =="
DEBIAN_FRONTEND=noninteractive apt-get install -y $PACKAGES

echo "== verify =="
command -v rsync
command -v mount.nfs
echo "base tooling present"
