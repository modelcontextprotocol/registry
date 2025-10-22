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
	"strings"
	"time"
)

const (
	oidcIDTokenFilePath       = ".mcpregistry_oidc_id_token"       // #nosec:G101
	oidcRegistryTokenFilePath = ".mcpregistry_oidc_registry_token" // #nosec:G101
)

// OIDCProviderConfig represents the OIDC discovery document
type OIDCProviderConfig struct {
	Issuer                      string `json:"issuer"`
	AuthorizationEndpoint       string `json:"authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	UserinfoEndpoint            string `json:"userinfo_endpoint"`
	JwksURI                     string `json:"jwks_uri"`
}

// OIDCHealthResponse represents the response from the health endpoint for OIDC config
type OIDCHealthResponse struct {
	Status       string `json:"status"`
	OIDCIssuer   string `json:"oidc_issuer,omitempty"`
	OIDCClientID string `json:"oidc_client_id,omitempty"`
}

// OIDCDeviceCodeResponse represents the response from OAuth/OIDC device code endpoints
type OIDCDeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// OIDCAccessTokenResponse represents the response from OAuth/OIDC access token endpoints
type OIDCAccessTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token,omitempty"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error,omitempty"`
}

// OIDCRegistryTokenResponse represents the response from registry's token exchange endpoint
type OIDCRegistryTokenResponse struct {
	RegistryToken string `json:"registry_token"`
	ExpiresAt     int64  `json:"expires_at"`
}

// OIDCStoredRegistryToken represents the registry token with expiration stored locally
type OIDCStoredRegistryToken struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// OIDCProvider implements the Provider interface using OIDC device flow
type OIDCProvider struct {
	clientID    string
	issuer      string
	registryURL string
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

// NeedsLogin appears to be unused, just return true
func (o *OIDCProvider) NeedsLogin() bool {
	return true
}

// Login performs the OIDC device flow authentication
func (o *OIDCProvider) Login(ctx context.Context) error {
	// Get OIDC configuration from health endpoint if not set
	if o.clientID == "" || o.issuer == "" {
		clientID, issuer, err := o.getOIDCConfigFromRegistry(ctx)
		if err != nil {
			return fmt.Errorf("error getting OIDC configuration: %w", err)
		}
		o.clientID = clientID
		o.issuer = issuer
	}

	// Discover OIDC endpoints
	discovery, err := o.getOIDCConfigFromProvider(ctx)
	if err != nil {
		return fmt.Errorf("error discovering OIDC endpoints: %w", err)
	}

	// Use shared device flow implementation
	idToken, err := o.runOIDCDeviceFlow(ctx, discovery, "openid profile email")
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

// getOIDCConfigFromRegistry retrieves issuer and client id from the health endpoint
func (o *OIDCProvider) getOIDCConfigFromRegistry(ctx context.Context) (string, string, error) {
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

	var healthResponse OIDCHealthResponse
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

// getOIDCConfigFromProvider discovers OIDC config from the OIDC provider's /.well-known/openid-configuration document
func (o *OIDCProvider) getOIDCConfigFromProvider(ctx context.Context) (*OIDCProviderConfig, error) {
	if o.issuer == "" {
		return nil, fmt.Errorf("OIDC issuer is required for endpoint discovery")
	}

	discoveryURL, err := url.JoinPath(o.issuer, ".well-known", "openid-configuration")
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

	var discovery OIDCProviderConfig
	err = json.NewDecoder(resp.Body).Decode(&discovery)
	if err != nil {
		return nil, err
	}

	return &discovery, nil
}

// runOIDCDeviceFlow performs a generic OAuth/OIDC device authorization flow
func (o *OIDCProvider) runOIDCDeviceFlow(ctx context.Context, config *OIDCProviderConfig, scope string) (string, error) {
	if o.clientID == "" {
		return "", fmt.Errorf("client ID is required for device flow")
	}

	// Request device code
	formData := url.Values{}
	formData.Set("client_id", o.clientID)
	formData.Set("scope", scope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.DeviceAuthorizationEndpoint, strings.NewReader(formData.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

	var deviceCodeResp OIDCDeviceCodeResponse
	err = json.Unmarshal(body, &deviceCodeResp)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal device code response: %w", err)
	}

	// Display instructions to user

	if deviceCodeResp.VerificationURIComplete != "" {
		_, _ = fmt.Fprintln(os.Stdout, "\nTo authenticate, please go to:")
		_, _ = fmt.Fprintln(os.Stdout, deviceCodeResp.VerificationURIComplete)
	} else {
		_, _ = fmt.Fprintln(os.Stdout, "\nTo authenticate, please:")
		_, _ = fmt.Fprintf(os.Stdout, "1. Go to: %s\n", deviceCodeResp.VerificationURI)
		_, _ = fmt.Fprintf(os.Stdout, "2. Enter code: %s\n", deviceCodeResp.UserCode)
	}
	_, _ = fmt.Fprintln(os.Stdout, "Waiting for authorization...")

	// Poll for token
	tokenFormData := url.Values{}
	tokenFormData.Set("client_id", o.clientID)
	tokenFormData.Set("device_code", deviceCodeResp.DeviceCode)
	tokenFormData.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

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
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.TokenEndpoint, strings.NewReader(tokenFormData.Encode()))
		if err != nil {
			return "", fmt.Errorf("failed to create token request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

		var tokenResp OIDCAccessTokenResponse
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

		if tokenResp.IDToken != "" {
			_, _ = fmt.Fprintln(os.Stdout, "Successfully authenticated!")
			return tokenResp.IDToken, nil
		}

		if tokenResp.AccessToken != "" && tokenResp.IDToken == "" {
			return "", fmt.Errorf("access token received but ID token missing (did you request 'openid' scope?)")
		}

		// If we reach here, something unexpected happened
		return "", fmt.Errorf("failed to obtain access token")
	}

	return "", fmt.Errorf("device code authorization timed out")
}

// saveToken saves the OIDC ID token to a local file
func (o *OIDCProvider) saveToken(token string) error {
	return os.WriteFile(oidcIDTokenFilePath, []byte(token), 0600)
}

// readToken reads the OIDC ID token from a local file
func (o *OIDCProvider) readToken() (string, error) {
	tokenData, err := os.ReadFile(oidcIDTokenFilePath)
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

	var tokenResp OIDCRegistryTokenResponse
	err = json.Unmarshal(body, &tokenResp)
	if err != nil {
		return "", 0, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return tokenResp.RegistryToken, tokenResp.ExpiresAt, nil
}

// saveRegistryToken saves the registry JWT token to a local file with expiration
func (o *OIDCProvider) saveRegistryToken(token string, expiresAt int64) error {
	storedToken := OIDCStoredRegistryToken{
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

	var storedToken OIDCStoredRegistryToken
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
