# macOS autossh rescue tunnel

An always-on reverse SSH tunnel from a Mac out to a relay host you
control, so the Mac stays reachable even when its own network moves,
its firewall changes, or DHCP hands it somewhere you can't find. The
relay exposes a **loopback-only** port; you reach the Mac by logging
into the relay with your own normal account and jumping through.

## Placeholders

| Placeholder | Meaning | Repeat per relay? |
| --- | --- | --- |
| `<vps>` | a relay host you control (e.g. `relay.example.net`) | **Yes** — repeat every part of this guide once per relay |
| `<client-name>` | stable label for this Mac, used in the relay account name and key comments | No |
| `<username>` | your login account on the Mac; also your normal login account on the relay | No |
| `<port>` | the loopback port bound on the relay for this Mac (e.g. `2222`) | No |

**On `<vps>` scaling**: the guide is written for one relay, but a single
relay is a single point of failure — if it is down or unreachable, so is
your rescue path. Running **two** unrelated relays means either one alone
is enough to get back in. To do that, run every part once per `<vps>`:
each relay gets its own key pair, its own account, and its own daemon.
Nothing is shared between them except the client name and the port.

**On `<client-name>`**: use `scutil --get LocalHostName`, not `hostname`.
`hostname` on macOS returns whatever DHCP/mDNS hands back at the moment
and can change on a network move; `LocalHostName` is the stable, user-set
identifier. Don't use `ComputerName` — it typically contains spaces and
punctuation, which makes it unusable as a Unix account name.

```bash
scutil --get LocalHostName
```

**On `<port>`**: it is bound on the relay's loopback, so if a relay serves
more than one client, **each client needs its own port** — two clients
sharing one means whichever connects second fails to bind. Check before
picking:

```bash
ss -tln | grep 127.0.0.1:<port>      # on the relay, before choosing
```

## Prerequisites

- A Mac with Homebrew installed.
- One or more relay hosts (`<vps>`) reachable from the internet, running
  `openssh-server`, with an account you can `sudo` from. This guide
  assumes the relay is Debian/Ubuntu.
- Each `<vps>` should be independent of the others and of the Mac — a
  relay on the same network as the Mac rescues nothing.

## Part 0 — Prepare the Mac

### Remote Login must be on

Verify this **before** going further, because a `-R <port>:22` tunnel
forwarding into a closed local port comes up looking perfectly healthy
and is completely useless: the relay end binds, the daemon reports
running, and every attempt to use it returns `Connection refused`.

```bash
netstat -an | grep LISTEN | grep '\.22 '
#    expect: tcp4 ... *.22 ... LISTEN
```

> [!WARNING]
> Do not check this with unprivileged `lsof`. `lsof -nP -iTCP:22
> -sTCP:LISTEN` run as a normal user prints *nothing* when sshd is
> running perfectly — it cannot see root-owned sockets, and reports that
> absence the same way it reports a genuinely closed port. It will tell
> you Remote Login is off when it is on. Use `netstat -an` (needs no
> privileges and sees every socket) or `sudo lsof`.

To enable it: **System Settings → General → Sharing → Remote Login**, or
`sudo systemsetup -setremotelogin on`. The CLI form needs Full Disk Access
granted to your terminal and fails with an unhelpful error without it, so
the GUI toggle is the reliable path.

Restrict *who* may log in while you are in that settings pane. "All
users" is the default and is broader than this needs.

### Sleep will drop the tunnel

A sleeping Mac is not reachable, tunnel or not. On wake, autossh
reconnects within roughly `ServerAliveInterval × ServerAliveCountMax`
(90s as configured below) — so a sleeping machine is a rescue tunnel that
works *eventually*, which is not what a rescue tunnel is for.

If this machine is meant to be reachable at any time, disable system
sleep:

```bash
pmset -g | grep -E '^\s*(sleep|displaysleep|standby)'
sudo pmset -a sleep 0                     # display sleep is fine; system sleep is not
```

This is reasonable on a desktop that lives on mains power. Do **not** do
it on a laptop running on battery.

### FileVault caps what this tunnel can rescue

