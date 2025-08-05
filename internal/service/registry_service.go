package service

import (
	"context"
	"errors"
	"time"

	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/modelcontextprotocol/registry/internal/verification"
)

// registryServiceImpl implements the RegistryService interface using our Database
type registryServiceImpl struct {
	db database.Database
}

// NewRegistryServiceWithDB creates a new registry service with the provided database
//
//nolint:ireturn // Factory function intentionally returns interface for dependency injection
func NewRegistryServiceWithDB(db database.Database) RegistryService {
	return &registryServiceImpl{
		db: db,
	}
}

// GetAll returns all registry entries
func (s *registryServiceImpl) GetAll() ([]model.Server, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use the database's List method with no filters to get all entries
	entries, _, err := s.db.List(ctx, nil, "", 30)
	if err != nil {
		return nil, err
	}

	// Convert from []*model.Server to []model.Server
	result := make([]model.Server, len(entries))
	for i, entry := range entries {
		result[i] = *entry
	}

	return result, nil
}

// List returns registry entries with cursor-based pagination
func (s *registryServiceImpl) List(cursor string, limit int) ([]model.Server, string, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// If limit is not set or negative, use a default limit
	if limit <= 0 {
		limit = 30
	}

	// Use the database's List method with pagination
	entries, nextCursor, err := s.db.List(ctx, nil, cursor, limit)
	if err != nil {
		return nil, "", err
	}

	// Convert from []*model.Server to []model.Server
	result := make([]model.Server, len(entries))
	for i, entry := range entries {
		result[i] = *entry
	}

	return result, nextCursor, nil
}

// GetByID retrieves a specific server detail by its ID
func (s *registryServiceImpl) GetByID(id string) (*model.ServerDetail, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use the database's GetByID method to retrieve the server detail
	serverDetail, err := s.db.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return serverDetail, nil
}

// Publish adds a new server detail to the registry
func (s *registryServiceImpl) Publish(serverDetail *model.ServerDetail) error {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if serverDetail == nil {
		return database.ErrInvalidInput
	}

	err := s.db.Publish(ctx, serverDetail)
	if err != nil {
		return err
	}

	return nil
}

// ClaimDomain generates a verification token for a domain and stores it as pending
func (s *registryServiceImpl) ClaimDomain(domain string) (*model.VerificationToken, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const maxAttempts = 10
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Generate a verification token
		token, err := verification.GenerateVerificationToken()
		if err != nil {
			return nil, err
		}

		// Create the verification token object
		verificationToken := &model.VerificationToken{
			Token:     token,
			CreatedAt: time.Now(),
		}

		// Try to store the token atomically
		err = s.db.StoreVerificationToken(ctx, domain, verificationToken)
		if err != nil {
			if errors.Is(err, database.ErrTokenAlreadyExists) {
				// Token collision, try again with a new token
				continue
			}
			// Other error, return it
			return nil, err
		}

		// Success! Token was stored atomically
		return verificationToken, nil
	}

	// If we've exhausted all attempts, return an error
	return nil, database.ErrMaxAttemptsExceeded
}

// GetDomainVerificationStatus retrieves the verification status for a domain
func (s *registryServiceImpl) GetDomainVerificationStatus(domain string) (*model.VerificationTokens, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get the verification tokens from the database
	tokens, err := s.db.GetVerificationTokens(ctx, domain)
	if err != nil {
		return nil, err
	}

	return tokens, nil
}
