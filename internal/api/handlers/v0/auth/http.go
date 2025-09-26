package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// MaxKeyResponseSize is the maximum size of the response body from the HTTP endpoint.
const MaxKeyResponseSize = 4096

// HTTPTokenExchangeInput represents the input for HTTP-based authentication
type HTTPTokenExchangeInput struct {
	Body struct {
		Domain          string `json:"domain" doc:"Domain name" example:"example.com" required:"true"`
		Timestamp       string `json:"timestamp" doc:"RFC3339 timestamp" example:"2023-01-01T00:00:00Z" required:"true"`
		SignedTimestamp string `json:"signed_timestamp" doc:"Hex-encoded Ed25519 signature of timestamp" example:"abcdef1234567890" required:"true"`
	}
}

// HTTPKeyFetcher defines the interface for fetching HTTP keys
type HTTPKeyFetcher interface {
	FetchKey(ctx context.Context, domain string) (string, error)
}

// DefaultHTTPKeyFetcher uses Go's standard HTTP client
type DefaultHTTPKeyFetcher struct {
	client *http.Client
}

// NewDefaultHTTPKeyFetcher creates a new HTTP key fetcher with timeout
func NewDefaultHTTPKeyFetcher() *DefaultHTTPKeyFetcher {
	return &DefaultHTTPKeyFetcher{
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Disable redirects for security purposes:
			// Prevents people doing weird things like sending us to internal endpoints at different paths
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// NewDefaultHTTPKeyFetcherWithClient creates a new HTTP key fetcher with a custom HTTP client.
// This is primarily useful in tests to inject transports or TLS settings.
func NewDefaultHTTPKeyFetcherWithClient(client *http.Client) *DefaultHTTPKeyFetcher {
	return &DefaultHTTPKeyFetcher{client: client}
}

// FetchKey fetches the public key from the well-known HTTP endpoint
func (f *DefaultHTTPKeyFetcher) FetchKey(ctx context.Context, domain string) (string, error) {
	url := fmt.Sprintf("https://%s/.well-known/mcp-registry-auth", domain)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "text/plain")
	req.Header.Set("User-Agent", "mcp-registry/1.0")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: failed to fetch key from %s", resp.StatusCode, url)
	}

	// Limit response size to prevent DoS attacks.
	// Read up to MaxKeyResponseSize+1 and error if exceeded.
	limited := io.LimitReader(resp.Body, MaxKeyResponseSize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}
	if len(body) > MaxKeyResponseSize {
		return "", fmt.Errorf("HTTP auth key response too large")
	}

	return strings.TrimSpace(string(body)), nil
}

// HTTPAuthHandler handles HTTP-based authentication
type HTTPAuthHandler struct {
	config     *config.Config
	jwtManager *auth.JWTManager
	fetcher    HTTPKeyFetcher
}

// NewHTTPAuthHandler creates a new HTTP authentication handler
func NewHTTPAuthHandler(cfg *config.Config) *HTTPAuthHandler {
	return &HTTPAuthHandler{
		config:     cfg,
		jwtManager: auth.NewJWTManager(cfg),
		fetcher:    NewDefaultHTTPKeyFetcher(),
	}
}

// SetFetcher sets a custom HTTP key fetcher (used for testing)
func (h *HTTPAuthHandler) SetFetcher(fetcher HTTPKeyFetcher) {
	h.fetcher = fetcher
}

// RegisterHTTPEndpoint registers the HTTP authentication endpoint
func RegisterHTTPEndpoint(api huma.API, cfg *config.Config) {
	handler := NewHTTPAuthHandler(cfg)

	// HTTP authentication endpoint
	huma.Register(api, huma.Operation{
		OperationID: "exchange-http-token",
		Method:      http.MethodPost,
		Path:        "/v0/auth/http",
		Summary:     "Exchange HTTP signature for Registry JWT",
		Description: "Authenticate using HTTP-hosted public key and signed timestamp",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, input *HTTPTokenExchangeInput) (*v0.Response[auth.TokenResponse], error) {
		response, err := handler.ExchangeToken(ctx, input.Body.Domain, input.Body.Timestamp, input.Body.SignedTimestamp)
		if err != nil {
			return nil, huma.Error401Unauthorized("HTTP authentication failed", err)
		}

		return &v0.Response[auth.TokenResponse]{
			Body: *response,
		}, nil
	})
}