FileVault, macOS's built-in full-disk encryption, unlocks the data
volume during pre-boot authentication. Once the machine reaches the
login window the tunnel starts normally and no login is required — but
the machine will not get that far unattended. An unexpected reboot parks
it at the FileVault prompt with no network stack and therefore no
tunnel, until somebody unlocks it at the physical keyboard.

> [!IMPORTANT]
> This tunnel rescues a machine that is *up but unreachable*. It cannot
> rescue one that has rebooted while you are away.

When rebooting deliberately from a remote session, use an authenticated
restart, which stashes the unlock key for exactly one boot:

```bash
sudo fdesetup authrestart
```

## Part 1 — Install autossh

```bash
brew install autossh
```

The Homebrew path is used directly by the daemon. That is safe here
**only because the tunnel does not run as root** — see
[Why this does not run as root](#why-this-does-not-run-as-root) below.

Note the prefix, since it differs by architecture (`/opt/homebrew` on
Apple Silicon, `/usr/local` on Intel) and is needed for the plist:

```bash
echo "$(brew --prefix)/bin/autossh"
```

### Why this does not run as root

The usual Linux recipe runs the tunnel as root, and the obvious move is to
mirror that. Don't. Work out what the tunnel actually needs:

- The relay-side listener on `<port>` is bound **on the relay**, by
  `tunnel-<client-name>`. Nothing binds a port on this machine.
- The local end of `-R <port>:22` is an ordinary TCP connect to
  `127.0.0.1:22`. Any user can do that; it is a client connection, not a
  privileged bind.
- Reading the private key needs only ownership of the key file.

That is zero root privileges, so the daemon runs as `<username>`.

This is not just tidiness — it removes an actual hole. Homebrew's
directory is group-writable by `admin`:

```text
drwxrwxr-x  548 <username>  admin  /opt/homebrew/bin
-r-xr-xr-x    1 <username>  admin  /opt/homebrew/Cellar/autossh/1.4g/bin/autossh
```

A **root** daemon pointed at that path would mean anything compromising
your account also chooses what root executes at every boot — so a root
setup would need a root-owned copy of the binary, plus the discipline to
re-copy it after every `brew upgrade`. Running as `<username>` makes the
whole problem vanish: you owning the binary you execute is not an
escalation. The brew path is used as-is and upgrades need no follow-up.

**Daemon, not Agent.** It still belongs in `/Library/LaunchDaemons`
(system-wide, starts at boot) with a `UserName` key — *not* in
`~/Library/LaunchAgents`. A LaunchAgent only runs inside a GUI login
session, so a reboot that stops at the login window would leave the
tunnel down, which is exactly the moment a rescue tunnel has to be up. A
LaunchDaemon with `UserName` gets boot-time start *and* an unprivileged
process.

If you later want this stricter, the next step is a dedicated service
account (`_autossh`) so the tunnel process cannot read your login keys at
all. That is real hardening, at the cost of `dscl` user creation; running
as `<username>` is a reasonable stopping point for a single-user
workstation.

## Part 2 — Generate the tunnel keys

One key pair per relay, owned by `<username>` — the daemon runs as you,
so root-owned keys would simply be unreadable to it.

`/usr/local` itself is `root:wheel` and not writable by you (and on Apple
Silicon `/usr/local/etc` and `/usr/local/var` may not exist at all). So
the parent directories need `sudo` once, handed to you immediately;
everything after that is unprivileged:

```bash
sudo install -d -o <username> -g staff -m 755 /usr/local/etc/autossh
sudo install -d -o <username> -g staff -m 755 /usr/local/var/log/autossh
install -d -m 700 /usr/local/etc/autossh/keys

ssh-keygen -t ed25519 -N '' \
  -f /usr/local/etc/autossh/keys/<vps>_tunnel \
  -C "client-autossh@<client-name>"

chmod 600 /usr/local/etc/autossh/keys/<vps>_tunnel
chmod 644 /usr/local/etc/autossh/keys/<vps>_tunnel.pub
```

Repeat the `ssh-keygen` for each additional `<vps>`; the directories are
created once.

The log directory is created here for the same reason: launchd will not
create a missing parent for `StandardOutPath`, and a daemon running as
`<username>` cannot write to `/var/log` at all — that combination fails
silently, giving you a tunnel with no diagnostics.

`/usr/local/etc` rather than `/etc` because these are no longer
root-owned files, and rather than `~/.ssh` because a system LaunchDaemon
should not depend on a home directory — that keeps the path valid if the
tunnel is later moved to a dedicated service account.

These keys exist **only on this machine** and are never copied anywhere.
Only the `.pub` halves leave. They are also completely separate from your
normal login key — a tunnel key that can only request one port forward is
not a credential worth stealing.

## Part 3 — Pin the relay host keys

`StrictHostKeyChecking yes` is set in the daemon, so each relay's host key
must be known in advance or the tunnel will never connect. A bare keyscan
is trust-on-first-use, so fetch the key from **two independent vantage
points** and compare before trusting it — that is what rules out a
man-in-the-middle sitting between the Mac and the relay at setup time:

```bash
ssh-keyscan -t ed25519 <vps>   # run from this Mac
ssh-keyscan -t ed25519 <vps>   # run from a second, unrelated machine
```

Once the two agree:

```bash
ssh-keyscan -t ed25519 <vps> >> /usr/local/etc/autossh/keys/known_hosts
chmod 644 /usr/local/etc/autossh/keys/known_hosts
```

One shared `known_hosts` file for every relay is fine — `>>` appends
rather than overwrites, so run this once per `<vps>`.

## Part 4 — Relay-side setup

Run on **each** `<vps>`. The relay doesn't know or care whether the
client on the other end is a Mac or a Raspberry Pi, so this is the same
dedicated-account-and-restricted-key pattern used for any autossh
client. The commands, using this guide's `<client-name>` and `<port>`:

```bash
sudo useradd -r -m -d /home/tunnel-<client-name> \
  -s /usr/sbin/nologin tunnel-<client-name>
sudo passwd -l tunnel-<client-name>
```

Locked password + `nologin` shell is deliberate defense in depth on top
of the `authorized_keys` restriction below: even a misconfigured or
future-relaxed `authorized_keys` line still cannot produce an
interactive session for this account.

Then install the restricted public key:

```bash
relay_user=tunnel-<client-name>
sudo -u "$relay_user" mkdir -p -m 700 "/home/$relay_user/.ssh"
sudo -u "$relay_user" touch "/home/$relay_user/.ssh/authorized_keys"
key_line='command="/bin/false",no-agent-forwarding,no-X11-forwarding,no-pty,no-user-rc,permitlisten="127.0.0.1:<port>" <CONTENTS OF <vps>_tunnel.pub>'
sudo -u "$relay_user" grep -qxF "$key_line" \
  "/home/$relay_user/.ssh/authorized_keys" \
  || printf '%s\n' "$key_line" \
    | sudo -u "$relay_user" tee -a \
      "/home/$relay_user/.ssh/authorized_keys" \
      >/dev/null
sudo chmod 600 "/home/$relay_user/.ssh/authorized_keys"
```

This key can only establish the one forward it needs — nothing else.
`permitlisten` controls what the *reverse* forward (`-R`) is allowed to
bind to on the relay: this key can open `127.0.0.1:<port>` and nothing
else. `command="/bin/false"`, combined with the `nologin` shell above,
means the key can never produce an interactive session or run an
arbitrary command, even if used directly instead of through autossh.

Each relay gets **its own** relay-specific public key — don't reuse one
key pair across relays. Verify:

```bash
ssh <vps> 'sudo cat /home/tunnel-<client-name>/.ssh/authorized_keys'
```

## Part 5 — Write the LaunchDaemon

One plist per relay, at
`/Library/LaunchDaemons/local.autossh-tunnel-<vps>.plist`.

> [!IMPORTANT]
> Placeholders change form inside this file. Everywhere else this guide
> writes `<vps>`, `<port>`, `<client-name>`; a plist is XML, where
> anything in angle brackets is a tag, so `<vps>` would be parsed as
> markup and the file would not load — `plutil -lint` rejects it before
> launchd ever sees it.

Inside the XML the same placeholders appear bare, and `<key>` /
`<string>` are real XML you keep as-is:

| Elsewhere in this guide | In the plist below |
| --- | --- |
| `<vps>` | `RELAY` |
| `<port>` | `PORT` |
| `<client-name>` | `CLIENTNAME` |
| your macOS account | `USERNAME` |

As written below the file is valid XML, so you can lint it before
substituting and know that any later failure is your edit, not the
template.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>local.autossh-tunnel-RELAY</string>

  <key>UserName</key><string>USERNAME</string>
  <key>GroupName</key><string>staff</string>

  <key>ProgramArguments</key>
  <array>
    <string>/opt/homebrew/bin/autossh</string>
    <string>-M</string><string>0</string>
    <string>-N</string>
    <string>-F</string><string>/dev/null</string>
    <string>-o</string><string>ServerAliveInterval 30</string>
    <string>-o</string><string>ServerAliveCountMax 3</string>
    <string>-o</string><string>ExitOnForwardFailure yes</string>
    <string>-o</string><string>StrictHostKeyChecking yes</string>
    <string>-o</string><string>UserKnownHostsFile=/usr/local/etc/autossh/keys/known_hosts</string>
    <string>-o</string><string>IdentitiesOnly yes</string>
    <string>-i</string><string>/usr/local/etc/autossh/keys/RELAY_tunnel</string>
    <string>-R</string><string>127.0.0.1:PORT:127.0.0.1:22</string>
    <string>tunnel-CLIENTNAME@RELAY</string>
  </array>

  <key>EnvironmentVariables</key>
  <dict>
    <key>AUTOSSH_GATETIME</key><string>0</string>
    <key>AUTOSSH_POLL</key><string>30</string>
    <key>HOME</key><string>/Users/USERNAME</string>
  </dict>

  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>15</integer>

  <key>StandardOutPath</key>
  <string>/usr/local/var/log/autossh/RELAY.log</string>
  <key>StandardErrorPath</key>
  <string>/usr/local/var/log/autossh/RELAY.log</string>
</dict>
</plist>
```

Repeat the file once per relay, substituting that relay's `<vps>` in the
`Label`, the `-i` key path, the destination host, and both log paths. The
`<port>` stays the same across relays — it is bound on each relay's own
loopback, so there is no collision.

Set `ProgramArguments[0]` to the `brew --prefix` path from Part 1
(`/usr/local/bin/autossh` on Intel).

Three details are easy to miss:

- **`UserName` / `GroupName`** are what make this unprivileged. Without
  them a LaunchDaemon runs as root, which is what
  [Why this does not run as root](#why-this-does-not-run-as-root) rules
  out. The plist file itself still has to be root-owned (below) — that
  governs who may *define* the job, not who it runs as.
- **`-F /dev/null`** stops ssh reading `~/.ssh/config`. Everything the
  tunnel needs is already explicit on the command line, and a personal
  config entry for these hosts (a `ProxyJump`, a different `User`, an
  `IdentityFile`) would otherwise silently change what the daemon does.
- **`HOME`** is set because launchd gives a daemon a minimal environment.
  ssh does not need it given the flags above, but it warns without it,
  and the warning lands in the log where it is pure noise.

Ownership matters — launchd refuses to load a daemon plist that is
writable by anyone but root:

```bash
sudo chown root:wheel /Library/LaunchDaemons/local.autossh-tunnel-*.plist
sudo chmod 644 /Library/LaunchDaemons/local.autossh-tunnel-*.plist
sudo plutil -lint /Library/LaunchDaemons/local.autossh-tunnel-*.plist
```

## Part 6 — Load and verify

Modern `launchctl` syntax (`load`/`unload` are deprecated):

```bash
sudo launchctl bootstrap system \
  /Library/LaunchDaemons/local.autossh-tunnel-<vps>.plist

sudo launchctl print system/local.autossh-tunnel-<vps> | head -20
```

Then prove it end to end — a listener alone is not proof:

```bash
# 1. bound on the relay, owned by the right account
ssh <vps> 'sudo lsof -nP -i @127.0.0.1:<port> -sTCP:LISTEN'
#    expect owner: tunnel-<client-name>

# 2. the tunnel actually carries a session
ssh -J <username>@<vps> -p <port> <username>@127.0.0.1 'scutil --get LocalHostName'
#    expect: <client-name>

# 3. logs are clean (no auth retry loop)
tail -20 /usr/local/var/log/autossh/<vps>.log
```

Repeat for each additional relay. Do **one relay at a time** and confirm
each before starting the next, so a misconfiguration cannot turn into an
auth retry loop against every relay at once.

**If any of the three checks fail**, the log from check 3 is the place to
look. Common failure signatures:

- `Permission denied (publickey,password)` — the relay-side
  `authorized_keys` for `tunnel-<client-name>` is missing or wrong on
  that relay.
- `Connection refused` — the relay's firewall (`fail2ban` or similar) is
  blocking this Mac's IP, sshd is down there, or the hostname/port is
  wrong.
- Repeated `starting ssh (count N)` with `N` climbing fast (more than
  once every few seconds) — autossh's own internal retry logic hammering
  the relay, which can trip its `fail2ban`. Unload the daemon
  immediately rather than let it keep retrying:

  ```bash
  sudo launchctl bootout system/local.autossh-tunnel-<vps>
  ```

  Fix the underlying cause, then re-`bootstrap` it (Part 6, above).

## Everyday use

From anywhere you can reach the relay:

```bash
ssh -J <username>@<vps> -p <port> <username>@127.0.0.1
```

The relay-side port is loopback-only, so it is not exposed to the
internet — you must come through your own account on the relay first.

## File manifest

Everything this guide adds, per relay where noted.

### Created on the Mac

| Path | Owner / mode | Purpose |
| --- | --- | --- |
| `/usr/local/etc/autossh/` | `<username>:staff` 755 | config root (created with sudo, chowned to you) |
| `/usr/local/etc/autossh/keys/` | `<username>:staff` 700 | key directory |
| `/usr/local/etc/autossh/keys/<vps>_tunnel` | `<username>:staff` 600 | private key, one per relay |
| `/usr/local/etc/autossh/keys/<vps>_tunnel.pub` | `<username>:staff` 644 | matching public key |
| `/usr/local/etc/autossh/keys/known_hosts` | `<username>:staff` 644 | pinned relay host keys, all relays |
| `/usr/local/var/log/autossh/` | `<username>:staff` 755 | log dir; launchd will not create it |
| `/usr/local/var/log/autossh/<vps>.log` | `<username>:staff` | stdout+stderr, one per relay |
| `/Library/LaunchDaemons/local.autossh-tunnel-<vps>.plist` | `root:wheel` 644 | daemon, one per relay |

### Installed by Homebrew (not by this guide)

| Path | Purpose |
| --- | --- |
| `$(brew --prefix)/Cellar/autossh/` | the formula itself |
| `$(brew --prefix)/bin/autossh` | symlink into the Cellar; this is what the daemon runs |

### System settings changed

| Setting | Change | How to check |
| --- | --- | --- |
| Remote Login (sshd) | must be **on** | `netstat -an \| grep LISTEN \| grep '\.22 '` |
| System sleep | → `0` (never) | `pmset -g \| grep sleep` |

### On each relay (not macOS)

| Path | Purpose |
| --- | --- |
| `tunnel-<client-name>` account | locked, `nologin`, no password |
| `/home/tunnel-<client-name>/.ssh/authorized_keys` | 600, single restricted key line per relay |

### Not modified

`/etc/ssh/sshd_config`, `~/.ssh/`, your login keys, and the shell
environment are all untouched. Everything above is additive.

## Teardown

Run once per relay:

```bash
sudo launchctl bootout "system/local.autossh-tunnel-<vps>" 2>/dev/null
sudo rm -f "/Library/LaunchDaemons/local.autossh-tunnel-<vps>.plist"
rm -f "/usr/local/var/log/autossh/<vps>.log"
ssh <vps> 'sudo userdel -r tunnel-<client-name>'
```

Then, once all relays are removed:

```bash
rm -rf /usr/local/etc/autossh /usr/local/var/log/autossh
```

Remote Login and the sleep setting are left as they are — decide those
separately, since other things may now depend on them.
