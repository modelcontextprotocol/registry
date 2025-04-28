// Package auth provides authentication mechanisms for the MCP registry
package auth

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/registry/internal/model"
)

var (
	// ErrAuthRequired is returned when authentication is required but not provided
	ErrAuthRequired = errors.New("authentication required")
	// ErrUnsupportedAuthMethod is returned when an unsupported auth method is used
	ErrUnsupportedAuthMethod = errors.New("unsupported authentication method")
)

// Service defines the authentication service interface
type Service interface {
	// StartAuthFlow initiates an authentication flow and returns the flow information
	StartAuthFlow(ctx context.Context, method model.AuthMethod, repoRef string) (map[string]string, string, error)

	// CheckAuthStatus checks the status of an authentication flow using a status token
	CheckAuthStatus(ctx context.Context, statusToken string) (string, error)

	// ValidateAuth validates the authentication credentials
	ValidateAuth(ctx context.Context, auth model.Authentication) (bool, error)
}
