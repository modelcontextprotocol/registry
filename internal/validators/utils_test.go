package validators

import (
	"testing"
)

func TestIsValidRepositoryURL(t *testing.T) {
	tests := []struct {
		name   string
		source RepositorySource
		url    string
		want   bool
	}{
		// GitHub valid URLs
		{
			name:   "valid GitHub URL",
			source: SourceGitHub,
			url:    "https://github.com/owner/repo",
			want:   true,
		},
		{
			name:   "valid GitHub URL with www",
			source: SourceGitHub,
			url:    "https://www.github.com/owner/repo",
			want:   true,
		},
		{
			name:   "valid GitHub URL with trailing slash",
			source: SourceGitHub,
			url:    "https://github.com/owner/repo/",
			want:   true,
		},
		// GitHub invalid URLs
		{
			name:   "GitHub URL missing repo",
			source: SourceGitHub,
			url:    "https://github.com/owner",
			want:   false,
		},
		{
			name:   "GitHub URL with subgroup",
			source: SourceGitHub,
			url:    "https://github.com/org/team/repo",
			want:   false,
		},
		// GitLab valid URLs
		{
			name:   "valid GitLab URL",
			source: SourceGitLab,
			url:    "https://gitlab.com/owner/repo",
			want:   true,
		},
		{
			name:   "valid GitLab URL with www",
			source: SourceGitLab,
			url:    "https://www.gitlab.com/owner/repo",
			want:   true,
		},
		{
			name:   "valid GitLab URL with trailing slash",
			source: SourceGitLab,
			url:    "https://gitlab.com/owner/repo/",
			want:   true,
		},
		{
			name:   "valid GitLab URL with one subgroup",
			source: SourceGitLab,
			url:    "https://gitlab.com/myorg/team/my-mcp-server",
			want:   true,
		},
		{
			name:   "valid GitLab URL with nested subgroups",
			source: SourceGitLab,
			url:    "https://gitlab.com/myorg/team/subgroup/my-mcp-server",
			want:   true,
		},
		{
			name:   "valid GitLab URL with deeply nested subgroups",
			source: SourceGitLab,
			url:    "https://gitlab.com/org/group/subgroup/project/repo",
			want:   true,
		},
		// GitLab invalid URLs
		{
			name:   "GitLab URL missing repo",
			source: SourceGitLab,
			url:    "https://gitlab.com/owner",
			want:   false,
		},
		{
			name:   "GitLab URL missing owner and repo",
			source: SourceGitLab,
			url:    "https://gitlab.com",
			want:   false,
		},
		// Unknown source
		{
			name:   "unknown source",
			source: "unknown",
			url:    "https://example.com/owner/repo",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidRepositoryURL(tt.source, tt.url)
			if got != tt.want {
				t.Errorf("IsValidRepositoryURL(%v, %q) = %v, want %v", tt.source, tt.url, got, tt.want)
			}
		})
	}
}
