package state

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// readOne returns the next message on ch, or fails if none arrives promptly.
func readOne(t *testing.T, ch <-chan []byte) map[string]json.RawMessage {
	t.Helper()
	select {
	case msg := <-ch:
		var m map[string]json.RawMessage
		if err := json.Unmarshal(msg, &m); err != nil {
			t.Fatalf("unmarshal patch %q: %v", msg, err)
		}
		return m
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a broadcast")
		return nil
	}
}

func TestSubscribeInitialFrameIsFullSnapshot(t *testing.T) {
	h := NewHub()
	h.PublishCPU(CPU{Pct: 41, Cores: 4, LoadAvg: 0.62})

	initial, _, cancel := h.Subscribe()
	defer cancel()

	var snap map[string]json.RawMessage
	if err := json.Unmarshal(initial, &snap); err != nil {
		t.Fatalf("unmarshal initial frame: %v", err)
	}
	// The initial frame must carry every top-level key so no stale seed value
	// survives on the client.
	for _, key := range []string{"host", "net", "cpu", "mem", "ssd", "rootfs", "sdcard", "apps", "tunnels"} {
		if _, ok := snap[key]; !ok {
			t.Errorf("initial frame missing key %q", key)
		}
	}
	// apps/tunnels must serialize as [] (not null) so the front-end merges them
	// as empty lists.
	if string(snap["apps"]) != "[]" {
		t.Errorf("apps = %s, want []", snap["apps"])
	}
	if string(snap["tunnels"]) != "[]" {
		t.Errorf("tunnels = %s, want []", snap["tunnels"])
	}
}

func TestPublishBroadcastsOnlyChangedSlice(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		publish func(*Hub)
		check   func(*testing.T, *Hub, map[string]json.RawMessage)
	}{
		{
			name: "host",
			key:  "host",
			publish: func(h *Hub) {
				h.PublishHost(Host{Name: "cloudkey", OS: "linux", Arch: "arm64", UptimeSec: 123})
			},
			check: func(t *testing.T, h *Hub, patch map[string]json.RawMessage) {
				var host Host
				if err := json.Unmarshal(patch["host"], &host); err != nil {
					t.Fatalf("unmarshal host patch: %v", err)
				}
				if host.Name != "cloudkey" || host.OS != "linux" || host.Arch != "arm64" || host.UptimeSec != 123 {
					t.Errorf("host = %+v, want {cloudkey linux arm64 123}", host)
				}
				if got := h.Snapshot().Host; got.Name != "cloudkey" || got.OS != "linux" || got.Arch != "arm64" || got.UptimeSec != 123 {
					t.Errorf("snapshot host = %+v, want {cloudkey linux arm64 123}", got)
				}
			},
		},
		{
			name: "net",
			key:  "net",
			publish: func(h *Hub) {
				h.PublishNet(Net{LAN: "192.168.1.10", WAN: "203.0.113.10"})
			},
			check: func(t *testing.T, h *Hub, patch map[string]json.RawMessage) {
				var net Net
				if err := json.Unmarshal(patch["net"], &net); err != nil {
					t.Fatalf("unmarshal net patch: %v", err)
				}
				if net.LAN != "192.168.1.10" || net.WAN != "203.0.113.10" {
					t.Errorf("net = %+v, want {192.168.1.10 203.0.113.10}", net)
				}
				if got := h.Snapshot().Net; got.LAN != "192.168.1.10" || got.WAN != "203.0.113.10" {
					t.Errorf("snapshot net = %+v, want {192.168.1.10 203.0.113.10}", got)
				}
			},
		},
		{
			name: "cpu",
			key:  "cpu",
			publish: func(h *Hub) {
				h.PublishCPU(CPU{Pct: 55, Cores: 2, LoadAvg: 1.1})
			},
			check: func(t *testing.T, h *Hub, patch map[string]json.RawMessage) {
				var cpu CPU
				if err := json.Unmarshal(patch["cpu"], &cpu); err != nil {
					t.Fatalf("unmarshal cpu patch: %v", err)
				}
				if cpu.Pct != 55 || cpu.Cores != 2 || cpu.LoadAvg != 1.1 {
					t.Errorf("cpu = %+v, want {55 2 1.1}", cpu)
				}
				if got := h.Snapshot().CPU; got.Pct != 55 || got.Cores != 2 || got.LoadAvg != 1.1 {
					t.Errorf("snapshot cpu = %+v, want {55 2 1.1}", got)
				}
			},
		},
		{
			name: "mem",
			key:  "mem",
			publish: func(h *Hub) {
				h.PublishMem(Mem{Used: 10, Total: 20})
			},
			check: func(t *testing.T, h *Hub, patch map[string]json.RawMessage) {
				var mem Mem
				if err := json.Unmarshal(patch["mem"], &mem); err != nil {
					t.Fatalf("unmarshal mem patch: %v", err)
				}
				if mem.Used != 10 || mem.Total != 20 {
					t.Errorf("mem = %+v, want {10 20}", mem)
				}
				if got := h.Snapshot().Mem; got.Used != 10 || got.Total != 20 {
					t.Errorf("snapshot mem = %+v, want {10 20}", got)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := NewHub()
			_, ch, cancel := h.Subscribe()
			defer cancel()

			c.publish(h)

			patch := readOne(t, ch)
			if len(patch) != 1 {
				t.Fatalf("patch has %d keys, want exactly 1: %v", len(patch), patch)
			}
			if _, ok := patch[c.key]; !ok {
				t.Fatalf("patch missing %s key: %v", c.key, patch)
			}
			c.check(t, h, patch)
		})
	}
}

