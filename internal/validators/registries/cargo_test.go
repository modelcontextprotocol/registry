package registries_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateCargo_RealPackages(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		packageName  string
		version      string
		serverName   string
		expectError  bool
		errorMessage string
	}{
		{
			name:         "empty package identifier should fail",
			packageName:  "",
			version:      "0.1.0",
			serverName:   "io.github.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for Cargo packages",
		},
		{
			name:         "empty package version should fail",
			packageName:  "rust-faf-mcp",
			version:      "",
			serverName:   "io.github.example/test",
			expectError:  true,
			errorMessage: "package version is required for Cargo packages",
		},
		{
			name:         "non-existent crate should fail",
			packageName:  generateRandomPackageName(),
			version:      "0.1.0",
			serverName:   "io.github.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "non-existent version of real crate should fail",
			packageName:  "serde",
			version:      "99.99.99-not-real",
			serverName:   "io.github.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "real crate without mcp-name token should fail",
			packageName:  "serde", // most-downloaded crate; no MCP server claim
			version:      "1.0.219",
			serverName:   "io.github.example/test",
			expectError:  true,
			errorMessage: "ownership validation failed",
		},
		{
			name:         "real crate with mismatched mcp-name should fail",
			packageName:  "tokio",
			version:      "1.40.0",
			serverName:   "io.github.example/completely-different-name",
			expectError:  true,
			errorMessage: "ownership validation failed",
		},
		{
			name:         "additional real crate without mcp-name (rand)",
			packageName:  "rand",
			version:      "0.9.0",
			serverName:   "io.github.example/test",
			expectError:  true,
			errorMessage: "ownership validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeCargo,
				Identifier:   tt.packageName,
				Version:      tt.version,
			}

			err := registries.ValidateCargo(ctx, pkg, tt.serverName)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateCargo_RegistryBaseURLMismatch(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		baseURL string
	}{
		{name: "different host", baseURL: "https://example.com"},
		{name: "trailing slash", baseURL: "https://crates.io/"},
		{name: "http (not https)", baseURL: "http://crates.io"},
		{name: "subdomain", baseURL: "https://www.crates.io"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType:    model.RegistryTypeCargo,
				RegistryBaseURL: tt.baseURL,
				Identifier:      "rust-faf-mcp",
				Version:         "0.2.2",
			}

			err := registries.ValidateCargo(ctx, pkg, "io.github.Wolfe-Jam/rust-faf-mcp")
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "registry type and base URL do not match")
		})
	}
}

func TestValidateCargo_RejectsMCPBOnlyFields(t *testing.T) {
	ctx := context.Background()

	pkg := model.Package{
		RegistryType: model.RegistryTypeCargo,
		Identifier:   "rust-faf-mcp",
		Version:      "0.2.2",
		FileSHA256:   "0000000000000000000000000000000000000000000000000000000000000000",
	}

	err := registries.ValidateCargo(ctx, pkg, "io.github.Wolfe-Jam/rust-faf-mcp")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cargo packages must not have 'fileSha256' field")
}

// Server names follow io.github.OWNER/REPO and may contain dots, slashes,
// hyphens, underscores, and digits. None of these get HTML-escaped during
// README rendering, so substring match against the rendered HTML is reliable.
// These tests exercise format variations against a real crate that doesn't
// declare any mcp-name (serde) — every case fails ownership, but we verify
// the failure error preserves the exact server name unchanged.
func TestValidateCargo_ServerNameFormats(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		serverName string
	}{
		{name: "canonical io.github format", serverName: "io.github.Wolfe-Jam/rust-faf-mcp"},
		{name: "multiple hyphens", serverName: "io.github.example/multi-hyphen-test-name"},
		{name: "underscore", serverName: "io.github.example/snake_case_name"},
		{name: "numeric suffix", serverName: "io.github.example/server-v2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeCargo,
				Identifier:   "serde",
				Version:      "1.0.219",
			}

			err := registries.ValidateCargo(ctx, pkg, tt.serverName)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.serverName)
		})
	}
}

