package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
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

	return &DefaultHTTPKeyFetcher{client: newHTTPKeyFetcherClient(transport)}
}

// httpKeyFetchTimeout bounds a single well-known fetch attempt, including DNS
// resolution and the TLS handshake. It is part of the documented verification
// contract: an endpoint that cannot answer within this budget fails that
// attempt.
const httpKeyFetchTimeout = 10 * time.Second

// httpKeyFetchOverallTimeout includes both attempts, response-body reads and
// backoff. Keep the first attempt's existing budget without doubling the time
// an unauthenticated request can hold resources when an endpoint stalls.
const httpKeyFetchOverallTimeout = 15 * time.Second

// httpKeyFetchAttempts is the total number of attempts FetchKey makes before
// giving up. Transient unreachability of a verification endpoint — a dial
// timeout, a reset connection, a CDN edge answering 5xx — used to fail the
// whole HTTP login on a single attempt (#1625), even though the endpoint
// answers moments later. Permanent failures (client errors, redirects,
// oversized bodies) are never retried and still fail on the first attempt.
const httpKeyFetchAttempts = 2

// httpKeyFetchRetryBackoff is the delay between fetch attempts.
const httpKeyFetchRetryBackoff = 500 * time.Millisecond

var errHTTPKeyAddressBlocked = errors.New("refusing to connect to private or loopback address")

// newHTTPKeyFetcherClient builds the HTTP client used to fetch a domain's
// well-known verification key. The transport is a parameter so that tests can
// exercise the production timeout and redirect policy against a local server.
func newHTTPKeyFetcherClient(transport http.RoundTripper) *http.Client {
	return &http.Client{
		Timeout: httpKeyFetchTimeout,
		// Disable redirects for security purposes:
		// Prevents people doing weird things like sending us to internal endpoints at different paths
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: transport,
	}
}

type ipAddrResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

var httpKeyDNSResolver ipAddrResolver = net.DefaultResolver

// safeDialContext resolves the target hostname and refuses to dial loopback,
// private (RFC1918, ULA), link-local, special-purpose, cluster-internal, or
// unspecified addresses. Combined with IsValidDomain rejecting IP literals and
// internal TLDs, this neutralises SSRF abuse of the well-known fetcher: an
// attacker cannot reach internal HTTPS services (Kubernetes API server,
// internal admin panels, internal DNS-resolved hosts) even if they control DNS
// for an attacker domain.
//
// Lookups append a trailing root dot (e.g. "example.com.") so Go's resolver
// issues a single rooted query instead of expanding Kubernetes ndots:5 search
// domains (.svc.cluster.local, .google.internal), while the HTTP request URL
// and TLS SNI ServerName retain the dot-free domain per RFC 6066.
//
// The hostname is resolved once here; we then dial the resolved IP directly,
// which pins the connection against DNS rebinding (a TOCTOU where the resolver
// returns a public IP to a pre-flight check and an internal IP to the actual
// dial).
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	var ips []net.IPAddr
	if parsedIP := net.ParseIP(host); parsedIP != nil {
		ips = []net.IPAddr{{IP: parsedIP}}
	} else {
		fqdn := strings.TrimSuffix(host, ".") + "."
		ips, err = httpKeyDNSResolver.LookupIPAddr(ctx, fqdn)
		if err != nil {
			return nil, err
		}
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
		return nil, fmt.Errorf("dial %s: %w", host, errHTTPKeyAddressBlocked)
	}
	return nil, fmt.Errorf("dial %s: all resolved public addresses failed: %w", host, lastErr)
}

// blockedIPv4Prefixes covers special-purpose and cluster-internal IPv4 ranges
// that Go's stdlib Is* helpers do not classify as private or loopback:
//
//	0.0.0.0/8        RFC 1122 "this host on this network" (routes to loopback on Linux/macOS)
//	100.64.0.0/10    RFC 6598 Carrier-Grade NAT
//	192.0.0.0/24     RFC 6890 IETF protocol assignments
//	198.18.0.0/15    RFC 2544 benchmarking (frequently used by service meshes/VPCs)
//	240.0.0.0/4      RFC 1112 Class E reserved + 255.255.255.255 limited broadcast
//	34.118.224.0/20  GKE default privately-used public IP Service CIDR
var blockedIPv4Prefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("34.118.224.0/20"),
}

