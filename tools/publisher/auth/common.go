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
	"time"
)

// CryptoProvider provides common functionality for DNS and HTTP authentication
type CryptoProvider struct {
	registryURL string
	domain      string
	hexSeed     string
	authMethod  string
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
