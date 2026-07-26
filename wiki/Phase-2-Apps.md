# Setting up a home media server: NZBGet, Sonarr, Radarr, Prowlarr

This is a walkthrough of setting up an automated media-downloading stack
on top of the Debian server built in [Phase 1](Phase-1-De-Ubiquitizing), which
already gives you a working `/volume` for bulk storage — nothing here
assumes a temporary storage location, since a real drive is already in
place by this point.

It's written for someone reading about this kind of thing for the first
time — every piece of jargon gets explained the first time it shows up,
and every command gets a short "why" before the "how." If you already
know what a systemd service or a group ID (GID) is, skim freely.

For the fuller rationale and the pitfalls hit building this the first
time, see [Phase 2 Overview](Phase-2-Overview) — this doc sticks to the steps. Once
everything below is running, [Phase 2 Hardening](Phase-2-Hardening) covers keeping it
that way (surviving upgrades, catching a silent hang, recovering from a
bad download) — a separate doc, because those are all things you add
*after* the stack is up, not part of getting there.

> [!WARNING]
> Since this setup uses an internal volume, and the size of files are
> often large, this guide is for the UCK-G2-PLUS only, and not the
> base UCK-G2.

## Index

- [Part 1: Shared group and directory structure](#part-1-shared-group-and-directory-structure)
- [Part 2: Installing NZBGet (the downloader)](#part-2-installing-nzbget-the-downloader)
- [Part 3: Installing Sonarr (TV shows)](#part-3-installing-sonarr-tv-shows)
- [Part 4: Installing Radarr (movies)](#part-4-installing-radarr-movies)
- [Part 5: Installing Prowlarr (the indexer manager)](#part-5-installing-prowlarr-the-indexer-manager)
- [Part 6: Introducing NZBGet to Sonarr and Radarr](#part-6-introducing-nzbget-to-sonarr-and-radarr)
- [Part 7: Connecting Prowlarr to Sonarr and Radarr](#part-7-connecting-prowlarr-to-sonarr-and-radarr)
- [Registering Sonarr and Radarr's root folders](#registering-sonarr-and-radarrs-root-folders)
- [Adding a real indexer](#adding-a-real-indexer-needs-your-own-credentials)
- **Continues in [Phase 2 Hardening](Phase-2-Hardening)**: Part 8 (surviving upgrades),
  Part 9 (liveness probe), Part 10 (obfuscated-download auto-fix)

## What are we actually building?

Four programs, each with one job, that talk to each other automatically:

- **NZBGet** — the actual downloader. Point it at a source and it fetches
  files.
- **Sonarr** — a TV show manager. You tell it "I want this show," and it
  watches for new episodes, tells NZBGet to download them, and files them
  away neatly once they arrive.
- **Radarr** — the same idea as Sonarr, but for movies instead of TV
  shows.
- **Prowlarr** — an indexer manager. It holds your actual search-provider
  credentials once and keeps Sonarr/Radarr in sync automatically, instead
  of entering them twice.

Think of it like this: Sonarr and Radarr are the ones with taste — they
know what you want and keep an eye out for it. NZBGet is the delivery
truck — it doesn't know or care *what* it's carrying, it just fetches
whatever it's told to fetch. Prowlarr is the phone book both of them use
to find things in the first place.

Docker isn't used here — see [Phase 2 Overview](Phase-2-Overview) for why. Everything
below installs as a plain systemd-managed program instead.

## A few words you'll see a lot in this doc

Quick reference, so nothing below is a surprise:

- **Server / device / box** — the small computer these programs are
  installed on. You're not sitting in front of it; you connect to it over
  the network from your own computer, using a tool called SSH (Secure
  Shell).
- **Package** — a ready-made, installable bundle of a piece of software,
  installed via Debian's `apt` tool.
- **Repository (repo)** — a remote catalog of packages that `apt` can
  pull from.
- **systemd service** — the mechanism Linux uses to keep a program running
  in the background, start it automatically at boot, and restart it if it
  crashes.
- **User and group** — every running program on Linux runs "as" some
  user, and that user belongs to one or more groups, which controls
  *which programs are allowed to read and write which files*. A shared
  group is what lets NZBGet, Sonarr, and Radarr all see the same
  downloaded files without permission errors.
- **API (Application Programming Interface) key** — a long random
  password-like string that lets two programs talk to each other
  automatically, without a human typing a username and password.

## Part 1: Shared group and directory structure

This has to come first — Sonarr's and Radarr's own installers assign
their new user to the `media` group at creation time, so that group
needs to exist before Parts 3 and 4 below, not after.

```bash
groupadd media   # only if it doesn't already exist
```

Now the actual folders these programs will use, directly on `/volume`
(already mounted by Phase 1 — no temporary location needed):

```bash
mkdir -p /volume/downloads/completed /volume/media/tv /volume/media/movies
touch /volume/media/.keep /volume/media/tv/.keep /volume/media/movies/.keep \
  /volume/downloads/completed/.keep
chown root:media /volume /volume/media /volume/media/tv /volume/media/movies
chmod -R 2775 /volume
```

The libraries go under a single parent — `media/` here — rather than
sitting beside each other at the top of `/volume`. The name is yours to
pick; what matters is that there is one. Everything Sonarr and Radarr
own then has a common root, so copying the libraries to a NAS, backing
them up, or exporting them to a media server is one path instead of a
list you have to keep in sync every time you add a library (`music/`,
`books/`). Keep `downloads/` *outside* that parent: it holds in-progress
and post-import leftovers, not library content, and you rarely want it
swept along with the libraries.

The `.keep` files aren't optional — see [Phase 2 Overview](Phase-2-Overview) for why
(a still-active board-level tool deletes empty directories under
`/volume` on every boot). The `2775` mode: `775` means "owner and group
can read/write/enter, everyone else can only look"; the leading `2` (the
"setgid bit") means any new file or folder created inside automatically
belongs to the `media` group too, forever, with no one having to
remember to set that by hand.

**Packaged version**: this step is
[`scripts/runbook/phase2-00-provision-drive.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase2-00-provision-drive.sh),
deployed at `/usr/local/bin/phase2-00-provision-drive.sh` (see File
manifest below) — also what you re-run after replacing the drive, see
the last section of this doc.

## Part 2: Installing NZBGet (the downloader)

NZBGet's maker runs their own package repository:

```bash
apt-get install -y ca-certificates curl gnupg
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://nzbgetcom.github.io/nzbgetcom.asc \
  -o /etc/apt/keyrings/nzbgetcom.asc
chmod a+r /etc/apt/keyrings/nzbgetcom.asc

repo="deb [arch=all signed-by=/etc/apt/keyrings/nzbgetcom.asc]"
repo="$repo https://nzbgetcom.github.io/deb stable main"
echo "$repo" > /etc/apt/sources.list.d/nzbgetcom.list

apt-get update
apt-get install -y nzbget
```

That installs NZBGet as a systemd service, creates its own dedicated
system user, and starts it running on **port 6789**.

Join it to the shared group and point it at `/volume`.

> [!IMPORTANT]
> NZBGet ships with a public, documented default login (`nzbget` /
> `tegbzn6789`). Change it immediately — the commands below generate a
> new password and write it into NZBGet's config.

```bash
usermod -a -G media nzbget
chown -R nzbget:media /volume/downloads

sed -i 's|^MainDir=.*|MainDir=/volume/downloads|' /var/lib/nzbget/nzbget.conf

newpass=$(openssl rand -base64 24 | tr -dc 'A-Za-z0-9' | head -c 24)
sed -i "s|^ControlPassword=.*|ControlPassword=$newpass|" /var/lib/nzbget/nzbget.conf
echo "NZBGet control password: $newpass"   # write this down
```

Gate its startup on `/volume` actually being mounted first — without
this, a cold boot can start NZBGet before the drive is ready and it'll
fail outright instead of waiting (see [Phase 2 Overview](Phase-2-Overview)):

```bash
mkdir -p /etc/systemd/system/nzbget.service.d
cat > /etc/systemd/system/nzbget.service.d/volume-mount.conf <<'EOF'
[Unit]
RequiresMountsFor=/volume
EOF
systemctl daemon-reload
systemctl restart nzbget
```

**Packaged version**: [`scripts/runbook/phase2-01-nzbget.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase2-01-nzbget.sh),
deployed at `/usr/local/bin/phase2-01-nzbget.sh`.

## Part 3: Installing Sonarr (TV shows)

Sonarr doesn't have an `apt` repository. Its maker provides an install
script instead:

```bash
curl -fsSL -o install-sonarr.sh \
  https://raw.githubusercontent.com/Sonarr/Sonarr/develop/distribution/debian/install.sh
```

Normally this asks two questions — what user and group to run as. Over a
remote SSH session those answers never arrive; see [Phase 2 Overview](Phase-2-Overview)
for why. The fix: open the script, find the two lines starting with
`read -r -p`, and replace that whole block with:

```bash
app_uid="sonarr"
app_guid="media"
```

Then run the edited script:

```bash
bash install-sonarr.sh
```

That installs Sonarr to `/opt/Sonarr`, settings in `/var/lib/sonarr`, its
own systemd service, and starts it on **port 8989**.

Gate its startup on `/volume` the same way as NZBGet:

```bash
mkdir -p /etc/systemd/system/sonarr.service.d
cat > /etc/systemd/system/sonarr.service.d/volume-mount.conf <<'EOF'
[Unit]
RequiresMountsFor=/volume
EOF
systemctl daemon-reload
systemctl restart sonarr
```

**Packaged version**: [`scripts/runbook/phase2-02-sonarr.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase2-02-sonarr.sh),
deployed at `/usr/local/bin/phase2-02-sonarr.sh`.

## Part 4: Installing Radarr (movies)

Radarr has no `apt` repository and no install script — a manual,
step-by-step process:

```bash
apt-get install -y curl sqlite3
adduser --system --no-create-home --ingroup media radarr

cd /root
wget --content-disposition \
  'https://radarr.servarr.com/v1/update/master/updatefile?os=linux&runtime=netcore&arch=arm64'
tar -xzf Radarr.*.linux-core-arm64.tar.gz
rm -rf /opt/Radarr
mv Radarr /opt/
chown radarr:media -R /opt/Radarr

mkdir -p /var/lib/radarr
chown radarr:media /var/lib/radarr
chmod 775 /var/lib/radarr
```

(the `arch=arm64` part of that URL matters — it must match this
server's actual processor type, or the program simply won't run)

```bash
cat << 'EOF' > /lib/systemd/system/radarr.service
[Unit]
Description=Radarr Daemon
After=syslog.target network.target
[Service]
User=radarr
Group=media
Type=simple
ExecStart=/opt/Radarr/Radarr -nobrowser -data=/var/lib/radarr/
TimeoutStopSec=20
KillMode=process
Restart=on-failure
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now radarr
```

Radarr bundles a database helper that's incompatible with Debian 11's
GLIBC (a core system library most Linux programs link against) — see
[Phase 2 Overview](Phase-2-Overview) for the full story. Check for it and fix it if
present:

```bash
journalctl -u radarr -n 30 | grep -iE 'exception|error'
```

If you see `DllNotFoundException` / `GLIBC_2.33` (expect to, on this
OS):

```bash
apt-get install -y libsqlite3-0   # usually already present
cd /opt/Radarr
mv libe_sqlite3.so libe_sqlite3.so.backup
ln -s /usr/lib/aarch64-linux-gnu/libsqlite3.so.0 libe_sqlite3.so   # path varies by processor type
systemctl restart radarr
```

Confirm it's actually running now on **port 7878**.

> [!IMPORTANT]
> This symlink doesn't survive an upgrade — updates overwrite
> `/opt/Radarr` wholesale, symlink included. [Phase 2 Hardening](Phase-2-Hardening)
> Part 8 makes that repair automatic; until you've set that up, redo
> the symlink by hand after every update.

Gate its startup on `/volume`:

```bash
mkdir -p /etc/systemd/system/radarr.service.d
cat > /etc/systemd/system/radarr.service.d/volume-mount.conf <<'EOF'
[Unit]
RequiresMountsFor=/volume
EOF
systemctl daemon-reload
systemctl restart radarr
```

**Packaged version**: [`scripts/runbook/phase2-03-radarr.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase2-03-radarr.sh),
deployed at `/usr/local/bin/phase2-03-radarr.sh` — checks for the GLIBC
error automatically and applies the fix only if needed.

## Part 5: Installing Prowlarr (the indexer manager)

An **indexer** is what actually *finds* things to download — the search
engine Sonarr and Radarr use behind the scenes. Prowlarr holds your
indexer credentials once and keeps Sonarr/Radarr in sync automatically.

Same manual install pattern as Radarr, but its own dedicated group (it
never touches media files, only talks to the other apps over HTTP, so it
doesn't need the shared `media` group):

```bash
apt-get install -y curl sqlite3
adduser --system --group --no-create-home prowlarr

cd /root
wget --content-disposition \
  'https://prowlarr.servarr.com/v1/update/master/updatefile?os=linux&runtime=netcore&arch=arm64'
tar -xzf Prowlarr.*.linux-core-arm64.tar.gz
rm -rf /opt/Prowlarr
mv Prowlarr /opt/
chown prowlarr:prowlarr -R /opt/Prowlarr

mkdir -p /var/lib/prowlarr
chown prowlarr:prowlarr /var/lib/prowlarr
chmod 775 /var/lib/prowlarr
```

```bash
cat << 'EOF' > /lib/systemd/system/prowlarr.service
[Unit]
Description=Prowlarr Daemon
After=syslog.target network.target
[Service]
User=prowlarr
Group=prowlarr
Type=simple
ExecStart=/opt/Prowlarr/Prowlarr -nobrowser -data=/var/lib/prowlarr/
TimeoutStopSec=20
KillMode=process
Restart=on-failure
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now prowlarr
```

Prowlarr ships the same GLIBC-incompatible database helper Radarr does,
so apply the identical fix (swap every `Radarr` for `Prowlarr`):

```bash
cd /opt/Prowlarr
mv libe_sqlite3.so libe_sqlite3.so.backup
ln -s /usr/lib/aarch64-linux-gnu/libsqlite3.so.0 libe_sqlite3.so
systemctl restart prowlarr
```

Do this even if Prowlarr starts cleanly without it. Its bundled copy can
be unloadable (`ldd libe_sqlite3.so` reporting `not found`) while the app
still runs, so a clean startup is not evidence the library is sound —
it just means nothing has demanded it yet. Confirm the swap took by
looking for the system library's version in the log:

```bash
journalctl -u prowlarr -n 50 | grep -i 'DatabaseEngineVersionCheck'
```

Confirm it's running on **port 9696**. Prowlarr doesn't touch `/volume`
directly, so it needs no `RequiresMountsFor` drop-in.

**Packaged version**: [`scripts/runbook/phase2-04-prowlarr.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase2-04-prowlarr.sh),
deployed at `/usr/local/bin/phase2-04-prowlarr.sh`.

## Part 6: Introducing NZBGet to Sonarr and Radarr

Now connect the pieces: telling Sonarr and Radarr where NZBGet lives and
how to log into it, so they can hand it files to download automatically.
This uses each app's **API** — the same thing you could do by clicking
through Settings → Download Clients → Add in each app's web page, just
scriptable and repeatable.

Get each app's API key (a long random string each app generates and
stores in its own settings file):

```bash
grep -oP '(?<=<ApiKey>)[^<]+' /var/lib/sonarr/config.xml
grep -oP '(?<=<ApiKey>)[^<]+' /var/lib/radarr/config.xml
```

Tell Sonarr about NZBGet (swap in Sonarr's API key and NZBGet's
password from Part 2):

```bash
curl -X POST \
  -H 'X-Api-Key: <SONARR_API_KEY>' \
  -H 'Content-Type: application/json' \
  --data '{
    "enable": true,
    "protocol": "usenet",
    "priority": 1,
    "removeCompletedDownloads": true,
    "removeFailedDownloads": true,
    "name": "NZBGet",
    "fields": [
      {"name": "host", "value": "localhost"},
      {"name": "port", "value": 6789},
      {"name": "useSsl", "value": false},
      {"name": "username", "value": "nzbget"},
      {"name": "password", "value": "<YOUR_NZBGET_PASSWORD>"},
      {"name": "tvCategory", "value": "Series"},
      {"name": "recentTvPriority", "value": 0},
      {"name": "olderTvPriority", "value": 0},
      {"name": "addPaused", "value": false}
    ],
    "implementation": "Nzbget",
    "configContract": "NzbgetSettings"
  }' \
  http://localhost:8989/api/v3/downloadclient
```

And the equivalent for Radarr — same shape, different port/key, and
`movieCategory` instead of `tvCategory`:

```bash
curl -X POST \
  -H 'X-Api-Key: <RADARR_API_KEY>' \
  -H 'Content-Type: application/json' \
  --data '{
    "enable": true,
    "protocol": "usenet",
    "priority": 1,
    "removeCompletedDownloads": true,
    "removeFailedDownloads": true,
    "name": "NZBGet",
    "fields": [
      {"name": "host", "value": "localhost"},
      {"name": "port", "value": 6789},
      {"name": "useSsl", "value": false},
      {"name": "username", "value": "nzbget"},
      {"name": "password", "value": "<YOUR_NZBGET_PASSWORD>"},
      {"name": "movieCategory", "value": "Movies"},
      {"name": "recentMoviePriority", "value": 0},
      {"name": "olderMoviePriority", "value": 0},
      {"name": "addPaused", "value": false}
    ],
    "implementation": "Nzbget",
    "configContract": "NzbgetSettings"
  }' \
  http://localhost:7878/api/v3/downloadclient
```

### Checking it actually worked

Test with the app-assigned `id` included (usually `1`, the first
download client added — check with `GET .../api/v3/downloadclient` if
unsure); a successful test returns an empty `{}`:

```bash
curl -X POST \
  -H 'X-Api-Key: <SONARR_API_KEY>' \
  -H 'Content-Type: application/json' \
  --data '{ ... same as above ..., "id": 1 }' \
  http://localhost:8989/api/v3/downloadclient/test
```

```bash
curl -H 'X-Api-Key: <SONARR_API_KEY>' http://localhost:8989/api/v3/health
curl -H 'X-Api-Key: <RADARR_API_KEY>' http://localhost:7878/api/v3/health
```

Both should report only one complaint at this point: no indexers
configured yet — expected, see Part 7 and "Adding a real indexer" below.

## Part 7: Connecting Prowlarr to Sonarr and Radarr

Mirror image of Part 6: tell Prowlarr where to find Sonarr and Radarr, so
once a real indexer is added to Prowlarr, it can push results to both
automatically.

```bash
grep -oP '(?<=<ApiKey>)[^<]+' /var/lib/prowlarr/config.xml
```

```bash
curl -X POST \
  -H 'X-Api-Key: <PROWLARR_API_KEY>' \
  -H 'Content-Type: application/json' \
  --data '{
    "syncLevel": "fullSync",
    "enable": true,
    "name": "Radarr",
    "fields": [
      {"name": "prowlarrUrl", "value": "http://localhost:9696"},
      {"name": "baseUrl", "value": "http://localhost:7878"},
      {"name": "apiKey", "value": "<RADARR_API_KEY>"},
      {"name": "syncCategories", "value": [2000,2010,2020,2030,2040,2045,2050,2060,2070,2080,2090]}
    ],
    "implementation": "Radarr",
    "configContract": "RadarrSettings"
  }' \
  http://localhost:9696/api/v1/applications
```

```bash
curl -X POST \
  -H 'X-Api-Key: <PROWLARR_API_KEY>' \
  -H 'Content-Type: application/json' \
  --data '{
    "syncLevel": "fullSync",
    "enable": true,
    "name": "Sonarr",
    "fields": [
      {"name": "prowlarrUrl", "value": "http://localhost:9696"},
      {"name": "baseUrl", "value": "http://localhost:8989"},
      {"name": "apiKey", "value": "<SONARR_API_KEY>"},
      {"name": "syncCategories", "value": [5000,5010,5020,5030,5040,5045,5050,5090]},
      {"name": "animeSyncCategories", "value": [5070]},
      {"name": "syncAnimeStandardFormatSearch", "value": true}
    ],
    "implementation": "Sonarr",
    "configContract": "SonarrSettings"
  }' \
  http://localhost:9696/api/v1/applications
```

Test the same way as Part 6 (include the assigned `id` — check with `GET
.../api/v1/applications` if unsure), a successful test returns `{}`:

```bash
curl -X POST \
  -H 'X-Api-Key: <PROWLARR_API_KEY>' \
  -H 'Content-Type: application/json' \
  --data '{ ... same body as above ..., "id": 1 }' \
  http://localhost:9696/api/v1/applications/test
```

## Registering Sonarr and Radarr's root folders

Point both apps at their actual libraries under the `media/` parent
created in Part 1:

```bash
curl -X POST \
  -H 'X-Api-Key: <SONARR_API_KEY>' \
  -H 'Content-Type: application/json' \
  --data '{"path": "/volume/media/tv"}' \
  http://localhost:8989/api/v3/rootfolder

curl -X POST \
  -H 'X-Api-Key: <RADARR_API_KEY>' \
  -H 'Content-Type: application/json' \
  --data '{"path": "/volume/media/movies"}' \
  http://localhost:7878/api/v3/rootfolder
```

This is where completed downloads actually end up. With Completed
Download Handling on (the default), Sonarr and Radarr poll NZBGet's
history and, on success, **move** the file out of
`/volume/downloads/completed/` into the root folder above, renaming and
foldering it on the way. That directory is therefore transient — it
drains within about a minute of a download finishing. Anything you build
downstream (a sync to a NAS, a media-server library path) should watch
the root folders, not `completed/`, or it will usually find nothing.

### Zero-pad Sonarr's season folders

Sonarr defaults to `Season {season}`, producing `Season 7`. Most
existing libraries — and every other tool that sorts a directory
listing — use zero-padded `Season 07`, which keeps seasons in order past
season 9 and matches what's likely already on a NAS.

> [!IMPORTANT]
> Set this before the library has any content. Changing it later means
> manually renaming folders that Sonarr's database already points at.

Set it now:

```bash
curl -X PUT \
  -H 'X-Api-Key: <SONARR_API_KEY>' \
  -H 'Content-Type: application/json' \
  --data '{ ...current config/naming body..., "seasonFolderFormat": "Season {season:00}" }' \
  http://localhost:8989/api/v3/config/naming
```

`GET .../api/v3/config/naming` first and edit that body — the endpoint
replaces the whole object, so posting only the one field wipes the rest.

Note this is independent of `renameEpisodes`, which stays `false` here.
That setting controls whether Sonarr rewrites episode *filenames* to its
own format; leaving it off keeps the original release names. If you
change the season format after files exist, Sonarr's rename command
won't move them while `renameEpisodes` is false — move the folders by
hand and trigger `RescanSeries` so the database follows.

## Adding a real indexer (needs your own credentials)

Everything above is done and doesn't need repeating. All that's left is
telling Prowlarr about a real place to search, which needs an account
with an actual Usenet indexer/provider — a separate signup process
outside anything covered here. Once you have those credentials: open
Prowlarr (port 9696) → Settings → Indexers → Add Indexer, fill in
whatever your provider's instructions say (a different API key from any
used above — this one talks to an outside service, not between the
programs on this server). Because Prowlarr's already connected to Sonarr
and Radarr from Part 7, this is the only remaining step — it syncs out to
both automatically. Once done, both apps' health checks stop complaining
about missing indexers — the final signal everything's wired up
end to end.

## What's next

The stack above is complete and works on a plain internet connection —
nothing past this point is required to use it. From here, a few
independent decisions, each its own phase, each skippable on its own
merits:

- **Do you want these apps' outbound traffic (searches, downloads,
  indexer lookups) to fail closed behind a VPN** — no path out at all
  if the tunnel drops, rather than silently falling back to your normal
  connection? That's [Phase 4](Phase-4-WireGuard). If you build it, come back
  to [Phase 2 Hardening](Phase-2-Hardening) Part 9 and Part 11 — both work either way,
  but need one line of config set if you did.
- **Do you want the ongoing operational hardening** (surviving in-app
  upgrades, a liveness probe, auto-fixing a class of bad download,
  delivering the finished library elsewhere)? That's
  [Phase 2 Hardening](Phase-2-Hardening), and doesn't depend on the VPN choice above at
  all.
- **Do you want remote access to this box, or its own coordination
  tunnel?** That's Phases 3 and 5, both independent of anything here.

## File manifest

Everything created by this doc, for a final checklist. See
[Phase 2 Hardening](Phase-2-Hardening)'s own File manifest for Parts 8–10.

| Path | Purpose |
| --- | --- |
| `/usr/local/bin/phase2-00-provision-drive.sh` | Part 1 — shared group + `/volume` directory structure; source of truth [`scripts/runbook/phase2-00-provision-drive.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase2-00-provision-drive.sh) |
| `/usr/local/bin/phase2-01-nzbget.sh` | Part 2 — NZBGet install; source of truth [`scripts/runbook/phase2-01-nzbget.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase2-01-nzbget.sh) |
| `/usr/local/bin/phase2-02-sonarr.sh` | Part 3 — Sonarr install; source of truth [`scripts/runbook/phase2-02-sonarr.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase2-02-sonarr.sh) |
| `/usr/local/bin/phase2-03-radarr.sh` | Part 4 — Radarr install; source of truth [`scripts/runbook/phase2-03-radarr.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase2-03-radarr.sh) |
| `/usr/local/bin/phase2-04-prowlarr.sh` | Part 5 — Prowlarr install; source of truth [`scripts/runbook/phase2-04-prowlarr.sh`](https://github.com/jnovack/cloudkey/blob/main/scripts/runbook/phase2-04-prowlarr.sh) |
| `/etc/systemd/system/{nzbget,sonarr,radarr}.service.d/volume-mount.conf` | gates each service's startup on `/volume` being mounted |

## If you ever replace the drive

Follow [Phase 1](Phase-1-De-Ubiquitizing) Step 7 to format and mount the new
drive at `/volume`, then just re-run Part 1's script:

```bash
phase2-00-provision-drive.sh
```

Sonarr/Radarr's root-folder registrations live in their own app
databases (`/var/lib/sonarr`, `/var/lib/radarr` — not on `/volume`), so
they survive a drive swap intact and don't need re-registering — once
the directories exist again with the right ownership, both apps'
`GET /api/v3/rootfolder` should report `"accessible": true` again on
their own.
