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

// GitHubATProvider implements the Provider interface using GitHub's device flow
type GitHubATProvider struct {
	clientID      string
	registryURL   string
	providedToken string // Token provided via --token flag or MCP_GITHUB_TOKEN env var
	githubToken   string // In-memory GitHub token set by Login()
}

// ServerHealthResponse represents the response from the health endpoint
type ServerHealthResponse struct {
	Status         string `json:"status"`
	GitHubClientID string `json:"github_client_id"`
}

// NewGitHubATProvider creates a new GitHub OAuth provider
func NewGitHubATProvider(registryURL, token string) Provider {
	// Check for token from flag or environment variable
	if token == "" {
		token = os.Getenv("MCP_GITHUB_TOKEN")
	}

	return &GitHubATProvider{
		registryURL:   registryURL,
		providedToken: token,
	}
}

// GetToken retrieves the registry JWT token (exchanges GitHub token if needed)
func (g *GitHubATProvider) GetToken(ctx context.Context) (string, error) {
	if g.githubToken == "" {
		return "", fmt.Errorf("no GitHub token available; run Login() first")
	}

	// Exchange GitHub token for registry token
	registryToken, _, err := g.exchangeTokenForRegistry(ctx, g.githubToken)
	// Clear the GitHub token from memory after exchange
	g.githubToken = ""
	if err != nil {
		return "", fmt.Errorf("failed to exchange token: %w", err)
	}

	return registryToken, nil
}

// Login performs the GitHub device flow authentication
func (g *GitHubATProvider) Login(ctx context.Context) error {
	// If a token was provided via --token or MCP_GITHUB_TOKEN, store it in memory and skip device flow
	if g.providedToken != "" {
		g.githubToken = g.providedToken
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

	// Store the token in memory
	g.githubToken = token

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
