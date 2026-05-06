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
		// Blocked — IPv6 6to4 (RFC 3056 2002::/16); bits 16-47 are an
		// arbitrary IPv4 address, so 2002:a9fe:a9fe:: tunnels to
		// 169.254.169.254 and 2002:0a00:0001:: tunnels to 10.0.0.1.
		{"2002:a9fe:a9fe::", true},
		{"2002:0a00:0001::", true},
		{"2002::1", true},
		// Blocked — IPv6 NAT64 well-known prefix (RFC 6052 64:ff9b::/96);
		// low 32 bits embed an IPv4 address.
		{"64:ff9b::a9fe:a9fe", true},
		{"64:ff9b::a00:1", true},
		// Blocked — IPv6 NAT64 local-use prefix (RFC 8215 64:ff9b:1::/48).
		{"64:ff9b:1::1", true},
		{"64:ff9b:1:abcd::", true},
		// Blocked — IPv6 deprecated site-local (RFC 3879 fec0::/10);
		// still routed into internal networks by some stacks.
		{"fec0::1", true},
		{"feff::1", true},
		// Blocked — IPv4-mapped IPv6 (::ffff:0:0/96) inherits the
		// classification of the wrapped IPv4 via To4() fast-path; covered
		// here as an explicit regression guard.
		{"::ffff:127.0.0.1", true},
		{"::ffff:10.0.0.1", true},
		{"::ffff:169.254.169.254", true},
		// Allowed — public
		{"1.1.1.1", false},
		{"8.8.8.8", false},
		{"2606:4700:4700::1111", false},
		{"2001:4860:4860::8888", false},
		// Allowed — just outside the new IPv6 blocks
		{"2001::1", false},    // outside 2002::/16
		{"2003::1", false},    // outside 2002::/16 on the other side
		{"64:ff9c::1", false}, // outside 64:ff9b::/96
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
