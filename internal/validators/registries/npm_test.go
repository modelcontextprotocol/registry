package registries_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

// newNPMMock stands in for registry.npmjs.org: it routes the version fetch and
// package probe by path shape and returns the given statuses (versionBody used on 200).
func newNPMMock(versionStatus int, versionBody string, packageStatus int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Route on the escaped path: a scoped name like @scope/pkg is sent as the
		// single segment @scope%2Fpkg, and decoding it would wrongly split it in two.
		parts := strings.Split(strings.Trim(r.URL.EscapedPath(), "/"), "/")
		// Pin the method per endpoint (GET fetch, HEAD probe) so a method
		// regression in the validator surfaces as a 405 instead of passing.
		switch len(parts) {
		case 2: // /{name}/{version}
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if versionStatus != http.StatusOK {
				w.WriteHeader(versionStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, versionBody)
		case 1: // /{name}  (package-existence probe)
			if r.Method != http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(packageStatus)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
}

// TestValidateNPM_VersionNotYetVisible is the #553 regression for npm: version 404s
// while the package exists, so the error must report a missing version, not a missing package.
func TestValidateNPM_VersionNotYetVisible(t *testing.T) {
	ctx := context.Background()
	mock := newNPMMock(http.StatusNotFound, "", http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "9.9.9"}
	err := registries.ValidateNPMPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exists, but version '9.9.9'", "package-exists/version-missing must be distinguished from package-missing")
}

// TestValidateNPM_PackageMissing: both endpoints 404, so "not found" is correct.
func TestValidateNPM_PackageMissing(t *testing.T) {
	ctx := context.Background()
	mock := newNPMMock(http.StatusNotFound, "", http.StatusNotFound)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	err := registries.ValidateNPMPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.NotContains(t, err.Error(), "exists, but version", "a genuinely missing package must not claim the version exists")
}

// TestValidateNPM_TransientUpstream: a 5xx on the version fetch must be retryable,
// not "not found".
func TestValidateNPM_TransientUpstream(t *testing.T) {
	ctx := context.Background()
	mock := newNPMMock(http.StatusBadGateway, "", http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	err := registries.ValidateNPMPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transient")
	assert.NotContains(t, err.Error(), "not found", "transient upstream errors must not be reported as 'not found'")
}

// TestValidateNPM_VersionNotFoundProbeInconclusive: version 404 plus a transient
// probe (503) leaves existence undetermined, so the validator must not say "not found".
func TestValidateNPM_VersionNotFoundProbeInconclusive(t *testing.T) {
	ctx := context.Background()
	mock := newNPMMock(http.StatusNotFound, "", http.StatusServiceUnavailable)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	err := registries.ValidateNPMPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transient")
	assert.NotContains(t, err.Error(), "not found", "an inconclusive probe must not assert the package is missing")
}

// TestValidateNPM_ProbeDeadlineBounded: a hung probe must be cut off by the
// probe's own short deadline instead of riding out the client's full 10s
// timeout, and the cutoff must read as inconclusive rather than "not found".
func TestValidateNPM_ProbeDeadlineBounded(t *testing.T) {
	ctx := context.Background()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.EscapedPath(), "/"), "/")
		if len(parts) == 2 { // version fetch
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Package probe: hang until the client gives up.
		<-r.Context().Done()
	}))
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	start := time.Now()
	err := registries.ValidateNPMPackage(ctx, pkg, "io.github.test/demo")
	elapsed := time.Since(start)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transient", "a probe cut off by its deadline is inconclusive, not 'not found'")
	assert.Less(t, elapsed, 8*time.Second, "a hung probe must be bounded by the probe deadline, not the client timeout")
}

// TestValidateNPM_ScopedVersionNotYetVisible covers scoped names (@scope/name), the
// escaped-single-segment case, through the version fetch and the HEAD probe.
func TestValidateNPM_ScopedVersionNotYetVisible(t *testing.T) {
	ctx := context.Background()
	mock := newNPMMock(http.StatusNotFound, "", http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: mock.URL, Identifier: "@scope/demo", Version: "9.9.9"}
	err := registries.ValidateNPMPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exists, but version '9.9.9'", "scoped names must route correctly through both endpoints")
}

