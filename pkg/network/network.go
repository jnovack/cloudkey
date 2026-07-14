package network

import (
	"errors"
	"net"

	ipify "github.com/rdegges/go-ipify"
)

// LANIP gives you the first non-loopback IPv4 address of a real LAN adapter.
func LANIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if !isLANInterface(iface) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", err
		}
		if ip := firstIPv4(addrs); ip != "" {
			return ip, nil
		}
	}
	return "", errors.New("network not found")
}

// isLANInterface reports whether iface is a plausible LAN adapter: up,
// non-loopback, and broadcast-capable. VPN tunnel interfaces such as
// WireGuard and Tailscale present as point-to-point devices with no
// broadcast flag, so this excludes them even without knowing their
// interface names ahead of time — otherwise LANIP can return the tunnel's
// address instead of the real LAN address once a VPN is enabled.
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

// WANIP gives you your WAN IP of the device
func WANIP() (string, error) {
	return ipify.GetIp()
}
