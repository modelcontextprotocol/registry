package auth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"

	v0auth "github.com/modelcontextprotocol/registry/internal/api/handlers/v0/auth"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoneHandler_GetAnonymousToken(t *testing.T) {
	// Generate a proper Ed25519 seed for testing
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey:       hex.EncodeToString(testSeed),
		EnableAnonymousAuth: true,
	}

	handler := v0auth.NewNoneHandler(cfg)
	ctx := context.Background()

	// Test getting anonymous token
	tokenResponse, err := handler.GetAnonymousToken(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenResponse.RegistryToken)
	assert.Greater(t, tokenResponse.ExpiresAt, 0)

	// Validate the token claims
	jwtManager := auth.NewJWTManager(cfg)
	claims, err := jwtManager.ValidateToken(ctx, tokenResponse.RegistryToken)
	require.NoError(t, err)

	// Check auth method
	assert.Equal(t, auth.MethodNone, claims.AuthMethod)
	assert.Equal(t, "anonymous", claims.AuthMethodSubject)

	// Check permissions - should have both publish and edit permissions
	require.Len(t, claims.Permissions, 2)

	// Check publish permission
	assert.Equal(t, auth.PermissionActionPublish, claims.Permissions[0].Action)
	assert.Equal(t, "io.modelcontextprotocol.anonymous/*", claims.Permissions[0].ResourcePattern)

	// Check edit permission
	assert.Equal(t, auth.PermissionActionEdit, claims.Permissions[1].Action)
	assert.Equal(t, "io.modelcontextprotocol.anonymous/*", claims.Permissions[1].ResourcePattern)
}
