package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
	}{
		// Blocked — loopback
		{"127.0.0.1", true},
		{"::1", true},
		// Blocked — RFC1918 / ULA (IsPrivate)
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"fc00::1", true},
		// Blocked — link-local (includes cloud metadata 169.254.169.254)
		{"169.254.169.254", true},
		{"fe80::1", true},
		// Blocked — unspecified
		{"0.0.0.0", true},
		{"::", true},
		// Blocked — admin-scoped and broader multicast
		{"239.0.0.1", true},
		{"ff00::1", true},
		// Blocked — Carrier-Grade NAT (RFC 6598)
		{"100.64.0.1", true},
		{"100.127.255.254", true},
		// Blocked — IPv6 6to4 (RFC 3056 2002::/16); bits 16-47 are an
		// arbitrary IPv4 address, so 2002:a9fe:a9fe:: tunnels to
		// 169.254.169.254 and 2002:0a00:0001:: tunnels to 10.0.0.1.
		{"2002:a9fe:a9fe::", true},
		{"2002:0a00:0001::", true},
		{"2002::1", true},
		// Blocked — IPv6 NAT64 well-known prefix (RFC 6052 64:ff9b::/96);
		// low 32 bits embed an IPv4 address.
		{"64:ff9b::a9fe:a9fe", true},
		{"64:ff9b::a00:1", true},
		// Blocked — IPv6 NAT64 local-use prefix (RFC 8215 64:ff9b:1::/48).
		{"64:ff9b:1::1", true},
		{"64:ff9b:1:abcd::", true},
		// Blocked — IPv6 deprecated site-local (RFC 3879 fec0::/10);
		// still routed into internal networks by some stacks.
		{"fec0::1", true},
		{"feff::1", true},
		// Blocked — IPv4-mapped IPv6 (::ffff:0:0/96) inherits the
		// classification of the wrapped IPv4 via To4() fast-path; covered
		// here as an explicit regression guard.
		{"::ffff:127.0.0.1", true},
		{"::ffff:10.0.0.1", true},
		{"::ffff:169.254.169.254", true},
		// Allowed — public
		{"1.1.1.1", false},
		{"8.8.8.8", false},
		{"2606:4700:4700::1111", false},
		{"2001:4860:4860::8888", false},
		// Allowed — just outside the new IPv6 blocks
		{"2001::1", false},    // outside 2002::/16
		{"2003::1", false},    // outside 2002::/16 on the other side
		{"64:ff9c::1", false}, // outside 64:ff9b::/96
		// Allowed — outside CGNAT range
		{"100.63.255.255", false},
		{"100.128.0.1", false},
	}
	for _, tc := range tests {
		t.Run(tc.ip, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("ParseIP(%q) returned nil", tc.ip)
			}
			if got := isBlockedIP(ip); got != tc.blocked {
				t.Errorf("isBlockedIP(%q) = %v, want %v", tc.ip, got, tc.blocked)
			}
		})
	}
}

// Mirrors the constants of the same purpose in the external test package; the
// well-known path and a placeholder domain are all FetchKey needs, since the
// transport dials the local test server regardless of the request hostname.
const (
	internalWellKnownPath = "/.well-known/mcp-registry-auth"
	internalTestDomain    = "example.com"
)

// newLocalTLSTransport builds a transport that reaches srv regardless of the
// hostname in the request URL, so the production client configuration can be
// exercised against a local server.
func newLocalTLSTransport(srv *httptest.Server) *http.Transport {
	dialAddr := srv.Listener.Addr().String()
	return &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // testing only
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := &net.Dialer{}
			return d.DialContext(ctx, network, dialAddr)
		},
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}
}

func newLocalTLSFetcher(srv *httptest.Server) *DefaultHTTPKeyFetcher {
	return &DefaultHTTPKeyFetcher{client: newHTTPKeyFetcherClient(newLocalTLSTransport(srv))}
}

// The published HTTP verification contract (see
// docs/modelcontextprotocol-io/authentication.mdx) tells users the endpoint must
// answer 200 OK directly, so the client must never follow a redirect to another
// host or path.
func TestHTTPKeyFetcherClient_DoesNotFollowRedirects(t *testing.T) {
	var redirectTargetHit bool

	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectTargetHit = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("v=MCPv1; k=ed25519; p=REDIRECTED"))
	}))
	defer target.Close()

	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != internalWellKnownPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.Redirect(w, r, target.URL+internalWellKnownPath, http.StatusMovedPermanently)
	}))
	defer redirector.Close()

	fetcher := newLocalTLSFetcher(redirector)

	_, err := fetcher.FetchKey(context.Background(), internalTestDomain)
	if err == nil {
		t.Fatal("expected the redirect to fail verification, got nil error")
	}
	if !strings.Contains(err.Error(), "HTTP 301") {
		t.Fatalf("got err=%v, want it to report HTTP 301", err)
	}
	if redirectTargetHit {
		t.Error("the redirect target was requested; redirects must not be followed")
	}
}

// Pins the timeout users are told to satisfy and the request headers the
// documented fetch sends.
func TestHTTPKeyFetcherClient_TimeoutAndRequestHeaders(t *testing.T) {
	type observed struct {
		accept    string
		userAgent string
	}
	var got observed

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = observed{accept: r.Header.Get("Accept"), userAgent: r.Header.Get("User-Agent")}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("  v=MCPv1; k=ed25519; p=PUBLIC_KEY\n"))
	}))
	defer srv.Close()

	fetcher := newLocalTLSFetcher(srv)

	if fetcher.client.Timeout != httpKeyFetchTimeout {
		t.Errorf("client timeout = %v, want %v", fetcher.client.Timeout, httpKeyFetchTimeout)
	}

	key, err := fetcher.FetchKey(context.Background(), internalTestDomain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "v=MCPv1; k=ed25519; p=PUBLIC_KEY"; key != want {
		t.Errorf("key = %q, want %q (surrounding whitespace must be trimmed)", key, want)
	}
	if want := "text/plain"; got.accept != want {
		t.Errorf("Accept = %q, want %q", got.accept, want)
	}
	if want := "mcp-registry/1.0"; got.userAgent != want {
		t.Errorf("User-Agent = %q, want %q", got.userAgent, want)
	}
}

// A body above the documented 4096 byte limit must fail even when the server
// answers 200 OK.
func TestHTTPKeyFetcher_RejectsResponseAboveDocumentedLimit(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != internalWellKnownPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("A"), MaxKeyResponseSize+1))
	}))
	defer srv.Close()

	fetcher := newLocalTLSFetcher(srv)

	_, err := fetcher.FetchKey(context.Background(), internalTestDomain)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("got err=%v, want a response-too-large error", err)
	}
}