// TestValidateNPM_VersionEndpointRateLimited: a 429 on the version fetch is reported
// as rate-limited/transient.
func TestValidateNPM_VersionEndpointRateLimited(t *testing.T) {
	ctx := context.Background()
	mock := newNPMMock(http.StatusTooManyRequests, "", http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	err := registries.ValidateNPMPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate-limited")
	assert.NotContains(t, err.Error(), "not found")
}

// TestValidateNPM_VersionNotFoundProbeUnclassified: the version 404s and the probe
// returns an unclassifiable status, so the validator falls back to a plain
// version-not-found message.
func TestValidateNPM_VersionNotFoundProbeUnclassified(t *testing.T) {
	ctx := context.Background()
	mock := newNPMMock(http.StatusNotFound, "", http.StatusTeapot)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	err := registries.ValidateNPMPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "version '1.0.0' not found")
	assert.NotContains(t, err.Error(), "exists, but version")
}

// TestValidateNPM_PositivePathMock: a version response with the matching mcpName validates.
func TestValidateNPM_PositivePathMock(t *testing.T) {
	ctx := context.Background()
	const serverName = "io.github.test/demo"
	body := fmt.Sprintf(`{"mcpName":%q}`, serverName)
	mock := newNPMMock(http.StatusOK, body, http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	err := registries.ValidateNPMPackage(ctx, pkg, serverName)
	assert.NoError(t, err, "a version response with the matching mcpName should validate")
}

// TestValidateNPM_Executable covers the optional executable selector (#1629):
// it must name an entry of package.json "bin" in the published version.
func TestValidateNPM_Executable(t *testing.T) {
	ctx := context.Background()
	const serverName = "io.github.test/demo"

	tests := []struct {
		name        string
		identifier  string
		bin         string // raw JSON for the "bin" field; empty means omitted
		executable  string
		errContains string // empty means success is expected
	}{
		{
			name:       "selected executable is one of several bins",
			identifier: "sumlyzer",
			bin:        `{"sumlyzer":"bin/sumlyzer.mjs","sumlyzer-mcp-server":"bin/sumlyzer-mcp-server.mjs"}`,
			executable: "sumlyzer-mcp-server",
		},
		{
			name:        "selected executable is not a bin",
			identifier:  "sumlyzer",
			bin:         `{"sumlyzer-mcp-server":"bin/b.mjs","sumlyzer":"bin/a.mjs"}`,
			executable:  "sumlyzer-mcp",
			errContains: "has no executable named 'sumlyzer-mcp' in package.json 'bin' (available: sumlyzer, sumlyzer-mcp-server)",
		},
		{
			name:       "string bin shorthand is named after the unscoped package",
			identifier: "@scope/demo-mcp",
			bin:        `"dist/index.js"`,
			executable: "demo-mcp",
		},
		{
			name:        "string bin shorthand does not match another name",
			identifier:  "@scope/demo-mcp",
			bin:         `"dist/index.js"`,
			executable:  "scope",
			errContains: "(available: demo-mcp)",
		},
		{
			name:        "package without bin",
			identifier:  "demo-lib",
			executable:  "demo",
			errContains: "declares no executables in package.json 'bin'",
		},
		{
			name:        "malformed bin",
			identifier:  "demo-pkg",
			bin:         `["a","b"]`,
			executable:  "a",
			errContains: "failed to parse 'bin' field",
		},
		{
			name:       "no executable selected skips the bin check",
			identifier: "demo-lib",
			bin:        `["not","checked"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"mcpName":%q}`, serverName)
			if tt.bin != "" {
				body = fmt.Sprintf(`{"mcpName":%q,"bin":%s}`, serverName, tt.bin)
			}
			mock := newNPMMock(http.StatusOK, body, http.StatusOK)
			defer mock.Close()

			pkg := model.Package{
				RegistryType:    model.RegistryTypeNPM,
				RegistryBaseURL: mock.URL,
				Identifier:      tt.identifier,
				Version:         "1.0.0",
				Executable:      tt.executable,
			}
			err := registries.ValidateNPMPackage(ctx, pkg, serverName)
			if tt.errContains == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.errContains)
		})
	}
}

