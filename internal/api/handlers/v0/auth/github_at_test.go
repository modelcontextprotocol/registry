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
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	githubUserEndpoint = "/user"
	githubOrgsEndpoint = "/users/testuser/orgs"
)

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
		assert.Equal(t, model.AuthMethodGitHubAT, claims.AuthMethod)
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
				orgs := []v0auth.GitHubUserOrOrg{
					{Login: "test-org-1", ID: 1},
					{Login: "test-org-2", ID: 2},
				}
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
			case "/users/user with spaces/orgs":
				orgs := []v0auth.GitHubUserOrOrg{}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(orgs) //nolint:errcheck
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
				orgs := []v0auth.GitHubUserOrOrg{
					{Login: "valid-org", ID: 1},
					{Login: "org with spaces", ID: 2}, // Invalid name
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(orgs) //nolint:errcheck
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
		assert.Empty(t, claims.Permissions) // No permissions because one org has invalid name
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
			AuthMethod:        model.AuthMethodGitHubAT,
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
		assert.Equal(t, model.AuthMethodGitHubAT, validatedClaims.AuthMethod)
		assert.Equal(t, "testuser", validatedClaims.AuthMethodSubject)
		assert.Len(t, validatedClaims.Permissions, 1)
	})

	t.Run("token expiration", func(t *testing.T) {
		// Create claims with past expiration
		pastTime := time.Now().Add(-1 * time.Hour)
		claims := auth.JWTClaims{
			AuthMethod:        model.AuthMethodGitHubAT,
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
			AuthMethod:        model.AuthMethodGitHubAT,
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
			wantPerms: 0, // Should return nil if any name is invalid
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
				case "/users/" + tc.username + "/orgs":
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(tc.orgs) //nolint:errcheck
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
