package v0

import (
	"testing"
)

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple domain",
			input:    "example.com",
			expected: "example.com",
		},
		{
			name:     "domain with subdomain",
			input:    "api.example.com",
			expected: "api.example.com",
		},
		{
			name:     "domain with https protocol",
			input:    "https://example.com",
			expected: "example.com",
		},
		{
			name:     "domain with http protocol",
			input:    "http://example.com",
			expected: "example.com",
		},
		{
			name:     "domain with path",
			input:    "https://example.com/path/to/resource",
			expected: "example.com",
		},
		{
			name:     "domain with query parameters",
			input:    "https://example.com?param=value",
			expected: "example.com",
		},
		{
			name:     "domain with port",
			input:    "https://example.com:8080",
			expected: "example.com:8080",
		},
		{
			name:     "mixed case domain",
			input:    "EXAMPLE.COM",
			expected: "example.com",
		},
		{
			name:     "domain with mixed case and protocol",
			input:    "https://API.EXAMPLE.COM/path",
			expected: "api.example.com",
		},
		{
			name:     "github.io domain",
			input:    "username.github.io",
			expected: "username.github.io",
		},
		{
			name:     "github.io with protocol and path",
			input:    "https://username.github.io/project",
			expected: "username.github.io",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalizeDomain(tt.input)
			if err != nil {
				t.Errorf("normalizeDomain(%q) returned unexpected error: %v", tt.input, err)
				return
			}
			if result != tt.expected {
				t.Errorf("normalizeDomain(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeDomainErrors(t *testing.T) {
	errorTests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty string",
			input: "",
		},
		{
			name:  "whitespace only",
			input: "   ",
		},
		{
			name:  "malformed URL",
			input: "http://",
		},
	}

	for _, tt := range errorTests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalizeDomain(tt.input)
			if err == nil {
				t.Errorf("normalizeDomain(%q) = %q, expected error", tt.input, result)
			}
		})
	}
}