func TestValidateNPM_RealPackages(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		packageName  string
		version      string
		serverName   string
		executable   string
		expectError  bool
		errorMessage string
	}{
		{
			name:         "empty package identifier should fail",
			packageName:  "",
			version:      "1.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for NPM packages",
		},
		{
			name:         "empty package version should fail",
			packageName:  "test-package",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package version is required for NPM packages",
		},
		{
			name:         "both empty identifier and version should fail with identifier error first",
			packageName:  "",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for NPM packages",
		},
		{
			name:         "non-existent package should fail",
			packageName:  generateRandomPackageName(),
			version:      "1.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "real package without mcpName should fail",
			packageName:  "express", // Popular package without mcpName field
			version:      "4.18.2",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required 'mcpName' field",
		},
		{
			name:         "real package without mcpName should fail",
			packageName:  "lodash", // Another popular package
			version:      "4.17.21",
			serverName:   "com.example/completely-different-name",
			expectError:  true,
			errorMessage: "missing required 'mcpName' field",
		},
		{
			name:         "real package without mcpName should fail",
			packageName:  "airtable-mcp-server",
			version:      "1.5.0",
			serverName:   "io.github.domdomegg/airtable-mcp-server",
			expectError:  true,
			errorMessage: "missing required 'mcpName' field",
		},
		{
			name:         "real package with incorrect mcpName should fail",
			packageName:  "airtable-mcp-server",
			version:      "1.7.2",
			serverName:   "io.github.not-domdomegg/airtable-mcp-server",
			expectError:  true,
			errorMessage: "Expected mcpName 'io.github.not-domdomegg/airtable-mcp-server', got 'io.github.domdomegg/airtable-mcp-server'",
		},
		{
			name:        "real package with correct mcpName should pass",
			packageName: "airtable-mcp-server",
			version:     "1.7.2",
			serverName:  "io.github.domdomegg/airtable-mcp-server",
			expectError: false,
		},
		{
			name:         "scoped package that doesn't exist should fail",
			packageName:  "@nonexistent-scope/nonexistent-package",
			version:      "1.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "scoped package without mcpName should fail",
			packageName:  "@types/node",
			version:      "20.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required 'mcpName' field",
		},
		{
			name:        "scoped package with mcpName should pass",
			packageName: "@hellocoop/admin-mcp",
			version:     "1.5.7",
			serverName:  "io.github.hellocoop/admin-mcp",
			expectError: false,
		},
		{
			// Ships two bins ("mcp", "hello-mcp-http"), neither named after the
			// package, so `npx @hellocoop/admin-mcp@1.5.7` cannot pick one.
			name:        "multi-bin package with a declared executable should pass",
			packageName: "@hellocoop/admin-mcp",
			version:     "1.5.7",
			serverName:  "io.github.hellocoop/admin-mcp",
			executable:  "mcp",
			expectError: false,
		},
		{
			name:         "multi-bin package with an unknown executable should fail",
			packageName:  "@hellocoop/admin-mcp",
			version:      "1.5.7",
			serverName:   "io.github.hellocoop/admin-mcp",
			executable:   "admin-mcp",
			expectError:  true,
			errorMessage: "has no executable named 'admin-mcp' in package.json 'bin' (available: hello-mcp-http, mcp)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeNPM,
				Identifier:   tt.packageName,
				Version:      tt.version,
				Executable:   tt.executable,
			}

			err := registries.ValidateNPM(ctx, pkg, tt.serverName)

			// A live 429/5xx from the registry is inconclusive, not a failure
			// of the case under test.
			if err != nil && strings.Contains(err.Error(), "retry later") {
				t.Skipf("transient registry response: %v", err)
			}

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
