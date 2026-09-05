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

const (
	// defaultPollInterval is the initial device-flow polling interval in
	// seconds, per RFC 8628 §3.5.
	defaultPollInterval = 5
	// maxPollInterval caps how large the polling interval may grow. RFC 8628
	// §3.5 mandates a 5-second increase on each slow_down but leaves the
	// maximum to the implementation; this prevents a misbehaving auth server
	// from growing the interval unboundedly.
	maxPollInterval = 60
	// defaultExpiresIn is the device-code lifetime in seconds used when the
	// device-code response omits expires_in.
	defaultExpiresIn = 900
	// maxExpiresIn caps the device-code lifetime a response may claim. It sits
	// well past GitHub's documented 15 minutes; the cap stops a misbehaving or
	// hostile response from pushing the deadline (and the polling loop) far
	// into the future.
	maxExpiresIn = 3600
	// incorrectDeviceCodeGraceRetries is the total number of extra polls a
	// single login may spend on incorrect_device_code before giving up — a
	// budget for the whole login, not per occurrence. cli/cli#9302 reports the
	// same error for a code that had just been issued; a genuinely invalid code
	// still fails, only a few seconds later.
	incorrectDeviceCodeGraceRetries = 2
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
	// ErrorDescription and ErrorURI accompany Error in GitHub's responses;
	// surfacing them turns opaque failures like "incorrect_device_code" into
	// diagnosable reports.
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorURI         string `json:"error_uri,omitempty"`
}

// errorDetail renders Error together with any error_description and error_uri
// GitHub returned, so failures reach the user diagnosable rather than opaque.
func (r AccessTokenResponse) errorDetail() string {
	detail := r.Error
	if r.ErrorDescription != "" {
		detail += ": " + r.ErrorDescription
	}
	if r.ErrorURI != "" {
		detail += " (" + r.ErrorURI + ")"
	}
	return detail
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

	// accessTokenURL is the GitHub access-token polling endpoint. It is a field
	// (rather than the package constant) so tests can point it at a mock server.
	accessTokenURL string
	// deviceCodeURL is the GitHub device-code endpoint, a field for the same
	// reason as accessTokenURL.
	deviceCodeURL string
	// pollInterval is the initial polling interval in seconds. Defaults to
	// defaultPollInterval; overridable in tests to avoid real delays. Updated
	// from the device-code response's interval when GitHub returns one.
	pollInterval int
	// expiresIn is the device-code lifetime in seconds. Defaults to
	// defaultExpiresIn; updated from the device-code response's expires_in
	// when GitHub returns one.
	expiresIn int
	// sleep abstracts time.Sleep so tests can run without real delays and
	// assert the back-off sequence. Defaults to time.Sleep.
	sleep func(time.Duration)
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
		registryURL:    registryURL,
		providedToken:  token,
		accessTokenURL: GitHubAccessTokenURL,
		deviceCodeURL:  GitHubDeviceCodeURL,
		pollInterval:   defaultPollInterval,
		expiresIn:      defaultExpiresIn,
		sleep:          time.Sleep,
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.deviceCodeURL, bytes.NewBuffer(jsonData))
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

	// Per RFC 8628 §3.2 interval and expires_in are bound to this device code,
	// so reset any pacing left over from a previous code before adopting them.
	// Clamp both: a hostile or buggy response must not grow the interval past
	// maxPollInterval (the invariant maxPollInterval exists for) nor push the
	// deadline unboundedly into the future, and clamping also keeps the
	// derived time.Duration well clear of int64 overflow.
	g.pollInterval = defaultPollInterval
	g.expiresIn = defaultExpiresIn
	if deviceCodeResp.Interval > 0 {
		g.pollInterval = min(deviceCodeResp.Interval, maxPollInterval)
	}
	if deviceCodeResp.ExpiresIn > 0 {
		g.expiresIn = min(deviceCodeResp.ExpiresIn, maxExpiresIn)
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

	// Pacing comes from the device-code response (via requestDeviceCode),
	// falling back to GitHub's documented defaults.
	interval := g.pollInterval
	expiresIn := g.expiresIn
	// Zero-value fallback: providers built directly rather than through
	// requestDeviceCode (the test seam) carry no lifetime. Keep this guard.
	if expiresIn <= 0 {
		expiresIn = defaultExpiresIn
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)

	graceRetries := 0
	// serverErrorInterval grows the wait between consecutive 5xx retries and is
	// reset by any other response.
	serverErrorInterval := interval
	// lastErr holds the most recent retryable failure so a deadline reached
	// mid-retry reports that diagnostic instead of a bare "timed out". It is
	// cleared whenever polling recovers, so a genuine user timeout after a
	// recovered blip still reports plainly.
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.accessTokenURL, bytes.NewBuffer(jsonData))
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

		// A transient 5xx must not abandon a login the user may already have
		// approved in the browser. Each consecutive 5xx grows the wait by 5s up
		// to maxPollInterval, so a sustained outage costs a couple of dozen
		// requests rather than hundreds; the deadline bounds the number of
		// these retries.
		if resp.StatusCode >= http.StatusInternalServerError {
			lastErr = fmt.Errorf("token endpoint returned %d", resp.StatusCode)
			serverErrorInterval = min(serverErrorInterval+5, maxPollInterval)
			g.sleep(time.Duration(serverErrorInterval) * time.Second)
			continue
		}
		serverErrorInterval = interval

		// Anything else outside 200 is not a device-flow answer: surface the
		// status and body rather than failing later on a JSON parse error.
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
		}

		var tokenResp AccessTokenResponse
		err = json.Unmarshal(body, &tokenResp)
		if err != nil {
			return "", err
		}

		// Per RFC 8628 §3.5, both authorization_pending and slow_down indicate
		// the client should keep polling. slow_down additionally requires that
		// the polling interval be increased by 5 seconds.
		if tokenResp.Error == "authorization_pending" || tokenResp.Error == "slow_down" {
			if tokenResp.Error == "slow_down" {
				interval += 5
				if interval > maxPollInterval {
					interval = maxPollInterval
				}
			}
			lastErr = nil
			g.sleep(time.Duration(interval) * time.Second)
			continue
		}

		if tokenResp.Error != "" {
			failure := fmt.Errorf("token request failed: %s", tokenResp.errorDetail())

			// incorrect_device_code has been reported for codes that were in
			// fact valid (cli/cli#9302; cause never confirmed), so spend a
			// couple of grace polls before abandoning a login the user may
			// have approved.
			if tokenResp.Error == "incorrect_device_code" && graceRetries < incorrectDeviceCodeGraceRetries {
				graceRetries++
				lastErr = failure
				g.sleep(time.Duration(interval) * time.Second)
				continue
			}

			return "", failure
		}

		if tokenResp.AccessToken != "" {
			return tokenResp.AccessToken, nil
		}

		// If we reach here, something unexpected happened
		return "", fmt.Errorf("failed to obtain access token")
	}

	if lastErr != nil {
		return "", fmt.Errorf("device code authorization timed out; last response: %w", lastErr)
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