// TestValidateCargo_PositivePathMock exercises the success branch: a README
// that contains the exact mcp-name token must return no error. Uses httptest
// to stand in for crates.io, so the test is hermetic — it doesn't depend on
// any live crate publishing a specific mcp-name line or on network reachability.
// Calls the test-only ValidateCargoREADME shim so the mock URL can take the
// place of https://crates.io without tripping the exact-baseURL guard.
func TestValidateCargo_PositivePathMock(t *testing.T) {
	ctx := context.Background()
	const serverName = "io.github.test/positive-path"

	var mock *httptest.Server
	mock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/readme") {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]string{
				"url": mock.URL + "/static-readme",
			}); err != nil {
				t.Fatalf("encode meta response: %v", err)
			}
			return
		}
		// Rendered README HTML containing the mcp-name token.
		fmt.Fprintf(w, "<html><body><p>some content</p><p>mcp-name: %s</p></body></html>", serverName)
	}))
	defer mock.Close()

	pkg := model.Package{
		RegistryType:    model.RegistryTypeCargo,
		RegistryBaseURL: mock.URL,
		Identifier:      "test-crate",
		Version:         "0.1.0",
	}

	err := registries.ValidateCargoREADME(ctx, pkg, serverName)
	assert.NoError(t, err, "validator should accept a README containing the exact mcp-name token")
}

// TestValidateCargo_LivePositivePath is the live anchor — it validates against
// rust-faf-mcp v0.3.1 on real crates.io, the first crate published with the
// mcp-name token as visible markdown (v0.3.0 used a hidden HTML comment, which
// crates.io strips during README rendering — see package-types.mdx for the
// cargo-specific gotcha). Complements TestValidateCargo_PositivePathMock:
// the mock proves the validator works in principle (hermetic, fast); the live
// anchor proves it works against the real crates.io API + static CDN pipeline.
//
// If this test ever starts failing, check (in order):
//  1. Has rust-faf-mcp v0.3.1 been yanked or replaced?
//  2. Did crates.io change its README rendering pipeline (e.g., start
//     stripping markdown lines that look like email-like tokens)?
//  3. Is the test machine network-blocked from crates.io or static.crates.io?
func TestValidateCargo_LivePositivePath(t *testing.T) {
	ctx := context.Background()

	pkg := model.Package{
		RegistryType: model.RegistryTypeCargo,
		Identifier:   "rust-faf-mcp",
		Version:      "0.3.1",
	}

	err := registries.ValidateCargo(ctx, pkg, "io.github.Wolfe-Jam/rust-faf-mcp")
	assert.NoError(t, err, "validator should accept the live rust-faf-mcp v0.3.1 crate (the canonical live anchor for cargo positive-path)")
}

// TestValidateCargo_TransientUpstreamError exercises the 5xx-as-transient branch:
// a 502/503 from static.crates.io is upstream availability, not "crate missing",
// and the error message must signal a retryable failure rather than imply the
// crate doesn't exist (the previous behavior, flagged in PR #1207 review).
func TestValidateCargo_TransientUpstreamError(t *testing.T) {
	ctx := context.Background()

	var mock *httptest.Server
	mock = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/readme") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"url": mock.URL + "/static-readme",
			})
			return
		}
		// Simulate static.crates.io 502 — upstream blip, not a missing crate.
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer mock.Close()

	pkg := model.Package{
		RegistryType:    model.RegistryTypeCargo,
		RegistryBaseURL: mock.URL,
		Identifier:      "test-crate",
		Version:         "0.1.0",
	}

	err := registries.ValidateCargoREADME(ctx, pkg, "io.github.test/transient")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transient")
	assert.NotContains(t, err.Error(), "not found", "transient upstream errors should not be reported as 'not found'")
}

