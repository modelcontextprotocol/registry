package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

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

// commonWrongSelectors lists subdomain prefixes that users frequently mistake for the
// MCP DNS auth record location (DKIM-style intuition). MCP DNS auth uses the apex,
// like SPF — see #385, #1103, #1126 for the recurring confusion.
var commonWrongSelectors = []string{"_mcp-auth", "_mcp-registry"}

// ExchangeToken exchanges DNS signature for a Registry JWT token
func (h *DNSAuthHandler) ExchangeToken(ctx context.Context, domain, timestamp, signedTimestamp string) (*auth.TokenResponse, error) {
	keyFetcher := func(ctx context.Context, domain string) ([]string, error) {
		// Apply a timeout to DNS lookup to prevent resource exhaustion from slow/malicious DNS servers
		timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		// Lookup DNS TXT records
		// DNS implies a hierarchy where subdomains are treated as part of the parent domain,
		// therefore we grant permissions for all subdomains (e.g., com.example.*)
		// This is in line with other DNS-based authentication methods e.g. ACME DNS-01 challenges
		txtRecords, err := h.resolver.LookupTXT(timeoutCtx, domain)
		if err != nil {
			return nil, fmt.Errorf("failed to lookup DNS TXT records: %w", err)
		}

		if !hasMCPRecord(txtRecords) {
			if found := h.findMisplacedSelector(timeoutCtx, domain); found != "" {
				return nil, fmt.Errorf(
					"no MCPv1 TXT record at %q, but one was found at %q — "+
						"MCP DNS auth requires the record at the apex domain, not under a selector",
					domain, found,
				)
			}
		}

		return txtRecords, nil
	}

	allowSubdomains := true
	return h.CoreAuthHandler.ExchangeToken(ctx, domain, timestamp, signedTimestamp, keyFetcher, allowSubdomains, auth.MethodDNS)
}

// hasMCPRecord reports whether any of the supplied TXT records contains a well-formed
// MCPv1 proof record. Uses the same strict pattern as the parser so a malformed
// "v=MCPv1" string at the apex doesn't suppress the misplaced-selector probe.
func hasMCPRecord(records []string) bool {
	for _, r := range records {
		if MCPProofRecordPattern.MatchString(r) {
			return true
		}
	}
	return false
}

// findMisplacedSelector probes a small fixed set of common wrong selectors and returns the
// first one that holds an MCPv1 record, or "" if none do. Lookups run in parallel with a
// short individual timeout so a slow/missing zone never delays the response by much.
func (h *DNSAuthHandler) findMisplacedSelector(ctx context.Context, domain string) string {
	type result struct {
		name  string
		found bool
	}
	results := make(chan result, len(commonWrongSelectors))
	for _, selector := range commonWrongSelectors {
		name := selector + "." + domain
		go func(name string) {
			lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			records, err := h.resolver.LookupTXT(lookupCtx, name)
			if err != nil {
				results <- result{name: name, found: false}
				return
			}
			results <- result{name: name, found: hasMCPRecord(records)}
		}(name)
	}
	for range commonWrongSelectors {
		r := <-results
		if r.found {
			return r.name
		}
	}
	return ""
}
