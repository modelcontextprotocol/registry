package auth_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/registry/internal/api/handlers/v0/auth"
	internalauth "github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock OIDC validator for testing
type MockOIDCValidator struct {
	validateFunc func(ctx context.Context, token string, audience string) (*auth.GitHubOIDCClaims, error)
}

func (m *MockOIDCValidator) ValidateToken(ctx context.Context, token string, audience string) (*auth.GitHubOIDCClaims, error) {
	return m.validateFunc(ctx, token, audience)
}

func TestGitHubOIDCHandler_ExchangeToken(t *testing.T) {
	cfg := &config.Config{
		JWTPrivateKey:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", // 32 bytes hex
		GitHubOIDCAudience: "https://registry.test",
	}

	handler := auth.NewGitHubOIDCHandler(cfg)

	tests := []struct {
		name            string
		mockValidator   *MockOIDCValidator
		expectError     bool
		expectedSubject string
		expectedPerms   int
	}{
		{
			name: "successful token exchange",
			mockValidator: &MockOIDCValidator{
				validateFunc: func(_ context.Context, _ string, _ string) (*auth.GitHubOIDCClaims, error) {
					return &auth.GitHubOIDCClaims{
						RegisteredClaims: jwt.RegisteredClaims{
							Subject:   "repo:octo-org/octo-repo:environment:prod",
							ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
							Audience:  jwt.ClaimStrings{"mcp-registry"},
						},
						RepositoryOwner: "octo-org",
					}, nil
				},
			},
			expectError:     false,
			expectedSubject: "repo:octo-org/octo-repo:environment:prod",
			expectedPerms:   1,
		},
		{
			name: "validation failure",
			mockValidator: &MockOIDCValidator{
				validateFunc: func(_ context.Context, _ string, _ string) (*auth.GitHubOIDCClaims, error) {
					return nil, fmt.Errorf("token validation failed")
				},
			},
			expectError: true,
		},
		{
			name: "invalid repository owner name",
			mockValidator: &MockOIDCValidator{
				validateFunc: func(_ context.Context, _ string, _ string) (*auth.GitHubOIDCClaims, error) {
					return &auth.GitHubOIDCClaims{
						RegisteredClaims: jwt.RegisteredClaims{
							Subject:   "repo:invalid@name/octo-repo:environment:prod",
							ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
							Audience:  jwt.ClaimStrings{"mcp-registry"},
						},
						RepositoryOwner: "invalid@name", // invalid character
					}, nil
				},
			},
			expectError:     false, // Handler should succeed but return empty permissions
			expectedSubject: "repo:invalid@name/octo-repo:environment:prod",
			expectedPerms:   0, // No permissions due to invalid name
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler.SetValidator(tt.mockValidator)

			response, err := handler.ExchangeToken(context.Background(), "test-oidc-token")

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, response)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, response)
				assert.NotEmpty(t, response.RegistryToken)
				assert.Greater(t, response.ExpiresAt, 0)

				// Validate the generated JWT token
				jwtManager := internalauth.NewJWTManager(cfg)
				claims, err := jwtManager.ValidateToken(context.Background(), response.RegistryToken)
				require.NoError(t, err)

				assert.Equal(t, internalauth.MethodGitHubOIDC, claims.AuthMethod)
				assert.Equal(t, tt.expectedSubject, claims.AuthMethodSubject)
				assert.Len(t, claims.Permissions, tt.expectedPerms)

				if tt.expectedPerms > 0 {
					assert.Equal(t, internalauth.PermissionActionPublish, claims.Permissions[0].Action)
					assert.True(t, strings.HasPrefix(claims.Permissions[0].ResourcePattern, "io.github."))
				}
			}
		})
	}
}

