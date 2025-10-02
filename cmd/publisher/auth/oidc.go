package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	oidcTokenFilePath         = ".mcpregistry_oidc_token"          // #nosec:G101
	oidcRegistryTokenFilePath = ".mcpregistry_oidc_registry_token" // #nosec:G101
)

// OIDCDiscoveryDoc represents the OIDC discovery document
type OIDCDiscoveryDoc struct {
	Issuer                      string `json:"issuer"`
	AuthorizationEndpoint       string `json:"authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	UserinfoEndpoint            string `json:"userinfo_endpoint"`
	JwksURI                     string `json:"jwks_uri"`
}

// OIDCHelathResponse represents the response from the health endpoint for OIDC config
type OIDCHelathResponse struct {
	Status       string `json:"status"`
	OIDCIssuer   string `json:"oidc_issuer,omitempty"`
	OIDCClientID string `json:"oidc_client_id,omitempty"`
}

// OIDCProvider implements the Provider interface using OIDC device flow
type OIDCProvider struct {
	clientID    string
	issuer      string
	registryURL string
	forceLogin  bool
}

// NewOIDCProvider creates a new OIDC provider
func NewOIDCProvider(registryURL string) Provider {
	return &OIDCProvider{
		registryURL: registryURL,
	}
}

// GetToken retrieves the registry JWT token (exchanges OIDC ID token if needed)
func (o *OIDCProvider) GetToken(ctx context.Context) (string, error) {
	// Check if we have a valid registry token
	registryToken, err := o.readRegistryToken()
	if err == nil && registryToken != "" {
		return registryToken, nil
	}

	// If no valid registry token, exchange OIDC token for registry token
	oidcToken, err := o.readToken()
	if err != nil {
		return "", fmt.Errorf("failed to read OIDC token: %w", err)
	}

	// Exchange OIDC token for registry token
	registryToken, expiresAt, err := o.exchangeTokenForRegistry(ctx, oidcToken)
	if err != nil {
		return "", fmt.Errorf("failed to exchange OIDC token: %w", err)
	}

	// Store the registry token
	err = o.saveRegistryToken(registryToken, expiresAt)
	if err != nil {
		return "", fmt.Errorf("failed to save registry token: %w", err)
	}

	return registryToken, nil
}

// NeedsLogin checks if a new login is required
func (o *OIDCProvider) NeedsLogin() bool {
	if o.forceLogin {
		return true
	}

	// Check if OIDC token exists
	_, statErr := os.Stat(oidcTokenFilePath)
	if os.IsNotExist(statErr) {
		return true
	}

	// Check if valid registry token exists
	_, err := o.readRegistryToken()
	if err != nil {
		// No valid registry token, but we have OIDC token
		// We don't need to login, just exchange tokens
		return false
	}

	return false
}

// Login performs the OIDC device flow authentication
func (o *OIDCProvider) Login(ctx context.Context) error {
	// Get OIDC configuration from health endpoint if not set
	if o.clientID == "" || o.issuer == "" {
		clientID, issuer, err := o.getOIDCConfig(ctx)
		if err != nil {
			return fmt.Errorf("error getting OIDC configuration: %w", err)
		}
		o.clientID = clientID
		o.issuer = issuer
	}

	// Discover OIDC endpoints
	discovery, err := o.discoverOIDCEndpoints(ctx)
	if err != nil {
		return fmt.Errorf("error discovering OIDC endpoints: %w", err)
	}

	// Use shared device flow implementation
	idToken, err := runDeviceFlow(ctx, o.clientID, discovery.DeviceAuthorizationEndpoint, discovery.TokenEndpoint, "openid profile email")
	if err != nil {
		return fmt.Errorf("error in OIDC device flow: %w", err)
	}

	// Store the token locally
	err = o.saveToken(idToken)
	if err != nil {
		return fmt.Errorf("error saving OIDC token: %w", err)
	}

	return nil
}

// Name returns the name of this auth provider
func (o *OIDCProvider) Name() string {
	return "oidc"
}

// getOIDCConfig retrieves OIDC configuration from the health endpoint
func (o *OIDCProvider) getOIDCConfig(ctx context.Context) (string, string, error) {
	if o.registryURL == "" {
		return "", "", fmt.Errorf("registry URL is required to get OIDC configuration")
	}

	healthURL := o.registryURL + "/v0/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return "", "", err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("health endpoint returned status %d: %s", resp.StatusCode, body)
	}

	var healthResponse OIDCHelathResponse
	err = json.NewDecoder(resp.Body).Decode(&healthResponse)
	if err != nil {
		return "", "", err
	}

	if healthResponse.OIDCClientID == "" {
		return "", "", fmt.Errorf("OIDC Client ID is not set in the server's health response")
	}

	if healthResponse.OIDCIssuer == "" {
		return "", "", fmt.Errorf("OIDC issuer is not set in the server's health response")
	}

	return healthResponse.OIDCClientID, healthResponse.OIDCIssuer, nil
}

// discoverOIDCEndpoints discovers OIDC endpoints from the discovery document
func (o *OIDCProvider) discoverOIDCEndpoints(ctx context.Context) (*OIDCDiscoveryDoc, error) {
	if o.issuer == "" {
		return nil, fmt.Errorf("OIDC issuer is required for endpoint discovery")
	}

	discoveryURL, err := url.JoinPath(o.issuer, ".well-known", "openid_configuration")
	if err != nil {
		return nil, fmt.Errorf("failed to construct discovery URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discovery endpoint returned status %d: %s", resp.StatusCode, body)
	}

	var discovery OIDCDiscoveryDoc
	err = json.NewDecoder(resp.Body).Decode(&discovery)
	if err != nil {
		return nil, err
	}

	return &discovery, nil
}

// saveToken saves the OIDC ID token to a local file
func (o *OIDCProvider) saveToken(token string) error {
	return os.WriteFile(oidcTokenFilePath, []byte(token), 0600)
}

// readToken reads the OIDC ID token from a local file
func (o *OIDCProvider) readToken() (string, error) {
	tokenData, err := os.ReadFile(oidcTokenFilePath)
	if err != nil {
		return "", err
	}
	return string(tokenData), nil
}

// exchangeTokenForRegistry exchanges an OIDC ID token for a registry JWT token
func (o *OIDCProvider) exchangeTokenForRegistry(ctx context.Context, oidcToken string) (string, int64, error) {
	if o.registryURL == "" {
		return "", 0, fmt.Errorf("registry URL is required for token exchange")
	}

	// Prepare the request body
	payload := map[string]string{
		"oidc_token": oidcToken,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make the token exchange request
	exchangeURL := fmt.Sprintf("%s/v0/auth/oidc", o.registryURL)
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
func (o *OIDCProvider) saveRegistryToken(token string, expiresAt int64) error {
	storedToken := StoredRegistryToken{
		Token:     token,
		ExpiresAt: expiresAt,
	}

	data, err := json.Marshal(storedToken)
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	return os.WriteFile(oidcRegistryTokenFilePath, data, 0600)
}

// readRegistryToken reads the registry JWT token from a local file
func (o *OIDCProvider) readRegistryToken() (string, error) {
	data, err := os.ReadFile(oidcRegistryTokenFilePath)
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
		os.Remove(oidcRegistryTokenFilePath)
		return "", fmt.Errorf("registry token has expired")
	}

	return storedToken.Token, nil
}
