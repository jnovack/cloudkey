# UCK-G2 hardware reference

Low-level reference for the Cloud Key's own hardware — the front-panel
framebuffer, LEDs, and firmware-layer processes underneath whatever
replaces `ck-ui` (see [Phase 1](Phase-1-De-Ubiquitizing)'s Step 3 and
[jnovack/cloudkey](https://github.com/jnovack/cloudkey)). This is
implementation detail, not a purge or setup walkthrough — see
[Phase 1](Phase-1-De-Ubiquitizing) for that.

Verified by SSH against a live Gen2 Plus (`UCKP.apq8053.v5.1.19`) and a
live plain Gen2 (`UCKG2.apq8053.v5.1.12`), both already past Phase 1.

## Hardware summary

| Field | Value |
| --- | --- |
| SoC | Qualcomm APQ8053, ARM Cortex-A53 (`CPU part 0xd03`), aarch64 — **identical on Gen2 and Gen2 Plus** |
| Power | PoE (802.3af) with on-board battery backup |
| Display | 160×60 framebuffer, `fb_sp8110` driver over SPI |
| LEDs | Blue + white front-panel LEDs, plus a third `ulogo_ctrl` LED |
| Network | Single Ethernet port (`eth0`) |

---

## Framebuffer display

### Device

```text
/dev/fb0
```

Driver: `fb_sp8110` (SPI display, `modalias spi:sp8110`) — identical on
both models.

### Resolution and default mode

```text
160 × 60 pixels, 16 bits per pixel by default
```

Confirmed via sysfs on both units:

```bash
cat /sys/class/graphics/fb0/virtual_size    # 160,60
cat /sys/class/graphics/fb0/bits_per_pixel  # 16
cat /sys/class/graphics/fb0/stride          # 320  (160 px x 2 bytes)
```

The stock configuration is 16bpp (BGR565) — the buffer is
`320 x 60 = 19200` bytes.

### Pixel formats

The kernel driver reports via `FBIOGET_VSCREENINFO`/`FBIOGET_FSCREENINFO`.
A framebuffer client needs to handle three formats depending on
`bits_per_pixel`, since nothing guarantees which mode is active:

| bpp | Format | Channel layout |
| --- | --- | --- |
| 16 | BGR565 | B[4:0] G[5:0] R[4:0] — **stock default on this hardware** |
| 24 | BGR | B G R (8 bits each) |
| 32 | BGR32 or NBGRA | B G R [A] (8 bits each) |

### ioctl constants

```text
FBIOGET_VSCREENINFO = 0x4600
FBIOPUT_VSCREENINFO = 0x4601
FBIOGET_FSCREENINFO = 0x4602
FB_TYPE_PACKED_PIXELS = 0
FB_VISUAL_TRUECOLOR   = 2
```

These are standard Linux UAPI values (`linux/fb.h`), not device-specific —
neither test unit ships kernel headers to check directly (no
`/usr/include`), but the values are stable across every architecture
Linux supports.

### Memory mapping

Open `O_RDWR`, then `mmap(PROT_READ|PROT_WRITE, MAP_SHARED)` with length
from `FixScreenInfo.Smem_len` — on this hardware at the default 16bpp
mode, that's 19200 bytes (`stride x height`), not `width x height x 3`.

### Fonts

Practical font sizes for the 160×60 display: **8–14pt** at 72 DPI. Sizes
below 8 are hard to read; above 14 overflow a single line.  As the screen
ages and dead pixels proliferate, larger fonts are necessary to make up
for the lack of detail.

### Quick test — clear the screen

```bash
# Fill framebuffer with black: 160 x 60 x 2 bytes = 19200 bytes at the
# stock 16bpp mode (confirmed via /sys/class/graphics/fb0/stride = 320).
# Use 28800 only if you've actually switched the mode to 24bpp.
dd if=/dev/zero of=/dev/fb0 bs=19200 count=1 2>/dev/null
```

### Replacing `ck-ui`

The official display daemon is `/usr/bin/ck-ui` (`ii ck-ui 1.4.9-...` on
both units — same package name and version on Gen2 and Gen2 Plus).

> [!WARNING]
> Don't purge the `ck-ui` package — see [Phase 1](Phase-1-De-Ubiquitizing)'s
> danger-zone section. Purging it cascades into removing
> `cloudkey-apq8053-initramfs`, which is what actually bricked a device the
> first time this was attempted.

To replace it at the binary level instead of the package level:

```bash
mv /usr/bin/ck-ui /usr/bin/ck-ui.original
# drop your binary in as /usr/bin/ck-ui
```

Or — the approach [Phase 1](Phase-1-De-Ubiquitizing)'s Step 3 actually
uses — leave the package and binary untouched, and just stop/disable
`ck-ui.service`, then run your own systemd unit instead:

```bash
systemctl disable --now ck-ui
# install your .service to /lib/systemd/system/
```

---

## LEDs

### Sysfs paths

```text
/sys/class/leds/blue/
/sys/class/leds/white/
```

Both units also expose a third, undocumented LED:

```text
/sys/class/leds/ulogo_ctrl/
```

likely the Ubiquiti logo LED, driven by power/battery state (see its
trigger list below) rather than manual on/off like blue/white.

### Control files

| File | Purpose |
| --- | --- |
| `brightness` | Current brightness (`0` = off, `max_brightness` = full — confirmed `max_brightness = 255` on both units) |
| `max_brightness` | Read to get the ceiling value |
| `trigger` | Active trigger — see below, **not** a generic `none`/`timer`/`heartbeat` set |

> [!WARNING]
>
> ```text
> $ cat /sys/class/leds/blue/trigger
> [none] mains-online ext_battery-charging-or-full ext_battery-charging
>   ext_battery-full ext_battery-charging-blink-full-solid typec-online
>   mmc0 mmc1 external0 external1 usb-online battery-charging-or-full
>   battery-charging battery-full battery-charging-blink-full-solid
> ```
>
> Writing `timer` to `trigger` silently succeeds (`exit 0`) but leaves the
> trigger at `none` and creates no `delay_on`/`delay_off` files — it's a
> no-op, not an error, so it's easy to miss. The plain Gen2 has a
> slightly different list (`bms-online` in place of the `ext_battery-*`
> set), but neither model has a software timer trigger. Blinking has to
> be done by your own program toggling `brightness` directly — not via
> the kernel LED trigger framework.

### Examples

```bash
# Turn white LED on at full brightness
echo none  > /sys/class/leds/white/trigger
cat /sys/class/leds/white/max_brightness > /sys/class/leds/white/brightness

# Turn blue LED off
echo none > /sys/class/leds/blue/trigger
echo 0    > /sys/class/leds/blue/brightness
```

Confirmed working as documented, via direct `brightness` writes, on both
units.

### Stock startup sequence

The original `ck-ui` binary uses this LED pattern:

| Phase | Blue | White |
| --- | --- | --- |
| Boot / splash | off | on |
| Ready | on | off |
| Speed test running | blink 500/500 | off |
| Shutdown | off | off |

Both test units, currently in the "Ready" state, matched this exactly
(`blue/brightness = 255`, `white/brightness = 0`) — though given the
trigger findings above, `ck-ui`'s own blink during a speed test is
almost certainly done by its own polling loop, not the kernel `timer`
trigger.

---

## System processes (firmware layer)

These are part of the UCK-G2 base OS and are unrelated to UniFi
Controller — do not remove them (see [Phase 1](Phase-1-De-Ubiquitizing)'s
danger-zone section for the full dependency-chain explanation):

| Process | Role |
| --- | --- |
| `ubnt_monitor` | Hardware health monitor — confirmed running on both units |
| `ck-ui` | Display/LED daemon (replaceable — see above) |
| `monitor-gw-ip.py` | Gateway IP monitor — confirmed running on both units |
| `infctld` | Ubiquiti's network discovery daemon (UDP 10001 discovery + CDP advertisements) — **not** a generic "interface control" daemon, see [Phase 1](Phase-1-De-Ubiquitizing) |

> [!NOTE]
> `ubnt-systemhub`, `uos-agent`, `uos-discovery-client`, and
> `ucore-setup-listener` are also part of the stock firmware layer, but
> both test units had already been through
> [Phase 1](Phase-1-De-Ubiquitizing) and no longer had those packages
> installed at all (only `ck-ui`, `ubnt-tools`, and `earlyoom` remained,
> matching Phase 1's kept-package list) — this table reflects a
> pre-Phase-1 device.

`earlyoom` is confirmed (via `/etc/default/earlyoom` on both units) to
protect `ubios-udapi-ser`, `systemd-network`, and `unifi-core` from OOM
kills:

```text
EARLYOOM_ARGS="-n -r 60 -M 256000 -s 10 -p --avoid '(^|/)(ubios-udapi-ser|systemd-network|unifi-core)$'"
```

---

## Build target

```bash
GOOS=linux GOARCH=arm64 go build -o myapp ./...
```

> [!NOTE]
> Both the Gen2 Plus and the plain Gen2 are `arm64` — confirmed via
> `dpkg --print-architecture` (`arm64` on both) and `file` on
> `/usr/bin/ck-ui` (ELF 64-bit aarch64 on both). There is no separate
> 32-bit target for the plain Gen2.
> [jnovack/cloudkey](https://github.com/jnovack/cloudkey) currently
> publishes only a 32-bit `cloudkey-linux-arm` binary, which still runs
> on both models via the SoC's AArch32 compat execution state (see
> [Phase 1](Phase-1-De-Ubiquitizing)'s Step 3) — that's a packaging
> choice, not a hardware constraint.

## References

- [`github.com/jnovack/cloudkey`](https://github.com/jnovack/cloudkey) —
  replacement `ck-ui` with framebuffer + LED control in Go
- [`r/Ubiquiti` — Repurpose a CloudKey Gen 2 Plus](https://www.reddit.com/r/Ubiquiti/comments/z53id1/repurpose_a_cloudkey_gen_2_plus/)
