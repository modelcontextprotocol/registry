package validators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/modelcontextprotocol/registry/internal/validators"
)

// TestIsValidRemoteURL_LoopbackAndPrivateAddresses exercises IsValidRemoteURL
// directly against every bypass listed in the GitHub issue: the previous
// implementation only rejected the literal strings "localhost", "127.0.0.1",
// and "*.localhost", so IPv6 loopback, the rest of 127.0.0.0/8, unspecified
// addresses, IPv4-mapped loopback, and RFC1918/link-local addresses all
// passed validation despite the "no localhost allowed" contract.
func TestIsValidRemoteURL_LoopbackAndPrivateAddresses(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		valid bool
	}{
		// Previously-caught cases - must keep working
		{name: "literal localhost", url: "https://localhost/", valid: false},
		{name: "literal localhost with port", url: "https://localhost:8443/mcp", valid: false},
		{name: "localhost subdomain", url: "https://foo.localhost/", valid: false},
		{name: "literal 127.0.0.1", url: "https://127.0.0.1/", valid: false},

		// Previously-missed bypasses called out in the issue
		{name: "IPv6 loopback", url: "https://[::1]/", valid: false},
		{name: "rest of 127.0.0.0/8", url: "https://127.0.0.2/", valid: false},
		{name: "127.0.0.0/8 upper range", url: "https://127.255.255.254/", valid: false},
		{name: "IPv4 unspecified", url: "https://0.0.0.0/", valid: false},
		{name: "IPv6 unspecified", url: "https://[::]/", valid: false},
		{name: "IPv4-mapped IPv6 loopback", url: "https://[::ffff:127.0.0.1]/", valid: false},
		{name: "RFC1918 10.0.0.0/8", url: "https://10.0.0.1/", valid: false},
		{name: "RFC1918 172.16.0.0/12", url: "https://172.16.0.1/", valid: false},
		{name: "RFC1918 192.168.0.0/16", url: "https://192.168.1.1/", valid: false},
		{name: "IPv4 link-local (cloud metadata endpoint)", url: "https://169.254.169.254/", valid: false},
		{name: "IPv6 unique local address", url: "https://[fc00::1]/", valid: false},
		{name: "IPv6 link-local unicast", url: "https://[fe80::1]/", valid: false},

		// Case and trailing-dot variants - DNS is case-insensitive and the
		// root dot is equivalent to its absence.
		{name: "uppercase localhost", url: "https://LOCALHOST/", valid: false},
		{name: "mixed-case localhost subdomain", url: "https://Foo.LOCALHOST/", valid: false},
		{name: "localhost with trailing root dot", url: "https://localhost./", valid: false},

		// Shared address space and non-unicast - not publicly routable
		{name: "CGNAT 100.64.0.0/10", url: "https://100.64.0.1/", valid: false},
		{name: "CGNAT upper range", url: "https://100.127.255.255/", valid: false},
		{name: "IPv4 multicast", url: "https://224.0.0.1/", valid: false},
		{name: "IPv4 broadcast", url: "https://255.255.255.255/", valid: false},
		{name: "IPv6 link-local multicast", url: "https://[ff02::1]/", valid: false},

		// Sanity checks - must not overblock legitimate remotes
		{name: "public hostname", url: "https://example.com/mcp", valid: true},
		{name: "public IPv4 address", url: "https://8.8.8.8/mcp", valid: true},
		{name: "public IPv6 address", url: "https://[2001:4860:4860::8888]/mcp", valid: true},
		{name: "just below CGNAT range", url: "https://100.63.255.255/mcp", valid: true},
		{name: "just above CGNAT range", url: "https://100.128.0.1/mcp", valid: true},
		{name: "http scheme still rejected regardless of host", url: "http://example.com/mcp", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validators.IsValidRemoteURL(tt.url)
			assert.Equal(t, tt.valid, got, "IsValidRemoteURL(%q)", tt.url)
		})
	}
}
