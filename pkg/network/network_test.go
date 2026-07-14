package network

import (
	"net"
	"testing"
)

func TestIsLANInterface(t *testing.T) {
	tests := []struct {
		name  string
		flags net.Flags
		want  bool
	}{
		{"up broadcast lan adapter", net.FlagUp | net.FlagBroadcast | net.FlagMulticast, true},
		{"down interface", net.FlagBroadcast, false},
		{"loopback", net.FlagUp | net.FlagLoopback, false},
		{"point-to-point wireguard/tailscale tunnel", net.FlagUp | net.FlagPointToPoint, false},
		{"up with no other flags", net.FlagUp, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iface := net.Interface{Flags: tt.flags}
			if got := isLANInterface(iface); got != tt.want {
				t.Errorf("isLANInterface(%v) = %v, want %v", tt.flags, got, tt.want)
			}
		})
	}
}

func TestFirstIPv4(t *testing.T) {
	tests := []struct {
		name  string
		addrs []net.Addr
		want  string
	}{
		{
			name:  "ipnet ipv4",
			addrs: []net.Addr{&net.IPNet{IP: net.ParseIP("192.168.1.50")}},
			want:  "192.168.1.50",
		},
		{
			name:  "ipaddr ipv4",
			addrs: []net.Addr{&net.IPAddr{IP: net.ParseIP("10.0.0.5")}},
			want:  "10.0.0.5",
		},
		{
			name:  "skips loopback then returns ipv4",
			addrs: []net.Addr{&net.IPNet{IP: net.ParseIP("127.0.0.1")}, &net.IPNet{IP: net.ParseIP("192.168.1.50")}},
			want:  "192.168.1.50",
		},
		{
			name:  "skips ipv6 then returns ipv4",
			addrs: []net.Addr{&net.IPNet{IP: net.ParseIP("fe80::1")}, &net.IPNet{IP: net.ParseIP("192.168.1.50")}},
			want:  "192.168.1.50",
		},
		{
			name:  "no ipv4 present",
			addrs: []net.Addr{&net.IPNet{IP: net.ParseIP("fe80::1")}},
			want:  "",
		},
		{
			name:  "empty addrs",
			addrs: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstIPv4(tt.addrs); got != tt.want {
				t.Errorf("firstIPv4() = %q, want %q", got, tt.want)
			}
		})
	}
}