func TestPublishAppsNormalizesNilAndBroadcastsAppsSlice(t *testing.T) {
	h := NewHub()
	_, ch, cancel := h.Subscribe()
	defer cancel()

	// A nil slice must still serialize as [] (not null), both in the
	// broadcast patch and in the merged snapshot — see doc.go's wire
	// contract and NewHub's comment on why apps/tunnels start non-nil.
	h.PublishApps(nil)
	patch := readOne(t, ch)
	if len(patch) != 1 {
		t.Fatalf("patch has %d keys, want exactly 1: %v", len(patch), patch)
	}
	if got, ok := patch["apps"]; !ok || string(got) != "[]" {
		t.Fatalf("apps patch = %v (present=%v), want []", string(got), ok)
	}
	if snap := h.Snapshot().Apps; snap == nil || len(snap) != 0 {
		t.Fatalf("snapshot apps = %+v, want non-nil empty slice", snap)
	}
	if b, err := json.Marshal(h.Snapshot().Apps); err != nil || string(b) != "[]" {
		t.Fatalf("marshal snapshot apps = %s, err %v, want []", b, err)
	}

	// A populated input round-trips its element through both the patch and
	// the snapshot.
	h.PublishApps([]App{{Name: "x", Port: 80, Up: true}})
	patch = readOne(t, ch)
	var apps []App
	if err := json.Unmarshal(patch["apps"], &apps); err != nil {
		t.Fatalf("unmarshal apps patch: %v", err)
	}
	if len(apps) != 1 || apps[0].Name != "x" || apps[0].Port != 80 || !apps[0].Up {
		t.Fatalf("apps patch = %+v, want single {x 80 true}", apps)
	}
	if snap := h.Snapshot().Apps; len(snap) != 1 || snap[0].Name != "x" || snap[0].Port != 80 || !snap[0].Up {
		t.Fatalf("snapshot apps = %+v, want single {x 80 true}", snap)
	}
}

