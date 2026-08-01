package auth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	v0auth "github.com/modelcontextprotocol/registry/internal/api/handlers/v0/auth"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	githubUserEndpoint = "/user"
	// githubOrgsEndpoint is the endpoint used to list the authenticated user's org
	// memberships (with role). It replaces the old public-only /users/{user}/orgs.
	githubOrgsEndpoint = "/user/memberships/orgs"
)

// orgMembership mirrors a single entry of GET /user/memberships/orgs for use in
// mock GitHub API responses. Role is "admin" (Owner) or "member".
type orgMembership struct {
	State        string                 `json:"state"`
	Role         string                 `json:"role"`
	Organization v0auth.GitHubUserOrOrg `json:"organization"`
}

// adminMemberships wraps orgs as active "admin"-role memberships, the shape the
// memberships endpoint returns for an org Owner.
func adminMemberships(orgs []v0auth.GitHubUserOrOrg) []orgMembership {
	memberships := make([]orgMembership, 0, len(orgs))
	for _, org := range orgs {
		memberships = append(memberships, orgMembership{State: "active", Role: "admin", Organization: org})
	}
	return memberships
}

// newMockGitHubServer returns a mock GitHub API server that serves a fixed
// "testuser" on /user and delegates the org-memberships endpoint to orgsHandler.
// It keeps individual test cases small (only the memberships behavior varies),
// which also keeps their cyclomatic complexity down.
func newMockGitHubServer(orgsHandler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case githubUserEndpoint:
			user := v0auth.GitHubUserOrOrg{Login: "testuser", ID: 12345}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(user) //nolint:errcheck
		case githubOrgsEndpoint:
			orgsHandler(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// grantedPatterns exchanges the token and returns the resource patterns granted
// in the resulting JWT.
func grantedPatterns(t *testing.T, cfg *config.Config, server *httptest.Server) []string {
	t.Helper()
	handler := v0auth.NewGitHubHandler(cfg)
	handler.SetBaseURL(server.URL)

	ctx := context.Background()
	response, err := handler.ExchangeToken(ctx, "valid-github-token")
	require.NoError(t, err)
	require.NotNil(t, response)

	claims, err := auth.NewJWTManager(cfg).ValidateToken(ctx, response.RegistryToken)
	require.NoError(t, err)

	patterns := make([]string, 0, len(claims.Permissions))
	for _, perm := range claims.Permissions {
		patterns = append(patterns, perm.ResourcePattern)
	}
	return patterns
}

// assertExchangeFailsClosed asserts that a token exchange against server returns
// an error and no token (used for the fail-closed 403 variants).
func assertExchangeFailsClosed(t *testing.T, cfg *config.Config, server *httptest.Server) {
	t.Helper()
	handler := v0auth.NewGitHubHandler(cfg)
	handler.SetBaseURL(server.URL)

	response, err := handler.ExchangeToken(context.Background(), "valid-github-token")
	require.Error(t, err)
	assert.Nil(t, response)
}

func TestGitHubHandler_ExchangeToken(t *testing.T) {
	// Create test handler with mock config
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey: hex.EncodeToString(testSeed),
	}

	t.Run("successful token exchange with user only", func(t *testing.T) {
		// Create mock GitHub API server
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify authorization header
			authHeader := r.Header.Get("Authorization")
			assert.Equal(t, "Bearer valid-github-token", authHeader)

			switch r.URL.Path {
			case githubUserEndpoint:
				user := v0auth.GitHubUserOrOrg{
					Login: "testuser",
					ID:    12345,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(user) //nolint:errcheck
			case githubOrgsEndpoint:
				orgs := []v0auth.GitHubUserOrOrg{}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(orgs) //nolint:errcheck
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer mockServer.Close()

		// Create handler and set mock server URL
		handler := v0auth.NewGitHubHandler(cfg)
		handler.SetBaseURL(mockServer.URL)

		// Test token exchange
		ctx := context.Background()
		response, err := handler.ExchangeToken(ctx, "valid-github-token")

		require.NoError(t, err)
		assert.NotNil(t, response)
		assert.NotEmpty(t, response.RegistryToken)
		assert.Greater(t, response.ExpiresAt, 0)

		// Validate the JWT token
		jwtManager := auth.NewJWTManager(cfg)
		claims, err := jwtManager.ValidateToken(ctx, response.RegistryToken)
		require.NoError(t, err)
		assert.Equal(t, auth.MethodGitHubAT, claims.AuthMethod)
		assert.Equal(t, "testuser", claims.AuthMethodSubject)
		assert.Len(t, claims.Permissions, 1)
		assert.Equal(t, auth.PermissionActionPublish, claims.Permissions[0].Action)
		assert.Equal(t, "io.github.testuser/*", claims.Permissions[0].ResourcePattern)
	})

	t.Run("successful token exchange with organizations", func(t *testing.T) {
		// Create mock GitHub API server
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case githubUserEndpoint:
				user := v0auth.GitHubUserOrOrg{
					Login: "testuser",
					ID:    12345,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(user) //nolint:errcheck
			case githubOrgsEndpoint:
				memberships := adminMemberships([]v0auth.GitHubUserOrOrg{
					{Login: "test-org-1", ID: 1},
					{Login: "test-org-2", ID: 2},
				})
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(memberships) //nolint:errcheck
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer mockServer.Close()

		// Create handler and set mock server URL
		handler := v0auth.NewGitHubHandler(cfg)
		handler.SetBaseURL(mockServer.URL)

		// Test token exchange
		ctx := context.Background()
		response, err := handler.ExchangeToken(ctx, "valid-github-token")

		require.NoError(t, err)
		assert.NotNil(t, response)

		// Validate the JWT token
		jwtManager := auth.NewJWTManager(cfg)
		claims, err := jwtManager.ValidateToken(ctx, response.RegistryToken)
		require.NoError(t, err)
		assert.Equal(t, "testuser", claims.AuthMethodSubject)
		assert.Len(t, claims.Permissions, 3) // User + 2 orgs

		// Check permissions
		expectedPatterns := []string{
			"io.github.testuser/*",
			"io.github.test-org-1/*",
			"io.github.test-org-2/*",
		}
		for i, perm := range claims.Permissions {
			assert.Equal(t, auth.PermissionActionPublish, perm.Action)
			assert.Equal(t, expectedPatterns[i], perm.ResourcePattern)
		}
	})

	t.Run("invalid token returns error", func(t *testing.T) {
		// Create mock GitHub API server that returns 401
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message": "Bad credentials"}`)) //nolint:errcheck
		}))
		defer mockServer.Close()

		// Create handler and set mock server URL
		handler := v0auth.NewGitHubHandler(cfg)
		handler.SetBaseURL(mockServer.URL)

		// Test token exchange
		ctx := context.Background()
		response, err := handler.ExchangeToken(ctx, "invalid-token")

		require.Error(t, err)
		assert.Nil(t, response)
		assert.Contains(t, err.Error(), "GitHub API error")
		assert.Contains(t, err.Error(), "401")
	})

	t.Run("GitHub API error on user fetch", func(t *testing.T) {
		// Create mock GitHub API server that returns 500
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == githubUserEndpoint {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"message": "Internal server error"}`)) //nolint:errcheck
			}
		}))
		defer mockServer.Close()

		// Create handler and set mock server URL
		handler := v0auth.NewGitHubHandler(cfg)
		handler.SetBaseURL(mockServer.URL)

		// Test token exchange
		ctx := context.Background()
		response, err := handler.ExchangeToken(ctx, "valid-token")

		require.Error(t, err)
		assert.Nil(t, response)
		assert.Contains(t, err.Error(), "failed to get GitHub user")
	})

	t.Run("GitHub API error on orgs fetch", func(t *testing.T) {
		// Create mock GitHub API server
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case githubUserEndpoint:
				user := v0auth.GitHubUserOrOrg{
					Login: "testuser",
					ID:    12345,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(user) //nolint:errcheck
			case githubOrgsEndpoint:
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"message": "Internal server error"}`)) //nolint:errcheck
			}
		}))
		defer mockServer.Close()

		// Create handler and set mock server URL
		handler := v0auth.NewGitHubHandler(cfg)
		handler.SetBaseURL(mockServer.URL)

		// Test token exchange
		ctx := context.Background()
		response, err := handler.ExchangeToken(ctx, "valid-token")

		require.Error(t, err)
		assert.Nil(t, response)
		assert.Contains(t, err.Error(), "failed to get GitHub organizations")
	})

	t.Run("invalid GitHub username returns empty permissions", func(t *testing.T) {
		// Create mock GitHub API server with invalid username
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case githubUserEndpoint:
				user := v0auth.GitHubUserOrOrg{
					Login: "user with spaces", // Invalid name
					ID:    12345,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(user) //nolint:errcheck
			case githubOrgsEndpoint:
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]orgMembership{}) //nolint:errcheck
			}
		}))
		defer mockServer.Close()

		// Create handler and set mock server URL
		handler := v0auth.NewGitHubHandler(cfg)
		handler.SetBaseURL(mockServer.URL)

		// Test token exchange
		ctx := context.Background()
		response, err := handler.ExchangeToken(ctx, "valid-token")

		require.NoError(t, err)
		assert.NotNil(t, response)

		// Validate the JWT token
		jwtManager := auth.NewJWTManager(cfg)
		claims, err := jwtManager.ValidateToken(ctx, response.RegistryToken)
		require.NoError(t, err)
		assert.Equal(t, "user with spaces", claims.AuthMethodSubject)
		assert.Empty(t, claims.Permissions) // No permissions due to invalid name
	})

	t.Run("invalid org name is filtered out", func(t *testing.T) {
		// Create mock GitHub API server with invalid org name
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case githubUserEndpoint:
				user := v0auth.GitHubUserOrOrg{
					Login: "testuser",
					ID:    12345,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(user) //nolint:errcheck
			case githubOrgsEndpoint:
				memberships := adminMemberships([]v0auth.GitHubUserOrOrg{
					{Login: "valid-org", ID: 1},
					{Login: "org with spaces", ID: 2}, // Invalid name
				})
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(memberships) //nolint:errcheck
			}
		}))
		defer mockServer.Close()

		// Create handler and set mock server URL
		handler := v0auth.NewGitHubHandler(cfg)
		handler.SetBaseURL(mockServer.URL)

		// Test token exchange
		ctx := context.Background()
		response, err := handler.ExchangeToken(ctx, "valid-token")

		require.NoError(t, err)
		assert.NotNil(t, response)

		// Validate the JWT token
		jwtManager := auth.NewJWTManager(cfg)
		claims, err := jwtManager.ValidateToken(ctx, response.RegistryToken)
		require.NoError(t, err)
		assert.Equal(t, "testuser", claims.AuthMethodSubject)

		// The invalid org is skipped, but the personal namespace and the valid org
		// are still granted — one weird org name must not strip everything else.
		patterns := make([]string, 0, len(claims.Permissions))
		for _, perm := range claims.Permissions {
			patterns = append(patterns, perm.ResourcePattern)
		}
		assert.ElementsMatch(t, []string{"io.github.testuser/*", "io.github.valid-org/*"}, patterns)
		assert.NotContains(t, patterns, "io.github.org with spaces/*")
	})

	t.Run("malformed JSON response", func(t *testing.T) {
		// Create mock GitHub API server that returns invalid JSON
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == githubUserEndpoint {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{invalid json`)) //nolint:errcheck
			}
		}))
		defer mockServer.Close()

		// Create handler and set mock server URL
		handler := v0auth.NewGitHubHandler(cfg)
		handler.SetBaseURL(mockServer.URL)

		// Test token exchange
		ctx := context.Background()
		response, err := handler.ExchangeToken(ctx, "valid-token")

		require.Error(t, err)
		assert.Nil(t, response)
		assert.Contains(t, err.Error(), "failed to decode")
	})
}

func TestGitHubHandler_ExchangeToken_OrgRoles(t *testing.T) {
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey: hex.EncodeToString(testSeed),
	}

	t.Run("member-role org is not granted, only admin orgs", func(t *testing.T) {
		// The user is an Owner (admin) of one org and an ordinary member of another.
		// Only the org they administer should yield a publish permission.
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case githubUserEndpoint:
				user := v0auth.GitHubUserOrOrg{Login: "testuser", ID: 12345}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(user) //nolint:errcheck
			case githubOrgsEndpoint:
				memberships := []orgMembership{
					{State: "active", Role: "admin", Organization: v0auth.GitHubUserOrOrg{Login: "admin-org", ID: 1}},
					{State: "active", Role: "member", Organization: v0auth.GitHubUserOrOrg{Login: "member-org", ID: 2}},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(memberships) //nolint:errcheck
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer mockServer.Close()

		handler := v0auth.NewGitHubHandler(cfg)
		handler.SetBaseURL(mockServer.URL)

		ctx := context.Background()
		response, err := handler.ExchangeToken(ctx, "valid-github-token")
		require.NoError(t, err)
		require.NotNil(t, response)

		jwtManager := auth.NewJWTManager(cfg)
		claims, err := jwtManager.ValidateToken(ctx, response.RegistryToken)
		require.NoError(t, err)

		patterns := make([]string, 0, len(claims.Permissions))
		for _, perm := range claims.Permissions {
			patterns = append(patterns, perm.ResourcePattern)
		}
		assert.ElementsMatch(t, []string{"io.github.testuser/*", "io.github.admin-org/*"}, patterns)
		assert.NotContains(t, patterns, "io.github.member-org/*")
	})

	t.Run("missing read:org scope (403) still grants personal namespace", func(t *testing.T) {
		// A minimal token without read:org authenticates fine but is forbidden from
		// reading org memberships. Personal-namespace publishing must still work.
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case githubUserEndpoint:
				user := v0auth.GitHubUserOrOrg{Login: "testuser", ID: 12345}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(user) //nolint:errcheck
			case githubOrgsEndpoint:
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"message": "Token does not have the required scope"}`)) //nolint:errcheck
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer mockServer.Close()

		handler := v0auth.NewGitHubHandler(cfg)
		handler.SetBaseURL(mockServer.URL)

		ctx := context.Background()
		response, err := handler.ExchangeToken(ctx, "valid-github-token")
		require.NoError(t, err)
		require.NotNil(t, response)

		jwtManager := auth.NewJWTManager(cfg)
		claims, err := jwtManager.ValidateToken(ctx, response.RegistryToken)
		require.NoError(t, err)
		assert.Len(t, claims.Permissions, 1)
		assert.Equal(t, "io.github.testuser/*", claims.Permissions[0].ResourcePattern)
	})

	t.Run("admin org on a later page is still granted (pagination)", func(t *testing.T) {
		// Page 1 is a full page (100) of member-role orgs; the only admin org is on
		// page 2. The pagination loop must not stop at the first page, or the Owner's
		// org grant would be silently dropped.
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case githubUserEndpoint:
				user := v0auth.GitHubUserOrOrg{Login: "testuser", ID: 12345}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(user) //nolint:errcheck
			case githubOrgsEndpoint:
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Query().Get("page") == "1" {
					page1 := make([]orgMembership, 0, 100)
					for i := 0; i < 100; i++ {
						page1 = append(page1, orgMembership{
							State: "active", Role: "member",
							Organization: v0auth.GitHubUserOrOrg{Login: fmt.Sprintf("member-org-%d", i), ID: 1000 + i},
						})
					}
					json.NewEncoder(w).Encode(page1) //nolint:errcheck
					return
				}
				// page 2: a single admin org, signalling the last (short) page.
				page2 := []orgMembership{
					{State: "active", Role: "admin", Organization: v0auth.GitHubUserOrOrg{Login: "late-admin-org", ID: 2}},
				}
				json.NewEncoder(w).Encode(page2) //nolint:errcheck
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer mockServer.Close()

		handler := v0auth.NewGitHubHandler(cfg)
		handler.SetBaseURL(mockServer.URL)

		ctx := context.Background()
		response, err := handler.ExchangeToken(ctx, "valid-github-token")
		require.NoError(t, err)
		require.NotNil(t, response)

		jwtManager := auth.NewJWTManager(cfg)
		claims, err := jwtManager.ValidateToken(ctx, response.RegistryToken)
		require.NoError(t, err)

		patterns := make([]string, 0, len(claims.Permissions))
		for _, perm := range claims.Permissions {
			patterns = append(patterns, perm.ResourcePattern)
		}
		assert.ElementsMatch(t, []string{"io.github.testuser/*", "io.github.late-admin-org/*"}, patterns)
	})

	t.Run("memberships server error fails closed (no token issued)", func(t *testing.T) {
		// A 5xx (or any non-200, non-scope-403) from the memberships endpoint must
		// abort the exchange rather than silently degrade to personal-only perms.
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case githubUserEndpoint:
				user := v0auth.GitHubUserOrOrg{Login: "testuser", ID: 12345}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(user) //nolint:errcheck
			case githubOrgsEndpoint:
				w.WriteHeader(http.StatusInternalServerError)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer mockServer.Close()

		handler := v0auth.NewGitHubHandler(cfg)
		handler.SetBaseURL(mockServer.URL)

		ctx := context.Background()
		response, err := handler.ExchangeToken(ctx, "valid-github-token")
		require.Error(t, err)
		assert.Nil(t, response)
	})

	t.Run("rate-limit 403 fails closed (not treated as missing scope)", func(t *testing.T) {
		// A 403 with X-RateLimit-Remaining: 0 is a throttle, not a missing-scope
		// signal. Degrading would strip a real Owner's org grant, so it must error.
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case githubUserEndpoint:
				user := v0auth.GitHubUserOrOrg{Login: "testuser", ID: 12345}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(user) //nolint:errcheck
			case githubOrgsEndpoint:
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"message": "API rate limit exceeded"}`)) //nolint:errcheck
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer mockServer.Close()

		handler := v0auth.NewGitHubHandler(cfg)
		handler.SetBaseURL(mockServer.URL)

		ctx := context.Background()
		response, err := handler.ExchangeToken(ctx, "valid-github-token")
		require.Error(t, err)
		assert.Nil(t, response)
	})
}

func TestGitHubHandler_ExchangeToken_MembershipFiltering(t *testing.T) {
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{JWTPrivateKey: hex.EncodeToString(testSeed)}

	t.Run("pending admin membership is not granted (state filter)", func(t *testing.T) {
		// A user invited as an Owner but who has not accepted has role "admin" with
		// state "pending". They are not an Owner yet, so the org must not be granted
		// even if the state=active query filter is somehow bypassed.
		server := newMockGitHubServer(func(w http.ResponseWriter, _ *http.Request) {
			memberships := []orgMembership{
				{State: "pending", Role: "admin", Organization: v0auth.GitHubUserOrOrg{Login: "pending-org", ID: 1}},
				{State: "active", Role: "admin", Organization: v0auth.GitHubUserOrOrg{Login: "active-org", ID: 2}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(memberships) //nolint:errcheck
		})
		defer server.Close()

		patterns := grantedPatterns(t, cfg, server)
		assert.ElementsMatch(t, []string{"io.github.testuser/*", "io.github.active-org/*"}, patterns)
		assert.NotContains(t, patterns, "io.github.pending-org/*")
	})

	t.Run("billing_manager role is not granted", func(t *testing.T) {
		server := newMockGitHubServer(func(w http.ResponseWriter, _ *http.Request) {
			memberships := []orgMembership{
				{State: "active", Role: "billing_manager", Organization: v0auth.GitHubUserOrOrg{Login: "billing-org", ID: 1}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(memberships) //nolint:errcheck
		})
		defer server.Close()

		patterns := grantedPatterns(t, cfg, server)
		assert.Equal(t, []string{"io.github.testuser/*"}, patterns)
	})
}

func TestGitHubHandler_ExchangeToken_403FailClosed(t *testing.T) {
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{JWTPrivateKey: hex.EncodeToString(testSeed)}

	t.Run("Retry-After 403 fails closed (secondary rate limit)", func(t *testing.T) {
		// A 403 carrying only Retry-After (no X-RateLimit-Remaining: 0) is a secondary
		// rate limit, not a missing scope. Degrading would strip a real Owner's grant.
		server := newMockGitHubServer(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message": "You have exceeded a secondary rate limit"}`)) //nolint:errcheck
		})
		defer server.Close()

		assertExchangeFailsClosed(t, cfg, server)
	})

	t.Run("SSO-enforced 403 fails closed (not treated as missing scope)", func(t *testing.T) {
		// A SAML/SSO-enforced org returns 403 with X-GitHub-SSO when the token is not
		// SSO-authorized. That is an Owner being blocked, so it must fail closed rather
		// than silently degrade to personal-only.
		server := newMockGitHubServer(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-GitHub-SSO", "required; two_factor_authentication=false")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message": "Resource protected by organization SAML enforcement."}`)) //nolint:errcheck
		})
		defer server.Close()

		assertExchangeFailsClosed(t, cfg, server)
	})
}

func TestGitHubHandler_ExchangeToken_PaginationCap(t *testing.T) {
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{JWTPrivateKey: hex.EncodeToString(testSeed)}

	t.Run("all-full pages past the cap fails closed", func(t *testing.T) {
		// Every page comes back full (100), so the loop never sees a short final page
		// and runs to maxOrgMembershipPages. Rather than return a possibly-truncated
		// org set, the exchange must fail closed (error, no token).
		server := newMockGitHubServer(func(w http.ResponseWriter, _ *http.Request) {
			page := make([]orgMembership, 0, 100)
			for i := 0; i < 100; i++ {
				page = append(page, orgMembership{
					State: "active", Role: "member",
					Organization: v0auth.GitHubUserOrOrg{Login: fmt.Sprintf("org-%d", i), ID: i},
				})
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(page) //nolint:errcheck
		})
		defer server.Close()

		assertExchangeFailsClosed(t, cfg, server)
	})
}

func TestJWTTokenValidation(t *testing.T) {
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey: hex.EncodeToString(testSeed),
	}

	jwtManager := auth.NewJWTManager(cfg)
	ctx := context.Background()

	t.Run("generate and validate token", func(t *testing.T) {
		// Create test claims
		claims := auth.JWTClaims{
			AuthMethod:        auth.MethodGitHubAT,
			AuthMethodSubject: "testuser",
			Permissions: []auth.Permission{
				{
					Action:          auth.PermissionActionPublish,
					ResourcePattern: "io.github.testuser/*",
				},
			},
		}

		// Generate token
		tokenResponse, err := jwtManager.GenerateTokenResponse(ctx, claims)
		require.NoError(t, err)
		assert.NotEmpty(t, tokenResponse.RegistryToken)

		// Validate token
		validatedClaims, err := jwtManager.ValidateToken(ctx, tokenResponse.RegistryToken)
		require.NoError(t, err)
		assert.Equal(t, auth.MethodGitHubAT, validatedClaims.AuthMethod)
		assert.Equal(t, "testuser", validatedClaims.AuthMethodSubject)
		assert.Len(t, validatedClaims.Permissions, 1)
	})

	t.Run("token expiration", func(t *testing.T) {
		// Create claims with past expiration
		pastTime := time.Now().Add(-1 * time.Hour)
		claims := auth.JWTClaims{
			AuthMethod:        auth.MethodGitHubAT,
			AuthMethodSubject: "testuser",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(pastTime),
				IssuedAt:  jwt.NewNumericDate(pastTime.Add(-1 * time.Hour)),
			},
		}

		// Generate token
		tokenResponse, err := jwtManager.GenerateTokenResponse(ctx, claims)
		require.NoError(t, err)

		// Validate token - should fail due to expiration
		_, err = jwtManager.ValidateToken(ctx, tokenResponse.RegistryToken)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "token is expired")
	})

	t.Run("invalid signature", func(t *testing.T) {
		// Create test claims
		claims := auth.JWTClaims{
			AuthMethod:        auth.MethodGitHubAT,
			AuthMethodSubject: "testuser",
		}

		// Generate token
		tokenResponse, err := jwtManager.GenerateTokenResponse(ctx, claims)
		require.NoError(t, err)

		// Tamper with the token
		tamperedToken := tokenResponse.RegistryToken + "tampered"

		// Validate token - should fail due to invalid signature
		_, err = jwtManager.ValidateToken(ctx, tamperedToken)
		require.Error(t, err)
	})
}

func TestPermissionResourceMatching(t *testing.T) {
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey: hex.EncodeToString(testSeed),
	}

	jwtManager := auth.NewJWTManager(cfg)

	testCases := []struct {
		name          string
		resource      string
		pattern       string
		action        auth.PermissionAction
		expectedMatch bool
	}{
		{
			name:          "exact match",
			resource:      "io.github.testuser/myrepo",
			pattern:       "io.github.testuser/myrepo",
			action:        auth.PermissionActionPublish,
			expectedMatch: true,
		},
		{
			name:          "wildcard match",
			resource:      "io.github.testuser/myrepo",
			pattern:       "io.github.testuser/*",
			action:        auth.PermissionActionPublish,
			expectedMatch: true,
		},
		{
			name:          "global wildcard",
			resource:      "io.github.anyuser/anyrepo",
			pattern:       "*",
			action:        auth.PermissionActionPublish,
			expectedMatch: true,
		},
		{
			name:          "no match different user",
			resource:      "io.github.otheruser/repo",
			pattern:       "io.github.testuser/*",
			action:        auth.PermissionActionPublish,
			expectedMatch: false,
		},
		{
			name:          "no match different action",
			resource:      "io.github.testuser/repo",
			pattern:       "io.github.testuser/*",
			action:        auth.PermissionActionEdit,
			expectedMatch: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			permissions := []auth.Permission{
				{
					Action:          auth.PermissionActionPublish,
					ResourcePattern: tc.pattern,
				},
			}

			hasPermission := jwtManager.HasPermission(tc.resource, tc.action, permissions)
			assert.Equal(t, tc.expectedMatch, hasPermission)
		})
	}
}

func TestValidGitHubNames(t *testing.T) {
	// Create a minimal handler to test name validation
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey: hex.EncodeToString(testSeed),
	}

	validNameTests := []struct {
		name      string
		username  string
		orgs      []v0auth.GitHubUserOrOrg
		wantPerms int
	}{
		{
			name:      "valid username only",
			username:  "valid-user",
			orgs:      []v0auth.GitHubUserOrOrg{},
			wantPerms: 1,
		},
		{
			name:      "valid username with numbers",
			username:  "user123",
			orgs:      []v0auth.GitHubUserOrOrg{},
			wantPerms: 1,
		},
		{
			name:     "valid username with org",
			username: "valid-user",
			orgs: []v0auth.GitHubUserOrOrg{
				{Login: "valid-org", ID: 1},
			},
			wantPerms: 2,
		},
		{
			name:      "invalid username with spaces",
			username:  "invalid user",
			orgs:      []v0auth.GitHubUserOrOrg{},
			wantPerms: 0, // Should return nil/empty permissions
		},
		{
			name:      "invalid username with special chars",
			username:  "user@invalid",
			orgs:      []v0auth.GitHubUserOrOrg{},
			wantPerms: 0,
		},
		{
			name:     "valid username with invalid org",
			username: "valid-user",
			orgs: []v0auth.GitHubUserOrOrg{
				{Login: "invalid org", ID: 1},
			},
			wantPerms: 1, // Personal namespace kept; the invalid org is skipped
		},
		{
			name:     "valid username with one valid and one invalid org",
			username: "valid-user",
			orgs: []v0auth.GitHubUserOrOrg{
				{Login: "valid-org", ID: 1},
				{Login: "invalid org", ID: 2},
			},
			wantPerms: 2, // Personal + valid org; the invalid org is skipped
		},
	}

	for _, tc := range validNameTests {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock server
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/user":
					user := v0auth.GitHubUserOrOrg{
						Login: tc.username,
						ID:    12345,
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(user) //nolint:errcheck
				case githubOrgsEndpoint:
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(adminMemberships(tc.orgs)) //nolint:errcheck
				}
			}))
			defer mockServer.Close()

			// Create handler and set mock server URL
			handler := v0auth.NewGitHubHandler(cfg)
			handler.SetBaseURL(mockServer.URL)

			// Test token exchange
			ctx := context.Background()
			response, err := handler.ExchangeToken(ctx, "valid-token")
			require.NoError(t, err)

			// Validate the JWT token and check permissions
			jwtManager := auth.NewJWTManager(cfg)
			claims, err := jwtManager.ValidateToken(ctx, response.RegistryToken)
			require.NoError(t, err)
			assert.Len(t, claims.Permissions, tc.wantPerms)
		})
	}
}

func TestGitHubHandler_Creation(t *testing.T) {
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey: hex.EncodeToString(testSeed),
	}

	handler := v0auth.NewGitHubHandler(cfg)
	assert.NotNil(t, handler, "handler should not be nil")
}

func TestConcurrentTokenExchange(t *testing.T) {
	// Test that the handler is thread-safe
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey: hex.EncodeToString(testSeed),
	}

	// Create mock GitHub API server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			user := v0auth.GitHubUserOrOrg{
				Login: "testuser",
				ID:    12345,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(user) //nolint:errcheck
		case githubOrgsEndpoint:
			orgs := []v0auth.GitHubUserOrOrg{}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(orgs) //nolint:errcheck
		}
	}))
	defer mockServer.Close()

	handler := v0auth.NewGitHubHandler(cfg)
	handler.SetBaseURL(mockServer.URL)

	// Run multiple concurrent exchanges
	concurrency := 10
	errors := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			ctx := context.Background()
			_, err := handler.ExchangeToken(ctx, fmt.Sprintf("token-%d", i))
			errors <- err
		}()
	}

	// Collect results
	for i := 0; i < concurrency; i++ {
		err := <-errors
		assert.NoError(t, err)
	}
}
