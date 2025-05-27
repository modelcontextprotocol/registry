// Package auth provides authentication mechanisms for the MCP registry
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

var (
	// ErrAuthFailed is returned when authentication fails
	ErrAuthFailed = errors.New("authentication failed")
	// ErrInvalidToken is returned when a token is invalid
	ErrInvalidToken = errors.New("invalid token")
	// ErrMissingScope is returned when a token doesn't have the required scope
	ErrMissingScope = errors.New("token missing required scope")
)

// GitHubOAuthConfig holds the configuration for GitHub OAuth
type GitHubOAuthConfig struct {
	ClientID     string
	ClientSecret string
}

// DeviceCodeResponse represents the response from GitHub's device code endpoint
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// AccessTokenResponse represents the response from GitHub's access token endpoint
type AccessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error,omitempty"`
}

// TokenValidationResponse represents the response from GitHub's token validation endpoint
type TokenValidationResponse struct {
	ID          int      `json:"id"`
	URL         string   `json:"url"`
	Scopes      []string `json:"scopes"`
	SingleFile  string   `json:"single_file,omitempty"`
	Repository  string   `json:"repository,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// GitHubDeviceAuth provides methods for GitHub device OAuth authentication
type GitHubDeviceAuth struct {
	config GitHubOAuthConfig
}

// NewGitHubDeviceAuth creates a new GitHub device auth instance
func NewGitHubDeviceAuth(config GitHubOAuthConfig) *GitHubDeviceAuth {
	return &GitHubDeviceAuth{
		config: config,
	}
}

// ValidateToken validates if a GitHub token has the necessary permissions to access the required repository.
// It verifies the token owner matches the repository owner or is a member of the owning organization.
// It also verifies that the token was created for the same ClientID used to set up the authentication.
// Returns true if valid, false otherwise along with an error explaining the validation failure.
func (g *GitHubDeviceAuth) ValidateToken(ctx context.Context, token string, requiredRepo string) (bool, error) {
	// If no repo is required, we can't validate properly
	if requiredRepo == "" {
		return false, fmt.Errorf("repository reference is required for token validation")
	}

	// First, validate that the token is associated with our ClientID
	tokenReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://api.github.com/applications/"+g.config.ClientID+"/token",
		nil,
	)
	if err != nil {
		return false, err
	}

	// The applications endpoint requires basic auth with client ID and secret
	tokenReq.SetBasicAuth(g.config.ClientID, g.config.ClientSecret)
	tokenReq.Header.Set("Accept", "application/vnd.github+json")

	// Create request body with the token
	type tokenCheck struct {
		AccessToken string `json:"access_token"`
	}

	checkBody, err := json.Marshal(tokenCheck{AccessToken: token})
	if err != nil {
		return false, err
	}

	// POST instead of GET for security reasons per GitHub API
	tokenURL := "https://api.github.com/applications/" + g.config.ClientID + "/token"
	tokenReq, err = http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, io.NopCloser(bytes.NewReader(checkBody)))
	if err != nil {
		return false, err
	}

	tokenReq.SetBasicAuth(g.config.ClientID, g.config.ClientSecret)
	tokenReq.Header.Set("Accept", "application/vnd.github+json")
	tokenReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return false, err
	}
	defer tokenResp.Body.Close()

	// Check response - 200 means token is valid and associated with our app
	// 404 means token is not associated with our app
	if tokenResp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("token is not associated with this application (status: %d)", tokenResp.StatusCode)
	}

	var tokenInfo TokenValidationResponse
	tokenRespBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return false, err
	}

	if err := json.Unmarshal(tokenRespBody, &tokenInfo); err != nil {
		return false, err
	}

	// Check if there's an error in the response
	if tokenInfo.Error != "" {
		return false, fmt.Errorf("token validation error: %s", tokenInfo.Error)
	}

	// Get the authenticated user
	userReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return false, err
	}

	userReq.Header.Set("Accept", "application/vnd.github+json")
	userReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	client = &http.Client{}
	userResp, err := client.Do(userReq)
	if err != nil {
		return false, err
	}
	defer userResp.Body.Close()

	if userResp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("failed to get user info: status %d", userResp.StatusCode)
	}

	var userInfo struct {
		Login string `json:"login"`
	}

	userBody, err := io.ReadAll(userResp.Body)
	if err != nil {
		return false, err
	}

	if err := json.Unmarshal(userBody, &userInfo); err != nil {
		return false, err
	}

	// Extract owner from the required repo
	owner, _, err := g.ExtractGitHubRepoFromName(requiredRepo)
	if err != nil {
		return false, err
	}

	// Verify that the authenticated user matches the owner
	if userInfo.Login != owner {
		// Check if the user is a member of the organization
		isMember, err := g.checkOrgMembership(ctx, token, userInfo.Login, owner)
		if err != nil {
			return false, fmt.Errorf("failed to check org membership: %s", owner)
		}

		if !isMember {
			return false, fmt.Errorf(
				"token belongs to user %s, but repository is owned by %s and user is not a member of the organization",
				userInfo.Login, owner)
		}
	}

	// If we've reached this point, the token has access the repo and the user matches
	// the owner or is a member of the owner org
	return true, nil
}

func (g *GitHubDeviceAuth) ExtractGitHubRepoFromName(n string) (owner, repo string, err error) {
	// match io.github.<owner>/<repo>
	regexp := regexp.MustCompile(`io\.github\.([^/]+)/([^/]+)`)
	matches := regexp.FindStringSubmatch(n)
	if len(matches) != 3 {
		return "", "", fmt.Errorf("invalid GitHub repository name: %s", n)
	}
	return matches[1], matches[2], nil
}

// extractGitHubRepo extracts the owner and repository name from a GitHub repository URL
func (g *GitHubDeviceAuth) ExtractGitHubRepo(repoURL string) (owner, repo string, err error) {
	regexp := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)`)
	matches := regexp.FindStringSubmatch(repoURL)
	if len(matches) != 3 {
		return "", "", fmt.Errorf("invalid GitHub repository URL: %s", repoURL)
	}
	return matches[1], matches[2], nil
}

// checkOrgMembership checks if a user is a member of an organization
func (g *GitHubDeviceAuth) checkOrgMembership(ctx context.Context, token, username, org string) (bool, error) {
	// Create request to check if user is a member of the organization
	// GitHub API endpoint: GET /orgs/{org}/members/{username}
	// true if status code is 204 No Content
	// false if status code is 404 Not Found
	url := fmt.Sprint("https://api.github.com/orgs/", org, "/members/", username)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return true, nil
	}

	return false, fmt.Errorf("failed to check org membership: status %d", resp.StatusCode)
}
