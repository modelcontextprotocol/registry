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

func TestClaimDomain_Uniqueness(t *testing.T) {
	// Create an in-memory database for testing
	memDB := database.NewMemoryDB(make(map[string]*model.Server))
	service := NewRegistryServiceWithDB(memDB)

	domain1 := "example.com"
	domain2 := "test.org"

	// Generate first token
	token1, err := service.ClaimDomain(domain1)
	require.NoError(t, err)
	require.NotNil(t, token1)
	assert.NotEmpty(t, token1.Token)

	// Generate second token
	token2, err := service.ClaimDomain(domain2)
	require.NoError(t, err)
	require.NotNil(t, token2)
	assert.NotEmpty(t, token2.Token)

	// Tokens should be different
	assert.NotEqual(t, token1.Token, token2.Token, "Generated tokens should be unique")

	// Verify tokens are stored correctly
	retrievedTokens1, err := service.GetDomainVerificationStatus(domain1)
	require.NoError(t, err)
	require.Len(t, retrievedTokens1.PendingTokens, 1)
	assert.Equal(t, token1.Token, retrievedTokens1.PendingTokens[0].Token)

	retrievedTokens2, err := service.GetDomainVerificationStatus(domain2)
	require.NoError(t, err)
	require.Len(t, retrievedTokens2.PendingTokens, 1)
	assert.Equal(t, token2.Token, retrievedTokens2.PendingTokens[0].Token)
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
	err = memDB.StoreVerificationToken(ctx, "example.com", token)
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

func TestGetDomainVerificationStatus(t *testing.T) {
	// Create an in-memory database for testing
	memDB := database.NewMemoryDB(make(map[string]*model.Server))
	service := NewRegistryServiceWithDB(memDB)

	domain := "example.com"

	// Test when domain doesn't exist
	_, err := service.GetDomainVerificationStatus(domain)
	require.Error(t, err)
	assert.Equal(t, database.ErrNotFound, err)

	// Claim the domain (adds a pending token)
	token, err := service.ClaimDomain(domain)
	require.NoError(t, err)

	// Now status should be unverified with a pending token
	status, err := service.GetDomainVerificationStatus(domain)
	require.NoError(t, err)
	assert.Nil(t, status.VerifiedToken)
	assert.Len(t, status.PendingTokens, 1)
	assert.Equal(t, token.Token, status.PendingTokens[0].Token)
}

func TestClaimDomain_MaxAttempts(t *testing.T) {
	// Create a mock database that always returns false for IsVerificationTokenUnique
	// to simulate the scenario where we can't find a unique token
	memDB := &mockDBAlwaysNonUnique{}
	service := NewRegistryServiceWithDB(memDB)

	domain := "example.com"

	// Attempt to claim domain should fail after max attempts
	token, err := service.ClaimDomain(domain)
	require.Error(t, err)
	assert.Nil(t, token)
	assert.Equal(t, database.ErrMaxAttemptsExceeded, err)
}

// mockDBAlwaysNonUnique is a mock database that always returns false for IsVerificationTokenUnique
type mockDBAlwaysNonUnique struct{}

func (m *mockDBAlwaysNonUnique) List(ctx context.Context, filter map[string]any, cursor string, limit int) ([]*model.Server, string, error) {
	return nil, "", nil
}

func (m *mockDBAlwaysNonUnique) GetByID(ctx context.Context, id string) (*model.ServerDetail, error) {
	return nil, database.ErrNotFound
}

func (m *mockDBAlwaysNonUnique) Publish(ctx context.Context, serverDetail *model.ServerDetail) error {
	return nil
}

func (m *mockDBAlwaysNonUnique) StoreVerificationToken(ctx context.Context, domain string, token *model.VerificationToken) error {
	// Always return ErrTokenAlreadyExists to simulate that tokens are never unique
	return database.ErrTokenAlreadyExists
}

func (m *mockDBAlwaysNonUnique) GetVerificationTokens(ctx context.Context, domain string) (*model.VerificationTokens, error) {
	return nil, database.ErrNotFound
}

func (m *mockDBAlwaysNonUnique) IsVerificationTokenUnique(ctx context.Context, token string) (bool, error) {
	// Always return false to simulate the case where no unique token can be found
	return false, nil
}

func (m *mockDBAlwaysNonUnique) ImportSeed(ctx context.Context, seedFilePath string) error {
	return nil
}

func (m *mockDBAlwaysNonUnique) Close() error {
	return nil
}
