// Package network reports the device's LAN and WAN IPv4 addresses, so the
// display and web layers can show them. LAN detection is local (net.Interfaces);
// the WAN address requires an outbound round-trip to ipify.
package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
)

// LANIP gives you the first non-loopback IPv4 address of a real LAN adapter.
func LANIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("list interfaces: %w", err)
	}
	for _, iface := range ifaces {
		if !isLANInterface(iface) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", fmt.Errorf("interface %s addrs: %w", iface.Name, err)
		}
		if ip := firstIPv4(addrs); ip != "" {
			return ip, nil
		}
	}
	return "", errors.New("network not found")
}

// isLANInterface reports whether iface is a plausible LAN adapter: up,
// non-loopback, and not point-to-point. VPN tunnel interfaces such as
// WireGuard and Tailscale present as point-to-point devices, so this excludes
// them even without knowing their interface names ahead of time — otherwise
// LANIP can return the tunnel's address instead of the real LAN address once a
// VPN is enabled. Note this deliberately does NOT require net.FlagBroadcast:
// bridges and some tap devices carry a real LAN address without setting it.
func isLANInterface(iface net.Interface) bool {
	if iface.Flags&net.FlagUp == 0 {
		return false // interface down
	}
	if iface.Flags&net.FlagLoopback != 0 {
		return false // loopback interface
	}
	if iface.Flags&net.FlagPointToPoint != 0 {
		return false // VPN/tunnel interface (e.g. WireGuard, Tailscale)
	}
	return true
}

// firstIPv4 returns the first non-loopback IPv4 address among addrs, or ""
// if none is found.
func firstIPv4(addrs []net.Addr) string {
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		ip = ip.To4()
		if ip == nil {
			continue // not an ipv4 address
		}
		return ip.String()
	}
	return ""
}

// wanIPEndpoint is queried by WANIP for this device's public IPv4 address.
// It's a var, not a const, so tests can point it at a local httptest server.
var wanIPEndpoint = "https://api.ipify.org"

// wanIPClient is reused across calls so repeated invocations (buildNetwork's
// hourly refresh) can pool TCP/TLS connections instead of dialing fresh each
// time.
var wanIPClient = &http.Client{}

// WANIP gives you your WAN IP of the device. ctx bounds the round trip —
// callers should attach a timeout, since a dead WAN link otherwise leaves the
// underlying HTTP request outstanding indefinitely (this replaced go-ipify,
// which offered no way to bound or cancel the request).
func WANIP(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wanIPEndpoint, nil)
	if err != nil {
		return "", fmt.Errorf("build ipify request: %w", err)
	}

	resp, err := wanIPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("query ipify: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("query ipify: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read ipify response: %w", err)
	}

	ip := string(body)
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("query ipify: invalid ip %q", ip)
	}
	return ip, nil
}
