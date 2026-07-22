package wireguard

import (
	"strings"
	"testing"
	"time"
)

// A representative `wg show <iface>` body with one up peer, matching the target
// device's output (transfer in IEC units, handshake age with a trailing "ago").
const showUp = `interface: mullvad
  public key: q+J3CV1HG9ApHoOOhp460bybgAgCYQf3PnRJOcynRxc=
  private key: (hidden)
  listening port: 53794

peer: zfNQqDyPmSUY8+20wxACe/wpk4Q5jpZm5iBqjXj2hk8=
  endpoint: 138.199.6.233:51820
  allowed ips: 0.0.0.0/0, ::/0
  latest handshake: 31 seconds ago
  transfer: 15.88 MiB received, 3.00 MiB sent
`

func TestParseShow(t *testing.T) {
	// A peer that has never handshaked omits its handshake/transfer lines.
	neverHandshaked := "interface: wg0\n  public key: AAAA\n\n" +
		"peer: BBBB\n  endpoint: 1.2.3.4:51820\n  allowed ips: 10.0.0.2/32\n"

	// Two peers, one stale and one recent, with combined transfer counters.
	twoPeers := "interface: wg0\n\n" +
		"peer: AAAA\n  endpoint: 1.2.3.4:51820\n  latest handshake: 10 minutes, 5 seconds ago\n  transfer: 1.00 KiB received, 2.00 KiB sent\n\n" +
		"peer: BBBB\n  endpoint: 5.6.7.8:51820\n  latest handshake: 5 seconds ago\n  transfer: 1.00 KiB received, 0 B sent\n"

	cases := []struct {
		name         string
		out          string
		wantUp       bool
		wantRx       uint64
		wantTx       uint64
		wantEndpoint string
	}{
		{"single up peer", showUp, true, 16651386, 3145728, "138.199.6.233"},
		{"crlf line endings", strings.ReplaceAll(showUp, "\n", "\r\n"), true, 16651386, 3145728, "138.199.6.233"},
		{"never handshaked is down", neverHandshaked, false, 0, 0, "1.2.3.4"},
		{"no peers is down", "interface: wg0\n  listening port: 51820\n", false, 0, 0, ""},
		{"two peers sum transfer, recent wins up", twoPeers, true, 2 * 1024, 2 * 1024, "1.2.3.4"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseShow([]byte(c.out))
			if got.Up != c.wantUp {
				t.Errorf("Up = %v, want %v", got.Up, c.wantUp)
			}
			if got.Rx != c.wantRx {
				t.Errorf("Rx = %d, want %d", got.Rx, c.wantRx)
			}
			if got.Tx != c.wantTx {
				t.Errorf("Tx = %d, want %d", got.Tx, c.wantTx)
			}
			if got.Endpoint != c.wantEndpoint {
				t.Errorf("Endpoint = %q, want %q", got.Endpoint, c.wantEndpoint)
			}
		})
	}
}

func TestEndpointHost(t *testing.T) {
	cases := []struct {
		name string
		ep   string
		want string
	}{
		{"bracketed IPv6 with port", "[fd7a::1]:51820", "fd7a::1"},
		{"no port falls back to raw value", "1.2.3.4", "1.2.3.4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := endpointHost(c.ep); got != c.want {
				t.Errorf("endpointHost(%q) = %q, want %q", c.ep, got, c.want)
			}
		})
	}
}

func TestStatCommandNotFound(t *testing.T) {
	_, err := Stat("cloudkey-no-such-binary", "wg0")
	if err == nil {
		t.Fatal("expected error for missing wg binary")
	}
}

func TestParseHandshakeAge(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Duration
		ok   bool
	}{
		{"seconds with ago", "31 seconds ago", 31 * time.Second, true},
		{"minute and seconds", "1 minute, 5 seconds ago", time.Minute + 5*time.Second, true},
		{"days hours minutes seconds", "2 days, 3 hours, 4 minutes, 12 seconds", 2*24*time.Hour + 3*time.Hour + 4*time.Minute + 12*time.Second, true},
		{"singular second", "1 second ago", time.Second, true},
		{"no units", "just now", 0, false},
		{"empty", "", 0, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseHandshakeAge(c.in)
			if ok != c.ok {
				t.Fatalf("parseHandshakeAge(%q) ok = %v, want %v", c.in, ok, c.ok)
			}
			if ok && got != c.want {
				t.Errorf("parseHandshakeAge(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestParseTransfer(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		wantRx uint64
		wantTx uint64
	}{
		{"mib both", " 15.88 MiB received, 3.00 MiB sent", 16651386, 3145728},
		{"bytes and kib", " 0 B received, 1.00 KiB sent", 0, 1024},
		{"gib", " 2.00 GiB received, 512.00 MiB sent", 2 * 1024 * 1024 * 1024, 512 * 1024 * 1024},
		{"malformed ignored", " garbage", 0, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rx, tx := parseTransfer(c.in)
			if rx != c.wantRx || tx != c.wantTx {
				t.Errorf("parseTransfer(%q) = (%d, %d), want (%d, %d)", c.in, rx, tx, c.wantRx, c.wantTx)
			}
		})
	}
}
