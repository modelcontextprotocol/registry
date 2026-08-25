package commands

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseGitHubOwnerRepo(t *testing.T) {
	tests := []struct {
		name    string
		repoURL string
		owner   string
		repo    string
		ok      bool
	}{
		{name: "plain https URL", repoURL: "https://github.com/acme/weather", owner: "acme", repo: "weather", ok: true},
		{name: "www host", repoURL: "https://www.github.com/acme/weather", owner: "acme", repo: "weather", ok: true},
		{name: "trailing slash", repoURL: "https://github.com/acme/weather/", owner: "acme", repo: "weather", ok: true},
		{name: "dot-git suffix", repoURL: "https://github.com/acme/weather.git", owner: "acme", repo: "weather", ok: true},
		{name: "gitlab is not github", repoURL: "https://gitlab.com/acme/weather"},
		// A lookalike host must not be treated as GitHub — the ID we would fetch
		// would belong to whatever that host decided to return.
		{name: "lookalike host", repoURL: "https://github.com.evil.example/acme/weather"},
		{name: "deep path is not a repo root", repoURL: "https://github.com/acme/weather/tree/main/src"},
		{name: "owner only", repoURL: "https://github.com/acme"},
		{name: "empty", repoURL: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ok := parseGitHubOwnerRepo(tt.repoURL)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.owner, owner)
			assert.Equal(t, tt.repo, repo)
		})
	}
}

func TestDetectRepoID(t *testing.T) {
	t.Run("records the numeric id GitHub reports", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_, _ = w.Write([]byte(`{"id": 123456789, "full_name": "acme/weather"}`))
		}))
		defer srv.Close()
		t.Setenv("GITHUB_TOKEN", "")

		originalBaseURL := githubAPIBaseURL
		githubAPIBaseURL = srv.URL
		defer func() { githubAPIBaseURL = originalBaseURL }()

		assert.Equal(t, "123456789", detectRepoID(MethodGitHub, "https://github.com/acme/weather"))
		assert.Equal(t, "/repos/acme/weather", gotPath)
	})

	t.Run("stays empty rather than failing init when the lookup does not work", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		t.Setenv("GITHUB_TOKEN", "")

		originalBaseURL := githubAPIBaseURL
		githubAPIBaseURL = srv.URL
		defer func() { githubAPIBaseURL = originalBaseURL }()

		// 404 / private / rate limited.
		assert.Empty(t, detectRepoID(MethodGitHub, "https://github.com/acme/weather"))
		// Non-GitHub sources are never looked up, so no request is made at all.
		assert.Empty(t, detectRepoID("gitlab", "https://gitlab.com/acme/weather"))
		assert.Empty(t, detectRepoID(MethodGitHub, ""))
	})
}