// TestValidateCargoCombinedFixture exercises path-encoding and the full status
// matrix in one httptest fixture — the same pattern praised by @P4ST4S on
// PR #1321 for Go modules. One server dispatches on the crate identifier
// embedded in the URL path; wrong encoding routes to the fallback 500, which
// breaks the appropriate assertion. The explicit path assertion (assert.Equal
// on lastMetaPath) also catches encoding regressions directly.
func TestValidateCargoCombinedFixture(t *testing.T) {
	ctx := context.Background()
	const serverName = "io.github.test/combined"

	tests := []struct {
		name            string
		crateName       string
		version         string
		metaStatus      int
		readmeStatus    int
		readmeBody      string
		wantErr         bool
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:         "happy_path_visible_token",
			crateName:    "combined-happy",
			version:      "0.1.0",
			metaStatus:   http.StatusOK,
			readmeStatus: http.StatusOK,
			readmeBody:   fmt.Sprintf("<html><body><p>mcp-name: %s</p></body></html>", serverName),
		},
		{
			name:         "metadata_404",
			crateName:    "combined-meta404",
			version:      "0.1.0",
			metaStatus:   http.StatusNotFound,
			wantErr:      true,
			wantContains: []string{"metadata fetch failed", "status: 404"},
		},
		{
			name:         "readme_403_s3_not_found",
			crateName:    "combined-readme403",
			version:      "0.1.0",
			metaStatus:   http.StatusOK,
			readmeStatus: http.StatusForbidden,
			wantErr:      true,
			wantContains: []string{"not found", "status: 403"},
		},
		{
			name:            "readme_502_transient",
			crateName:       "combined-readme502",
			version:         "0.1.0",
			metaStatus:      http.StatusOK,
			readmeStatus:    http.StatusBadGateway,
			wantErr:         true,
			wantContains:    []string{"transient"},
			wantNotContains: []string{"not found"},
		},
	}

	// lastMetaPath captures the metadata request path seen by the handler so
	// each sub-test can assert the exact url.PathEscape-encoded form.
	var lastMetaPath atomic.Value
	lastMetaPath.Store("")

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := range tests {
			tt := &tests[i]
			metaPath := fmt.Sprintf("/api/v1/crates/%s/%s/readme",
				url.PathEscape(tt.crateName), url.PathEscape(tt.version))
			staticPath := "/readme-static/" + url.PathEscape(tt.crateName)

			if r.URL.Path == metaPath {
				lastMetaPath.Store(r.URL.Path)
				if tt.metaStatus != http.StatusOK {
					http.Error(w, "simulated non-200", tt.metaStatus)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"url": srv.URL + staticPath})
				return
			}
			if r.URL.Path == staticPath {
				if tt.readmeStatus != http.StatusOK {
					http.Error(w, "simulated non-200", tt.readmeStatus)
					return
				}
				fmt.Fprint(w, tt.readmeBody)
				return
			}
		}
		http.Error(w, "unexpected path: "+r.URL.Path, http.StatusInternalServerError)
	}))
	defer srv.Close()

	for i := range tests {
		tt := tests[i]
		lastMetaPath.Store("")
		t.Run(tt.name, func(t *testing.T) {
			wantMetaPath := fmt.Sprintf("/api/v1/crates/%s/%s/readme",
				url.PathEscape(tt.crateName), url.PathEscape(tt.version))

			pkg := model.Package{
				RegistryType:    model.RegistryTypeCargo,
				RegistryBaseURL: srv.URL,
				Identifier:      tt.crateName,
				Version:         tt.version,
			}

			err := registries.ValidateCargoREADME(ctx, pkg, serverName)

			// Assert encode step: the validator must have requested exactly the
			// url.PathEscape-encoded path; wrong encoding hits the fallback 500.
			assert.Equal(t, wantMetaPath, lastMetaPath.Load().(string),
				"meta request path must be exactly the url.PathEscape-encoded form")

			if tt.wantErr {
				assert.Error(t, err)
				for _, want := range tt.wantContains {
					assert.Contains(t, err.Error(), want)
				}
				for _, notWant := range tt.wantNotContains {
					assert.NotContains(t, err.Error(), notWant)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
