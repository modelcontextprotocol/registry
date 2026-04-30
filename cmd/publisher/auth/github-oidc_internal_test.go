package auth

import "testing"

func TestAudienceFromRegistryURL(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		// Canonical forms
		{"https://registry.modelcontextprotocol.io", "https://registry.modelcontextprotocol.io", false},
		{"https://staging.registry.modelcontextprotocol.io", "https://staging.registry.modelcontextprotocol.io", false},

		// Trailing slash and path are stripped
		{"https://registry.modelcontextprotocol.io/", "https://registry.modelcontextprotocol.io", false},
		{"https://registry.modelcontextprotocol.io/api/v0", "https://registry.modelcontextprotocol.io", false},

		// Host is lowercased
		{"https://Registry.Example.COM", "https://registry.example.com", false},

		// Whitespace is tolerated
		{"  https://registry.example  ", "https://registry.example", false},

		// Invalid inputs
		{"", "", true},
		{"registry.example", "", true}, // missing scheme
		{"https://", "", true},         // missing host
		{"://nothing", "", true},       // missing scheme
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := audienceFromRegistryURL(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %q", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("audienceFromRegistryURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
