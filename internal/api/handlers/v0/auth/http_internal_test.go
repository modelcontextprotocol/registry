package auth

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
	}{
		// Blocked — loopback
		{"127.0.0.1", true},
		{"::1", true},
		// Blocked — RFC1918 / ULA (IsPrivate)
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"fc00::1", true},
		// Blocked — link-local (includes cloud metadata 169.254.169.254)
		{"169.254.169.254", true},
		{"fe80::1", true},
		// Blocked — unspecified
		{"0.0.0.0", true},
		{"::", true},
		// Blocked — admin-scoped and broader multicast
		{"239.0.0.1", true},
		{"ff00::1", true},
		// Blocked — Carrier-Grade NAT (RFC 6598)
		{"100.64.0.1", true},
		{"100.127.255.254", true},
		// Allowed — public
		{"1.1.1.1", false},
		{"8.8.8.8", false},
		{"2606:4700:4700::1111", false},
		// Allowed — outside CGNAT range
		{"100.63.255.255", false},
		{"100.128.0.1", false},
	}
	for _, tc := range tests {
		t.Run(tc.ip, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("ParseIP(%q) returned nil", tc.ip)
			}
			if got := isBlockedIP(ip); got != tc.blocked {
				t.Errorf("isBlockedIP(%q) = %v, want %v", tc.ip, got, tc.blocked)
			}
		})
	}
}
