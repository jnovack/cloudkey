package state

import (
	"encoding/json"
	"sync"

	"github.com/rs/zerolog/log"
)

// subscriberBuffer is how many pending messages a single SSE subscriber's
// channel holds before the Hub starts dropping updates for that client. A
// browser that stalls (backgrounded tab, dead socket not yet timed out) must
// never block a collector goroutine, so Publish does a non-blocking send and
// drops on a full buffer rather than waiting. The next update that does get
// through carries fresh state, so a dropped intermediate frame only costs that
// client a little latency, never correctness.
const subscriberBuffer = 8

// Snapshot is the full dashboard model. Its JSON tags are the wire contract
// with website/dashboard.html — the keys and casing must match the seed()
// object there exactly, since the browser merges each message into its state by
// these top-level keys. The three disks are named fields rather than a map so
// their keys ("ssd"/"rootfs"/"sdcard") and presence are fixed at compile time.
type Snapshot struct {
	Host    Host     `json:"host"`
	Net     Net      `json:"net"`
	CPU     CPU      `json:"cpu"`
	Mem     Mem      `json:"mem"`
	Ssd     Disk     `json:"ssd"`
	Rootfs  Disk     `json:"rootfs"`
	Sdcard  Disk     `json:"sdcard"`
	Apps    []App    `json:"apps"`
	Tunnels []Tunnel `json:"tunnels"`
}

