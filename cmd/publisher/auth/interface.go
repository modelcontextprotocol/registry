package auth

import "context"

// Provider defines the interface for authentication mechanisms
type Provider interface {
	// GetToken retrieves or generates an authentication token
	// It returns the token string and any error encountered
	GetToken(ctx context.Context) (string, error)

	// NeedsLogin checks if a new login is required
	// This can check for existing tokens, expiry, etc.
	NeedsLogin() bool

	// Login performs the authentication flow
	// This might involve user interaction, device flows, etc.
	Login(ctx context.Context) error

	// Name returns the name of the authentication provider
	Name() string
}
