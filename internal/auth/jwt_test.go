package auth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWTManager_GenerateAndVerifyToken(t *testing.T) {
	// Generate a proper Ed25519 seed for testing
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey: hex.EncodeToString(testSeed),
	}

	jwtManager := auth.NewJWTManager(cfg)
	ctx := context.Background()

	t.Run("generate and verify valid token", func(t *testing.T) {
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
		assert.Greater(t, tokenResponse.ExpiresAt, 0)

		// Verify token
		verifiedClaims, err := jwtManager.ValidateToken(ctx, tokenResponse.RegistryToken)
		require.NoError(t, err)
		assert.Equal(t, auth.MethodGitHubAT, verifiedClaims.AuthMethod)
		assert.Equal(t, "testuser", verifiedClaims.AuthMethodSubject)
		assert.Equal(t, "mcp-registry", verifiedClaims.Issuer)
		assert.Len(t, verifiedClaims.Permissions, 1)
		assert.Equal(t, auth.PermissionActionPublish, verifiedClaims.Permissions[0].Action)
		assert.Equal(t, "io.github.testuser/*", verifiedClaims.Permissions[0].ResourcePattern)
	})

	t.Run("token with custom claims", func(t *testing.T) {
		issuedAt := jwt.NewNumericDate(time.Now().Add(-1 * time.Minute))
		expiresAt := jwt.NewNumericDate(time.Now().Add(10 * time.Minute))
		notBefore := jwt.NewNumericDate(time.Now().Add(-30 * time.Second))

		claims := auth.JWTClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				IssuedAt:  issuedAt,
				ExpiresAt: expiresAt,
				NotBefore: notBefore,
				Issuer:    "custom-issuer",
			},
			AuthMethod:        auth.MethodNone,
			AuthMethodSubject: "anonymous",
			Permissions: []auth.Permission{
				{
					Action:          auth.PermissionActionEdit,
					ResourcePattern: "*",
				},
			},
		}

		// Generate token
		tokenResponse, err := jwtManager.GenerateTokenResponse(ctx, claims)
		require.NoError(t, err)

		// Verify token
		verifiedClaims, err := jwtManager.ValidateToken(ctx, tokenResponse.RegistryToken)
		require.NoError(t, err)
		assert.Equal(t, auth.MethodNone, verifiedClaims.AuthMethod)
		assert.Equal(t, "anonymous", verifiedClaims.AuthMethodSubject)
		assert.Equal(t, "custom-issuer", verifiedClaims.Issuer)
		assert.Equal(t, issuedAt.Unix(), verifiedClaims.IssuedAt.Unix())
		assert.Equal(t, expiresAt.Unix(), verifiedClaims.ExpiresAt.Unix())
		assert.Equal(t, notBefore.Unix(), verifiedClaims.NotBefore.Unix())
	})

	t.Run("expired token should fail validation", func(t *testing.T) {
		claims := auth.JWTClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)), // Already expired
			},
			AuthMethod:        auth.MethodGitHubAT,
			AuthMethodSubject: "testuser",
		}

		// Generate token
		tokenResponse, err := jwtManager.GenerateTokenResponse(ctx, claims)
		require.NoError(t, err)

		// Verify token should fail
		_, err = jwtManager.ValidateToken(ctx, tokenResponse.RegistryToken)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse token")
	})

	t.Run("invalid token signature should fail", func(t *testing.T) {
		// Create a different seed
		differentSeed := make([]byte, ed25519.SeedSize)
		_, err := rand.Read(differentSeed)
		require.NoError(t, err)

		differentCfg := &config.Config{
			JWTPrivateKey: hex.EncodeToString(differentSeed),
		}
		differentJWTManager := auth.NewJWTManager(differentCfg)

		claims := auth.JWTClaims{
			AuthMethod:        auth.MethodGitHubAT,
			AuthMethodSubject: "testuser",
		}

		// Generate token with different key
		tokenResponse, err := differentJWTManager.GenerateTokenResponse(ctx, claims)
		require.NoError(t, err)

		// Try to verify with original key - should fail
		_, err = jwtManager.ValidateToken(ctx, tokenResponse.RegistryToken)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse token")
	})

	t.Run("malformed token should fail", func(t *testing.T) {
		// Try to validate a malformed token
		_, err := jwtManager.ValidateToken(ctx, "not.a.valid.token")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse token")
	})

	t.Run("multiple permissions", func(t *testing.T) {
		claims := auth.JWTClaims{
			AuthMethod:        auth.MethodGitHubAT,
			AuthMethodSubject: "admin",
			Permissions: []auth.Permission{
				{
					Action:          auth.PermissionActionPublish,
					ResourcePattern: "io.github.admin/*",
				},
				{
					Action:          auth.PermissionActionEdit,
					ResourcePattern: "*",
				},
				{
					Action:          auth.PermissionActionPublish,
					ResourcePattern: "io.github.org/*",
				},
			},
		}

		// Generate token
		tokenResponse, err := jwtManager.GenerateTokenResponse(ctx, claims)
		require.NoError(t, err)

		// Verify token
		verifiedClaims, err := jwtManager.ValidateToken(ctx, tokenResponse.RegistryToken)
		require.NoError(t, err)
		assert.Len(t, verifiedClaims.Permissions, 3)
		assert.Equal(t, auth.PermissionActionPublish, verifiedClaims.Permissions[0].Action)
		assert.Equal(t, auth.PermissionActionEdit, verifiedClaims.Permissions[1].Action)
		assert.Equal(t, auth.PermissionActionPublish, verifiedClaims.Permissions[2].Action)
	})
}

