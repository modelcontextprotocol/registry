package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsValidSchemaURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		// Valid schema URLs
		{
			name:     "current schema version 2025-10-17",
			url:      "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
			expected: true,
		},
		{
			name:     "schema version 2025-10-11",
			url:      "https://static.modelcontextprotocol.io/schemas/2025-10-11/server.schema.json",
			expected: true,
		},
		{
			name:     "schema version 2025-09-29",
			url:      "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
			expected: true,
		},
		{
			name:     "schema version 2025-09-16",
			url:      "https://static.modelcontextprotocol.io/schemas/2025-09-16/server.schema.json",
			expected: true,
		},
		{
			name:     "schema version 2025-07-09",
			url:      "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json",
			expected: true,
		},
		{
			name:     "CurrentSchemaURL constant",
			url:      CurrentSchemaURL,
			expected: true,
		},
		// Invalid schema URLs
		{
			name:     "empty string",
			url:      "",
			expected: false,
		},
		{
			name:     "non-existent version",
			url:      "https://static.modelcontextprotocol.io/schemas/2025-01-27/server.schema.json",
			expected: false,
		},
		{
			name:     "wrong domain",
			url:      "https://example.com/schemas/2025-10-17/server.schema.json",
			expected: false,
		},
		{
			name:     "wrong suffix",
			url:      "https://static.modelcontextprotocol.io/schemas/2025-10-17/wrong.json",
			expected: false,
		},
		{
			name:     "missing version",
			url:      "https://static.modelcontextprotocol.io/schemas//server.schema.json",
			expected: false,
		},
		{
			name:     "random URL",
			url:      "https://example.com/my-schema.json",
			expected: false,
		},
		{
			name:     "partial match - version only",
			url:      "2025-10-17",
			expected: false,
		},
		{
			name:     "http instead of https",
			url:      "http://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidSchemaURL(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidSchemaURLs(t *testing.T) {
	urls := ValidSchemaURLs()

	// Should return the same number of URLs as ValidSchemaVersions
	assert.Equal(t, len(ValidSchemaVersions), len(urls))

	// All returned URLs should be valid
	for _, url := range urls {
		assert.True(t, IsValidSchemaURL(url), "URL %s should be valid", url)
	}

	// CurrentSchemaURL should be in the list
	found := false
	for _, url := range urls {
		if url == CurrentSchemaURL {
			found = true
			break
		}
	}
	assert.True(t, found, "CurrentSchemaURL should be in ValidSchemaURLs()")
}

func TestValidSchemaVersions(t *testing.T) {
	// Ensure CurrentSchemaVersion is in the list
	found := false
	for _, version := range ValidSchemaVersions {
		if version == CurrentSchemaVersion {
			found = true
			break
		}
	}
	assert.True(t, found, "CurrentSchemaVersion should be in ValidSchemaVersions")

	// Ensure all versions follow the expected date format
	for _, version := range ValidSchemaVersions {
		assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, version, "Version %s should be in YYYY-MM-DD format", version)
	}
}

func TestSchemaURLConstruction(t *testing.T) {
	// Verify that SchemaURLPrefix + version + SchemaURLSuffix = valid URL
	for _, version := range ValidSchemaVersions {
		expectedURL := SchemaURLPrefix + version + SchemaURLSuffix
		assert.True(t, IsValidSchemaURL(expectedURL), "Constructed URL %s should be valid", expectedURL)
	}
}
