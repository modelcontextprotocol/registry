// Package auth provides authentication mechanisms for the MCP registry
package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

const (
	GitHubValidateTokenURL = "https://api.github.com/applications/CLIENT_ID/token"
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

// ValidateToken checks if a token has write:packages access to the required repository
func (g *GitHubDeviceAuth) ValidateToken(token string, requiredRepo string) (bool, error) {
	// If no repo is required, we can't validate properly
	if requiredRepo == "" {
		return false, fmt.Errorf("repository reference is required for token validation")
	}

	// First, check if the token is valid using the app token check endpoint
	req, err := http.NewRequest("POST", GitHubValidateTokenURL, bytes.NewBuffer([]byte(`{"access_token": "`+token+`"}`)))
	if err != nil {
		return false, err
	}

	req.URL.Path = fmt.Sprintf("/applications/%s/token", g.config.ClientID)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(g.config.ClientID, g.config.ClientSecret)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("token validation failed: %s", body)
	}

	var validationResp TokenValidationResponse
	err = json.Unmarshal(body, &validationResp)
	if err != nil {
		return false, err
	}

	// Now check if the token has permissions to the specific repository
	// Use the GitHub API to check repository-specific permissions
	repoCheckURL := fmt.Sprintf("https://api.github.com/repos/%s", requiredRepo)
	repoReq, err := http.NewRequest("GET", repoCheckURL, nil)
	if err != nil {
		return false, err
	}

	repoReq.Header.Set("Accept", "application/vnd.github+json")
	repoReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	repoResp, err := client.Do(repoReq)
	if err != nil {
		return false, err
	}
	defer repoResp.Body.Close()

	// Read and check response
	if repoResp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("token does not have access to repository: %s (status: %d)", requiredRepo, repoResp.StatusCode)
	}

	// If we've reached this point, the token has access the repo
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
