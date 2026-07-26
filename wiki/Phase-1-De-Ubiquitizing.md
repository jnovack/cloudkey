# De-Ubiquitizing a UniFi Cloud Key Gen2 Plus (and the mistake that bricked one)

The UCK-G2-Plus is a genuinely nice piece of hardware for $200: powered over
Ethernet (PoE, so one cable does both data and power), 64-bit ARM (ARM64), a
2.5" drive bay, a battery for clean shutdown on power loss, and a 160×60
color LCD on the front. Under the hood it's just Debian 11. This is a
complete guide to stripping the UniFi software off one and turning it into a
generic Debian server — including the exact mistake that bricked the first
attempt, so you don't repeat it.

## Hardware and OS overview

- SoC (system-on-chip): Qualcomm APQ8053, aarch64 — the kernel's name for
  the same 64-bit ARM architecture called "ARM64" above. The board's
  base-files package gives the exact model away —
  `cloudkey-plus-apq8053-base-files` on the Gen2 **Plus**,
  `cloudkey-g2-apq8053-base-files` on the plain Gen2. The SoC, kernel, package
  set, and every command below are identical across the two; only that package
  name and the Plus's internal drive bay (Step 7) differ, so this guide applies
  to both — substitute your own base-files name wherever the `-plus-` one
  appears.
- OS: Debian 11 (bullseye), kernel `3.18.44-ui-qcom`.
- This is the **newer "native" UniFi OS** architecture — no Docker
  involved. Every UniFi component is an ordinary `.deb` package installed on
  top of stock Debian, not baked into a locked firmware image, and `apt`
  sources are plain Debian (`deb.debian.org`) with no Ubiquiti repo
  configured. This is what makes de-Ubiquitizing tractable at all.
