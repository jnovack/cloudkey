# Cloud Key admin tools: status dashboard and on-box docs

This one's different from the other docs in this repo — it isn't
tied to one project (de-Ubiquitizing, the media stack, or remote access).
It covers two small pieces of tooling that exist to make *diagnosing* the
box easier, regardless of which project the problem turns out to belong
to.

## The status dashboard

Running `cloudkey-dashboard` (installed at `/usr/local/sbin/`, on `$PATH`
for root) prints a colored summary of:

- The four media-stack apps (NZBGet, Sonarr, Radarr, Prowlarr) — running,
  down, or not installed, with their ports.
- The four autossh rescue-tunnel systemd units (Tier-1 and Tier-2, both
  VPS's) — running, down, or standby (Tier-2 is *meant* to show
  "standby" when idle — that's not a problem, see
  [Phase 3](Phase-3-AutoSSH)).
- The Phase 4 VPN killswitch — a live health check (`vpn-check.sh -q`,
  not the tunnel unit's own `Active` state, which can go stale — see
  [Phase 4](Phase-4-WireGuard) Part 6) plus the auto-heal
  timer's unit status. Gated on `vpn-check.sh` existing, since the
  tunnel unit itself is present as soon as you follow Part 3 and so
  can't tell "built" from "not built" the way an absent unit does
  elsewhere.
- Tailscale status — checks live, so it reads correctly whichever phase
  5 stage you're at: "installed, not connected" if the client's
  installed but not yet joined, "up" once enrolled.

Every phase past 1 is optional, and there's no fixed order to which ones
a given box has — so each **section** only appears at all if at least
one of its sub-checks is actually installed (present, whether or not
it's currently running). A box that's only done Phase 1 sees no "Media
stack" header, no autossh section, no VPN section, and no Tailscale
section — not four headers full of "not installed" lines. Within a
section that IS showing (say, only 2 of the 4 media apps are
installed), the not-yet-installed ones still get their own line, since
a partially-built phase is worth calling out.

It runs automatically on interactive SSH logins (`ssh root@<device-ip>`
with a real terminal), via `/etc/profile.d/zz-cloudkey-dashboard.sh`. That
loader checks that the session is genuinely interactive before calling the
dashboard script, so it **never** fires for `ssh host 'command'`, cron,
scp/rsync, or anything scripted.

> [!NOTE]
> The loader checks three things before calling the dashboard script:
> that the shell is genuinely interactive (`$-` contains `i`), that
> stdout is a real terminal (`[ -t 1 ]`), and that the terminal claims at
> least 8 colors (`tput colors`). This was deliberately tested during
> setup: a plain non-interactive `ssh root@<device-ip> 'echo test'` was
> confirmed to print only its own output, nothing from the dashboard,
> even though OpenSSH runs the shell as a login shell (and therefore
> sources `/etc/profile`) in both the interactive and command-execution
> cases — the `$-` check is what actually distinguishes the two, not
> which files get sourced.

You can also just run `cloudkey-dashboard` by hand any time, from any
session, for a quick status check while debugging.

**Packaged version**: [`scripts/runbook/cloudkey-dashboard.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/cloudkey-dashboard.sh)
and [`scripts/runbook/zz-cloudkey-dashboard.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/zz-cloudkey-dashboard.sh)
are the source of truth for the two files described above. Deploy them:

```bash
scp -i .local/cloudkey scripts/runbook/cloudkey-dashboard.sh \
  root@<device-ip>:/usr/local/sbin/cloudkey-dashboard
scp -i .local/cloudkey scripts/runbook/zz-cloudkey-dashboard.sh \
  root@<device-ip>:/etc/profile.d/zz-cloudkey-dashboard.sh
ssh -i .local/cloudkey root@<device-ip> \
  'chmod 755 /usr/local/sbin/cloudkey-dashboard /etc/profile.d/zz-cloudkey-dashboard.sh'
```

The rescue-tunnel section discovers configured relays by globbing
`/etc/systemd/system/autossh-tunnel-*.service` rather than hardcoding one
`<vps>`, so it reads correctly whether you built one relay or several (see
[Phase 3](Phase-3-AutoSSH)'s "Dashboard integration" section,
which this script implements).

## File manifest

| Path | Purpose |
| --- | --- |
| `/usr/local/sbin/cloudkey-dashboard` | the status dashboard; source of truth is [`scripts/runbook/cloudkey-dashboard.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/cloudkey-dashboard.sh) |
| `/etc/profile.d/zz-cloudkey-dashboard.sh` | interactive-login loader that calls it; source of truth is [`scripts/runbook/zz-cloudkey-dashboard.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/zz-cloudkey-dashboard.sh) |
| `/opt/docs/runbook.md` | on-box diagnostic runbook, hand-maintained (see below) |

## Diagnostic runbook mirrored on the box itself

`/opt/docs/runbook.md` on the Cloud Key is a **focused operational
runbook only** — restarting/checking autossh tunnels, checking app
status, common failure signatures. It is deliberately *not* a copy of the
full setup docs (design rationale, install walkthroughs, incident
history for de-Ubiquitizing/media-stack/remote-access) — those stay only
in the docs directory on the machine that did the setup. The box should
carry just enough to diagnose connections and applications from the box
itself, not the whole project history.

**Keep `runbook.md` in sync by hand** when the operational picture
changes (new service, new port, new common failure mode) — there's no
automation pushing updates to `/opt/docs/`:

```bash
scp -i .local/cloudkey runbook.md root@<device-ip>:/opt/docs/runbook.md
```
