# Media stack: rationale and pitfalls

This is the companion piece to [Phase 2](Phase-2-Apps) — that doc is the
runbook (do this, then this); this one is prose: why things are built
the way they are, and the real problems hit along the way, kept here so
none of it gets lost or has to be relearned. Read it if you're
debugging one of these exact symptoms or just curious why a step
exists — it's context for *why*, not steps to run, and skipping it
won't stop you from following the runbook.

## Why not just use Docker?

If you've read about self-hosting before, you've probably seen everyone
recommend Docker (a way of running each program in its own self-contained
little box, isolated from the rest of the system). We tried that first
here and it didn't work — this particular device runs on a genuinely old
version of Linux under the hood, old enough that it's missing a technical
feature Docker depends on to run more than very simple programs
(specifically: multi-layer overlayfs, added in kernel 4.0; this device's
kernel predates it). That's not something we can easily fix, so this
device runs everything as regular, plainly-installed programs instead.

## Sonarr's installer reads its prompts from `/dev/tty`, not stdin

Sonarr's official install script normally asks two questions
interactively — what user and group to run as. Over a remote SSH
session, those answers never arrive, even if you try to pipe them in.
The reason: the script deliberately reads from `/dev/tty` directly,
bypassing stdin entirely — a genuinely thoughtful upstream design choice
(it means the script still works correctly even when invoked as
`curl | bash`, where stdin is the piped script content, not real user
input). It just means the usual "pipe answers in" tricks
(`yes "" | ...`, `DEBIAN_FRONTEND=noninteractive`, etc.) don't reach it —
`DEBIAN_FRONTEND` only affects `apt`/`dpkg`'s own debconf prompts,
nothing about a custom bash script's own `read` calls. The runbook's fix
(download the script, patch out the two `read` lines, run the patched
copy) is the one approach confirmed to actually work here.

## Radarr's GLIBC/SQLite mismatch

Checking on Radarr right after its first install showed something
confusing: `systemctl status` said "active (running)," but it wasn't
actually working. Its own log (`journalctl -u radarr`) had the real
error:

