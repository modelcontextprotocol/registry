package registries

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// TestNuGetRedirectAllowed covers the redirect SSRF policy directly, including
// the real-base scheme/port branch that the httptest-driven tests below cannot
// exercise (they use http/loopback bases). Mirrors cargo's TestCargoURLAllowed.
func TestNuGetRedirectAllowed(t *testing.T) {
	const prod = model.RegistryURLNuGet // https://api.nuget.org/v3/index.json
	const prodOrigin = "https://api.nuget.org/v3-flatcontainer/x/1.0.0/readme"
	const mockBase = "http://127.0.0.1:54321"
	const mockOrigin = "http://127.0.0.1:54321/x/1.0.0/readme"

	cases := []struct {
		desc    string
		target  string
		origin  string
		baseURL string
		want    bool
	}{
		{"prod: same-host https default", prodOrigin, prodOrigin, prod, true},
		{"prod: same-host explicit :443", "https://api.nuget.org:443/x", prodOrigin, prod, true},
		{"prod: http downgrade refused", "http://api.nuget.org/x", prodOrigin, prod, false},
		{"prod: non-default port refused", "https://api.nuget.org:8080/internal", prodOrigin, prod, false},
		{"prod: cross-host refused", "https://evil.example/x", prodOrigin, prod, false},
		{"prod: metadata IP refused", "https://169.254.169.254/latest/meta-data/", prodOrigin, prod, false},
		{"prod: userinfo trick refused", "https://api.nuget.org@evil.example/x", prodOrigin, prod, false},
		{"test base: same host any scheme/port ok", "http://127.0.0.1:54321/readme", mockOrigin, mockBase, true},
		{"test base: cross host refused", "http://127.0.0.2:54321/x", mockOrigin, mockBase, false},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			target, err := url.Parse(tc.target)
			if err != nil {
				t.Fatalf("parse target %q: %v", tc.target, err)
			}
			origin, err := url.Parse(tc.origin)
			if err != nil {
				t.Fatalf("parse origin %q: %v", tc.origin, err)
			}
			if got := nugetRedirectAllowed(target, origin, tc.baseURL); got != tc.want {
				t.Fatalf("nugetRedirectAllowed(%q, origin=%q, base=%q) = %v, want %v", tc.target, tc.origin, tc.baseURL, got, tc.want)
			}
		})
	}
}

// nugetTestIndex builds a minimal service index whose ReadmeUriTemplate points
// at the given base, so validateReadme fetches the README from a mock server.
func nugetTestIndex(base string) *serviceIndex {
	return &serviceIndex{Resources: []serviceIndexResource{
		{Type: "ReadmeUriTemplate/6.13.0", ID: base + "/{lower_id}/{lower_version}/readme"},
	}}
}

// TestValidateReadme_TruncatesOversizedBody verifies the README read is bounded:
// a token placed past maxNuGetReadmeBytes is truncated away, so it is not found.
func TestValidateReadme_TruncatesOversizedBody(t *testing.T) {
	const serverName = "io.github.test/pkg"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// maxNuGetReadmeBytes of filler first, THEN the token — a correct
		// io.LimitReader stops before the token is reached.
		_, _ = w.Write(bytes.Repeat([]byte("a"), maxNuGetReadmeBytes))
		_, _ = fmt.Fprintf(w, "\nmcp-name: %s\n", serverName)
	}))
	defer srv.Close()

	client := newNuGetHTTPClient(srv.URL)
	state, err := validateReadme(context.Background(), serverName, "pkg", "1.0.0", client, nugetTestIndex(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != InvalidReadme {
		t.Fatalf("token past the %d-byte cap should be truncated -> InvalidReadme, got %v", maxNuGetReadmeBytes, state)
	}
}

// TestValidateReadme_TokenWithinCap is the positive control: a token within the
// cap is found normally.
func TestValidateReadme_TokenWithinCap(t *testing.T) {
	const serverName = "io.github.test/pkg"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "intro text\nmcp-name: %s\n", serverName)
	}))
	defer srv.Close()

	client := newNuGetHTTPClient(srv.URL)
	state, err := validateReadme(context.Background(), serverName, "pkg", "1.0.0", client, nugetTestIndex(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != ValidReadme {
		t.Fatalf("expected ValidReadme, got %v", state)
	}
}

// TestValidateReadme_RefusesCrossHostRedirect verifies the client refuses a
// redirect that leaves the README host (SSRF guard). The redirect target is
// never connected to, because CheckRedirect rejects it first.
func TestValidateReadme_RefusesCrossHostRedirect(t *testing.T) {
	const serverName = "io.github.test/pkg"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 302 to a different host; must NOT be followed.
		http.Redirect(w, r, "http://evil.example/readme", http.StatusFound)
	}))
	defer srv.Close()

	client := newNuGetHTTPClient(srv.URL)
	_, err := validateReadme(context.Background(), serverName, "pkg", "1.0.0", client, nugetTestIndex(srv.URL))
	if err == nil {
		t.Fatal("expected an error: a cross-host redirect should be refused")
	}
	if !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("expected a redirect-refusal error, got: %v", err)
	}
}
