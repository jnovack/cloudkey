# Headscale coordination server: rationale and pitfalls

This is the companion piece to [Phase 5](Phase-5-Headscale) — that doc is the
runbook (do this, then this); this one is prose: why things are built
the way they are, and the real problems hit along the way, kept here so
none of it gets lost or has to be relearned. Read it if you're
debugging one of these exact symptoms or just curious why a step
exists — it's context for *why*, not steps to run, and skipping it
won't stop you from following the runbook.

## Why headplane over the other admin UIs

[headplane](https://github.com/tale/headplane) is actively maintained,
tracks Headscale's API version dynamically instead of hardcoding to one
release, and does support ACL editing.

[headscale-admin](https://github.com/GoodiesHQ/headscale-admin) and
[gurucomputing/headscale-ui](https://github.com/gurucomputing/headscale-ui)
were both considered and passed over — neither has kept pace with
current Headscale releases, and neither has an ACL/policy editor.

## headplane's registry tag didn't exist

`ghcr.io/tale/headplane:v0.7.0` — the exact tag matching the GitHub
release actually being targeted — turned out not to exist on the
registry at all (`docker pull` failed with `manifest unknown`).
headplane's release automation simply never pushed a versioned tag for
that release, only `latest`.

Confirmed what `latest` actually was before trusting it: pulled its
image config blob and checked the `org.opencontainers.image.version`
label baked in at build time, plus its `created` timestamp, against the
GitHub release date. Both matched `0.7.0` exactly. Rather than deploy
against a floating `latest` tag (which could move out from under this
setup on the next push), the image reference in `headscale.yml` pins to
that specific build's immutable digest
(`ghcr.io/tale/headplane@sha256:...`) instead — more reproducible than
even a normal version tag, since a digest can't be silently repointed.

If headplane later starts publishing real version tags again, this is
worth revisiting — a plain `vX.Y.Z` tag is more readable in a diff than
an opaque digest, once it's actually trustworthy.

## A 128M memory limit didn't crash the container — it stalled it

First deploy attempt with headplane capped at `128M` came up, logged a
completely normal startup (connected to Headscale, started listening on
its port), then went unresponsive to every single request — including
its own Docker healthcheck, which timed out repeatedly until Swarm
eventually replaced the task.

Nothing in the logs pointed at this. What did: `docker stats` showed the
container pinned at `121.5MiB / 128MiB` (94.9%) with **0% CPU** — alive,
not crashed, just wedged. A Node.js process running this close to its
cgroup memory ceiling can end up spending all its time on GC pressure
trying to stay under the limit, effectively freezing forward progress
without ever triggering an OOM kill. Raised to `384M` / `0.5` CPU and it
came up clean and stayed under a third of that limit in steady state.

> [!TIP]
> Sizing containers on this kind of box generally: a container that
> "starts, logs normally, then stops responding" with no error in its
> own logs is worth checking `docker stats` on before assuming it's a
> config or code problem — an unresponsive-but-not-crashed process
> under a tight memory limit looks exactly like a hung application
> from the logs alone.

## Enrolling the client introduced a boot-time race with the VPN killswitch

First reboot after enrolling the Cloud Key ([Phase 5](Phase-5-Headscale) Part
9) came back with the entire Phase 4 VPN killswitch failed (Phase 4 —
see there for why it's optional and what it protects) —
`netns-vpn.service` exited with "Another app is currently holding
the xtables lock." The Phase 2 apps correctly refused to start as a
result (their fail-closed design working exactly as intended, not a
separate bug), and a pre-existing Phase 4 self-heal timer
(`vpn-heal.timer`, `OnBootSec=60`) caught and fixed it about
35 seconds later on its own.

Root cause: `tailscaled` configures its own iptables rules at boot at
roughly the same moment `netns-vpn-up.sh` runs its own — confirmed
in the journal, both logging iptables activity in the same second. The
namespace script's `iptables` calls didn't pass `-w` (wait for the
lock), so whichever one lost the race failed outright instead of just
waiting the split-second for the other to finish.

Fixed by adding `-w 5` to all four `iptables` calls in
`netns-vpn-up.sh` (folded into [Phase 4](Phase-4-WireGuard)/
`scripts/runbook/phase4-netns-vpn-up.sh`, since this isn't really
Tailscale-specific — any boot-time process that also touches iptables,
Docker included, could have caused the same race). Confirmed fixed
with a second real reboot: clean single-attempt startup for every
service, no retry needed. The self-heal timer remains in place
regardless, as it should — this fix removes the common cause of needing
it at boot, not the value of having it. Only relevant at all if Phase 4
was built — a setup without the VPN killswitch has no namespace or
iptables setup at boot to race against in the first place.

## headplane's ACL editor requires `policy.mode: database`, not `file`

Tried to edit the ACL through headplane's UI and got "Read-only ACL
Policy," pointing at `policy.mode` being `file`. The instinct was that
this was just a filesystem permissions issue — `acl.hujson` was already
mounted read-write into the headplane container — but that wasn't it at
all: headplane's ACL editor doesn't touch the file directly, it calls
Headscale's own `SetPolicy` API, and Headscale rejects that call outright
in file mode regardless of file permissions. File mode is meant to be
hand-edited and reloaded by an administrator; `database` mode is what
makes the live API-driven read/write path (which headplane depends on)
actually work.

Fixed by switching `policy.mode` to `database` in `config.yaml`, then
seeding the database with the existing policy via `headscale policy set
--file /etc/headscale/acl.hujson` (`make policy-set`) so the switch
didn't silently reset anyone to a different default policy. Confirmed
all three already-enrolled devices stayed connected and online through
the config change, the restart, and the reseed. `acl.hujson` is no
longer live-enforced — it's now just the seed file for that one command
— so the now-pointless read-write mount into the headplane container
was removed too.