```text
System.DllNotFoundException: Unable to load shared library 'e_sqlite3'
/lib/aarch64-linux-gnu/libc.so.6: version `GLIBC_2.33' not found (required by /opt/Radarr/libe_sqlite3.so)
```

In plain terms: Radarr bundles its own copy of a small helper component
it uses to talk to its internal SQLite database, and that bundled copy
was built expecting a newer GLIBC (one of the most fundamental shared
pieces of the OS) than Debian 11 actually ships. It's like a modern
appliance expecting a newer style of electrical outlet than the one in
the wall — the appliance itself is fine, it just can't connect to what's
there. The fix (in the runbook) is to point Radarr at the system's own
compatible copy of that library instead of its bundled one, via a
symlink.

**This came back later, exactly as predicted.** Clicking Radarr's own
in-app upgrade button replaced `/opt/Radarr` wholesale — symlink
included — and the new version crashed on startup. The upgrade itself
had worked fine; only the symlink was collateral. What made it
confusing was the failure mode: Radarr printed `Press enter to exit...`
and blocked on standard input rather than exiting, so the process
stayed alive, `systemctl status` cheerfully reported "active
(running)," and `Restart=on-failure` never fired — there was no failure
for systemd to see. Same trap as the "active but not working" symptom
above, and the same lesson: on these apps, `systemctl status` is not
evidence of health.

> [!IMPORTANT]
> `systemctl status` reporting "active (running)" doesn't mean any of
> these four apps is actually serving requests. Part 9 of the runbook
> adds a real network-based liveness check for exactly this reason —
> trust that over process status.

Since the repair is mechanical and the trigger is predictable, Part 8
of the runbook now runs it as `ExecStartPre` before every start instead
of relying on anyone remembering it after an upgrade.

The obvious-looking alternative — force the app to exit so systemd's
`Restart=` policy can catch it, on the theory that it's blocked waiting
for that keypress — does not work, and it's worth writing down why so
nobody spends an afternoon on it. It isn't blocked on input. systemd
already gives services `StandardInput=null`, so the process's stdin is
`/dev/null` and `Console.ReadLine()` returns instantly at EOF. Thread
states during an actual hang confirm it: the main thread sits in
`futex_wait_queue_me` (a lock/join wait), and nothing is reading fd 0 —
a genuine console read would show as `n_tty_read`. `NRestarts` stays
at 0 throughout.

What actually keeps it alive is that a .NET process only exits once all
its *foreground* threads finish. The failure happens inside ASP.NET's
host startup, after Kestrel and the thread pool are running, and those
threads outlive the failed startup path. So the message prints,
`ReadLine` returns, and the runtime stays resident anyway — no exit, no
exit code, nothing for `Restart=on-failure` to fire on. `Restart=always`
is no better against a process that never exits, and `WatchdogSec`
needs `sd_notify` support these apps don't have.

What does work is asking the app a question over the network instead of
asking the kernel whether a process exists — the only test that
distinguishes the two states. During a hang, nothing is listening on the
app's port at all, so an unauthenticated `/ping` (or NZBGet's
`jsonrpc/version`) cleanly separates "serving" from "resident but
dead." That's Part 9 of the runbook, covering all four apps for any
cause of a silent hang, with Part 8 remaining the specific prevention
for the upgrade trigger.

Prowlarr ships from the same upstream family and carries the identical
warning in its own docs for this Debian version. On this server it
started cleanly the first time, which initially read as "not affected"
— but that was wrong, and worth flagging as a general trap: its bundled
library was in fact unloadable the whole time (`ldd` reported unmet
GLIBC versions), the app simply hadn't exercised it in a way that
failed. A clean startup says nothing about whether a bundled library is
sound. Apply the symlink to Prowlarr regardless rather than waiting for
a symptom, because that symptom will otherwise arrive on its next
upgrade.

## `/etc/fstab` is silently reset on every boot

Confirmed by direct testing (2026-07-13, two full reboot cycles): a
still-active UniFi boot-time tool resets `/etc/fstab` to a bare minimum
on every single boot, silently dropping any line added by hand, with no
warning anywhere. A plain systemd `.mount` unit isn't touched by this and
has since survived multiple real reboots — see [Phase 1](Phase-1-De-Ubiquitizing)
Step 7 for the full investigation (what was ruled out, how the behavior
was actually confirmed). The runbook here just uses the `.mount` unit
directly, without re-deriving why.

## Empty directories under `/volume` get deleted on every boot

Also confirmed by direct testing (2026-07-13): a separate tool from that
same boot-hook framework (`mp-clean volume`) deletes any **empty**,
plain-named subdirectory directly under `/volume` on every boot — meant
to clean up stale native-UniFi partition mountpoints, but it can't tell
those apart from ordinary app directories that just happen to be empty
right now. When the libraries sat at `/volume/tv` and `/volume/movies`
that was exactly their state whenever Sonarr/Radarr had nothing yet, and
it was watched deleting both across a real reboot.

Two things defuse it. Only subdirectories *directly* under `/volume` are
candidates, so nesting the libraries as `/volume/media/tv` and
`/volume/media/movies` puts them out of reach entirely — `media/` is the
only thing `mp-clean` sees, and it is non-empty as long as either
library directory exists. The placeholder `.keep` file in each directory
stays anyway as belt-and-braces: it is cheap, it protects `media/`
itself in the moment before the libraries are created, and it keeps the
guarantee from depending on a nesting detail someone might later
flatten. Neither approach touches or disables any Ubiquiti component.

## Services need to be told they depend on `/volume`

Once `/volume` actually mounts reliably at boot (previous section),
`nzbget`/`sonarr`/`radarr` — ordinary package-installed units, nothing
this project wrote — still have no systemd-level knowledge that they
care about that path. Confirmed by direct testing: on a cold boot they
start before `volume.mount` finishes and hard-fail (`could not create
directory /volume/downloads: Permission denied`) instead of waiting. A
`RequiresMountsFor=/volume` drop-in fixes this the same way Phase 4's
VPN drop-in (if you built that optional phase) already gates these
services on the tunnel.

## A movie download that never imported: obfuscated, par2-less releases

Radarr grabbed a release for a movie, NZBGet reported it as completely
and successfully downloaded, and the movie never showed up in the
library. Radarr's log on a later manual-import attempt said `Could not
find a part of the path '/volume/media/movies/<Title>'` — the folder
had never been created, because Radarr's disk scanner only recognizes
video files by extension, and every file this particular release
downloaded to `completed/` had a garbage name with no extension at all
(`[PRiVATE]_xxx...yEnc  1547065448 (1_2159)`).

The content wasn't actually broken. Checking the largest file with
`file` showed it was a completely valid x265 MP4 — the download itself
was fine. What had gone wrong was purely cosmetic: the release (an
"-xpost" repost) shipped with every filename deliberately obfuscated
and, critically, **no par2 recovery files at all**. NZBGet's own
de-obfuscation only works by cross-referencing a par2 file's internal
filename list against what it downloaded — with none present, it had
nothing to recover the real name from, and moved the files through
exactly as garbled as they arrived. This is invisible ahead of time:
Usenet indexer search results don't expose a release's internal file
listing, so neither Prowlarr nor Radarr can tell a release is missing
its par2 before grabbing it.

The one-off fix was simple once diagnosed: identify the real video file
by its container signature (`ftyp` at byte offset 4 for MP4, in this
case), rename it with the right extension, and hand it to Radarr's
manual-import API. Part 10's `fix-obfuscated.sh` automates exactly that
check for every future download, so it doesn't require catching this by
hand again.

Getting NZBGet to actually run that script took a few wrong turns worth
recording, since NZBGet's own logging doesn't distinguish "extension
malformed" from "extension not found" — both just print `'name'
doesn't exist` — even in debug logging:

- A plain script file with an ordinary comment describing what it does
  is silently ignored. NZBGet scans `ScriptDir` for an exact banner
  structure to recognize a file as a post-processing script at all.
- A `manifest.json`-plus-folder "extension" (the format used for
  scripts installed through NZBGet's Extension Manager/marketplace) is
  *also* not what a hand-placed local script needs — it was accepted as
  valid JSON but never picked up as a script either.
- What actually works, confirmed by testing: a single script file with
  the classic banner — one `### NZBGET POST-PROCESSING SCRIPT` block
  followed by a description, then a separate `### OPTIONS` block (even
  with no options declared under it) — referenced by filename in
  `Extensions=` in `nzbget.conf`. Matching NZBGet's own bundled
  `EMail.py` byte-for-byte in structure was what finally made the
  difference between silent failure and a clean load.
