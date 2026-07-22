package api

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/jnovack/cloudkey/internal/state"
)

// appsPollInterval is how often the configured apps' liveness is refreshed.
// Apps come and go rarely and a listen-table read is cheap, so a modest
// interval keeps load negligible on the low-power device.
const appsPollInterval = 15 * time.Second

// dialTimeout bounds a single fallback liveness probe so an app whose port
// silently blackholes can't stall the collector.
const dialTimeout = 500 * time.Millisecond

// procTCPFiles are the kernel connection tables read per network namespace.
// Both are consulted so an app listening on either IPv4 or IPv6 is seen; only
// the local port is extracted, so the address family doesn't matter past that.
var procTCPFiles = []string{"tcp", "tcp6"}

// procRoot is the procfs mount point. A variable so tests can point the scan at
// a fixture tree instead of the live kernel.
var procRoot = "/proc"

// tcpListenState is the st column value for a socket in LISTEN
// (TCP_LISTEN = 10 = 0x0A). Only listening sockets count as an app being up;
// an ESTABLISHED/TIME_WAIT entry for the same port is an active client
// connection, not the service accepting new ones.
const tcpListenState = "0A"

// parseApps parses the CLOUDKEY_APPS value — a comma-separated list of
// "name:port" entries, e.g. "Grafana:3000,Sonarr:8989" — into apps. It is
// lenient: a malformed entry is logged and skipped rather than failing the
// whole list, since the dashboard is an optional feature that should not take
// down the service over one bad line. Names may not contain ':' or ','.
func parseApps(raw string) []state.App {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var apps []state.App
	for entry := range strings.SplitSeq(raw, ",") {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		app, err := parseApp(entry)
		if err != nil {
			log.Warn().Err(err).Str("entry", entry).Msg("skipping malformed app entry")
			continue
		}
		apps = append(apps, app)
	}
	return apps
}

// parseApp parses one "name:port" entry. The port must be a valid TCP port
// (1-65535). Names may not contain ':'.
func parseApp(entry string) (state.App, error) {
	parts := strings.Split(entry, ":")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	if len(parts) != 2 {
		return state.App{}, fmt.Errorf("want name:port, got %q", entry)
	}
	name, portStr := parts[0], parts[1]
	if name == "" {
		return state.App{}, fmt.Errorf("empty name")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return state.App{}, fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	if port < 1 || port > 65535 {
		return state.App{}, fmt.Errorf("port %d out of range 1-65535", port)
	}
	return state.App{Name: name, Port: port}, nil
}

// collectApps refreshes every app's liveness on a fixed interval and publishes
// the full apps slice to the hub each tick. Liveness comes from the kernel's
// TCP listen tables (/proc/net/tcp{,6}, read once per network namespace — see
// netTableDirs): each tick yields the set of locally-listening ports, so an app
// is up iff its port is in that set. This beats dialing each port — it covers
// every local bind address (loopback, 0.0.0.0, or any interface IP) without a
// connect handshake, so a down app costs nothing instead of a dial timeout.
// Where the listen table is unreadable (non-Linux dev machines) it falls back
// to a loopback TCP dial per app. The published model deliberately omits any
// probe address; browser links use the current dashboard host with only the
// port changed.
func collectApps(ctx context.Context, hub *state.Hub, apps []state.App) {
	publish := func() {
		ports, ok := listeningPorts()
		snapshot := make([]state.App, len(apps))
		for i, app := range apps {
			snapshot[i] = app
			if ok {
				snapshot[i].Up = ports[app.Port]
			} else {
				snapshot[i].Up = probeTCP("", app.Port)
			}
		}
		hub.PublishApps(snapshot)
	}

	publish()

	ticker := time.NewTicker(appsPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			publish()
		}
	}
}

// listeningPorts returns the set of TCP ports in LISTEN state on this host,
// read from the kernel connection tables of every network namespace on the box.
// ok is false only when no table could be read at all (e.g. a non-Linux host),
// which the caller treats as a signal to fall back to dialing; a
// readable-but-empty table returns (set, true).
func listeningPorts() (set map[int]bool, ok bool) {
	return listeningPortsIn(procRoot)
}

func listeningPortsIn(root string) (set map[int]bool, ok bool) {
	set = make(map[int]bool)
	for _, dir := range netTableDirs(root) {
		for _, name := range procTCPFiles {
			f, err := os.Open(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			parseListeningPorts(f, set)
			_ = f.Close()
			ok = true
		}
	}
	return set, ok
}

// netTableDirs lists one procfs "net" directory per distinct network namespace
// on this box, starting with the caller's own.
//
// Each network namespace has its own connection table: a socket listening
// inside another namespace does NOT appear in the root namespace's
// /proc/net/tcp. Apps commonly run in one (a VPN split-tunnel netns, a
// container), and reading only the root table reports every one of them as
// down. Deduping by the namespace's inode — the target of /proc/<pid>/ns/net —
// keeps this to one table read per namespace rather than one per process.
func netTableDirs(root string) []string {
	dirs := []string{filepath.Join(root, "net")}
	seen := make(map[string]bool)
	if self, err := os.Readlink(filepath.Join(root, "self", "ns", "net")); err == nil {
		seen[self] = true
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return dirs
	}
	for _, e := range entries {
		if !e.IsDir() || !isPID(e.Name()) {
			continue
		}
		ns, err := os.Readlink(filepath.Join(root, e.Name(), "ns", "net"))
		if err != nil || seen[ns] {
			// Unreadable means the process exited or is not ours to inspect;
			// either way another process in the same namespace will do.
			continue
		}
		seen[ns] = true
		dirs = append(dirs, filepath.Join(root, e.Name(), "net"))
	}
	return dirs
}

// isPID reports whether a /proc entry name is a process directory rather than
// one of the many non-numeric entries (self, sys, net, …).
func isPID(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseListeningPorts scans one /proc/net/tcp{,6} table and adds the local port
// of every LISTEN row to set. The columns are whitespace-separated; field 1 is
// the local address as HEXIP:HEXPORT and field 3 is the connection state. Only
// the port is read, so IPv4 (8-hex IP) and IPv6 (32-hex IP) rows parse
// identically. The header line and any malformed row are skipped rather than
// failing the scan — a garbled line must not blank out every app's status.
func parseListeningPorts(r io.Reader, set map[int]bool) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 || fields[3] != tcpListenState {
			continue
		}
		_, hexPort, found := strings.Cut(fields[1], ":")
		if !found {
			continue
		}
		port, err := strconv.ParseUint(hexPort, 16, 32)
		if err != nil {
			continue
		}
		set[int(port)] = true
	}
}

// probeTCP reports whether a TCP connection to the probe address succeeds. It
// is the fallback liveness signal used only when the listen table is
// unreadable. An empty host means this box, probed via loopback.
func probeTCP(host string, port int) bool {
	if host == "" {
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
