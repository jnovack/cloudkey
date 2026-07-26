# Phase 9 — Backup and restore

Everything the earlier phases built — scripts, systemd units, per-service
drop-ins, the VPN tunnel config and namespace DNS, the autossh keys, and
each app's configuration database — is scattered across `/usr/local`,
`/etc`, `/var/lib`, and `/opt`. This phase captures all of it in one place
so a single command can put it back.

## What this protects against

Two scenarios, one tool:

- **A bad change you want to undo** — a media app's config edited into a
  broken state, a drop-in that won't start. Roll back to the last good
  snapshot.
- **A full rebuild from bare metal** — firmware-restore the Cloud Key, run
  phases 1–8 again to reinstall the OS baseline and the apps, then restore
  this backup to land back on your working configuration instead of
  reconfiguring every app by hand.

> [!IMPORTANT]
> This is a config/state backup, not a disk image. Restore assumes the
> base OS and phases 1–8 have already been re-run (packages installed,
> the app binaries reinstalled under `/opt`, `/volume` mounted) — it
> only lays your configuration back on top of that base.

That keeps the backup tiny (tens of MB, mostly the apps' SQLite
databases) and portable across a reflash.

## Where it goes, and why the SD card

Backups are written to `/sdcard` by default, which on boards with that
slot is a **separate physical device** from both the eMMC root filesystem
and any bulk data drive. A snapshot there survives wiping or reflashing
either of the other two — which is the whole point of a pre-rebuild
backup. It's best mounted as `ext4`, so `rsync` preserves permissions,
ownership, ACLs (extended file-permission rules), and symlinks exactly.

On a Cloud Key without `/sdcard` already mounted, mount removable storage
there first, or set `BACKUP_ROOT=/mnt/your-backup/cloudkey-backup` for
both backup and restore. The default remains `/sdcard/cloudkey-backup`.

## What is and isn't captured