- Storage: internal eMMC (`mmcblk0`, ~29G) holds the boot/rootfs in a
  Qualcomm A/B-style partition layout — **never touch this**, it's the
  bootloader and recovery path. A separate internal SATA/SSD (`sda`, up to
  the drive's full size) is what UniFi OS partitions for its own swap,
  system volumes, and a large `/volume` partition for bulk storage
  (Protect footage, backups, etc. — check what's actually on yours before
  reusing it).
- Front panel: a 160×60, 16bpp framebuffer at `/dev/fb0` (driver
  `fb_sp8110`), plus two controllable LEDs at `/sys/class/leds/blue` and
  `/sys/class/leds/white`.
- There's a genuine **serial console header on the board: J22, 3.3V TTL** —
  but you cannot access it without opening the device.
- Battery: internal, used for exactly one thing — giving the box enough
  runtime to shut down cleanly if PoE/USB power is cut. See the deep dive
  below; it's a much simpler mechanism than it sounds.

## Before you start

1. **Have a recovery path that doesn't depend on the network.** Accept that
   your fallback is a full factory restore via Ubiquiti's firmware/recovery
   mode, which wipes the device back to stock. SSH-only access with no
   console fallback is how a small mistake turns into "start over from
   scratch."
2. **Enable SSH** via System Settings on the device first, and confirm you
   can log in as `root` with your key before doing anything else.
3. **Back up the package/unit state**, even though it's not a true restore
   mechanism (a factory reset wipes it too — it's just useful context if
   something goes wrong before that point):

   ```bash
   ssh root@<device-ip> "dpkg --get-selections > /root/pre-cleanup-dpkg-selections.txt"
   ssh root@<device-ip> "systemctl list-unit-files --state=enabled > /root/pre-cleanup-enabled-units.txt"
   ```

4. **Check what's actually on your `/volume` partition** before assuming
   it's safe to reclaim — if this device was ever running UniFi Protect,
   there may be camera footage or backups on it.

## The danger zone: what not to remove

This is the section that would have saved a full factory reset. Read it
before running anything.

`apt-get purge` **silently removes anything that depends on what you list**,
with no prompt — unlike plain `apt remove`/`apt purge` run interactively,
which shows "the following additional packages will be REMOVED" before
acting. If you don't simulate first, you won't see this happening until
it's too late.

Two packages must **never** be purged, because of real dependency chains
(not guesses — confirmed via `apt-cache depends`):

```text
base-files-deps-common   Depends: ubnt-tools
cloudkey-plus-apq8053-base-files   Depends: base-files-deps-common

ck-splash   Depends: ck-ui | <udr-ui>      (nothing else provides udr-ui)
cloudkey-apq8053-initramfs   Depends: ck-splash
```

> [!WARNING]
> Purging `ubnt-tools` cascades into removing
> `cloudkey-plus-apq8053-base-files` (or `cloudkey-g2-apq8053-base-files` on
> the plain Gen2 — same chain, model-specific name). Purging `ck-ui`
> cascades into removing `cloudkey-apq8053-initramfs` (not model-specific —
> the same package on both). Losing either is very likely to brick the
> device because SSH is either turned off or unavailable (hard to tell).

The first of those base-files packages is this board's actual
`base-files`-equivalent package (apt/dpkg hooks, sysctl tuning, SSH PAM
`usermap` config, and — critically — the entire power-loss protection
chain, see below). The second, the initramfs package, manages the
initramfs the board boots from. This is exactly what happened on the
first attempt: the transaction silently swept up both packages alongside
the intended UniFi removal, the box came back from a manual reboot **not
even responding to `ping`** — a boot failure, not just a stopped service —
and a full firmware-mode factory restore was required.

Neither `ck-ui` nor `ubnt-tools` need to actually be removed to get a clean
result. `ck-ui` is Ubiquiti's front-panel LCD app — replace it with your
own program at the *service* level (stop/disable `ck-ui.service`, run your
own systemd unit instead) and leave the package installed but dormant.
`ubnt-tools` mainly provides `infctld`, which — despite the tempting
"emergency" systemd unit name — is just Ubiquiti's **network discovery
daemon** (UDP 10001 discovery + CDP advertisements), unrelated to anything
critical. Disable its service if you don't want it; don't purge the
package.

## Deep dive: how the power-loss protection actually works

This is worth understanding on its own, because it's simpler — and more
generically useful — than "a closed-source binary listening for
interrupts." The whole chain:

1. The kernel exposes standard Linux `power_supply` class devices at
   `/sys/class/power_supply/` (`battery`, `ext_battery`, `mains`, `usb`,
   `typec` — via the Qualcomm `qpnp-smbcharger` PMIC driver). When PoE/USB
   power is lost, the relevant supply's `online` attribute flips to `0`.
2. A udev rule, `/lib/udev/rules.d/40-powerloss.rules`, owned by
   `cloudkey-plus-apq8053-base-files`:

   ```text
   # Ubiquiti APQ8053 Cloudkey powerloss
   ACTION=="change", SUBSYSTEM=="power_supply", ATTR{online}=="0", \
     RUN+="/bin/kill -SIGPWR 1"
   ```

   catches that change and sends `SIGPWR` straight to PID 1.
3. `sigpwr.target` is a **stock systemd special target** (part of systemd
   itself, `man systemd.special`), which systemd reaches on receiving that
   signal — nothing Ubiquiti-specific here at all.
4. `device-powerloss.service` (`WantedBy=sigpwr.target`, also owned by
   `cloudkey-plus-apq8053-base-files`) runs on reaching that target.
5. That "binary" is actually a one-line POSIX shell script:

   ```bash
   systemctl --no-block poweroff
   ```

That's it. The battery's whole job is to buy enough time for a completely
ordinary `systemctl poweroff` to run — systemd's normal shutdown ordering
stops every running service (sending SIGTERM, letting each one flush and
exit) before finally unmounting filesystems and halting. There's no bespoke
data-safety logic to worry about breaking. And because it's just "cleanly
shut down whatever's currently running," **this protection keeps working
for free** for whatever you run on the box afterward — it was never
specific to UniFi's own software in the first place.

> [!WARNING]
> Never purge the board's base-files package
> (`cloudkey-plus-apq8053-base-files`, or `cloudkey-g2-apq8053-base-files` on
> the plain Gen2), and never delete or disable `40-powerloss.rules` or
> `device-powerloss.service`. There is no legitimate reason removing the
> UniFi application layer would ever need to touch this — it isn't a UniFi
> package, and losing it means no clean shutdown the next time PoE or USB
> power drops.

## Step-by-step removal process

### Step 0 — Simulate before every real purge

This is the single highest-leverage habit in this whole process:

```bash
apt-get purge --simulate <package list>
```

Read the *entire* "will be REMOVED" list, not just the packages you typed.
If anything unexpected shows up — especially anything with "initramfs",
"base-files", "bootloader", "kernel", or a board/model name in it — stop
and run `apt-cache depends <that package>` to find out *why* it's being
pulled in, rather than guessing or forcing past it. That single check
would have prevented the entire incident described above.

### Step 1 — Disable the UniFi systemd units

```bash
for u in ck-splash-reboot.service ck-splash-shutdown.service \
         ck-ui.service infctld-emergency.service infctld.service \
         ubnt-systemhub.service ubnt-unifi-setup.service \
         ucore-setup-listener.service ucs-agent.service uhwd.service \
         uid-agent.service ulp-go.service unifi-core.service \
         unifi-directory.service unifi-identity-update.service \
         "unifi-sdcard@.service" unifi.service uos-agent.service \
         uos-discovery-client.service usd.service usdbd.service \
         ubnt-dpkg-restore.service; do
  systemctl disable --now "$u"
done
```

A couple of things to expect here, both harmless:

- `unifi-sdcard@.service` is a systemd template unit; you'll get a
  "missing the instance name" error trying to stop the bare template. The
  actual enablement symlink still gets removed correctly.
- `ubnt-dpkg-restore.service` is worth specifically calling out: it's
  gated by `ConditionPathExists=/boot/.fwupdate`, so it's normally a no-op
  (it only fires right after an OTA firmware update). Still disable it —
  its only job is reinstalling the exact packages you're about to remove.

Verify:

```bash
systemctl list-unit-files --state=enabled | \
  grep -iE 'unifi|ubnt|uos-|ucs-|uid-agent|ulp-go|^ck|infctld|^usd'
```

Should return nothing except `ubnt-zram-swap.service` (a generic zram swap
setup script worth keeping regardless of UniFi).

### Step 2 — Purge, in small batches, with a liveness check between each

The corrected package list (`ck-ui` and `ubnt-tools` deliberately excluded,
per the danger-zone section above):

```text
unifi-assets-uckp unifi-assets-uckg2
unifi-email-templates-all python3-unifi-console-protos
mongodb-server mongodb-clients mongodb-server-core
unifi unifi-core
unifi-directory unifi-identity-update uid-agent ucs-agent uos-agent
uos-discovery-client uos ulp-go
ustd ubnt-systemhub ubnt-unifi-setup ucore-setup-listener
```

Package names vary a little by Cloud Key generation (`unifi-assets-uckp`
versus `unifi-assets-uckg2`, for example). Simulate the list that is
actually installed or left in config-only state on your box, confirm it
removes only approved targets and nothing else, then run each batch for
real:

```bash
apt-get purge -y <batch>
ssh root@<device-ip> uptime   # confirm still alive before the next batch
```

Don't run this as one 20-package command. If a batch fails or the box
stops responding, you want to know which 3-8 packages were involved, not
which 20. In practice `apt` will sometimes resolve dependency ordering
differently than your manual batching expects (e.g. `unifi-core` getting
pulled into an earlier batch because something in that batch depended on
it) — that's fine as long as everything pulled forward was already on your
approved list.

Finish with:

```bash
apt-get --purge autoremove -y
```

Verify:

```bash
dpkg -l | grep -iE 'unifi|ubnt|uos-|ucs-|uid-agent|ulp-go'
```

Should show only the deliberately-kept packages: `ck-ui`, `ubnt-tools`,
`ubnt-zram-swap`, `ubnt-disk-smart-mon`, `ubnt-archive-keyring`, and any
`~ubnt`-suffixed builds of stock tools like `jq` (functionally identical to
Debian's own build, not worth replacing).

### Step 3 — Replace the front-panel LCD app (optional)

If you want to keep the LCD useful, `ck-ui` needs a replacement — several
exist on GitHub (e.g. [jnovack/cloudkey](https://github.com/jnovack/cloudkey)).
That project publishes a pre-built `cloudkey-linux-arm` binary with every
release, so no cross-compile toolchain is needed on the device or off it —
download it directly on the box, rename it, and install:

```bash
curl -fsSL -o /root/cloudkey \
  https://github.com/jnovack/cloudkey/releases/latest/download/cloudkey-linux-arm
chmod 755 /root/cloudkey
```

> [!NOTE]
> Only one architecture is published (`cloudkey-linux-arm`, 32-bit). It
> runs unmodified on the Gen2 Plus covered here because this board's
> Qualcomm SoC supports the AArch32 execution state alongside its native
> 64-bit one — there is no separate `arm64` build to look for.

The systemd unit and an env template live in the repo itself rather than
as release assets — pull the copies matching the release you just
downloaded, using the tag from the GitHub API:

```bash
TAG=$(curl -fsSL https://api.github.com/repos/jnovack/cloudkey/releases/latest \
  | grep -m1 '"tag_name"' | cut -d'"' -f4)
curl -fsSL -o /root/cloudkey.service \
  https://raw.githubusercontent.com/jnovack/cloudkey/$TAG/cloudkey.service
curl -fsSL -o /root/cloudkey.env.example \
  https://raw.githubusercontent.com/jnovack/cloudkey/$TAG/cloudkey.env.example
```

On the device, **don't purge the `ck-ui` package** (see danger zone
above) — just make sure its service is stopped/disabled (Step 1 already
did this), then install the replacement:

```bash
install -m 755 /root/cloudkey /usr/local/bin/cloudkey
install -m 644 /root/cloudkey.service /lib/systemd/system/cloudkey.service
[ -f /etc/cloudkey.env ] || install -m 644 /root/cloudkey.env.example /etc/cloudkey.env
systemctl daemon-reload
systemctl enable --now cloudkey
```

Check it actually opened the framebuffer, not just that the process
is running:

```bash
journalctl -u cloudkey -n 20
```

You should see it report the correct resolution (160×60 on this
hardware) rather than an error opening `/dev/fb0`.

**Packaged version**:
[`scripts/runbook/phase1-cloudkey-install.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-cloudkey-install.sh),
deployed at `/usr/local/bin/phase1-cloudkey-install.sh`, wires up the
whole download-and-install sequence above. Safe to re-run — it won't
overwrite a `/etc/cloudkey.env` you've already hand-edited.

### Step 4 — Clean up leftover application data

The application packages leave their state behind:

```bash
# check these are empty/unwanted first
rm -rf /data/unifi /data/uos /data/autobackup
```

If you kept `postgresql` running for your own use (its own package is
stock Debian/PGDG, not Ubiquiti's — no reason to remove it just because
`unifi-core` used it), drop only the UniFi-created databases and roles,
not the server:

```bash
sudo -u postgres dropdb 'unifi-core'
sudo -u postgres dropuser 'unifi-core'
sudo -u postgres dropdb 'unifi-identity-update'
sudo -u postgres dropuser 'unifi-identity-update'
```

(Check `sudo -u postgres psql -c '\l'` first for the actual list on your
device — package versions differ.)

If you already removed PostgreSQL by hand before purging the UniFi
agents, some old package `postrm` scripts may complain that they cannot
drop their databases. That is noisy but expected until every config-only
package has been purged; the ordering above avoids creating that state on
a fresh run.

**Remove the orphaned UniFi service accounts.** `apt purge` removes a
package's files but **deliberately never deletes the system users its
postinst created** — `dpkg` can't know whether that UID owns files
elsewhere, so it leaves user removal to you. After the purge you're left
with roughly two dozen orphaned appliance accounts (`unifi-drive*`,
`unifi-protect*`, `unifi-talk`, `apollo`, `ds`, `ms`, `ucs-*`, `uos-go`,
`fabric-agent`, …) plus a couple of member-less groups (`unifi-streaming`,
`unifi-talk`). They're all `nologin`, password-locked, key-less, and their
packages are gone — inert clutter, but clutter worth removing. Confirm the
scope first:

```bash
# accounts in the appliance namespace still present
awk -F: '$1 ~ /unifi|ucs|uos|apollo|fabric|^ds$|^ms$/{print $1}' /etc/passwd
```

Each is safe to `deluser` once you've confirmed it owns no files, has no
process, no password, and no `authorized_keys`. Two accounts are the
exception and must be **left in place**:

- **`ui`** — it's a UID-0 alias of `root`, which looks alarming, but it's
  created by `ck-ui`'s postinst, and `ck-ui` is one of the two packages the
  danger zone above says never to remove. Deleting the account just invites
  its postinst to recreate it on the next `ck-ui` update. A locked,
  key-less root alias is inert; leave it.
- **`postgres`** — it owns a real data directory (handled by the database
  cleanup just above), so it isn't an orphan.

**Packaged version**:
[`scripts/runbook/phase1-account-cleanup.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-account-cleanup.sh),
deployed at `/usr/local/bin/phase1-account-cleanup.sh`, does exactly this —
it works from a fixed candidate list (the superset seen across Cloud Key
models) but gates every removal behind per-account guards, so an account
still in use on a given box is skipped with a warning rather than deleted,
and `ui`/`postgres` are never candidates. Run
`phase1-account-cleanup.sh --dry-run` to preview, then `-y` to apply. It's
idempotent (already-removed accounts report as "already gone") and cleans
up member-less appliance groups and orphaned `/var/log` dirs the accounts
left behind in the same pass. It scans `/var` even when that's a separate
filesystem (it prunes `/volume` and the pseudo-filesystems rather than
using `find -xdev`, which would miss an account's logs on a box where
`/var` is its own mount).

**Packaged version**: Steps 0, 1, 2, and 4 above are all wired up as
[`scripts/runbook/phase1-purge.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-purge.sh), deployed on the box
at `/usr/local/bin/phase1-purge.sh` (see File manifest below) — run
`phase1-purge.sh` to do all of the above in one pass, with Step 0's
simulate check as a hard gate (it aborts loudly if a forbidden package
like `ck-ui`/`ubnt-tools` would be removed) and a real liveness check
(`sshd` still active and listening on `:22`) after every batch in Step 2.
It builds the purge set from the target packages still present on this
box, so it is safe to re-run after a partial manual cleanup or a previous
aborted attempt. It deliberately runs **on the box itself**, not
orchestrated from your own machine — no assumption that whatever you're
SSH'd in from has bash or scripting tooling available.

**Packaged version**: Step 3 is a separate install, not a purge. Use
[`scripts/runbook/phase1-cloudkey-install.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-cloudkey-install.sh)
for that instead (see its own "Packaged version" callout above for what
it does). The walkthrough above is the reference for what this script
does and why; the script exists for when you don't need the explanation
again.

### Step 5 — The reboot checkpoint (don't skip this)

This is the real test of whether the removal was actually clean, not just
apparently successful:

```bash
reboot
# wait, then:
ssh root@<device-ip> uptime
ip -4 addr show eth0          # confirm DHCP renewed
systemctl --failed            # should be empty (or explainable)
```

Expect a couple of orphaned units to show up as failed here, harmlessly:

- `nginx.service` — if it was only ever configured as a reverse proxy in
  front of `unifi-core`'s web UI, its config will reference a log path
  under `/data/unifi-core/...` that no longer exists. Nothing needs nginx
  running anymore unless you plan to reuse it for something else.
- Any UI/telemetry-reporting daemon whose whole job was exporting status
  for the now-removed console — it'll crash-loop once its targets are
  gone. Disable it; it's monitoring, not anything load-bearing.

Clear these once you've confirmed they're expected:

```bash
systemctl disable --now nginx <telemetry-daemon>
systemctl reset-failed
```

If you deployed a front-panel replacement, confirm it came back **on its
own**, without a manual restart — that's the actual proof the service
survives a real boot, not just a warm process.

**Packaged version**: the checks above (minus the reboot itself, which
can't script its own recovery — a script dies with a box that fails to
come back) are wired up as
[`scripts/runbook/phase1-verify.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-verify.sh), deployed at
`/usr/local/bin/phase1-verify.sh`. Reboot by hand, reconnect, then run
`phase1-verify.sh` to run through all of the above without relying on
memory for what to check.

### Step 6 — Update everything the base OS still has available

This is the one step in the whole process that's genuinely low-risk,
because it removes nothing. Native UniFi OS devices already point at real
Debian repos — check first, don't assume:

```bash
cat /etc/apt/sources.list
```

On a bullseye-based device you should see something like:

```text
deb https://deb.debian.org/debian/ bullseye main contrib non-free
deb https://deb.debian.org/debian/ bullseye-updates main contrib non-free
deb https://archive.debian.org/debian/ bullseye-backports main
deb https://security.debian.org/debian-security bullseye-security main contrib non-free
```

Seeing `archive.debian.org` for `bullseye-backports` while
`deb.debian.org` still serves the main suite is normal — backports gets
archived faster than mainline, it doesn't mean the release is dead. If
your device is genuinely missing proper Debian sources (older UniFi OS
builds sometimes are), add them yourself before continuing.

Even for a same-release patch cycle, simulate first — old habits:

```bash
apt-get update
apt-get full-upgrade --simulate
```

Unlike the application purge, a same-release upgrade **removes nothing**
(check the simulate output says `0 to remove`), so none of the
dependency-cascade danger from Step 2 applies here. If the simulate output
is clean, run it for real:

```bash
apt-get full-upgrade -y
apt-get --purge autoremove -y
```

Two things worth watching for:

- **Your system may self-report as `oldoldstable` or similar** in the
  upgrade output — that's apt telling you how far behind current Debian
  stable this release now is, not an error. It just means the *ceiling*
  for this step is low: only security-relevant packages get patched on an
  old release, so don't expect a large upgrade list. On a bullseye device
  in 2026, expect somewhere around a dozen packages (core tools like
  `dpkg`, `openssl`, `util-linux`), not a sweeping refresh.
- **Watch for Ubiquiti-custom builds getting replaced by stock ones as a
  side effect.** `jq`/`libjq1` on this device were a custom `~ubnt` build;
  the normal security upgrade replaced them with Debian's own patched
  build automatically. That's a welcome bonus, not something you need to
  handle separately — it's just what "install packages your Debian repos
  give you" naturally does once the apt sources are correct.

Confirm you're fully caught up:

```bash
apt list --upgradable   # should be empty
```

> [!NOTE]
> This is not the same as a release upgrade. If you want genuinely current
> package versions across the board — not just security patches on
> 2021-vintage bullseye — that means moving to a newer Debian release
> (bullseye → bookworm → trixie), which is a substantially bigger and
> riskier undertaking: new kernel ABI, new systemd version, and unknown
> compatibility with the board's still-installed custom kernel/initramfs/udev
> packages from the danger zone above. Treat that as a deliberate, separate
> decision, not something to bundle into routine patching.

**Install the base tooling later phases assume.** The stock image is
missing a couple of packages that every later phase takes for granted, so
put them on now rather than mid-phase — the box then has a complete
baseline in one place:

```bash
apt-get install rsync nfs-common
```

- `rsync` — file sync, used later to offload completed downloads to
  another host.
- `nfs-common` — the NFS (Network File System) client
  (`mount.nfs`/`mount.nfs4`), for mounting a remote share to sync into. It
  pulls `rpcbind`, `keyutils`, and `libnfsidmap2`; that's expected, not
  bloat.

A base-release install like this removes nothing (same as the upgrade
above), so it carries none of Step 2's dependency-cascade danger.

**Packaged version**: [`scripts/runbook/phase1-base-tools.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-base-tools.sh),
deployed at `/usr/local/bin/phase1-base-tools.sh`, runs the simulate +
install + a presence check, and is safe to re-run as a "is the baseline
here?" check.

### Step 7 — Repurposing the internal drive (and what to do if it's gone missing)

The Gen2 Plus's whole reason for existing over smaller Cloud Key models is
the internal 2.5" drive bay. If your device has no bulk-storage bay, or no
drive currently appears in `lsblk`, skip the format/mount commands and
move on; later phases that need storage should either mount removable
media explicitly or use a configurable path. On a Gen2 Plus where
`/volume` (or whatever the UniFi storage-mount partition was called on
your device) shows up empty and healthy, this step is simple: pick a
filesystem and mountpoint and move on. It wasn't simple here, and the
troubleshooting path below is worth documenting in full because none of
it was caused by anything in the steps above — it's worth knowing about
on any device with a USB-attached (rather than native SATA) drive bay.

**The happy path — installing a new or replacement drive that's healthy.**
Don't assume the drive is blank just because it's new to this box; wipe
it unconditionally rather than inspecting what might already be on it:

```bash
lsblk
```

Confirm the drive shows up as `sda` (or whatever name it takes) at
roughly its full advertised capacity — the transport/media symptoms in
the troubleshooting section below only apply to a struggling drive, not
this path.

> [!WARNING]
> `wipefs` and `mkfs` below destroy everything on the target drive.
> Double-check the device name (`sda` vs. any other disk you have attached)
> before running either command — there's no confirmation prompt and no
> undo.

Then clear any existing partition table or filesystem signature:

```bash
wipefs -a /dev/sda
```

Format the whole disk device directly as `ext4` — deliberately **no
partition table** (the "superfloppy" pattern; keeps `lsblk`/`blkid`
simple and matches how this box's drive has been set up each time it's
been replaced so far), labeled to match:

```bash
mkfs.ext4 -F -L volume /dev/sda
```

Mount it at `/volume` (this device's established convention for the
UniFi storage partition, kept for continuity even after de-Ubiquitizing).

> [!IMPORTANT]
> Don't use `/etc/fstab` for this. Confirmed by direct testing
> (2026-07-13, two full reboot cycles): this board still runs a live
> UniFi bootup-hook framework (owned by the load-bearing
> `cloudkey-plus-apq8053-base-files`/`ubnt-tools` packages, kept installed
> on purpose — see the danger-zone section above) that silently resets
> `/etc/fstab` to a minimal template on every boot, dropping any manually
> added line with no error or warning. A plain systemd `.mount` unit under
> `/etc/systemd/system/` isn't touched by this and has now survived
> multiple real reboots, so use that instead:

```bash
UUID=$(blkid -s UUID -o value /dev/sda)
cat > /etc/systemd/system/volume.mount <<EOF
[Unit]
Description=Bulk storage drive (whole-disk ext4, no partition table)

[Mount]
What=/dev/disk/by-uuid/$UUID
Where=/volume
Type=ext4
Options=defaults,noatime

[Install]
WantedBy=local-fs.target
EOF
systemctl daemon-reload
systemctl enable --now volume.mount
```

Confirm it actually took:

```bash
systemctl is-active volume.mount
mount | grep /volume
lsblk
```

If `/volume` comes back mounted to the right drive, you're done — skip
the rest of this step, which only applies if the drive turns out to be
unhealthy instead.

**A second landmine from the same source, worth knowing before you move
on to Phase 2**: a separate piece of that same still-active hook
framework (`mp-clean volume`, run from `/usr/lib/ubnt/hooks/system/bootup-{top,bottom}/*-srv-link`)
deletes any **empty**, plain-alphanumeric-named subdirectory directly
under `/volume` on every boot — it's meant to clean up stale native-UniFi
partition mountpoints, but it can't tell those apart from ordinary
subdirectories like `tv`/`movies` that just happen to be empty. See
[Phase 2](Phase-2-Apps)'s directory-recreation step for the `.keep`-file
workaround this requires.

**Packaged version**: every command above is also wired up as
[`scripts/runbook/phase1-format-mount-volume.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-format-mount-volume.sh),
deployed on the box at `/usr/local/bin/phase1-format-mount-volume.sh` (see
File manifest below) — run `phase1-format-mount-volume.sh /dev/sda` to do
all of the above in one shot (it prompts for confirmation before wiping;
pass `-y` as a second argument to skip that for a non-interactive run).
The walkthrough above is the reference for what it does and why; the
script exists for when you don't need the explanation again.

**Symptom**: the drive didn't show up in `lsblk` at all — not unmounted,
not failing to mount, just absent.

**First finding**: it *was* being probed at boot, per `dmesg` — it's
attached over USB through a bridge chip, not native SATA:

```bash
dmesg | grep -iE 'sda|scsi|uas|usb.*storage'
```

Look for a `New USB device found` line with an `idVendor`/`idProduct`, and
whatever bridge chip identifies itself over SCSI INQUIRY. On this device
it was an ASMedia `ASM1153E` (USB ID `174c:1153`).

**Second finding**: the drive span up (slowly — over a minute), then
failed reading sector 0 with a "critical target error," a command abort,
and a bus reset that itself failed — which is why the device vanished
instead of just failing to mount. The actual kernel message that pins this
down:

```text
xhci-hcd: ERROR Transfer event for disabled endpoint or incorrect stream ring
```

This is a known category of bug: USB Attached SCSI (UAS) uses USB3 stream
endpoints, and some xHCI host-controller implementations don't handle them
reliably with certain bridge/drive combinations. It is **not** a media
failure signature — that distinction matters for what comes next.

**The fix, applied live, no reboot required.** `uas` and `usb_storage` are
commonly compiled directly into embedded kernels rather than built as
loadable modules — check with `lsmod`. If neither shows up there, the
usual `modprobe blacklist` approach won't apply, but the `usb_storage`
driver's `quirks` parameter is still exposed and writable even when it's
built in:

```bash
echo '<idVendor>:<idProduct>:u' > /sys/module/usb_storage/parameters/quirks
```

The `u` flag tells the `uas` driver to back off and let `usb_storage`
claim the device instead, using the older Bulk-Only Transport instead of
UAS — sidestepping the stream-endpoint bug entirely. This only takes
effect on the *next* enumeration, not an already-dropped device, so it
needs a physical reseat (unplug/replug the drive) to actually apply. After
that: `scsi host2: usb-storage ...` in `dmesg` confirms the correct driver
bound, and the transport error is gone for good.

**But that only fixed one of two problems.** Even with the correct driver
now used, reads at sector 0 and at a sector right near the very end of the
disk — exactly where a GPT partition table's primary and backup headers
live — failed with a distinctly different error:

```text
Sense Key : 0x3 [current]   # MEDIUM ERROR
ASC=0x11 ASCQ=0x0           # Unrecovered read error
```

That's a genuine media-level failure, not a transport quirk — and it's why
`parted /dev/sda print` reports "unrecognised disk label" even after the
transport fix.

**A natural next question: does SMART confirm it?** Not on this
configuration — `smartctl -a /dev/sda` correctly identifies the drive
(model, serial, firmware) but every SMART command aborts at the SCSI
layer, with or without `-T permissive` or `-d sat`. Many USB-SATA bridges
simply don't implement ATA passthrough for SMART in Bulk-Only Transport
mode, even when they support it better in UAS mode — meaning the transport
fix above can cost you SMART visibility as a side effect. Worth knowing
before you assume SMART will confirm anything either way.

**Last resort tried**: consumer drives typically only remap a bad sector
on a *write* to it, not a read, so a fresh partition table write was worth
attempting — there's no data at risk if `/volume` was already confirmed
empty:

```bash
sgdisk --zap-all /dev/sda
```

This failed too, with `Error 5` (I/O error) on both the GPT header and MBR
writes, and the same medium errors recurring in `dmesg` during the
operation.

**Where this leaves you**: three independent methods (the original failing
read, the same read after fixing the transport bug, and an actual write
attempt) all confirm real, persistent, unrecoverable errors at both ends
of the disk. That's a hardware conclusion, not a software one — the
transport bug is fixed and worth applying regardless, but a drive (or
bridge board) exhibiting this pattern needs physical inspection or
replacement, not another software workaround. If you hit this, don't
spend more time on it in software once you've confirmed the same failure
persists across a transport fix and an actual write attempt — that's the
point where it stops being a Linux problem.

### Step 8 — Lock down access (admin user, key-only SSH, emergency toggle)

Out of the box you administer this device as `root`, and depending on how
it shipped, password login may still be reachable. Before it starts doing
real work, give it a proper access posture: an unprivileged admin user for
day-to-day use, and no password logins anywhere. The wrinkle specific to
this hardware is that it lives on the end of a PoE cable with no keyboard —
so "no password logins" needs an escape hatch for the day you lose your SSH
key, and that escape hatch is the rear-panel **reset button**.

The design is two states you flip between:

- **Safe state (the normal one).** `root`, `cloudkey` (and `ubnt` if your
  device has one) have their passwords *locked* — no password login at all —
  and SSH accepts keys only. You log in as `cloudkey` and `sudo` without a
  password; `root` remains reachable by key as a fallback.
- **Emergency state.** A reset-button hold flips SSH back to allowing root
  *password* login and unlocks root's existing password, so someone with
  physical access who has lost their key can still get in. Once back in, you
  flip to the safe state again by hand.

The reset-button half lives in a separate front-panel app (it's what drives
the LCD and watches the button); all it needs from this phase is that the
two toggle scripts exist at a stable path — `/usr/local/sbin/security-lock.sh`
and `/usr/local/sbin/security-unlock.sh`.

**Create the admin user with passwordless sudo and a key.** Locking a
password never blocks key-based SSH — `passwd -l` only invalidates the
password hash, which key auth doesn't consult — so the safe state is only
safe from lockout if `cloudkey` has a working key first. Copy the key you
already administer the box with (`root`'s `authorized_keys`) so applying the
lockdown can't strand you:

```bash
useradd -m -s /bin/bash cloudkey
usermod -aG sudo cloudkey

# passwordless sudo, via a drop-in validated before it's installed
echo 'cloudkey ALL=(ALL) NOPASSWD:ALL' > /tmp/cloudkey.sudoers
visudo -cf /tmp/cloudkey.sudoers && \
  install -m 0440 -o root -g root /tmp/cloudkey.sudoers /etc/sudoers.d/cloudkey

# bootstrap cloudkey's key from root's so the key-only state can't lock you out
install -d -m 700 -o cloudkey -g cloudkey /home/cloudkey/.ssh
cp /root/.ssh/authorized_keys /home/cloudkey/.ssh/authorized_keys
chown cloudkey:cloudkey /home/cloudkey/.ssh/authorized_keys
chmod 600 /home/cloudkey/.ssh/authorized_keys
```

> [!WARNING]
> Once you lock these passwords and switch SSH to key-only below, losing
> the `cloudkey` key means losing remote access — recovery falls back to
> the rear-panel reset button (see the emergency toggle further down). If
> you have any doubt the bootstrap above actually worked, open a fresh
> `ssh cloudkey@<device-ip>` session and confirm it logs in **before**
> running the block below.

**Apply the safe state.** Lock the privileged accounts' passwords and drop
in a key-only SSH policy:

```bash
for u in root ubnt cloudkey; do id "$u" >/dev/null 2>&1 && passwd -l "$u"; done

cat > /etc/ssh/sshd_config.d/10-security.conf <<'EOF'
PasswordAuthentication no
KbdInteractiveAuthentication no
ChallengeResponseAuthentication no
PermitRootLogin prohibit-password
EOF
sshd -t && systemctl reload ssh
```

The device's `sshd_config` puts `Include /etc/ssh/sshd_config.d/*.conf` on
its first line, and sshd takes the *first* value it sees for each option, so
this drop-in overrides the stock config below it — no need to edit
`sshd_config` itself.

> [!IMPORTANT]
> One device-specific quirk is worth knowing, because getting it wrong
> leaves a silent hole rather than an error: bullseye ships **OpenSSH
> 8.4**, which honors the *old* option name
> `ChallengeResponseAuthentication`; OpenSSH renamed it to
> `KbdInteractiveAuthentication` in 8.7. Set **both**. On 8.4, setting only
> the new name parses cleanly (so `sshd -t` passes and nothing warns you)
> but does nothing — and with `UsePAM yes`, a live keyboard-interactive
> method can tunnel a password login even though `PasswordAuthentication`
> is `no`. Both lines set to `no` closes that on either version.

Confirm the safe state took, and that `cloudkey` really can get in and
escalate:

```bash
sshd -T | grep -iE 'permitrootlogin|passwordauthentication|kbdinteractive|challengeresponse'
passwd -S root cloudkey        # both should show 'L' (locked)
ssh cloudkey@<device-ip> 'sudo -n whoami'   # -> root, no prompt
```

**The emergency toggle.** `security-unlock.sh` is the reverse: it unlocks
root's *existing* password (`passwd -u root` — it never sets one, so if root
had no password this grants nothing) and rewrites the same drop-in to allow
root password login. `security-lock.sh` puts everything back. Both validate
with `sshd -t` before reloading, so a bad edit can't wedge `sshd`.

**Packaged version**: all of the above is wired up as three scripts
deployed to `/usr/local/sbin/` (see File manifest below).
[`scripts/runbook/phase1-security-setup.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-security-setup.sh) does
the one-time setup — creates `cloudkey`, installs the sudoers drop-in,
bootstraps the key (or takes one as its first argument:
`phase1-security-setup.sh 'ssh-ed25519 AAAA...'`), and then execs
`security-lock.sh`. It **refuses to lock down** if `cloudkey` would end up
with no key, so a bad run can't strand you. The two toggles —
[`scripts/runbook/security-lock.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/security-lock.sh) and
[`scripts/runbook/security-unlock.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/security-unlock.sh) — are the safe/
emergency flip, idempotent and safe to re-run, and are the stable entry
points the rear-panel reset-button handler calls. The walkthrough above is
the reference for what they do and why.

## File manifest

Everything created by this guide, for a final checklist:

| Path | Purpose |
| --- | --- |
| `/usr/local/bin/phase1-purge.sh` | Steps 0/1/2/4 — simulate-gated batched purge + cleanup; source of truth is [`scripts/runbook/phase1-purge.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-purge.sh) in this repo |
| `/usr/local/bin/phase1-verify.sh` | Step 5's post-reboot checklist, scripted; source of truth is [`scripts/runbook/phase1-verify.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-verify.sh) in this repo |
| `/usr/local/bin/phase1-account-cleanup.sh` | Step 4 — removes the orphaned UniFi service accounts/groups the purge leaves behind (guarded; never touches `ui`/`postgres`); source of truth is [`scripts/runbook/phase1-account-cleanup.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-account-cleanup.sh) in this repo |
| `/usr/local/bin/phase1-base-tools.sh` | Step 6 — installs `rsync` + `nfs-common` baseline; source of truth is [`scripts/runbook/phase1-base-tools.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-base-tools.sh) in this repo |
| `/usr/local/bin/phase1-format-mount-volume.sh` | formats + mounts a replacement drive at `/volume` (Step 7); source of truth is [`scripts/runbook/phase1-format-mount-volume.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-format-mount-volume.sh) in this repo |
| `/etc/systemd/system/volume.mount` | mounts `/volume` at boot — generated fresh (new UUID) by the script above on every drive replacement; **not** `/etc/fstab`, which this board silently resets on every boot |
| `/usr/local/bin/phase1-cloudkey-install.sh` | Step 3 (optional) — downloads + installs the front-panel LCD replacement; source of truth is [`scripts/runbook/phase1-cloudkey-install.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-cloudkey-install.sh) in this repo |
| `/usr/local/bin/cloudkey` | Step 3 — the LCD-replacement binary itself, pulled pre-built from `jnovack/cloudkey`'s latest GitHub release |
| `/lib/systemd/system/cloudkey.service` | Step 3 — its systemd unit |
| `/etc/cloudkey.env` | Step 3 — its config (tunnel/app rows for the OLED); created empty-ish from the upstream example, edit by hand |
| `/usr/local/sbin/phase1-security-setup.sh` | Step 8 one-time setup — creates `cloudkey`, passwordless sudo, key bootstrap, then execs the lock; source of truth is [`scripts/runbook/phase1-security-setup.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase1-security-setup.sh) |
| `/usr/local/sbin/security-lock.sh` | Step 8 — flip to the safe state (passwords locked, key-only SSH); source of truth is [`scripts/runbook/security-lock.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/security-lock.sh); stable path the reset-button handler calls |
| `/usr/local/sbin/security-unlock.sh` | Step 8 — flip to emergency state (root password login); source of truth is [`scripts/runbook/security-unlock.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/security-unlock.sh); called by the reset-button handler |
| `/etc/sudoers.d/cloudkey` | passwordless sudo for `cloudkey` (0440, `visudo`-validated) |
| `/etc/ssh/sshd_config.d/10-security.conf` | the single managed SSH policy drop-in that lock/unlock overwrite in full — safe vs. emergency state is whatever this file currently says |

## Where this ends up

A stock, fully-patched Debian 11 ARM64 box: SSH access hardened to keys
only with an unprivileged `cloudkey` admin user (Step 8) and a
reset-button escape hatch for the day you lose your key, a working
front-panel LCD (if you replaced it), power-loss protection independently
verified intact through all of the above, roughly 15 fewer packages than
it started with, and nothing left in `apt list --upgradable`. The internal
drive bay's fate depends entirely on what you find in Step 7 — a healthy
drive just needs a filesystem and a mountpoint; a struggling one, like the
one documented above, needs a replacement before it's useful for anything.

Whether to stay on a fully-patched bullseye or pursue a full release
upgrade for genuinely current software (see Step 6's closing note) is the
one deliberate decision left on the table, worth making on its own rather
than as an afterthought of cleanup. Everything else here — what you
actually run on the box once it's a stock Debian server — is up to you.

## Prior art and further reading

Nobody documents doing exactly this — hand-purging the platform layer in
one shot while keeping the box stable. What's actually out there is more
conservative:

- The community-documented "safe" removal list is only the bolt-on
  Applications (Access, Connect, Protect, Talk, UID) via `apt purge`,
  followed by a normal `apt update && upgrade && full-upgrade &&
  autoremove` cycle — not the core platform (`unifi-core`, `unifi`,
  `mongodb-server`, `ustd`, `ubnt-tools`).
- One well-known "headless Linux server" conversion guide uses the
  device's own System Settings → Updates → Uninstall UI for Network/Protect
  rather than raw `apt purge` of platform packages, then repoints
  `/etc/apt/sources.list` at real Debian repos and upgrades through
  Debian versions — gradually outgrowing the Ubiquiti base rather than
  surgically excising it. That's a legitimate, more conservative
  alternative to everything above if you'd rather not touch the platform
  packages directly at all.
- A lighter-weight, SSH-accessible recovery option worth knowing about
  *before* you need it: Ubiquiti's own `ubnt-systool fwupdate <url>` and
  `ubnt-systool reset2defaults` — useful if SSH is still alive but
  something's gone wrong, as a step short of a full physical
  firmware-recovery-mode restore.

## Caveats

- Every command above was run and verified on **both** a Cloud Key **Gen2
  Plus** and a plain Cloud Key **Gen2** (both Qualcomm APQ8053, aarch64).
  The two behaved identically apart from the base-files package name (the
  `-plus-`/`-g2-` split noted throughout) and the Plus's internal drive bay
  (Step 7) — the dependency chains, unit list, and purge set were the same
  on each. Other UniFi OS console hardware (UDM, UNVR, or the original 32-bit
  Gen2) may differ in package names or the ARM vs ARM64 distinction — verify
  with `apt-cache depends` and `apt-get purge --simulate` on your own device
  before trusting this list verbatim.
- Some sources report Ubiquiti removed the internal battery entirely in
  later hardware revisions of this device — check whether yours actually
  has one (`ls /sys/class/power_supply/`) before assuming the power-loss
  chain above is present at all.
- This whole process operates purely at the package/service layer on the
  already-booted Debian rootfs — it never touches the eMMC boot partition
  table, bootloader, or A/B slot mechanism. That's what keeps "reinstall
  packages" as the worst case instead of "bricked device," as long as the
  danger-zone packages above are respected.
