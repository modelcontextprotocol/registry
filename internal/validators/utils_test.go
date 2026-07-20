package validators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/modelcontextprotocol/registry/internal/validators"
)

func TestIsValidRemoteURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		// Valid public hosts
		{name: "public host", url: "https://example.com/mcp", want: true},
		{name: "public host with port", url: "https://api.example.com:8443/mcp", want: true},

		// localhost names (regression guard for the original literal checks)
		{name: "localhost name", url: "https://localhost/mcp", want: false},
		{name: "subdomain of localhost", url: "https://sub.localhost/mcp", want: false},
		{name: "ipv4 loopback 127.0.0.1", url: "https://127.0.0.1/mcp", want: false},

		// Previously-missed loopback / unspecified / mapped forms
		{name: "ipv4 loopback 127.0.0.2", url: "https://127.0.0.2/mcp", want: false},
		{name: "ipv6 loopback", url: "https://[::1]/mcp", want: false},
		{name: "ipv4-mapped loopback", url: "https://[::ffff:127.0.0.1]/mcp", want: false},
		{name: "unspecified ipv4", url: "https://0.0.0.0/mcp", want: false},
		{name: "unspecified ipv6", url: "https://[::]/mcp", want: false},

		// Private / link-local ranges
		{name: "rfc1918 10.x", url: "https://10.0.0.1/mcp", want: false},
		{name: "rfc1918 192.168.x", url: "https://192.168.1.1/mcp", want: false},
		{name: "link-local metadata", url: "https://169.254.169.254/mcp", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, validators.IsValidRemoteURL(tt.url))
		})
	}
}
