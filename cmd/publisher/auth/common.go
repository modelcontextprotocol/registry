package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// CryptoProvider provides common functionality for DNS and HTTP authentication
type CryptoProvider struct {
	registryURL string
	domain      string
	hexSeed     string
	authMethod  string
}

// DeviceCodeResponse represents the response from OAuth/OIDC device code endpoints
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// AccessTokenResponse represents the response from OAuth/OIDC access token endpoints
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

// runDeviceFlow performs a generic OAuth/OIDC device authorization flow
func runDeviceFlow(ctx context.Context, clientID, deviceURL, tokenURL, scope string) (string, error) {
	if clientID == "" {
		return "", fmt.Errorf("client ID is required for device flow")
	}

	// Request device code
	payload := map[string]string{
		"client_id": clientID,
		"scope":     scope,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal device code request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request device code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read device code response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("device code request failed with status %d: %s", resp.StatusCode, body)
	}

	var deviceCodeResp DeviceCodeResponse
	err = json.Unmarshal(body, &deviceCodeResp)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal device code response: %w", err)
	}

	// Display instructions to user
	_, _ = fmt.Fprintln(os.Stdout, "\nTo authenticate, please:")
	_, _ = fmt.Fprintf(os.Stdout, "1. Go to: %s\n", deviceCodeResp.VerificationURI)
	_, _ = fmt.Fprintf(os.Stdout, "2. Enter code: %s\n", deviceCodeResp.UserCode)
	_, _ = fmt.Fprintln(os.Stdout, "3. Authorize this application")
	_, _ = fmt.Fprintln(os.Stdout, "Waiting for authorization...")

	// Poll for token
	tokenPayload := map[string]string{
		"client_id":   clientID,
		"device_code": deviceCodeResp.DeviceCode,
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
	}

	tokenJSONData, err := json.Marshal(tokenPayload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal token request: %w", err)
	}

	// Default polling parameters
	interval := deviceCodeResp.Interval
	if interval < 1 {
		interval = 5 // seconds
	}
	expiresIn := deviceCodeResp.ExpiresIn
	if expiresIn < 1 {
		expiresIn = 900 // 15 minutes
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewBuffer(tokenJSONData))
		if err != nil {
			return "", fmt.Errorf("failed to create token request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to request token: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("failed to read token response: %w", err)
		}

		var tokenResp AccessTokenResponse
		err = json.Unmarshal(body, &tokenResp)
		if err != nil {
			return "", fmt.Errorf("failed to unmarshal token response: %w", err)
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
			_, _ = fmt.Fprintln(os.Stdout, "Successfully authenticated!")
			return tokenResp.AccessToken, nil
		}

		// If we reach here, something unexpected happened
		return "", fmt.Errorf("failed to obtain access token")
	}

	return "", fmt.Errorf("device code authorization timed out")
}

// GetToken retrieves the registry JWT token using cryptographic authentication
func (c *CryptoProvider) GetToken(ctx context.Context) (string, error) {
	if c.domain == "" {
		return "", fmt.Errorf("%s domain is required", c.authMethod)
	}

	if c.hexSeed == "" {
		return "", fmt.Errorf("%s private key (hex seed) is required", c.authMethod)
	}

	// Decode hex seed to private key
	seedBytes, err := hex.DecodeString(c.hexSeed)
	if err != nil {
		return "", fmt.Errorf("invalid hex seed format: %w", err)
	}

	if len(seedBytes) != ed25519.SeedSize {
		return "", fmt.Errorf("invalid seed length: expected %d bytes, got %d", ed25519.SeedSize, len(seedBytes))
	}

	privateKey := ed25519.NewKeyFromSeed(seedBytes)

	// Generate current timestamp
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Sign the timestamp
	signature := ed25519.Sign(privateKey, []byte(timestamp))
	signedTimestamp := hex.EncodeToString(signature)

	// Exchange signature for registry token
	registryToken, err := c.exchangeTokenForRegistry(ctx, c.domain, timestamp, signedTimestamp)
	if err != nil {
		return "", fmt.Errorf("failed to exchange %s signature: %w", c.authMethod, err)
	}

	return registryToken, nil
}

// NeedsLogin always returns false for cryptographic auth since no interactive login is needed
func (c *CryptoProvider) NeedsLogin() bool {
	return false
}

// Login is not needed for cryptographic auth since authentication is cryptographic
func (c *CryptoProvider) Login(_ context.Context) error {
	return nil
}

// exchangeTokenForRegistry exchanges signature for a registry JWT token
func (c *CryptoProvider) exchangeTokenForRegistry(ctx context.Context, domain, timestamp, signedTimestamp string) (string, error) {
	if c.registryURL == "" {
		return "", fmt.Errorf("registry URL is required for token exchange")
	}

	// Prepare the request body
	payload := map[string]string{
		"domain":           domain,
		"timestamp":        timestamp,
		"signed_timestamp": signedTimestamp,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make the token exchange request
	exchangeURL := fmt.Sprintf("%s/v0/auth/%s", c.registryURL, c.authMethod)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, body)
	}

	var tokenResp RegistryTokenResponse
	err = json.Unmarshal(body, &tokenResp)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return tokenResp.RegistryToken, nil
}
