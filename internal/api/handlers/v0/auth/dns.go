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

// DNSAuthRecord represents a DNS TXT authentication record with optional name pattern
type DNSAuthRecord struct {
	PublicKey   ed25519.PublicKey
	NamePattern string // Defaults to "*" for wildcard access
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

	// Parse auth records from TXT records
	authRecords := h.parseAuthRecordsFromTXT(txtRecords)

	if len(authRecords) == 0 {
		return nil, fmt.Errorf("no valid MCP public keys found in DNS TXT records")
	}

	// Verify signature and collect all valid records
	messageBytes := []byte(timestamp)
	var validRecords []DNSAuthRecord
	for _, record := range authRecords {
		if ed25519.Verify(record.PublicKey, messageBytes, signature) {
			validRecords = append(validRecords, record)
		}
	}

	if len(validRecords) == 0 {
		return nil, fmt.Errorf("signature verification failed")
	}

	// Build permissions from all valid records
	var permissions []auth.Permission
	for _, record := range validRecords {
		permissions = append(permissions, h.buildPermissions(domain, record.NamePattern)...)
	}
	if len(permissions) == 0 {
		return nil, fmt.Errorf("no valid permissions found for the given name pattern")
	}

	// Create JWT claims
	jwtClaims := auth.JWTClaims{
		AuthMethod:        auth.MethodDNS,
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

// parseAuthRecordsFromTXT parses DNS authentication records from TXT records
// Supports optional n=<pattern> parameter for name scoping
func (h *DNSAuthHandler) parseAuthRecordsFromTXT(txtRecords []string) []DNSAuthRecord {
	var authRecords []DNSAuthRecord
	// Updated pattern to capture optional n=<pattern> parameter
	mcpPattern := regexp.MustCompile(`v=MCPv1;\s*k=ed25519;\s*p=([A-Za-z0-9+/=]+)(?:;\s*n=([^;]+))?`)

	for _, record := range txtRecords {
		matches := mcpPattern.FindStringSubmatch(record)
		if len(matches) >= 2 {
			// Decode base64 public key
			publicKeyBytes, err := base64.StdEncoding.DecodeString(matches[1])
			if err != nil {
				continue // Skip invalid keys
			}

			if len(publicKeyBytes) != ed25519.PublicKeySize {
				continue // Skip invalid key sizes
			}

			// Extract name pattern or default to wildcard
			namePattern := "*"
			if len(matches) > 2 && matches[2] != "" {
				namePattern = strings.TrimSpace(matches[2])
			}

			authRecords = append(authRecords, DNSAuthRecord{
				PublicKey:   ed25519.PublicKey(publicKeyBytes),
				NamePattern: namePattern,
			})
		}
	}

	return authRecords
}

// buildPermissions builds permissions based on domain and name pattern
// namePattern defaults to "*" for wildcard access (backward compatible)
func (h *DNSAuthHandler) buildPermissions(domain string, namePattern string) []auth.Permission {
	reverseDomain := reverseString(domain)

	// If namePattern is "*", grant traditional wildcard permissions
	if namePattern == "*" {
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

	// For specific name patterns, grant permission only for the specified pattern
	// This allows DNS controllers to scope permissions to specific prefixes
	// The name pattern MUST be scoped to the domain it is on.
	// We need to ensure proper delimiter checking to prevent prefix attacks
	// e.g., micro.com should not be able to claim com.microsoft/*
	if !strings.HasPrefix(namePattern, reverseDomain) {
		return []auth.Permission{}
	}

	// Check that after the reverse domain, there's either:
	// - nothing (exact match)
	// - a '.' (subdomain like com.example.api)
	// - a '/' (path like com.example/foo)
	if len(namePattern) > len(reverseDomain) {
		delimiter := namePattern[len(reverseDomain)]
		if delimiter != '.' && delimiter != '/' {
			// Invalid pattern - doesn't have proper delimiter after domain
			return []auth.Permission{}
		}
	}

	// Validate server name format: should have exactly one slash
	// This aligns with PR #476 requirements
	slashCount := strings.Count(namePattern, "/")
	if slashCount > 1 {
		// Invalid pattern - multiple slashes not allowed in server names
		return []auth.Permission{}
	}

	permissions := []auth.Permission{
		{
			Action:          auth.PermissionActionPublish,
			ResourcePattern: namePattern,
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
