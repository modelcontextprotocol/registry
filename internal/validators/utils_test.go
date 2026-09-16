package validators_test

import (
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators"
	"github.com/stretchr/testify/assert"
)

func TestIsValidRepositoryURL(t *testing.T) {
	tests := []struct {
		name   string
		source validators.RepositorySource
		url    string
		valid  bool
	}{
		{
			name:   "github owner repo",
			source: validators.SourceGitHub,
			url:    "https://github.com/modelcontextprotocol/registry",
			valid:  true,
		},
		{
			name:   "github rejects nested paths",
			source: validators.SourceGitHub,
			url:    "https://github.com/modelcontextprotocol/platform/registry",
			valid:  false,
		},
		{
			name:   "gitlab group repo",
			source: validators.SourceGitLab,
			url:    "https://gitlab.com/myorg/my-mcp-server",
			valid:  true,
		},
		{
			name:   "gitlab nested subgroup repo",
			source: validators.SourceGitLab,
			url:    "https://gitlab.com/myorg/team/subgroup/my-mcp-server",
			valid:  true,
		},
		{
			name:   "gitlab nested subgroup repo with www and trailing slash",
			source: validators.SourceGitLab,
			url:    "https://www.gitlab.com/myorg/team/subgroup/my-mcp-server/",
			valid:  true,
		},
		{
			name:   "gitlab rejects missing repo",
			source: validators.SourceGitLab,
			url:    "https://gitlab.com/myorg",
			valid:  false,
		},
		{
			name:   "gitlab rejects empty path segment",
			source: validators.SourceGitLab,
			url:    "https://gitlab.com/myorg//my-mcp-server",
			valid:  false,
		},
		{
			name:   "gitlab rejects query string",
			source: validators.SourceGitLab,
			url:    "https://gitlab.com/myorg/team/my-mcp-server?tab=readme",
			valid:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, validators.IsValidRepositoryURL(tt.source, tt.url))
		})
	}
}
