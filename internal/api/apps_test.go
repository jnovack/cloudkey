package api

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jnovack/cloudkey/internal/state"
)

func TestParseApp(t *testing.T) {
	cases := []struct {
		name    string
		entry   string
		want    state.App
		wantErr bool
	}{
		{"simple name port", "Grafana:3000", state.App{Name: "Grafana", Port: 3000}, false},
		{"name with spaces", "Home Assistant:8123", state.App{Name: "Home Assistant", Port: 8123}, false},
		{"surrounding spaces trimmed", "  Jellyfin : 8096 ", state.App{Name: "Jellyfin", Port: 8096}, false},
		{"missing colon", "Grafana", state.App{}, true},
		{"old name host port rejected", "Sonarr:nas:8989", state.App{}, true},
		{"empty name", ":3000", state.App{}, true},
		{"non-numeric port", "Grafana:abc", state.App{}, true},
		{"port too high", "Grafana:70000", state.App{}, true},
		{"port zero", "Grafana:0", state.App{}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseApp(c.entry)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseApp(%q) err = %v, wantErr %v", c.entry, err, c.wantErr)
			}
			if !c.wantErr && got != c.want {
				t.Errorf("parseApp(%q) = %+v, want %+v", c.entry, got, c.want)
			}
		})
	}
}

func TestParseApps(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []state.App
	}{
		{"empty", "", nil},
		{"whitespace only", "   ", nil},
		{
			"multiple valid",
			"Grafana:3000,Home Assistant:8123",
			[]state.App{{Name: "Grafana", Port: 3000}, {Name: "Home Assistant", Port: 8123}},
		},
		{
			"bad entry skipped, good kept",
			"Grafana:3000,broken,Jellyfin:8096",
			[]state.App{{Name: "Grafana", Port: 3000}, {Name: "Jellyfin", Port: 8096}},
		},
		{"trailing comma ignored", "Grafana:3000,", []state.App{{Name: "Grafana", Port: 3000}}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseApps(c.raw)
			if len(got) != len(c.want) {
				t.Fatalf("parseApps(%q) = %+v, want %+v", c.raw, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("parseApps(%q)[%d] = %+v, want %+v", c.raw, i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestProbeTCP(t *testing.T) {
	// An open listener on loopback must probe up.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	// Empty host defaults to loopback, and an explicit host is honored.
	if !probeTCP("", port) {
		t.Errorf(`probeTCP("", %d) = false for an open port, want true`, port)
	}
	if !probeTCP("127.0.0.1", port) {
		t.Errorf(`probeTCP("127.0.0.1", %d) = false for an open port, want true`, port)
	}

	// After closing, the same port must probe down.
	_ = ln.Close()
	if probeTCP("", port) {
		t.Errorf(`probeTCP("", %d) = true after close, want false`, port)
	}
}

func TestParseListeningPorts(t *testing.T) {
	// One header line, an IPv4 LISTEN row (0A) on 0x0BB8 = 3000, an IPv4
	// ESTABLISHED row (01) on 0x1F90 = 8080 that must NOT count as listening,
	// an IPv6 LISTEN row on 0x2383 = 9091, and a malformed row that must be
	// skipped without derailing the scan.
	const sample = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 111 1 0 0
   1: 0100007F:1F90 0100007F:C1A2 01 00000000:00000000 00:00000000 00000000  1000        0 222 1 0 0
   2: 00000000000000000000000000000000:2383 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 333 1 0 0
   garbage line with too few fields
`

	set := make(map[int]bool)
	parseListeningPorts(strings.NewReader(sample), set)

	if !set[3000] {
		t.Errorf("port 3000 (LISTEN) missing from set %v", set)
	}
	if !set[9091] {
		t.Errorf("port 9091 (IPv6 LISTEN) missing from set %v", set)
	}
	if set[8080] {
		t.Errorf("port 8080 (ESTABLISHED) present in set, want absent")
	}
}

// TestListeningPortsFindsPortsInOtherNetworkNamespaces guards the reason
// netTableDirs walks /proc at all. Every network namespace has its own
// connection table, so apps running inside one (a VPN split-tunnel netns, a
// container) are absent from the root namespace's /proc/net/tcp. Reading only
// the root table reported every such app as permanently down. Reverting this to
// a single-table read makes that regression return.
func TestListeningPortsFindsPortsInOtherNetworkNamespaces(t *testing.T) {
	const (
		rootNS  = "net:[4026531840]"
		otherNS = "net:[4026532999]"
		// Hex local ports as the kernel writes them.
		rootPort  = 8080 // 0x1F90
		otherPort = 8989 // 0x231D
	)
	row := func(hexPort, state string) string {
		return "   0: 00000000:" + hexPort + " 00000000:0000 " + state +
			" 00000000:00000000 00:00000000 00000000     0        0 1 1 0000000000000000 100 0 0 10 0\n"
	}

	root := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	link := func(path, target string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatalf("symlink %s: %v", path, err)
		}
	}

	header := "  sl  local_address rem_address   st\n"
	write(filepath.Join(root, "net", "tcp"), header+row("1F90", "0A"))
	link(filepath.Join(root, "self", "ns", "net"), rootNS)

	// A process in the caller's own namespace: already covered, must not add a
	// duplicate read.
	link(filepath.Join(root, "100", "ns", "net"), rootNS)
	write(filepath.Join(root, "100", "net", "tcp"), header+row("270F", "0A")) // 9999, must be ignored

	// A process in a different namespace, listening on otherPort.
	link(filepath.Join(root, "200", "ns", "net"), otherNS)
	write(filepath.Join(root, "200", "net", "tcp"), header+row("231D", "0A"))

	// Non-numeric /proc entries must be skipped, not treated as PIDs.
	write(filepath.Join(root, "sys", "net", "tcp"), header+row("0BB8", "0A")) // 3000

	set, ok := listeningPortsIn(root)
	if !ok {
		t.Fatal("listeningPortsIn reported no readable table")
	}
	if !set[rootPort] {
		t.Errorf("root namespace port %d missing", rootPort)
	}
	if !set[otherPort] {
		t.Errorf("port %d in other namespace missing: apps in a VPN/container netns read as down", otherPort)
	}
	if set[9999] {
		t.Error("read a second process in an already-scanned namespace")
	}
	if set[3000] {
		t.Error("treated non-numeric /proc entry as a PID")
	}
}

// TestListeningPortsFindsOpenListener exercises the live /proc read against a
// real listener. It is skipped where the listen table is unreadable (non-Linux
// hosts), which is exactly the case the dial fallback covers.
func TestListeningPortsFindsOpenListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	set, ok := listeningPorts()
	if !ok {
		t.Skip("no /proc/net/tcp on this host; dial fallback path is used instead")
	}
	if !set[port] {
		t.Errorf("open listener port %d not found in listen set", port)
	}
}
