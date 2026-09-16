package auth_test

import (
	"testing"
	"time"

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

func TestValidateDomainAndTimestampRejectsGitHubPages(t *testing.T) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	tests := []struct {
		domain    string
		wantError bool
	}{
		// GitHub Pages domains must not mint io.github.* namespaces via DNS/HTTP
		{"my-org.github.io", true},
		{"my-org.GitHub.IO", true},
		{"github.io", true},
		{"sub.my-org.github.io", true},

		// Lookalikes and ordinary domains stay allowed
		{"github.io.evil-example.com", false},
		{"example.com", false},
		{"my-org.github.io.example.com", false},
	}
	for _, tc := range tests {
		t.Run(tc.domain, func(t *testing.T) {
			_, err := auth.ValidateDomainAndTimestamp(tc.domain, timestamp)
			if tc.wantError && err == nil {
				t.Errorf("ValidateDomainAndTimestamp(%q) succeeded, want github.io rejection", tc.domain)
			}
			if !tc.wantError && err != nil {
				t.Errorf("ValidateDomainAndTimestamp(%q) failed: %v", tc.domain, err)
			}
		})
	}
}