// Host identifies the box: hostname, OS pretty-name, architecture, and uptime
// in whole seconds (the front-end formats the duration).
type Host struct {
	Name      string `json:"name"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	UptimeSec int64  `json:"uptimeSec"`
}

// Net holds the LAN and WAN IPv4 addresses as display strings.
type Net struct {
	LAN string `json:"lan"`
	WAN string `json:"wan"`
}

// CPU is utilization percent (0-100), physical core count, and 1-minute load
// average.
type CPU struct {
	Pct     float64 `json:"pct"`
	Cores   int     `json:"cores"`
	LoadAvg float64 `json:"loadAvg"`
}

// Mem is used and total bytes; the front-end derives the percentage.
type Mem struct {
	Used  uint64 `json:"used"`
	Total uint64 `json:"total"`
}

// Disk is one mount's used/total bytes plus whether anything is mounted there.
// A not-mounted disk (Mounted false) leaves Used/Total zero and renders as a
// dashed "NOT MOUNTED" tile in the front-end.
type Disk struct {
	Mounted bool   `json:"mounted"`
	Used    uint64 `json:"used"`
	Total   uint64 `json:"total"`
}

// App is one monitored application: display name, TCP port, and whether that
// port currently accepts a connection on this box. The dashboard links to the
// current browser scheme/hostname with only the port changed.
type App struct {
	Name string `json:"name"`
	Port int    `json:"port"`
	Up   bool   `json:"up"`
}

// Tunnel is one VPN/tunnel row. Type is one of "AUTOSSH", "WIREGUARD", or
// "TAILSCALE" (the front-end colors by it). Rx/Tx are cumulative bytes, Conn is
// connected time in seconds, Ping is round-trip latency in milliseconds, and
// LastSeen is a human string shown only when Up is false. Fields a given source
// cannot supply are left zero; the front-end renders those as "—".
type Tunnel struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Up       bool    `json:"up"`
	Rx       uint64  `json:"rx"`
	Tx       uint64  `json:"tx"`
	Conn     int64   `json:"conn"`
	Ping     float64 `json:"ping"`
	LastSeen string  `json:"lastSeen,omitempty"`
}

// Hub is the shared state store that fans dashboard updates out to every
// connected SSE client. It holds the authoritative merged Snapshot (so a
// newly-connected browser can be sent the full current state) and broadcasts
// each subsequent change as a minimal per-slice JSON patch. Collectors call the
// PublishX methods; the api package's SSE handler calls Subscribe.
//
// The zero value is not ready; use NewHub.
type Hub struct {
	mu   sync.Mutex
	snap Snapshot
	subs map[chan []byte]struct{}
}

// NewHub returns a Hub with an empty subscriber set and a zero-valued Snapshot.
// Apps and Tunnels start as empty (non-nil) slices so the initial frame
// serializes them as [] rather than null, which the front-end's merge treats as
// an empty list instead of a missing key.
func NewHub() *Hub {
	return &Hub{
		snap: Snapshot{Apps: []App{}, Tunnels: []Tunnel{}},
		subs: make(map[chan []byte]struct{}),
	}
}

// Subscribe registers a new SSE client. It returns, computed atomically under
// the hub lock, the full current snapshot as JSON (the caller writes this as
// the client's first event) and a channel of subsequent per-slice patches, plus
// a cancel func the caller must defer to unsubscribe and release the channel.
// Taking the snapshot and registering the channel under one lock guarantees the
// client neither misses an update that lands between the two nor receives one
// already reflected in its initial frame.
func (h *Hub) Subscribe() (initial []byte, ch <-chan []byte, cancel func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	initial, err := json.Marshal(h.snap)
	if err != nil {
		// Snapshot is composed entirely of JSON-safe types, so a marshal error
		// here is a programmer error, not a runtime condition; fall back to an
		// empty object so the client still gets a well-formed first frame.
		log.Error().Err(err).Msg("marshal initial snapshot")
		initial = []byte("{}")
	}

	c := make(chan []byte, subscriberBuffer)
	h.subs[c] = struct{}{}

	var once sync.Once
	cancel = func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if _, ok := h.subs[c]; ok {
				delete(h.subs, c)
				close(c)
			}
		})
	}
	return initial, c, cancel
}

// Snapshot returns a copy of the current merged state. The returned slices
// share backing arrays with the hub's; callers must not mutate their elements.
func (h *Hub) Snapshot() Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snap
}

// broadcast marshals patch and sends the bytes to every subscriber without
// blocking: a subscriber whose buffer is full is skipped (see subscriberBuffer)
// rather than allowed to stall the calling collector. It must be called with
// h.mu held, so a concurrent Subscribe can't race the subscriber map.
func (h *Hub) broadcast(patch any) {
	msg, err := json.Marshal(patch)
	if err != nil {
		log.Error().Err(err).Msg("marshal state patch")
		return
	}
	for c := range h.subs {
		select {
		case c <- msg:
		default:
			// Buffer full — drop this frame for this client. The next delivered
			// update carries current state, so the client self-heals.
		}
	}
}

// PublishHost updates the host slice and broadcasts {"host":{…}}.
func (h *Hub) PublishHost(v Host) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snap.Host = v
	h.broadcast(struct {
		Host Host `json:"host"`
	}{v})
}

// PublishNet updates the net slice and broadcasts {"net":{…}}.
func (h *Hub) PublishNet(v Net) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snap.Net = v
	h.broadcast(struct {
		Net Net `json:"net"`
	}{v})
}

// PublishCPU updates the cpu slice and broadcasts {"cpu":{…}}.
func (h *Hub) PublishCPU(v CPU) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snap.CPU = v
	h.broadcast(struct {
		CPU CPU `json:"cpu"`
	}{v})
}

// PublishMem updates the mem slice and broadcasts {"mem":{…}}.
func (h *Hub) PublishMem(v Mem) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snap.Mem = v
	h.broadcast(struct {
		Mem Mem `json:"mem"`
	}{v})
}

// PublishDisk updates one named disk ("ssd", "rootfs", or "sdcard") and
// broadcasts {"<key>":{…}}. An unknown key is ignored with a warning rather
// than silently creating a slice the front-end doesn't render.
func (h *Hub) PublishDisk(key string, v Disk) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch key {
	case "ssd":
		h.snap.Ssd = v
	case "rootfs":
		h.snap.Rootfs = v
	case "sdcard":
		h.snap.Sdcard = v
	default:
		log.Warn().Str("key", key).Msg("PublishDisk: unknown disk key")
		return
	}
	h.broadcast(map[string]Disk{key: v})
}

// PublishApps replaces the apps slice and broadcasts {"apps":[…]}. The apps
// collector gathers every app each tick, so this always carries the full list.
func (h *Hub) PublishApps(v []App) {
	if v == nil {
		v = []App{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snap.Apps = v
	h.broadcast(struct {
		Apps []App `json:"apps"`
	}{v})
}

// PublishTunnel upserts one tunnel (matched by Type and Name) into the master
// snapshot and broadcasts just that tunnel as {"tunnels":[{…}]}. Display names
// are user-configurable across tunnel sources, so Type is part of identity to
// avoid a WireGuard label overwriting a Tailscale or autossh row with the same
// visible name.
func (h *Hub) PublishTunnel(v Tunnel) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Copy-on-write: never mutate the current backing array in place, because a
	// caller of Snapshot() may still be reading a slice that shares it without
	// holding h.mu (Snapshot copies only the slice header). Building a fresh
	// array here — as PublishApps does by assigning a new slice — freezes every
	// previously handed-out snapshot for safe lock-free reads forever.
	next := make([]Tunnel, len(h.snap.Tunnels), len(h.snap.Tunnels)+1)
	copy(next, h.snap.Tunnels)
	found := false
	for i := range next {
		if next[i].Type == v.Type && next[i].Name == v.Name {
			next[i] = v
			found = true
			break
		}
	}
	if !found {
		next = append(next, v)
	}
	h.snap.Tunnels = next
	h.broadcast(struct {
		Tunnels []Tunnel `json:"tunnels"`
	}{[]Tunnel{v}})
}
