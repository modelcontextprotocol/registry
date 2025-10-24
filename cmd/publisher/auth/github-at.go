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

// RegistryTokenResponse represents the response from registry's token exchange endpoint
type RegistryTokenResponse struct {
	RegistryToken string `json:"registry_token"`
	ExpiresAt     int64  `json:"expires_at"`
}

// StoredRegistryToken represents the registry token with expiration stored locally
type StoredRegistryToken struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// GitHubATProvider implements the Provider interface using GitHub's device flow
type GitHubATProvider struct {
	clientID      string
	forceLogin    bool
	registryURL   string
	providedToken string // Token provided via --token flag or MCP_GITHUB_TOKEN env var
}

// ServerHealthResponse represents the response from the health endpoint
type ServerHealthResponse struct {
	Status         string `json:"status"`
	GitHubClientID string `json:"github_client_id"`
}

// NewGitHubATProvider creates a new GitHub OAuth provider
func NewGitHubATProvider(forceLogin bool, registryURL, token string) Provider {
	// Check for token from flag or environment variable
	if token == "" {
		token = os.Getenv("MCP_GITHUB_TOKEN")
	}

	return &GitHubATProvider{
		forceLogin:    forceLogin,
		registryURL:   registryURL,
		providedToken: token,
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
	// If a token was provided via --token or MCP_GITHUB_TOKEN, no login needed
	if g.providedToken != "" {
		return false
	}

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
	// If a token was provided via --token or MCP_GITHUB_TOKEN, save it and skip device flow
	if g.providedToken != "" {
		err := saveToken(g.providedToken)
		if err != nil {
			return fmt.Errorf("error saving provided token: %w", err)
		}
		return nil
	}

	// If clientID is not set, try to retrieve it from the server's health endpoint
	if g.clientID == "" {
		clientID, err := getClientID(ctx, g.registryURL)
		if err != nil {
			return fmt.Errorf("error getting GitHub Client ID: %w", err)
		}
		g.clientID = clientID
	}

	// Device flow login logic using GitHub's device flow
	// First, request a device code
	deviceCode, userCode, verificationURI, err := g.requestDeviceCode(ctx)
	if err != nil {
		return fmt.Errorf("error requesting device code: %w", err)
	}

	// Display instructions to the user
	_, _ = fmt.Fprintln(os.Stdout, "\nTo authenticate, please:")
	_, _ = fmt.Fprintln(os.Stdout, "1. Go to:", verificationURI)
	_, _ = fmt.Fprintln(os.Stdout, "2. Enter code:", userCode)
	_, _ = fmt.Fprintln(os.Stdout, "3. Authorize this application")

	// Poll for the token
	_, _ = fmt.Fprintln(os.Stdout, "Waiting for authorization...")
	token, err := g.pollForToken(ctx, deviceCode)
	if err != nil {
		return fmt.Errorf("error polling for token: %w", err)
	}

	// Store the token locally
	err = saveToken(token)
	if err != nil {
		return fmt.Errorf("error saving token: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, "Successfully authenticated!")
	return nil
}

// Name returns the name of this auth provider
func (g *GitHubATProvider) Name() string {
	return "github"
}

// requestDeviceCode initiates the device authorization flow
func (g *GitHubATProvider) requestDeviceCode(ctx context.Context) (string, string, string, error) {
	if g.clientID == "" {
		return "", "", "", fmt.Errorf("GitHub Client ID is required for device flow login")
	}

	payload := map[string]string{
		"client_id": g.clientID,
		"scope":     "read:org read:user",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GitHubDeviceCodeURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("request device code failed: %s", body)
	}

	var deviceCodeResp DeviceCodeResponse
	err = json.Unmarshal(body, &deviceCodeResp)
	if err != nil {
		return "", "", "", err
	}

	return deviceCodeResp.DeviceCode, deviceCodeResp.UserCode, deviceCodeResp.VerificationURI, nil
}

// pollForToken polls for access token after user completes authorization
func (g *GitHubATProvider) pollForToken(ctx context.Context, deviceCode string) (string, error) {
	if g.clientID == "" {
		return "", fmt.Errorf("GitHub Client ID is required for device flow login")
	}

	payload := map[string]string{
		"client_id":   g.clientID,
		"device_code": deviceCode,
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	// Default polling interval and expiration time
	interval := 5    // seconds
	expiresIn := 900 // 15 minutes
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GitHubAccessTokenURL, bytes.NewBuffer(jsonData))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", err
		}

		var tokenResp AccessTokenResponse
		err = json.Unmarshal(body, &tokenResp)
		if err != nil {
			return "", err
		}

		if tokenResp.Error == "authorization_pending" {
			// User hasn't authorized yet, wait and retry
			time.Sleep(time.Duration(interval) * time.Second)
			continue
		}

		if tokenResp.Error != "" {
			return "", fmt.Errorf("token request failed: %s", tokenResp.Error)
		}

		if tokenResp.AccessToken != "" {
			return tokenResp.AccessToken, nil
		}

		// If we reach here, something unexpected happened
		return "", fmt.Errorf("failed to obtain access token")
	}

	return "", fmt.Errorf("device code authorization timed out")
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