func TestPublishDiskUsesKeyAndIgnoresUnknown(t *testing.T) {
	h := NewHub()
	_, ch, cancel := h.Subscribe()
	defer cancel()

	h.PublishDisk("ssd", Disk{Mounted: true, Used: 10, Total: 20})
	patch := readOne(t, ch)
	if _, ok := patch["ssd"]; !ok || len(patch) != 1 {
		t.Fatalf("expected only ssd key, got %v", patch)
	}
	if got := h.Snapshot().Ssd; got.Used != 10 || !got.Mounted {
		t.Errorf("snapshot ssd = %+v, want {true 10 20}", got)
	}

	// An unknown key must not broadcast anything.
	h.PublishDisk("nvme", Disk{Mounted: true})
	select {
	case msg := <-ch:
		t.Fatalf("unknown disk key broadcast %q, want nothing", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPublishTunnelUpsertsSameTypeAndName(t *testing.T) {
	h := NewHub()
	_, ch, cancel := h.Subscribe()
	defer cancel()

	h.PublishTunnel(Tunnel{Name: "wg0", Type: "WIREGUARD", Up: true, Rx: 100})
	if p := readOne(t, ch); len(p) != 1 || p["tunnels"] == nil {
		t.Fatalf("first tunnel patch = %v, want single tunnels key", p)
	}

	// A patch for the same type and name must carry exactly one tunnel...
	h.PublishTunnel(Tunnel{Name: "wg0", Type: "WIREGUARD", Up: true, Rx: 200})
	patch := readOne(t, ch)
	var tunnels []Tunnel
	if err := json.Unmarshal(patch["tunnels"], &tunnels); err != nil {
		t.Fatalf("unmarshal tunnels patch: %v", err)
	}
	if len(tunnels) != 1 || tunnels[0].Rx != 200 {
		t.Fatalf("patch tunnels = %+v, want single wg0 with Rx 200", tunnels)
	}
	// ...and the master snapshot must hold one merged entry, not two.
	if snap := h.Snapshot().Tunnels; len(snap) != 1 || snap[0].Rx != 200 {
		t.Errorf("snapshot tunnels = %+v, want single wg0 with Rx 200", snap)
	}

	// A different name appends a second tunnel.
	h.PublishTunnel(Tunnel{Name: "ts0", Type: "TAILSCALE", Up: false})
	readOne(t, ch)
	if snap := h.Snapshot().Tunnels; len(snap) != 2 {
		t.Errorf("snapshot has %d tunnels, want 2", len(snap))
	}
}

func TestPublishTunnelKeepsSameNameDifferentTypes(t *testing.T) {
	h := NewHub()

	h.PublishTunnel(Tunnel{Name: "vpn", Type: "WIREGUARD", Up: true, Rx: 100})
	h.PublishTunnel(Tunnel{Name: "vpn", Type: "TAILSCALE", Up: true, Rx: 200})

	snap := h.Snapshot().Tunnels
	if len(snap) != 2 {
		t.Fatalf("snapshot tunnels = %+v, want two entries for same name with different types", snap)
	}
	if snap[0].Type != "WIREGUARD" || snap[1].Type != "TAILSCALE" {
		t.Fatalf("snapshot tunnels = %+v, want WireGuard and Tailscale entries preserved", snap)
	}
}

func TestBroadcastDropsForSlowSubscriberWithoutBlocking(t *testing.T) {
	h := NewHub()
	// Subscribe but never drain the channel.
	_, _, cancel := h.Subscribe()
	defer cancel()

	// Publish far more than subscriberBuffer. If Publish blocked on a full
	// buffer this would deadlock; the test's job is to prove it does not.
	done := make(chan struct{})
	go func() {
		for i := range subscriberBuffer * 10 {
			h.PublishCPU(CPU{Pct: float64(i)})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber buffer")
	}
}

func TestCancelStopsDelivery(t *testing.T) {
	h := NewHub()
	_, ch, cancel := h.Subscribe()

	cancel()
	// Second cancel must be a no-op, not a double-close panic.
	cancel()

	// Channel is closed; a receive returns the zero value with ok=false.
	if _, ok := <-ch; ok {
		t.Fatal("expected channel closed after cancel")
	}

	// Publishing after cancel must not panic (no send on closed channel).
	h.PublishCPU(CPU{Pct: 1})
}

// TestConcurrentSubscribeCancelAndPublishRaceFree exercises broadcast
// iterating h.subs concurrently with Subscribe/cancel mutating that same
// map — all three are documented as safe only because they share h.mu (see
// broadcast's doc comment). This test's job is to create that interleaving
// under `go test -race`; the race detector is what actually catches a
// regression (e.g. a future broadcast call made without h.mu held), so this
// only asserts the run completes without panicking or deadlocking.
// TestPublishTunnelSnapshotReadRaceFree pins PublishTunnel's copy-on-write
// contract. Snapshot() copies only the slice header, so the Tunnels slice it
// returns shares a backing array with the hub; a reader holding that slice
// touches no lock. The interleaving here — one goroutine ranging over a held
// snapshot's Tunnels reading fields, another repeatedly upserting the SAME
// Type+Name (the in-place-update branch, not append) — races that shared array
// under the Go memory model. `go test -race` flags it if PublishTunnel ever
// writes into an existing element instead of allocating a fresh array. Because
// the data race is the failure, this only asserts the run completes cleanly.
func TestPublishTunnelSnapshotReadRaceFree(t *testing.T) {
	h := NewHub()
	h.PublishTunnel(Tunnel{Name: "wg0", Type: "WIREGUARD", Up: true, Rx: 1})

	// Hold a snapshot whose Tunnels slice shares the hub's backing array.
	held := h.Snapshot().Tunnels

	const iterations = 1000
	var wg sync.WaitGroup

	// Reader: range over the held slice reading fields, no lock held.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iterations {
			for i := range held {
				_ = held[i].Rx
				_ = held[i].Name
			}
		}
	}()

	// Writer: upsert the same Type+Name, forcing the in-place-update path.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range iterations {
			h.PublishTunnel(Tunnel{Name: "wg0", Type: "WIREGUARD", Up: true, Rx: uint64(i)})
		}
	}()

	wg.Wait()
}

func TestConcurrentSubscribeCancelAndPublishRaceFree(t *testing.T) {
	h := NewHub()
	const goroutines = 8
	const duration = 300 * time.Millisecond

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Publisher: continuously publishes while subscribers churn.
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				h.PublishCPU(CPU{Pct: float64(i % 100)})
				i++
			}
		}
	}()

	// Subscribers: repeatedly subscribe, drain a few messages, then cancel.
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, ch, cancel := h.Subscribe()
				for range 3 {
					select {
					case <-ch:
					case <-time.After(50 * time.Millisecond):
					}
				}
				cancel()
			}
		}()
	}

	time.Sleep(duration)
	close(stop)
	wg.Wait()
}
