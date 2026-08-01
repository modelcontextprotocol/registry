package auth_test

import (
	"testing"

	"github.com/modelcontextprotocol/registry/internal/api/handlers/v0/auth"
)

func TestIsValidDomain(t *testing.T) {
	tests := []struct {
		domain string
		want   bool
	}{
		// Valid
		{"example.com", true},
		{"sub.example.com", true},
		{"a.b.c.d.example.com", true},
		{"foo-bar.example.com", true},
		{"123.example.com", true},

		// Invalid — empty / oversize
		{"", false},

		// Invalid — IP literals (SSRF vector)
		{"127.0.0.1", false},
		{"10.0.0.1", false},
		{"169.254.169.254", false},
		{"::1", false},
		{"fe80::1", false},

		// Invalid — single-label internal names (SSRF vector)
		{"localhost", false},
		{"kubernetes", false},
		{"internal", false},

		// Invalid — bad characters / structure
		{"-example.com", false},
		{"example.com-", false},
		{"exa mple.com", false},
		{"example..com", false},
	}
	for _, tc := range tests {
		t.Run(tc.domain, func(t *testing.T) {
			if got := auth.IsValidDomain(tc.domain); got != tc.want {
				t.Errorf("IsValidDomain(%q) = %v, want %v", tc.domain, got, tc.want)
			}
		})
	}
}
