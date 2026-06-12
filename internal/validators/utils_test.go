package validators_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/modelcontextprotocol/registry/internal/validators"
)

func TestIsValidRepositoryURL(t *testing.T) {
	tests := []struct {
		name   string
		source validators.RepositorySource
		url    string
		want   bool
	}{
		// GitHub
		{"github valid owner/repo", validators.SourceGitHub, "https://github.com/owner/repo", true},
		{"github valid with trailing slash", validators.SourceGitHub, "https://github.com/owner/repo/", true},
		{"github valid with www", validators.SourceGitHub, "https://www.github.com/owner/repo", true},
		{"github missing repo", validators.SourceGitHub, "https://github.com/owner", false},
		{"github extra path segment stays invalid", validators.SourceGitHub, "https://github.com/owner/repo/extra", false},

		// GitLab: flat owner/repo (no regression)
		{"gitlab valid owner/repo", validators.SourceGitLab, "https://gitlab.com/owner/repo", true},
		{"gitlab valid with trailing slash", validators.SourceGitLab, "https://gitlab.com/owner/repo/", true},
		{"gitlab valid with www", validators.SourceGitLab, "https://www.gitlab.com/owner/repo", true},
		{"gitlab valid with dots and dashes", validators.SourceGitLab, "https://gitlab.com/my-org.io/my_repo-v2.0", true},

		// GitLab: nested groups/subgroups (issue #1359)
		{"gitlab valid single subgroup", validators.SourceGitLab, "https://gitlab.com/group/subgroup/repo", true},
		{"gitlab valid deeply nested subgroups", validators.SourceGitLab, "https://gitlab.com/myorg/team/subgroup/my-mcp-server", true},
		{"gitlab valid nested with trailing slash", validators.SourceGitLab, "https://gitlab.com/group/subgroup/repo/", true},

		// GitLab: malformed URLs that must stay rejected
		{"gitlab missing owner and repo", validators.SourceGitLab, "https://gitlab.com", false},
		{"gitlab missing repo", validators.SourceGitLab, "https://gitlab.com/owner", false},
		{"gitlab empty path segment", validators.SourceGitLab, "https://gitlab.com/group//repo", false},
		{"gitlab leading empty segment", validators.SourceGitLab, "https://gitlab.com//group/repo", false},
		{"gitlab segment with space", validators.SourceGitLab, "https://gitlab.com/group/sub group/repo", false},
		{"gitlab spoofed host suffix", validators.SourceGitLab, "https://evilgitlab.com/group/repo", false},
		{"gitlab spoofed host prefix", validators.SourceGitLab, "https://gitlab.com.evil.com/group/repo", false},
		{"gitlab query string", validators.SourceGitLab, "https://gitlab.com/group/subgroup/repo?ref=main", false},
		{"gitlab fragment", validators.SourceGitLab, "https://gitlab.com/group/subgroup/repo#readme", false},

		// Unknown source
		{"unknown source", validators.RepositorySource("bitbucket"), "https://bitbucket.org/owner/repo", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validators.IsValidRepositoryURL(tc.source, tc.url)
			assert.Equal(t, tc.want, got, "url: %s", tc.url)
		})
	}
}
