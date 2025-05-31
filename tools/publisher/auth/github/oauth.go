package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

const (
	tokenFilePath = ".mcpregistry_token" // #nosec:G101
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

// OAuthProvider implements the AuthProvider interface using GitHub's device flow
type OAuthProvider struct {
	clientID    string
	forceLogin  bool
	registryURL string
}

// ServerHealthResponse represents the response from the health endpoint
type ServerHealthResponse struct {
	Status         string `json:"status"`
	GitHubClientID string `json:"github_client_id"`
}

// NewOAuthProvider creates a new GitHub OAuth provider
func NewOAuthProvider(forceLogin bool, registryURL string) *OAuthProvider {
	return &OAuthProvider{
		forceLogin:  forceLogin,
		registryURL: registryURL,
	}
}

// GetToken retrieves the GitHub access token
func (g *OAuthProvider) GetToken(_ context.Context) (string, error) {
	return readToken()
}

// NeedsLogin checks if a new login is required
func (g *OAuthProvider) NeedsLogin() bool {
	if g.forceLogin {
		return true
	}

	_, statErr := os.Stat(tokenFilePath)
	return os.IsNotExist(statErr)
}

// Login performs the GitHub device flow authentication
func (g *OAuthProvider) Login(ctx context.Context) error {
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
	log.Println("\nTo authenticate, please:")
	log.Println("1. Go to:", verificationURI)
	log.Println("2. Enter code:", userCode)
	log.Println("3. Authorize this application")

	// Poll for the token
	log.Println("Waiting for authorization...")
	token, err := g.pollForToken(ctx, deviceCode)
	if err != nil {
		return fmt.Errorf("error polling for token: %w", err)
	}

	// Store the token locally
	err = saveToken(token)
	if err != nil {
		return fmt.Errorf("error saving token: %w", err)
	}

	log.Println("Successfully authenticated!")
	return nil
}

// Name returns the name of this auth provider
func (g *OAuthProvider) Name() string {
	return "github-oauth"
}

// requestDeviceCode initiates the device authorization flow
func (g *OAuthProvider) requestDeviceCode(ctx context.Context) (string, string, string, error) {
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
func (g *OAuthProvider) pollForToken(ctx context.Context, deviceCode string) (string, error) {
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
	return os.WriteFile(tokenFilePath, []byte(token), 0600)
}

// readToken reads the GitHub access token from a local file
func readToken() (string, error) {
	tokenData, err := os.ReadFile(tokenFilePath)
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
		log.Printf("Error creating request: %s\n", err.Error())
		return "", err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error fetching health endpoint: %s\n", err.Error())
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Health endpoint returned status %d: %s\n", resp.StatusCode, body)
		return "", fmt.Errorf("health endpoint returned status %d: %s", resp.StatusCode, body)
	}

	var healthResponse ServerHealthResponse
	err = json.NewDecoder(resp.Body).Decode(&healthResponse)
	if err != nil {
		log.Printf("Error decoding health response: %s\n", err.Error())
		return "", err
	}
	if healthResponse.GitHubClientID == "" {
		log.Println("GitHub Client ID is not set in the server's health response.")
		return "", fmt.Errorf("GitHub Client ID is not set in the server's health response")
	}

	githubClientID := healthResponse.GitHubClientID

	return githubClientID, nil
}
