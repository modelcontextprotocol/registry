//go:build !noauth

package auth

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/model"
)

// ServiceImpl implements the Service interface
type ServiceImpl struct {
	config     *config.Config
	githubAuth *GitHubDeviceAuth
}

// NewAuthService creates a new authentication service
//
//nolint:ireturn // Factory function intentionally returns interface for dependency injection
func NewAuthService(cfg *config.Config) Service {
	githubConfig := GitHubOAuthConfig{
		ClientID:     cfg.GithubClientID,
		ClientSecret: cfg.GithubClientSecret,
	}

	return &ServiceImpl{
		config:     cfg,
		githubAuth: NewGitHubDeviceAuth(githubConfig),
	}
}

func (s *ServiceImpl) StartAuthFlow(_ context.Context, _ model.AuthMethod,
	_ string) (map[string]string, string, error) {
	// return not implemented error
	return nil, "", fmt.Errorf("not implemented")
}

func (s *ServiceImpl) CheckAuthStatus(_ context.Context, _ string) (string, error) {
	// return not implemented error
	return "", fmt.Errorf("not implemented")
}

// ValidateAuth validates authentication credentials
func (s *ServiceImpl) ValidateAuth(ctx context.Context, auth model.Authentication) (bool, error) {
	// If authentication is required but not provided
	if auth.Method == "" || auth.Method == model.AuthMethodNone {
		return false, ErrAuthRequired
	}

	switch auth.Method {
	case model.AuthMethodGitHub:
		// Extract repo reference from the repository URL if it's not provided
		return s.githubAuth.ValidateToken(ctx, auth.Token, auth.RepoRef)
	case model.AuthMethodNone:
		return false, ErrAuthRequired
	default:
		return false, ErrUnsupportedAuthMethod
	}
}
