package auth

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/model"
)

// AuthServiceImpl implements the Service interface
type AuthServiceImpl struct {
	config     *config.Config
	githubAuth *GitHubDeviceAuth
}

// NewAuthService creates a new authentication service
func NewAuthService(cfg *config.Config) Service {
	githubConfig := GitHubOAuthConfig{
		ClientID:     cfg.GithubClientID,
		ClientSecret: cfg.GithubClientSecret,
	}

	return &AuthServiceImpl{
		config:     cfg,
		githubAuth: NewGitHubDeviceAuth(githubConfig),
	}
}

func (s *AuthServiceImpl) StartAuthFlow(ctx context.Context, method model.AuthMethod, repoRef string) (map[string]string, string, error) {
	// return not implemented error
	return nil, "", fmt.Errorf("not implemented")
}

func (s *AuthServiceImpl) CheckAuthStatus(ctx context.Context, statusToken string) (string, error) {
	// return not implemented error
	return "", fmt.Errorf("not implemented")
}

// ValidateAuth validates authentication credentials
func (s *AuthServiceImpl) ValidateAuth(ctx context.Context, auth model.Authentication) (bool, error) {
	// If authentication is not required, allow access
	if !s.config.RequireAuth {
		return true, nil
	}

	// If authentication is required but not provided
	if auth.Method == "" || auth.Method == model.AuthMethodNone {
		return false, ErrAuthRequired
	}

	switch auth.Method {
	case model.AuthMethodGitHub:
		// Extract repo reference from the repository URL if it's not provided
		owner, repo, err := s.githubAuth.ExtractGitHubRepoFromName(auth.RepoRef)
		if err != nil {
			return false, err
		}
		repoRef := fmt.Sprintf("%s/%s", owner, repo)
		if repoRef == "" {
			return false, fmt.Errorf("repository reference is required for GitHub authentication")
		}
		return s.githubAuth.ValidateToken(auth.Token, repoRef)
	default:
		return false, ErrUnsupportedAuthMethod
	}
}