func TestGitHubOIDCHandler_AudienceBinding(t *testing.T) {
	t.Run("empty audience config fails closed", func(t *testing.T) {
		cfg := &config.Config{
			JWTPrivateKey:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			GitHubOIDCAudience: "",
		}
		handler := auth.NewGitHubOIDCHandler(cfg)
		// Validator should never be called — the audience precondition rejects first.
		handler.SetValidator(&MockOIDCValidator{
			validateFunc: func(_ context.Context, _ string, _ string) (*auth.GitHubOIDCClaims, error) {
				t.Fatal("validator must not be called when audience is unconfigured")
				return nil, fmt.Errorf("unreachable")
			},
		})

		_, err := handler.ExchangeToken(context.Background(), "any-token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no audience configured")
	})

	t.Run("configured audience is what the validator sees", func(t *testing.T) {
		cfg := &config.Config{
			JWTPrivateKey:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			GitHubOIDCAudience: "https://registry.example.com",
		}
		handler := auth.NewGitHubOIDCHandler(cfg)

		var seenAudience string
		handler.SetValidator(&MockOIDCValidator{
			validateFunc: func(_ context.Context, _ string, audience string) (*auth.GitHubOIDCClaims, error) {
				seenAudience = audience
				return &auth.GitHubOIDCClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:   "repo:octo-org/octo-repo:environment:prod",
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
						Audience:  jwt.ClaimStrings{audience},
					},
					RepositoryOwner: "octo-org",
				}, nil
			},
		})

		_, err := handler.ExchangeToken(context.Background(), "any-token")
		require.NoError(t, err)
		assert.Equal(t, "https://registry.example.com", seenAudience)
	})

	t.Run("token issued for another deployment is rejected", func(t *testing.T) {
		cfg := &config.Config{
			JWTPrivateKey:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			GitHubOIDCAudience: "https://registry.modelcontextprotocol.io",
		}
		handler := auth.NewGitHubOIDCHandler(cfg)

		// The mock validator does what the real one does: reject if the
		// expected audience is not present in the token's aud claim.
		handler.SetValidator(&MockOIDCValidator{
			validateFunc: func(_ context.Context, _ string, expected string) (*auth.GitHubOIDCClaims, error) {
				tokenAudience := "https://attacker-registry.example" //nolint:gosec // G101 false positive: URL string, not a credential
				if expected != tokenAudience {
					return nil, fmt.Errorf("invalid audience: expected %s, got [%s]", expected, tokenAudience)
				}
				return &auth.GitHubOIDCClaims{RepositoryOwner: "octo-org"}, nil
			},
		})

		_, err := handler.ExchangeToken(context.Background(), "captured-token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid audience")
	})
}

func TestBuildPermissionsFromOIDC(t *testing.T) {
	cfg := &config.Config{
		JWTPrivateKey:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		GitHubOIDCAudience: "https://registry.test",
	}
	handler := auth.NewGitHubOIDCHandler(cfg)

	tests := []struct {
		name          string
		claims        *auth.GitHubOIDCClaims
		expectedPerms []internalauth.Permission
	}{
		{
			name: "valid repository owner",
			claims: &auth.GitHubOIDCClaims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "repo:octo-org/octo-repo:environment:prod",
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
					Audience:  jwt.ClaimStrings{"mcp-registry"},
				},
				RepositoryOwner: "octo-org",
			},
			expectedPerms: []internalauth.Permission{
				{
					Action:          internalauth.PermissionActionPublish,
					ResourcePattern: "io.github.octo-org/*",
				},
			},
		},
		{
			name: "invalid repository owner name",
			claims: &auth.GitHubOIDCClaims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "repo:invalid@name/octo-repo:environment:prod",
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
					Audience:  jwt.ClaimStrings{"mcp-registry"},
				},
				RepositoryOwner: "invalid@name", // contains invalid character
			},
			expectedPerms: nil, // No permissions for invalid names
		},
		{
			name: "user repository",
			claims: &auth.GitHubOIDCClaims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "repo:username/octo-repo:environment:prod",
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
					Audience:  jwt.ClaimStrings{"mcp-registry"},
				},
				RepositoryOwner: "username",
			},
			expectedPerms: []internalauth.Permission{
				{
					Action:          internalauth.PermissionActionPublish,
					ResourcePattern: "io.github.username/*",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Access the method through reflection since it's not exported
			// For testing purposes, we'll create a simple test by calling ExchangeToken
			// and validating the resulting permissions
			mockValidator := &MockOIDCValidator{
				validateFunc: func(_ context.Context, _ string, _ string) (*auth.GitHubOIDCClaims, error) {
					return tt.claims, nil
				},
			}
			handler.SetValidator(mockValidator)

			response, err := handler.ExchangeToken(context.Background(), "test-token")

			if tt.expectedPerms == nil {
				// For invalid names, we expect empty permissions but successful token generation
				assert.NoError(t, err)
				assert.NotNil(t, response)

				// Validate the JWT to check permissions
				jwtManager := internalauth.NewJWTManager(cfg)
				claims, err := jwtManager.ValidateToken(context.Background(), response.RegistryToken)
				require.NoError(t, err)
				assert.Len(t, claims.Permissions, 0)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, response)

				// Validate the JWT to check permissions
				jwtManager := internalauth.NewJWTManager(cfg)
				claims, err := jwtManager.ValidateToken(context.Background(), response.RegistryToken)
				require.NoError(t, err)
				assert.Equal(t, tt.expectedPerms, claims.Permissions)
			}
		})
	}
}
