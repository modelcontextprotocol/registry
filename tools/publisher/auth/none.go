package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type NoneProvider struct {
	registryURL string
	token       string
}

type TokenResponse struct {
	RegistryToken string `json:"registry_token"`
	ExpiresAt     int64  `json:"expires_at"`
}

func NewNoneProvider(registryURL string) Provider {
	return &NoneProvider{
		registryURL: registryURL,
	}
}

func (p *NoneProvider) GetToken(ctx context.Context) (string, error) {
	if p.token != "" {
		return p.token, nil
	}

	// Get anonymous token from registry
	if !strings.HasSuffix(p.registryURL, "/") {
		p.registryURL += "/"
	}
	tokenURL := p.registryURL + "v0/auth/none"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error getting anonymous token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to get anonymous token (status %d): %s", resp.StatusCode, body)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("error decoding token response: %w", err)
	}

	p.token = tokenResp.RegistryToken
	return p.token, nil
}

func (p *NoneProvider) NeedsLogin() bool {
	return false
}

func (p *NoneProvider) Login(_ context.Context) error {
	// No login needed for anonymous auth
	return nil
}

func (p *NoneProvider) Name() string {
	return "none"
}