func TestJWTManager_HasPermission(t *testing.T) {
	// Generate a proper Ed25519 seed for testing
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey: hex.EncodeToString(testSeed),
	}

	jwtManager := auth.NewJWTManager(cfg)

	tests := []struct {
		name        string
		resource    string
		action      auth.PermissionAction
		permissions []auth.Permission
		expected    bool
	}{
		{
			name:     "exact match",
			resource: "io.github.testuser/server1",
			action:   auth.PermissionActionPublish,
			permissions: []auth.Permission{
				{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.testuser/server1"},
			},
			expected: true,
		},
		{
			name:     "wildcard match",
			resource: "io.github.testuser/server2",
			action:   auth.PermissionActionPublish,
			permissions: []auth.Permission{
				{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.testuser/*"},
			},
			expected: true,
		},
		{
			name:     "global wildcard",
			resource: "any.resource.here",
			action:   auth.PermissionActionEdit,
			permissions: []auth.Permission{
				{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
			},
			expected: true,
		},
		{
			name:     "wrong action",
			resource: "io.github.testuser/server1",
			action:   auth.PermissionActionEdit,
			permissions: []auth.Permission{
				{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.testuser/*"},
			},
			expected: false,
		},
		{
			name:     "no match",
			resource: "io.github.otheruser/server1",
			action:   auth.PermissionActionPublish,
			permissions: []auth.Permission{
				{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.testuser/*"},
			},
			expected: false,
		},
		{
			name:     "multiple permissions with match",
			resource: "io.github.org/server1",
			action:   auth.PermissionActionPublish,
			permissions: []auth.Permission{
				{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.testuser/*"},
				{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
				{Action: auth.PermissionActionPublish, ResourcePattern: "io.github.org/*"},
			},
			expected: true,
		},
		{
			name:        "empty permissions",
			resource:    "io.github.testuser/server1",
			action:      auth.PermissionActionPublish,
			permissions: []auth.Permission{},
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jwtManager.HasPermission(tt.resource, tt.action, tt.permissions)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewJWTManager_InvalidKeySize(t *testing.T) {
	// Test with invalid key size (should panic)
	cfg := &config.Config{
		JWTPrivateKey: "too-short-key",
	}

	assert.Panics(t, func() {
		auth.NewJWTManager(cfg)
	})
}

func TestJWTManager_BlockedNamespaces(t *testing.T) {
	// Generate a proper Ed25519 seed for testing
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey: hex.EncodeToString(testSeed),
	}

	ctx := context.Background()

	t.Run("blocked namespace should deny token", func(t *testing.T) {
		// Temporarily override blocked namespaces for testing
		originalBlocked := auth.BlockedNamespaces
		auth.BlockedNamespaces = []string{"io.github.spammer"}
		defer func() { auth.BlockedNamespaces = originalBlocked }()

		jwtManager := auth.NewJWTManager(cfg)

		claims := auth.JWTClaims{
			AuthMethod:        auth.MethodGitHubAT,
			AuthMethodSubject: "spammer",
			Permissions: []auth.Permission{
				{
					Action:          auth.PermissionActionPublish,
					ResourcePattern: "io.github.spammer/*",
				},
			},
		}

		tokenResponse, err := jwtManager.GenerateTokenResponse(ctx, claims)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "your namespace is blocked")
		assert.Nil(t, tokenResponse)
	})

	t.Run("non-blocked namespace should allow token", func(t *testing.T) {
		// Temporarily override blocked namespaces for testing
		originalBlocked := auth.BlockedNamespaces
		auth.BlockedNamespaces = []string{"io.github.spammer"}
		defer func() { auth.BlockedNamespaces = originalBlocked }()

		jwtManager := auth.NewJWTManager(cfg)

		claims := auth.JWTClaims{
			AuthMethod:        auth.MethodGitHubAT,
			AuthMethodSubject: "gooduser",
			Permissions: []auth.Permission{
				{
					Action:          auth.PermissionActionPublish,
					ResourcePattern: "io.github.gooduser/*",
				},
			},
		}

		tokenResponse, err := jwtManager.GenerateTokenResponse(ctx, claims)
		require.NoError(t, err)
		assert.NotEmpty(t, tokenResponse.RegistryToken)
	})

	t.Run("multiple permissions with one blocked should deny token", func(t *testing.T) {
		// Temporarily override blocked namespaces for testing
		originalBlocked := auth.BlockedNamespaces
		auth.BlockedNamespaces = []string{"io.github.badorg"}
		defer func() { auth.BlockedNamespaces = originalBlocked }()

		jwtManager := auth.NewJWTManager(cfg)

		claims := auth.JWTClaims{
			AuthMethod:        auth.MethodGitHubAT,
			AuthMethodSubject: "user",
			Permissions: []auth.Permission{
				{
					Action:          auth.PermissionActionPublish,
					ResourcePattern: "io.github.user/*", // allowed
				},
				{
					Action:          auth.PermissionActionPublish,
					ResourcePattern: "io.github.badorg/*", // blocked
				},
			},
		}

		tokenResponse, err := jwtManager.GenerateTokenResponse(ctx, claims)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "your namespace is blocked")
		assert.Nil(t, tokenResponse)
	})

	t.Run("global admin permissions should bypass denylist", func(t *testing.T) {
		// Temporarily override blocked namespaces for testing
		originalBlocked := auth.BlockedNamespaces
		auth.BlockedNamespaces = []string{"io.github.spammer"}
		defer func() { auth.BlockedNamespaces = originalBlocked }()

		jwtManager := auth.NewJWTManager(cfg)

		claims := auth.JWTClaims{
			AuthMethod:        auth.MethodNone,
			AuthMethodSubject: "admin",
			Permissions: []auth.Permission{
				{
					Action:          auth.PermissionActionPublish,
					ResourcePattern: "*", // global permission should bypass blocking
				},
			},
		}

		tokenResponse, err := jwtManager.GenerateTokenResponse(ctx, claims)
		require.NoError(t, err)
		assert.NotEmpty(t, tokenResponse.RegistryToken)
	})
}
