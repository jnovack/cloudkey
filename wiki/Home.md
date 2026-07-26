# Cloud Key Cleaning Runbook

Turning a UniFi Cloud Key Gen2 Plus (UCK-G2-Plus) into a generic Debian server,
then building a media stack, remote access, and a VPN killswitch on top of it.
Organized as numbered phases — do them in order, since later phases assume
earlier ones are done.

This wiki is the companion to the [cloudkey](https://github.com/jnovack/cloudkey)
daemon: the runbook gets you a clean Debian box, and cloudkey drives the
front-panel OLED and LEDs once the stock UniFi stack is gone
([Phase 1](Phase-1-De-Ubiquitizing) installs it).

Everything here was verified on both a Cloud Key **Gen2 Plus** and a plain
Cloud Key **Gen2** (same APQ8053/aarch64 platform). The two differ only in
the base-files package name (a `-plus-`/`-g2-` split, called out where it
matters in [Phase 1](Phase-1-De-Ubiquitizing)) and the Plus's internal
drive bay — so the storage-dependent parts of Phase 2 assume a Plus, while
Phases 1 and 3–9 apply to either.

| Phase | Doc |
| --- | --- |
| 1 | [Phase 1 — De-Ubiquitizing](Phase-1-De-Ubiquitizing) — strip the UniFi software stack, keep the box stable, harden access (admin user + key-only SSH) |
| 2 | [Phase 2 — Apps](Phase-2-Apps) — NZBGet, Sonarr, Radarr, Prowlarr (rationale/pitfalls in [Phase 2 Overview](Phase-2-Overview); keeping it running in [Phase 2 Hardening](Phase-2-Hardening)) |
| 3 | [Phase 3 — AutoSSH](Phase-3-AutoSSH) — emergency SSH fallback, the implementation runbook |
| 4 | [Phase 4 — WireGuard](Phase-4-WireGuard) — fail-closed VPN egress (netns + WireGuard) for the Phase 2 apps |
| 5 | [Phase 5 — Headscale](Phase-5-Headscale) — self-hosted Headscale + headplane coordination server, plus enrolling a client (rationale/pitfalls in [Phase 5 Overview](Phase-5-Overview)) |
| 6–8 | *(unallocated — reserved for future work)* |
| 9 | [Phase 9 — Backup & Restore](Phase-9-Backup-Restore) — back up every custom file to the SD card and restore it |

Not tied to a single phase:

- [CloudKey Admin Tools](CloudKey-Admin-Tools) — the status dashboard and
  on-box diagnostic runbook, useful across every phase.
- [macOS AutoSSH](macOS-AutoSSH) — the Phase 3 rescue tunnel on a macOS
  client instead of the Cloud Key: launchd rather than systemd, Homebrew
  rather than apt. The relay side is unchanged, so one relay can carry
  Cloud Keys and Macs together.
- [Tailscale for Synology](Tailscale-for-Synology) — enrolling a Synology
  NAS into the Phase 5 tailnet.

## Script assets

Every phase's scripts live in the main repo under
[`scripts/runbook/`](https://github.com/jnovack/cloudkey/tree/main/scripts/runbook)
as the deployed source of truth (named `phase<N>-*`), not just embedded in
these pages. Each page's own walkthrough explains what a script does and why;
the scripts exist for re-running the same steps without re-reading the
explanation.

The `install`/`scp` commands throughout these pages use paths relative to a
repo checkout, so clone it first and run them from the repo root:

```bash
git clone https://github.com/jnovack/cloudkey.git
cd cloudkey
```

## Hardware note

This board still runs a live, largely unmapped UniFi boot-time hook
framework underneath the de-Ubiquitized userspace (owned by packages kept
installed on purpose — see Phase 1's danger-zone section). Two confirmed
behaviors worth knowing before adding anything new that depends on
boot-time state:

> [!WARNING]
> Two things get silently reset on every boot, so anything you add that
> depends on boot-time state must account for them:
>
> - `/etc/fstab` resets to a bare template — use a systemd `.mount` unit
>   instead (see Phase 1, Step 7).
> - Any **empty** plain-named subdirectory directly under `/volume` gets
>   deleted — a placeholder file (e.g. `.keep`) prevents this (see
>   Phase 2's directory-recreation step).
>
> Both were confirmed by actually rebooting the box — "works when tested
> live" is not evidence it survives a reboot on this hardware.
