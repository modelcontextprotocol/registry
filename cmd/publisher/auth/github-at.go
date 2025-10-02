package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	gitHubTokenFilePath   = ".mcpregistry_github_token"   // #nosec:G101
	registryTokenFilePath = ".mcpregistry_registry_token" // #nosec:G101
	// GitHub OAuth URLs
	GitHubDeviceCodeURL  = "https://github.com/login/device/code"        // #nosec:G101
	GitHubAccessTokenURL = "https://github.com/login/oauth/access_token" // #nosec:G101
)

// StoredRegistryToken represents the registry token with expiration stored locally
type StoredRegistryToken struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// GitHubATProvider implements the Provider interface using GitHub's device flow
type GitHubATProvider struct {
	clientID    string
	forceLogin  bool
	registryURL string
}

// ServerHealthResponse represents the response from the health endpoint
type ServerHealthResponse struct {
	Status         string `json:"status"`
	GitHubClientID string `json:"github_client_id"`
}

// NewGitHubATProvider creates a new GitHub OAuth provider
func NewGitHubATProvider(forceLogin bool, registryURL string) Provider {
	return &GitHubATProvider{
		forceLogin:  forceLogin,
		registryURL: registryURL,
	}
}

// GetToken retrieves the registry JWT token (exchanges GitHub token if needed)
func (g *GitHubATProvider) GetToken(ctx context.Context) (string, error) {
	// Check if we have a valid registry token
	registryToken, err := readRegistryToken()
	if err == nil && registryToken != "" {
		return registryToken, nil
	}

	// If no valid registry token, exchange GitHub token for registry token
	githubToken, err := readToken()
	if err != nil {
		return "", fmt.Errorf("failed to read GitHub token: %w", err)
	}

	// Exchange GitHub token for registry token
	registryToken, expiresAt, err := g.exchangeTokenForRegistry(ctx, githubToken)
	if err != nil {
		return "", fmt.Errorf("failed to exchange token: %w", err)
	}

	// Store the registry token
	err = saveRegistryToken(registryToken, expiresAt)
	if err != nil {
		return "", fmt.Errorf("failed to save registry token: %w", err)
	}

	return registryToken, nil
}

// NeedsLogin checks if a new login is required
func (g *GitHubATProvider) NeedsLogin() bool {
	if g.forceLogin {
		return true
	}

	// Check if GitHub token exists
	_, statErr := os.Stat(gitHubTokenFilePath)
	if os.IsNotExist(statErr) {
		return true
	}

	// Check if valid registry token exists
	_, err := readRegistryToken()
	if err != nil {
		// No valid registry token, but we have GitHub token
		// We don't need to login, just exchange tokens
		return false
	}

	return false
}

// Login performs the GitHub device flow authentication
func (g *GitHubATProvider) Login(ctx context.Context) error {
	// If clientID is not set, try to retrieve it from the server's health endpoint
	if g.clientID == "" {
		clientID, err := getClientID(ctx, g.registryURL)
		if err != nil {
			return fmt.Errorf("error getting GitHub Client ID: %w", err)
		}
		g.clientID = clientID
	}

	// Use shared device flow implementation
	token, err := runDeviceFlow(ctx, g.clientID, GitHubDeviceCodeURL, GitHubAccessTokenURL, "read:org read:user")
	if err != nil {
		return fmt.Errorf("error in GitHub device flow: %w", err)
	}

	// Store the token locally
	err = saveToken(token)
	if err != nil {
		return fmt.Errorf("error saving token: %w", err)
	}

	return nil
}

// Name returns the name of this auth provider
func (g *GitHubATProvider) Name() string {
	return "github"
}

// saveToken saves the GitHub access token to a local file
func saveToken(token string) error {
	return os.WriteFile(gitHubTokenFilePath, []byte(token), 0600)
}

// readToken reads the GitHub access token from a local file
func readToken() (string, error) {
	tokenData, err := os.ReadFile(gitHubTokenFilePath)
	if err != nil {
		return "", err
	}
	return string(tokenData), nil
}

func getClientID(ctx context.Context, registryURL string) (string, error) {
	// This function should retrieve the GitHub Client ID from the registry URL
	// For now, we will return a placeholder value
	// In a real implementation, this would likely involve querying the registry or configuration
	if registryURL == "" {
		return "", fmt.Errorf("registry URL is required to get GitHub Client ID")
	}
	// get the clientID from the server's health endpoint
	healthURL := registryURL + "/v0/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("health endpoint returned status %d: %s", resp.StatusCode, body)
	}

	var healthResponse ServerHealthResponse
	err = json.NewDecoder(resp.Body).Decode(&healthResponse)
	if err != nil {
		return "", err
	}
	if healthResponse.GitHubClientID == "" {
		return "", fmt.Errorf("GitHub Client ID is not set in the server's health response")
	}

	githubClientID := healthResponse.GitHubClientID

	return githubClientID, nil
}

// exchangeTokenForRegistry exchanges a GitHub token for a registry JWT token
func (g *GitHubATProvider) exchangeTokenForRegistry(ctx context.Context, githubToken string) (string, int64, error) {
	if g.registryURL == "" {
		return "", 0, fmt.Errorf("registry URL is required for token exchange")
	}

	// Prepare the request body
	payload := map[string]string{
		"github_token": githubToken,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make the token exchange request
	exchangeURL := g.registryURL + "/v0/auth/github-at"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, body)
	}

	var tokenResp RegistryTokenResponse
	err = json.Unmarshal(body, &tokenResp)
	if err != nil {
		return "", 0, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return tokenResp.RegistryToken, tokenResp.ExpiresAt, nil
}

// saveRegistryToken saves the registry JWT token to a local file with expiration
func saveRegistryToken(token string, expiresAt int64) error {
	storedToken := StoredRegistryToken{
		Token:     token,
		ExpiresAt: expiresAt,
	}

	data, err := json.Marshal(storedToken)
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	return os.WriteFile(registryTokenFilePath, data, 0600)
}

// readRegistryToken reads the registry JWT token from a local file
func readRegistryToken() (string, error) {
	data, err := os.ReadFile(registryTokenFilePath)
	if err != nil {
		return "", err
	}

	var storedToken StoredRegistryToken
	err = json.Unmarshal(data, &storedToken)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal token: %w", err)
	}

	// Check if token has expired
	if time.Now().Unix() >= storedToken.ExpiresAt {
		// Token has expired, remove the file
		os.Remove(registryTokenFilePath)
		return "", fmt.Errorf("registry token has expired")
	}

	return storedToken.Token, nil
}