// ExchangeToken exchanges HTTP signature for a Registry JWT token
func (h *HTTPAuthHandler) ExchangeToken(ctx context.Context, domain, timestamp, signedTimestamp string) (*auth.TokenResponse, error) {
	// Validate domain format
	if !isValidDomain(domain) {
		return nil, fmt.Errorf("invalid domain format")
	}

	// Parse and validate timestamp
	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp format: %w", err)
	}

	// Check timestamp is within 15 seconds
	now := time.Now()
	if ts.Before(now.Add(-15*time.Second)) || ts.After(now.Add(15*time.Second)) {
		return nil, fmt.Errorf("timestamp outside valid window (±15 seconds)")
	}

	// Decode signature
	signature, err := hex.DecodeString(signedTimestamp)
	if err != nil {
		return nil, fmt.Errorf("invalid signature format, must be hex: %w", err)
	}

	if len(signature) != ed25519.SignatureSize {
		return nil, fmt.Errorf("invalid signature length: expected %d, got %d", ed25519.SignatureSize, len(signature))
	}

	// Fetch public key from HTTP endpoint
	keyResponse, err := h.fetcher.FetchKey(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch public key: %w", err)
	}

	// Parse public key from HTTP response
	publicKey, err := h.parsePublicKeyFromHTTP(keyResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	// Verify signature
	messageBytes := []byte(timestamp)
	if !ed25519.Verify(publicKey, messageBytes, signature) {
		return nil, fmt.Errorf("signature verification failed")
	}

	// Build permissions for domain and subdomains
	permissions := h.buildPermissions(domain)

	// Create JWT claims
	jwtClaims := auth.JWTClaims{
		AuthMethod:        auth.MethodHTTP,
		AuthMethodSubject: domain,
		Permissions:       permissions,
	}

	// Generate Registry JWT token
	tokenResponse, err := h.jwtManager.GenerateTokenResponse(ctx, jwtClaims)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JWT token: %w", err)
	}

	return tokenResponse, nil
}

// parsePublicKeyFromHTTP parses Ed25519 public key from HTTP response
func (h *HTTPAuthHandler) parsePublicKeyFromHTTP(response string) (ed25519.PublicKey, error) {
	// Expected format: v=MCPv1; k=ed25519; p=<base64-encoded-key>
	mcpPattern := regexp.MustCompile(`v=MCPv1;\s*k=ed25519;\s*p=([A-Za-z0-9+/=]+)`)

	matches := mcpPattern.FindStringSubmatch(response)
	if len(matches) != 2 {
		return nil, fmt.Errorf("invalid key format, expected: v=MCPv1; k=ed25519; p=<base64-key>")
	}

	// Decode base64 public key
	publicKeyBytes, err := base64.StdEncoding.DecodeString(matches[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 public key: %w", err)
	}

	if len(publicKeyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key length: expected %d, got %d", ed25519.PublicKeySize, len(publicKeyBytes))
	}

	return ed25519.PublicKey(publicKeyBytes), nil
}

// buildPermissions builds permissions for a domain and its subdomains using reverse DNS notation
func (h *HTTPAuthHandler) buildPermissions(domain string) []auth.Permission {
	reverseDomain := reverseString(domain)

	permissions := []auth.Permission{
		// Grant permissions for the exact domain (e.g., com.example/*)
		{
			Action:          auth.PermissionActionPublish,
			ResourcePattern: fmt.Sprintf("%s/*", reverseDomain),
		},
		// HTTP does not imply a hierarchy of ownership of subdomains, unlike DNS
		// Therefore this does not give permissions for subdomains
		// This is consistent with similar protocols, e.g. ACME HTTP-01
	}

	return permissions
}
