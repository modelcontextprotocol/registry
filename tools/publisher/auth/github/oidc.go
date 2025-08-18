package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type OIDCProvider struct {
	registryURL string
}

func NewOIDCProvider(registryURL string) *OIDCProvider {
	return &OIDCProvider{
		registryURL: registryURL,
	}
}

// GetToken retrieves the registry JWT token using GitHub Actions OIDC token
func (o *OIDCProvider) GetToken(ctx context.Context) (string, error) {
	// Get OIDC token from environment variable set by GitHub Actions
	oidcToken := os.Getenv("ACTIONS_ID_TOKEN")
	if oidcToken == "" {
		return "", fmt.Errorf("ACTIONS_ID_TOKEN environment variable not found - are you running in GitHub Actions with id-token: write permissions?")
	}

	// Exchange OIDC token for registry token
	registryToken, err := o.exchangeOIDCTokenForRegistry(ctx, oidcToken)
	if err != nil {
		return "", fmt.Errorf("failed to exchange OIDC token: %w", err)
	}

	return registryToken, nil
}

// NeedsLogin always returns false for OIDC since the token is provided by GitHub Actions
func (o *OIDCProvider) NeedsLogin() bool {
	// OIDC tokens are provided by GitHub Actions runtime, no interactive login needed
	return false
}

// Login is not needed for OIDC since tokens are provided by GitHub Actions
func (o *OIDCProvider) Login(_ context.Context) error {
	// No interactive login needed for OIDC
	return nil
}

// Name returns the name of this auth provider
func (o *OIDCProvider) Name() string {
	return "github-oidc"
}

// exchangeOIDCTokenForRegistry exchanges a GitHub OIDC token for a registry JWT token
func (o *OIDCProvider) exchangeOIDCTokenForRegistry(ctx context.Context, oidcToken string) (string, error) {
	if o.registryURL == "" {
		return "", fmt.Errorf("registry URL is required for token exchange")
	}

	// Prepare the request body
	payload := map[string]string{
		"oidc_token": oidcToken,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make the token exchange request
	exchangeURL := o.registryURL + "/v0/auth/github-oidc"
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