// blockedIPv6Prefixes are IPv6 ranges that either embed an arbitrary IPv4
// address (and therefore tunnel into RFC1918 / cloud-metadata space on
// hosts with the corresponding routing), are discard-only, or are routed into
// site-local internal networks. None of these are caught by Go's per-class Is*
// helpers.
//
//	2002::/16         RFC 3056 6to4 — bits 16-47 are an IPv4 address
//	2001::/32         RFC 4380 Teredo — low 32 bits are an obfuscated IPv4 address
//	64:ff9b::/96      RFC 6052 NAT64 well-known — low 32 bits are IPv4
//	64:ff9b:1::/48    RFC 8215 NAT64 local-use — same IPv4-embedding shape
//	::/96             RFC 4291 IPv4-compatible (deprecated) — low 32 bits are IPv4
//	                  (:: and ::1 also fall in ::/96 and remain blocked via
//	                  IsUnspecified / IsLoopback)
//	::ffff:0:0:0/96   RFC 2765 SIIT IPv4-translated — low 32 bits are IPv4
//	100::/64          RFC 6666 discard-only address block
//	fec0::/10         RFC 3879 site-local (deprecated, still routed by some stacks)
var blockedIPv6Prefixes = []netip.Prefix{
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("::ffff:0:0:0/96"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("fec0::/10"),
}

// isBlockedIP reports whether an IP must not be dialled by the namespace
// verification fetcher. Covers loopback (127/8, ::1), RFC1918 + ULA via
// IsPrivate, link-local (169.254/16, fe80::/10 — includes cloud metadata
// 169.254.169.254), unspecified (0.0.0.0, ::), all multicast, special-purpose
// IPv4/IPv6 blocks, GKE's default Service CIDR (34.118.224.0/20), and
// KUBERNETES_SERVICE_HOST when set. IPv4-mapped IPv6 (::ffff:0:0/96) is
// unmapped so all IPv4 checks apply consistently.
func isBlockedIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok || !addr.IsValid() || addr.Zone() != "" {
		return true
	}
	if addr.Is6() && !addr.Is4In6() {
		for _, p := range blockedIPv6Prefixes {
			if p.Contains(addr) {
				return true
			}
		}
	}
	unmapped := addr.Unmap()
	if unmapped.IsLoopback() || unmapped.IsPrivate() ||
		unmapped.IsLinkLocalUnicast() || unmapped.IsLinkLocalMulticast() ||
		unmapped.IsMulticast() || unmapped.IsUnspecified() {
		return true
	}
	if unmapped.Is4() {
		for _, p := range blockedIPv4Prefixes {
			if p.Contains(unmapped) {
				return true
			}
		}
	}
	if k8sHost := os.Getenv("KUBERNETES_SERVICE_HOST"); k8sHost != "" {
		if k8sAddr, err := netip.ParseAddr(k8sHost); err == nil && unmapped == k8sAddr.Unmap() {
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

// FetchKey fetches the public key from the well-known HTTP endpoint. Transient
// failures are retried up to httpKeyFetchAttempts total attempts with
// httpKeyFetchRetryBackoff between them; permanent failures (non-transient
// status responses, oversized bodies) fail immediately. If every attempt
// fails, the returned error reports how many attempts were made so fetch
// problems can be distinguished from key problems (#1625).
func (f *DefaultHTTPKeyFetcher) FetchKey(ctx context.Context, domain string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, httpKeyFetchOverallTimeout)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= httpKeyFetchAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				// The caller is gone; surface the cancellation alongside the
				// last fetch failure.
				return "", fmt.Errorf("%w (retry stopped: %w)", lastErr, ctx.Err())
			case <-time.After(httpKeyFetchRetryBackoff):
			}
		}
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return "", fmt.Errorf("%w (retry stopped: %w)", lastErr, err)
			}
			return "", fmt.Errorf("failed to fetch key: %w", err)
		}

		key, retryable, err := f.fetchKeyOnce(ctx, domain)
		if err == nil {
			return key, nil
		}
		lastErr = err
		if !retryable {
			if attempt > 1 {
				return "", fmt.Errorf("%w (after %d attempts)", err, attempt)
			}
			return "", err
		}
	}
	return "", fmt.Errorf("%w (after %d attempts)", lastErr, httpKeyFetchAttempts)
}

// fetchKeyOnce makes a single attempt at the well-known fetch. The retryable
// result reports whether the failure is transient — worth another attempt —
// or permanent.
func (f *DefaultHTTPKeyFetcher) fetchKeyOnce(ctx context.Context, domain string) (string, bool, error) {
	url := fmt.Sprintf("https://%s/.well-known/mcp-registry-auth", domain)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", false, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "text/plain")
	req.Header.Set("User-Agent", "mcp-registry/1.0")

	resp, err := f.client.Do(req)
	if err != nil {
		// Retry connection failures while there is time left, but fail fast on
		// certificate verification, blocked addresses and nonexistent domains.
		return "", ctx.Err() == nil && isRetryableHTTPFetchError(err), fmt.Errorf("failed to fetch key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", isTransientHTTPStatus(resp.StatusCode), fmt.Errorf("HTTP %d: failed to fetch key from %s", resp.StatusCode, url)
	}

	// Limit response size to prevent DoS attacks.
	// Read up to MaxKeyResponseSize+1 and error if exceeded.
	limited := io.LimitReader(resp.Body, MaxKeyResponseSize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", ctx.Err() == nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if len(body) > MaxKeyResponseSize {
		return "", false, fmt.Errorf("HTTP auth key response too large")
	}

	// A 200 with an empty body is not a key. Without this check, an endpoint or
	// edge that answers 200 with zero bytes would be reported as a successful
	// fetch of "", skipping the retry and surfacing later as the confusing
	// "no valid MCP public key found" instead of what actually happened.
	//
	// This is the empty-body case only: a body cut off mid-read (including by
	// the client timeout) already comes back as a read error from Do/ReadAll,
	// not as an empty string, so it is not what this branch catches.
	//
	// Treated as transient: an empty response is usually a deploy or edge
	// glitch, and the second attempt normally carries the key.
	key := strings.TrimSpace(string(body))
	if key == "" {
		return "", true, fmt.Errorf("HTTP auth key response was empty")
	}

	return key, false, nil
}

func isRetryableHTTPFetchError(err error) bool {
	var certificateErr *tls.CertificateVerificationError
	var dnsErr *net.DNSError
	return !errors.Is(err, errHTTPKeyAddressBlocked) &&
		!errors.As(err, &certificateErr) &&
		(!errors.As(err, &dnsErr) || !dnsErr.IsNotFound)
}

// isTransientHTTPStatus reports whether an unsuccessful response status
// describes a momentary condition worth retrying: request timeouts, rate
// limits, and server-side errors typically clear on their own (overloaded CDN
// edge, deploy in progress). Other 4xx and 3xx responses describe the request
// itself and will not change on retry — redirects are pinned to fail, see
// newHTTPKeyFetcherClient.
func isTransientHTTPStatus(status int) bool {
	return status == http.StatusRequestTimeout ||
		status == http.StatusTooManyRequests ||
		(status >= 500 && status < 600)
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
		Tags:        []string{tagAuth},
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
