package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/model"
)

// DNSTokenExchangeInput represents the input for DNS-based authentication
type DNSTokenExchangeInput struct {
	Body struct {
		Domain          string `json:"domain" doc:"Domain name" example:"example.com" required:"true"`
		Timestamp       string `json:"timestamp" doc:"RFC3339 timestamp" example:"2023-01-01T00:00:00Z" required:"true"`
		SignedTimestamp string `json:"signed_timestamp" doc:"Hex-encoded Ed25519 signature of timestamp" example:"abcdef1234567890" required:"true"`
	}
}

// DNSResolver defines the interface for DNS resolution
type DNSResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// DefaultDNSResolver uses Go's standard DNS resolution
type DefaultDNSResolver struct{}

// LookupTXT performs DNS TXT record lookup
func (r *DefaultDNSResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return (&net.Resolver{}).LookupTXT(ctx, name)
}

// DNSAuthHandler handles DNS-based authentication
type DNSAuthHandler struct {
	config     *config.Config
	jwtManager *auth.JWTManager
	resolver   DNSResolver
}

// NewDNSAuthHandler creates a new DNS authentication handler
func NewDNSAuthHandler(cfg *config.Config) *DNSAuthHandler {
	return &DNSAuthHandler{
		config:     cfg,
		jwtManager: auth.NewJWTManager(cfg),
		resolver:   &DefaultDNSResolver{},
	}
}

// SetResolver sets a custom DNS resolver (used for testing)
func (h *DNSAuthHandler) SetResolver(resolver DNSResolver) {
	h.resolver = resolver
}

// RegisterDNSEndpoint registers the DNS authentication endpoint
func RegisterDNSEndpoint(api huma.API, cfg *config.Config) {
	handler := NewDNSAuthHandler(cfg)

	// DNS authentication endpoint
	huma.Register(api, huma.Operation{
		OperationID: "exchange-dns-token",
		Method:      http.MethodPost,
		Path:        "/v0/auth/dns",
		Summary:     "Exchange DNS signature for Registry JWT",
		Description: "Authenticate using DNS TXT record public key and signed timestamp",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, input *DNSTokenExchangeInput) (*v0.Response[auth.TokenResponse], error) {
		response, err := handler.ExchangeToken(ctx, input.Body.Domain, input.Body.Timestamp, input.Body.SignedTimestamp)
		if err != nil {
			return nil, huma.Error401Unauthorized("DNS authentication failed", err)
		}

		return &v0.Response[auth.TokenResponse]{
			Body: *response,
		}, nil
	})
}

// ExchangeToken exchanges DNS signature for a Registry JWT token
func (h *DNSAuthHandler) ExchangeToken(ctx context.Context, domain, timestamp, signedTimestamp string) (*auth.TokenResponse, error) {
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

	// Lookup DNS TXT records
	txtRecords, err := h.resolver.LookupTXT(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup DNS TXT records: %w", err)
	}

	// Parse public keys from TXT records
	publicKeys := h.parsePublicKeysFromTXT(txtRecords)

	if len(publicKeys) == 0 {
		return nil, fmt.Errorf("no valid MCP public keys found in DNS TXT records")
	}

	// Verify signature with any of the public keys
	messageBytes := []byte(timestamp)
	signatureValid := false
	for _, publicKey := range publicKeys {
		if ed25519.Verify(publicKey, messageBytes, signature) {
			signatureValid = true
			break
		}
	}

	if !signatureValid {
		return nil, fmt.Errorf("signature verification failed")
	}

	// Build permissions for domain and subdomains
	permissions := h.buildPermissions(domain)

	// Create JWT claims
	jwtClaims := auth.JWTClaims{
		AuthMethod:        model.AuthMethodDNS,
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

// parsePublicKeysFromTXT parses Ed25519 public keys from DNS TXT records
func (h *DNSAuthHandler) parsePublicKeysFromTXT(txtRecords []string) []ed25519.PublicKey {
	var publicKeys []ed25519.PublicKey
	mcpPattern := regexp.MustCompile(`v=MCPv1;\s*k=ed25519;\s*p=([A-Za-z0-9+/=]+)`)

	for _, record := range txtRecords {
		matches := mcpPattern.FindStringSubmatch(record)
		if len(matches) == 2 {
			// Decode base64 public key
			publicKeyBytes, err := base64.StdEncoding.DecodeString(matches[1])
			if err != nil {
				continue // Skip invalid keys
			}

			if len(publicKeyBytes) != ed25519.PublicKeySize {
				continue // Skip invalid key sizes
			}

			publicKeys = append(publicKeys, ed25519.PublicKey(publicKeyBytes))
		}
	}

	return publicKeys
}

// buildPermissions builds permissions for a domain and its subdomains using reverse DNS notation
func (h *DNSAuthHandler) buildPermissions(domain string) []auth.Permission {
	reverseDomain := reverseString(domain)

	permissions := []auth.Permission{
		// Grant permissions for the exact domain (e.g., com.example/*)
		{
			Action:          auth.PermissionActionPublish,
			ResourcePattern: fmt.Sprintf("%s/*", reverseDomain),
		},
		// DNS implies a hierarchy where subdomains are treated as part of the parent domain,
		// therefore we grant permissions for all subdomains (e.g., com.example.*)
		// This is in line with other DNS-based authentication methods e.g. ACME DNS-01 challenges
		{
			Action:          auth.PermissionActionPublish,
			ResourcePattern: fmt.Sprintf("%s.*", reverseDomain),
		},
	}

	return permissions
}

// reverseString reverses a domain string (example.com -> com.example)
func reverseString(domain string) string {
	parts := strings.Split(domain, ".")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, ".")
}

func isValidDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}

	// Check for valid characters and structure
	domainPattern := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$`)
	return domainPattern.MatchString(domain)
}
