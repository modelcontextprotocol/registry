package registries

import (
	"net/url"
	"testing"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// TestCargoURLAllowed covers the SSRF allow-check: for the real crates.io base,
// the host must be allow-listed AND the scheme/port must be https/default; for a
// test (httptest) base only the host is checked so mocks keep working.
func TestCargoURLAllowed(t *testing.T) {
	prodHosts := cargoAllowedHosts(model.RegistryURLCrates) // {crates.io, static.crates.io}
	mockBase := "http://127.0.0.1:54321"
	mockHosts := cargoAllowedHosts(mockBase) // {127.0.0.1}

	cases := []struct {
		desc    string
		raw     string
		baseURL string
		hosts   map[string]struct{}
		want    bool
	}{
		{"prod: https static.crates.io", "https://static.crates.io/readmes/x/x.html", model.RegistryURLCrates, prodHosts, true},
		{"prod: https crates.io", "https://crates.io/api/v1/crates/x/1.0.0", model.RegistryURLCrates, prodHosts, true},
		{"prod: http downgrade rejected", "http://static.crates.io/x", model.RegistryURLCrates, prodHosts, false},
		{"prod: non-default port rejected", "https://static.crates.io:8443/x", model.RegistryURLCrates, prodHosts, false},
		{"prod: explicit 443 ok", "https://static.crates.io:443/x", model.RegistryURLCrates, prodHosts, true},
		{"prod: foreign host rejected", "https://evil.example/x", model.RegistryURLCrates, prodHosts, false},
		{"prod: userinfo host is evil rejected", "https://static.crates.io@evil.example/x", model.RegistryURLCrates, prodHosts, false},
		{"test base: mock host any scheme/port ok", "http://127.0.0.1:54321/readme-static/x", mockBase, mockHosts, true},
		{"test base: foreign host rejected", "http://127.0.0.2:54321/x", mockBase, mockHosts, false},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			u, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.raw, err)
			}
			if got := cargoURLAllowed(u, tc.baseURL, tc.hosts); got != tc.want {
				t.Fatalf("cargoURLAllowed(%q, base=%q) = %v, want %v", tc.raw, tc.baseURL, got, tc.want)
			}
		})
	}
}
