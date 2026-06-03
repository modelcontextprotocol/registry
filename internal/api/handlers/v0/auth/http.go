package auth

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
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
	Body SignatureTokenExchangeInput
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
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = safeDialContext

	return &DefaultHTTPKeyFetcher{
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Disable redirects for security purposes:
			// Prevents people doing weird things like sending us to internal endpoints at different paths
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: transport,
		},
	}
}

// safeDialContext resolves the target hostname and refuses to dial loopback,
// private (RFC1918, ULA), link-local, or unspecified addresses. Combined with
// IsValidDomain rejecting IP literals, this neutralises SSRF abuse of the
// well-known fetcher: an attacker cannot reach internal HTTPS services
// (Kubernetes API server, internal admin panels, internal DNS-resolved hosts)
// even if they control DNS for an attacker domain.
//
// The hostname is resolved once here; we then dial the resolved IP directly,
// which pins the connection against DNS rebinding (a TOCTOU where the resolver
// returns a public IP to a pre-flight check and an internal IP to the actual
// dial). TLS SNI and the Host header continue to use the original hostname
// since they are set by http.Transport from the request URL, not the dial
// address.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	var resolver net.Resolver
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	// Try each non-blocked address in order, falling through on dial failure.
	// Without this, a stale public AAAA record that no longer routes (or any
	// individually-unreachable IP) breaks auth where the default transport
	// would have recovered by trying the next answer.
	//
	// Each attempt is bounded by perIPDialTimeout so that a single hanging
	// address can't consume the whole http.Client budget. This is a
	// simpler substitute for Happy Eyeballs (parallel A/AAAA racing) — we
	// fail fast and try the next answer instead of racing them.
	const perIPDialTimeout = 3 * time.Second

	var lastErr error
	allBlocked := true
	for _, ip := range ips {
		if isBlockedIP(ip.IP) {
			continue
		}
		allBlocked = false
		dialCtx, cancel := context.WithTimeout(ctx, perIPDialTimeout)
		var d net.Dialer
		conn, dialErr := d.DialContext(dialCtx, network, net.JoinHostPort(ip.IP.String(), port))
		cancel()
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if allBlocked {
		return nil, fmt.Errorf("dial %s: refusing to connect to private or loopback address", host)
	}
	return nil, fmt.Errorf("dial %s: all resolved public addresses failed: %w", host, lastErr)
}

// mustCIDR parses a CIDR literal at init; panics on malformed input so a
// typo surfaces at startup rather than letting a nil *IPNet through to the
// blocklist check (which would silently fail-open).
func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(fmt.Sprintf("auth: invalid CIDR %q: %v", s, err))
	}
	return n
}

// cgnatRange covers RFC 6598 Carrier-Grade NAT (100.64.0.0/10), which the
// stdlib does not classify via any Is* helper but is reachable on some
// cloud / mobile networks where it shadows internal infrastructure.
var cgnatRange = mustCIDR("100.64.0.0/10")

// blockedIPv6Prefixes are IPv6 ranges that either embed an arbitrary IPv4
// address (and therefore tunnel into RFC1918 / cloud-metadata space on
// hosts with the corresponding routing) or are routed into site-local
// internal networks. None of these are caught by Go's per-class Is*
// helpers.
//
//	2002::/16       RFC 3056 6to4 — bits 16-47 are an IPv4 address
//	64:ff9b::/96    RFC 6052 NAT64 well-known — low 32 bits are IPv4
//	64:ff9b:1::/48  RFC 8215 NAT64 local-use — same IPv4-embedding shape
//	fec0::/10       RFC 3879 site-local (deprecated, still routed by some
//	                stacks)
var blockedIPv6Prefixes = []*net.IPNet{
	mustCIDR("2002::/16"),
	mustCIDR("64:ff9b::/96"),
	mustCIDR("64:ff9b:1::/48"),
	mustCIDR("fec0::/10"),
}

// isBlockedIP reports whether an IP must not be dialled by the namespace
// verification fetcher. Covers loopback (127/8, ::1), RFC1918 + ULA via
// IsPrivate, link-local (169.254/16, fe80::/10 — includes cloud metadata
// 169.254.169.254), unspecified (0.0.0.0, ::), all multicast (admin-scoped
// 239/8 and ff00::/8 in addition to link-local-multicast), CGNAT, and
// IPv6 prefix families that tunnel to or embed arbitrary IPv4 addresses
// (see blockedIPv6Prefixes). IPv4-mapped IPv6 (::ffff:0:0/96) is handled
// implicitly: the stdlib Is* helpers honour the To4() fast-path, so e.g.
// ::ffff:10.0.0.1 is correctly classified as IsPrivate.
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsMulticast() ||
		ip.IsUnspecified() ||
		cgnatRange.Contains(ip) {
		return true
	}
	for _, p := range blockedIPv6Prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
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
	CoreAuthHandler
	fetcher HTTPKeyFetcher
}

// NewHTTPAuthHandler creates a new HTTP authentication handler
func NewHTTPAuthHandler(cfg *config.Config) *HTTPAuthHandler {
	return &HTTPAuthHandler{
		CoreAuthHandler: *NewCoreAuthHandler(cfg),
		fetcher:         NewDefaultHTTPKeyFetcher(),
	}
}

// SetFetcher sets a custom HTTP key fetcher (used for testing)
func (h *HTTPAuthHandler) SetFetcher(fetcher HTTPKeyFetcher) {
	h.fetcher = fetcher
}

// RegisterHTTPEndpoint registers the HTTP authentication endpoint
func RegisterHTTPEndpoint(api huma.API, pathPrefix string, cfg *config.Config) {
	handler := NewHTTPAuthHandler(cfg)

	// HTTP authentication endpoint
	huma.Register(api, huma.Operation{
		OperationID: "exchange-http-token" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/auth/http",
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
	keyFetcher := func(ctx context.Context, domain string) ([]string, error) {
		keyResponse, err := h.fetcher.FetchKey(ctx, domain)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch public key: %w", err)
		}
		return []string{keyResponse}, nil
	}

	allowSubdomains := false
	return h.CoreAuthHandler.ExchangeToken(ctx, domain, timestamp, signedTimestamp, keyFetcher, allowSubdomains, auth.MethodHTTP)
}
