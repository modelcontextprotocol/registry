package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// GitHubTokenExchangeInput represents the input for GitHub token exchange
type GitHubTokenExchangeInput struct {
	Body struct {
		GitHubToken string `json:"github_token" doc:"GitHub OAuth token" required:"true"`
	}
}

// GitHubHandler handles GitHub authentication
type GitHubHandler struct {
	config     *config.Config
	jwtManager *auth.JWTManager
	baseURL    string // Configurable for testing
}

// NewGitHubHandler creates a new GitHub handler
func NewGitHubHandler(cfg *config.Config) *GitHubHandler {
	return &GitHubHandler{
		config:     cfg,
		jwtManager: auth.NewJWTManager(cfg),
		baseURL:    "https://api.github.com",
	}
}

// SetBaseURL sets the base URL for GitHub API (used for testing)
func (h *GitHubHandler) SetBaseURL(url string) {
	h.baseURL = url
}

// RegisterGitHubATEndpoint registers the GitHub access token authentication endpoint with a custom path prefix
func RegisterGitHubATEndpoint(api huma.API, pathPrefix string, cfg *config.Config) {
	handler := NewGitHubHandler(cfg)

	// GitHub token exchange endpoint
	huma.Register(api, huma.Operation{
		OperationID: "exchange-github-token" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/auth/github-at",
		Summary:     "Exchange GitHub OAuth access token for Registry JWT",
		Description: "Exchange a GitHub OAuth access token for a short-lived Registry JWT token",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, input *GitHubTokenExchangeInput) (*v0.Response[auth.TokenResponse], error) {
		response, err := handler.ExchangeToken(ctx, input.Body.GitHubToken)
		if err != nil {
			return nil, huma.Error401Unauthorized("Token exchange failed", err)
		}

		return &v0.Response[auth.TokenResponse]{
			Body: *response,
		}, nil
	})
}

// ExchangeToken exchanges a GitHub OAuth token for a Registry JWT token
func (h *GitHubHandler) ExchangeToken(ctx context.Context, githubToken string) (*auth.TokenResponse, error) {
	// Get GitHub user information
	user, err := h.getGitHubUser(ctx, githubToken)
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub user: %w", err)
	}

	// Get user's organizations
	orgs, err := h.getGitHubUserOrgs(ctx, user.Login, githubToken)
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub organizations: %w", err)
	}

	// Build permissions based on user and organizations
	permissions := h.buildPermissions(user.Login, orgs)

	// Create JWT claims with GitHub user info
	claims := auth.JWTClaims{
		AuthMethod:        auth.MethodGitHubAT,
		AuthMethodSubject: user.Login,
		Permissions:       permissions,
	}

	// Generate Registry JWT token
	tokenResponse, err := h.jwtManager.GenerateTokenResponse(ctx, claims)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JWT token: %w", err)
	}

	return tokenResponse, nil
}

type GitHubUserOrOrg struct {
	Login string `json:"login"`
	ID    int    `json:"id"`
}

// getGitHubUser gets the authenticated user's information
func (h *GitHubHandler) getGitHubUser(ctx context.Context, token string) (*GitHubUserOrOrg, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/user", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, body)
	}

	var user GitHubUserOrOrg
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}

	return &user, nil
}

func (h *GitHubHandler) getGitHubUserOrgs(ctx context.Context, username string, token string) ([]GitHubUserOrOrg, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/users/"+username+"/orgs", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user organizations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, body)
	}

	var orgs []GitHubUserOrOrg
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, fmt.Errorf("failed to decode organizations response: %w", err)
	}

	return orgs, nil
}

// buildPermissions builds permissions based on GitHub user and their organizations
func (h *GitHubHandler) buildPermissions(username string, orgs []GitHubUserOrOrg) []auth.Permission {
	permissions := []auth.Permission{}

	// Assert user and org names match expected regex, to harden against people doing weird things in names
	if !isValidGitHubName(username) {
		return nil
	}
	for _, org := range orgs {
		if !isValidGitHubName(org.Login) {
			return nil
		}
	}

	// Add permission for user's own namespace
	permissions = append(permissions, auth.Permission{
		Action:          auth.PermissionActionPublish,
		ResourcePattern: fmt.Sprintf("io.github.%s/*", username),
	})

	// Add permissions for each organization
	for _, org := range orgs {
		permissions = append(permissions, auth.Permission{
			Action:          auth.PermissionActionPublish,
			ResourcePattern: fmt.Sprintf("io.github.%s/*", org.Login),
		})
	}

	return permissions
}

func isValidGitHubName(name string) bool {
	return regexp.MustCompile(`^[a-zA-Z0-9-]+$`).MatchString(name)
}
