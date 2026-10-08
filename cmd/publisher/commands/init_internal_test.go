package commands

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

		id, err := detectRepoID(MethodGitHub, "https://github.com/acme/weather")
		require.NoError(t, err)
		assert.Equal(t, "123456789", id)
		assert.Equal(t, "/repos/acme/weather", gotPath)
	})

	t.Run("stays empty rather than failing init when the lookup does not work", func(t *testing.T) {
		// A failed lookup leaves the field empty, but it reports why, so init can
		// say so on stderr instead of silently producing a GitHub entry that
		// serializes exactly like a non-GitHub one.
		for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
			t.Run(http.StatusText(status), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(status)
				}))
				defer srv.Close()
				t.Setenv("GITHUB_TOKEN", "")

				originalBaseURL := githubAPIBaseURL
				githubAPIBaseURL = srv.URL
				defer func() { githubAPIBaseURL = originalBaseURL }()

				// 404 is private or missing, 403 is rate limited.
				id, err := detectRepoID(MethodGitHub, "https://github.com/acme/weather")
				assert.Empty(t, id)
				require.Error(t, err)
				assert.Contains(t, err.Error(), strconv.Itoa(status))
			})
		}

		// Non-GitHub sources are never looked up, so no request is made at all —
		// and the empty ID is correct there, so there is nothing to report.
		id, err := detectRepoID("gitlab", "https://gitlab.com/acme/weather")
		assert.Empty(t, id)
		assert.NoError(t, err)

		// A GitHub source we cannot even build a request for is still a GitHub
		// entry that ended up without its ID.
		id, err = detectRepoID(MethodGitHub, "")
		assert.Empty(t, id)
		assert.Error(t, err)
	})
}
