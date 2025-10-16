package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// DNSTokenExchangeInput represents the input for DNS-based authentication
type DNSTokenExchangeInput struct {
	Body SignatureTokenExchangeInput
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
	CoreAuthHandler
	resolver DNSResolver
}

// NewDNSAuthHandler creates a new DNS authentication handler
func NewDNSAuthHandler(cfg *config.Config) *DNSAuthHandler {
	return &DNSAuthHandler{
		CoreAuthHandler: *NewCoreAuthHandler(cfg),
		resolver:        &DefaultDNSResolver{},
	}
}

// SetResolver sets a custom DNS resolver (used for testing)
func (h *DNSAuthHandler) SetResolver(resolver DNSResolver) {
	h.resolver = resolver
}

// RegisterDNSEndpoint registers the DNS authentication endpoint
func RegisterDNSEndpoint(api huma.API, pathPrefix string, cfg *config.Config) {
	handler := NewDNSAuthHandler(cfg)

	// DNS authentication endpoint
	huma.Register(api, huma.Operation{
		OperationID: "exchange-dns-token" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/auth/dns",
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
	keyFetcher := func(ctx context.Context, domain string) ([]string, error) {
		// Lookup DNS TXT records
		// DNS implies a hierarchy where subdomains are treated as part of the parent domain,
		// therefore we grant permissions for all subdomains (e.g., com.example.*)
		// This is in line with other DNS-based authentication methods e.g. ACME DNS-01 challenges
		txtRecords, err := h.resolver.LookupTXT(ctx, domain)
		if err != nil {
			return nil, fmt.Errorf("failed to lookup DNS TXT records: %w", err)
		}
		return txtRecords, nil
	}

	allowSubdomains := true
	return h.CoreAuthHandler.ExchangeToken(ctx, domain, timestamp, signedTimestamp, keyFetcher, allowSubdomains, auth.MethodDNS)
}
