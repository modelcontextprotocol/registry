package service

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateVerificationToken_Uniqueness(t *testing.T) {
	// Create an in-memory database for testing
	memDB := database.NewMemoryDB(make(map[string]*model.Server))
	service := NewRegistryServiceWithDB(memDB)

	serverID1 := "io.github.test/server1"
	serverID2 := "io.github.test/server2"

	// Generate first token
	token1, err := service.GenerateVerificationToken(serverID1)
	require.NoError(t, err)
	require.NotNil(t, token1)
	assert.NotEmpty(t, token1.Token)

	// Generate second token
	token2, err := service.GenerateVerificationToken(serverID2)
	require.NoError(t, err)
	require.NotNil(t, token2)
	assert.NotEmpty(t, token2.Token)

	// Tokens should be different
	assert.NotEqual(t, token1.Token, token2.Token, "Generated tokens should be unique")

	// Verify tokens are stored correctly
	retrievedToken1, err := service.GetVerificationToken(serverID1)
	require.NoError(t, err)
	assert.Equal(t, token1.Token, retrievedToken1.Token)

	retrievedToken2, err := service.GetVerificationToken(serverID2)
	require.NoError(t, err)
	assert.Equal(t, token2.Token, retrievedToken2.Token)
}

func TestIsVerificationTokenUnique(t *testing.T) {
	// Create an in-memory database for testing
	memDB := database.NewMemoryDB(make(map[string]*model.Server))
	ctx := context.Background()

	// Initially, any token should be unique
	isUnique, err := memDB.IsVerificationTokenUnique(ctx, "test-token-123")
	require.NoError(t, err)
	assert.True(t, isUnique)

	// Store a token
	token := &model.VerificationToken{
		Token:     "test-token-123",
		CreatedAt: time.Now(),
	}
	err = memDB.StoreVerificationToken(ctx, "server1", token)
	require.NoError(t, err)

	// Same token should no longer be unique
	isUnique, err = memDB.IsVerificationTokenUnique(ctx, "test-token-123")
	require.NoError(t, err)
	assert.False(t, isUnique)

	// Different token should still be unique
	isUnique, err = memDB.IsVerificationTokenUnique(ctx, "different-token-456")
	require.NoError(t, err)
	assert.True(t, isUnique)
}
