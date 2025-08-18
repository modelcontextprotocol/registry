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

type DNSProvider struct {
	registryURL string
	domain      string
	hexSeed     string
}

//nolint:ireturn
func NewDNSProvider(registryURL, domain, hexSeed string) Provider {
	return &DNSProvider{
		registryURL: registryURL,
		domain:      domain,
		hexSeed:     hexSeed,
	}
}

// GetToken retrieves the registry JWT token using DNS authentication
func (d *DNSProvider) GetToken(ctx context.Context) (string, error) {
	if d.domain == "" {
		return "", fmt.Errorf("DNS domain is required")
	}

	if d.hexSeed == "" {
		return "", fmt.Errorf("DNS private key (hex seed) is required")
	}

	// Decode hex seed to private key
	seedBytes, err := hex.DecodeString(d.hexSeed)
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
	registryToken, err := d.exchangeDNSTokenForRegistry(ctx, d.domain, timestamp, signedTimestamp)
	if err != nil {
		return "", fmt.Errorf("failed to exchange DNS signature: %w", err)
	}

	return registryToken, nil
}

// NeedsLogin always returns false for DNS since no interactive login is needed
func (d *DNSProvider) NeedsLogin() bool {
	return false
}

// Login is not needed for DNS since authentication is cryptographic
func (d *DNSProvider) Login(_ context.Context) error {
	return nil
}

// Name returns the name of this auth provider
func (d *DNSProvider) Name() string {
	return "dns"
}

// exchangeDNSTokenForRegistry exchanges DNS signature for a registry JWT token
func (d *DNSProvider) exchangeDNSTokenForRegistry(ctx context.Context, domain, timestamp, signedTimestamp string) (string, error) {
	if d.registryURL == "" {
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
	exchangeURL := d.registryURL + "/v0/auth/dns"
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