Captured (kept in sync with each phase's own File manifest):

| Source | From |
| --- | --- |
| `/usr/local/bin`, `/usr/local/sbin` | every custom script, all phases (including these backup/restore scripts) |
| `/etc/systemd/system` + `/lib/systemd/system/cloudkey.service` | units, per-service drop-ins, enablement |
| `/etc/profile.d/zz-cloudkey-dashboard.sh` | interactive-login status dashboard loader |
| `/etc/cloudkey.env` | Phase 1 Step 3 LCD-app config (tunnel/app rows) |
| `/etc/wireguard`, `/etc/netns` | Phase 4 tunnel config + namespace DNS |
| `/etc/autossh` | Phase 3 tunnel keys + pinned `known_hosts` |
| `/etc/apt/keyrings/tailscale-*.gpg`, `/etc/apt/sources.list.d/tailscale.list`, `/var/lib/tailscale` | Phase 5 client (state included, so restore keeps the node identity) |
| `/var/lib/{nzbget,sonarr,radarr,prowlarr}` | Phase 2 app configs + databases |
| `/opt/docs` | on-box diagnostic runbook |

Deliberately **not** captured:

- **Media under `/volume`** — hundreds of GB, not configuration; it's the
  library, not the setup.
- **App binaries under `/opt/{Sonarr,Radarr,Prowlarr}`** — phase 2
  reinstalls these; backing them up would just bloat the snapshot.

The backup list is written by **directory** wherever possible, so the
real, deployment-specific filenames (your VPN config, your per-relay
autossh keys) are captured without the script naming them — nothing
device-specific is hardcoded.

## Backing up

Run the backup. By default it briefly stops the four media apps
(`nzbget`, `sonarr`, `radarr`, `prowlarr`) so their live SQLite databases
are captured in a consistent state, then restarts them:

```bash
phase9-backup.sh
BACKUP_ROOT=/mnt/backup/cloudkey-backup phase9-backup.sh
```

Each run creates a timestamped snapshot under
`/sdcard/cloudkey-backup/<YYYYmmdd-HHMMSS>/` (or under `BACKUP_ROOT`),
updates a `latest` symlink to point at it, writes a `MANIFEST.txt` (time,
host, the exact captured path list, skipped missing paths, rsync result),
and prunes to the most recent seven snapshots. Pass `--no-quiesce` for a
faster live copy that doesn't touch the apps, at the cost of a possibly
slightly-stale database in the snapshot.

The backup refuses to run if the destination would land on the root
filesystem, because the failure it's guarding against is a silent one:
`/sdcard` exists as an empty mountpoint directory whether or not the card
is actually mounted, so a backup started with the card unmounted would
quietly fill the eMMC root instead — and be lost with it in exactly the
reflash scenario the backup exists for. Mount the card first:

```bash
findmnt /sdcard || mount /dev/mmcblk1p1 /sdcard
phase9-backup.sh
```

Any `BACKUP_ROOT` on a real mounted filesystem passes the check on its own
— including `/tmp` where it's a tmpfs, which is the easiest way to smoke-test
the script without backup media attached. `--allow-rootfs` is only needed to
force a throwaway run onto the root filesystem itself:

```bash
BACKUP_ROOT=/tmp/cloudkey-backup phase9-backup.sh --no-quiesce
```

**Packaged version**: [`scripts/runbook/phase9-backup.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase9-backup.sh),
deployed at `/usr/local/sbin/phase9-backup.sh`. The path list at the top
of the script is the single source of truth — when a later change adds a
new custom file, add its path (or its parent directory) there.

## Restoring

> [!WARNING]
> Restoring overwrites live configuration and app databases on the box
> with whatever is in the snapshot, including the four media apps'
> state. Run with `--dry-run` first (below) to see exactly what would
> change before committing to it.

Preview first — a dry run itemizes exactly what would change and touches
nothing, so it's safe on a live box:

```bash
phase9-restore.sh --dry-run
```

Then restore. With no path it uses a promoted `good` snapshot if one
exists, otherwise `latest`; you can also name a specific snapshot
directory:

```bash
phase9-restore.sh                                  # good, else latest
phase9-restore.sh /sdcard/cloudkey-backup/20260717-223742
BACKUP_ROOT=/mnt/backup/cloudkey-backup phase9-restore.sh --dry-run
```

It confirms before doing anything (type `RESTORE`, or pass `--yes` for an
unattended run), stops the services that own the files being overwritten,
`rsync`s the snapshot back onto `/` preserving all metadata, and reloads
systemd. It **never deletes** anything outside the backed-up paths (no
`rsync --delete`).

**Finish with a reboot.** The restore recommends it (or does it for you
with `--reboot`) because a reboot is the reliable way to bring the
namespace → VPN tunnel → netns-pinned apps back up in the correct order
via their own enabled boot units, rather than racing them by hand. The
restore script deliberately doesn't hardcode the VPN/namespace/autossh
unit names for exactly this reason — it restarts only the
deployment-agnostic `tailscaled` itself and leaves the ordered stack to
boot.

**Packaged version**: [`scripts/runbook/phase9-restore.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase9-restore.sh),
deployed at `/usr/local/sbin/phase9-restore.sh`.

### Promoting a "known good" snapshot

Restore prefers a `good` snapshot over `latest` when one exists. Marking a
tested snapshot as known-good is just repointing that symlink:

```bash
ln -sfn <YYYYmmdd-HHMMSS> /sdcard/cloudkey-backup/good
```

The reset-button-press recovery flow (which selects and restores the last
known-good snapshot automatically) is handled outside these scripts and is
out of scope here — this phase only guarantees a complete backup and a
correct restore for it to call.

## Verifying a restore

After the reboot:

```bash
phase1-verify.sh      # base OS sanity
vpn-verify-netns.sh   # VPN kill switch + no-leak proof (Phase 4)
tailscale status      # coordination tunnel reconnected
```

## File manifest

| Path | Purpose |
| --- | --- |
| `/usr/local/sbin/phase9-backup.sh` | timestamped config/state backup to `/sdcard`; source of truth is [`scripts/runbook/phase9-backup.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase9-backup.sh) |
| `/usr/local/sbin/phase9-restore.sh` | restore (with `--dry-run`) from a snapshot; source of truth is [`scripts/runbook/phase9-restore.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase9-restore.sh) |
| `/sdcard/cloudkey-backup/<stamp>/` | one snapshot: the captured tree under full paths, plus `MANIFEST.txt` |
| `/sdcard/cloudkey-backup/latest` | symlink to the newest snapshot |
| `/sdcard/cloudkey-backup/good` | optional symlink to a promoted known-good snapshot (restore prefers it) |
