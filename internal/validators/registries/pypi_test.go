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

// newPyPIMock stands in for pypi.org: it routes the version fetch and package
// probe by path shape and returns the given statuses (versionBody is used on 200).
func newPyPIMock(versionStatus int, versionBody string, packageStatus int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Route on the escaped path so an identifier containing an encoded
		// separator is not silently re-split into extra segments.
		parts := strings.Split(strings.Trim(r.URL.EscapedPath(), "/"), "/")
		// Pin the method per endpoint (GET fetch, HEAD probe) so a method
		// regression in the validator surfaces as a 405 instead of passing.
		switch len(parts) {
		case 4: // /pypi/{name}/{version}/json
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
		case 3: // /pypi/{name}/json  (package-existence probe)
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

// TestValidatePyPI_VersionNotYetVisible is the #553 regression: version 404s while
// the package exists, so the error must report a missing version, not a missing package.
func TestValidatePyPI_VersionNotYetVisible(t *testing.T) {
	ctx := context.Background()
	mock := newPyPIMock(http.StatusNotFound, "", http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypePyPI, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "9.9.9"}
	err := registries.ValidatePyPIPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exists, but version '9.9.9'", "package-exists/version-missing must be distinguished from package-missing")
}

// TestValidatePyPI_PackageMissing: both the version and the package endpoints 404,
// so the package genuinely does not exist and "not found" is correct.
func TestValidatePyPI_PackageMissing(t *testing.T) {
	ctx := context.Background()
	mock := newPyPIMock(http.StatusNotFound, "", http.StatusNotFound)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypePyPI, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	err := registries.ValidatePyPIPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.NotContains(t, err.Error(), "exists, but version", "a genuinely missing package must not claim the version exists")
}

// TestValidatePyPI_TransientUpstream: a 5xx on the version fetch is upstream
// availability, not "package missing", and must be reported as retryable.
func TestValidatePyPI_TransientUpstream(t *testing.T) {
	ctx := context.Background()
	mock := newPyPIMock(http.StatusServiceUnavailable, "", http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypePyPI, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	err := registries.ValidatePyPIPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transient")
	assert.NotContains(t, err.Error(), "not found", "transient upstream errors must not be reported as 'not found'")
}

// TestValidatePyPI_VersionNotFoundProbeInconclusive: version 404 plus a transient
// probe (429) leaves existence undetermined, so the validator must not say "not found".
func TestValidatePyPI_VersionNotFoundProbeInconclusive(t *testing.T) {
	ctx := context.Background()
	mock := newPyPIMock(http.StatusNotFound, "", http.StatusTooManyRequests)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypePyPI, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	err := registries.ValidatePyPIPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transient")
	assert.NotContains(t, err.Error(), "not found", "an inconclusive probe must not assert the package is missing")
}

// TestValidatePyPI_ProbeDeadlineBounded: a hung probe must be cut off by the
// probe's own short deadline instead of riding out the client's full 10s
// timeout, and the cutoff must read as inconclusive rather than "not found".
func TestValidatePyPI_ProbeDeadlineBounded(t *testing.T) {
	ctx := context.Background()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.EscapedPath(), "/"), "/")
		if len(parts) == 4 { // version fetch
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Package probe: hang until the client gives up.
		<-r.Context().Done()
	}))
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypePyPI, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	start := time.Now()
	err := registries.ValidatePyPIPackage(ctx, pkg, "io.github.test/demo")
	elapsed := time.Since(start)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transient", "a probe cut off by its deadline is inconclusive, not 'not found'")
	assert.Less(t, elapsed, 8*time.Second, "a hung probe must be bounded by the probe deadline, not the client timeout")
}

// TestValidatePyPI_VersionEndpointRateLimited: a 429 on the version fetch is reported
// as rate-limited/transient.
func TestValidatePyPI_VersionEndpointRateLimited(t *testing.T) {
	ctx := context.Background()
	mock := newPyPIMock(http.StatusTooManyRequests, "", http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypePyPI, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	err := registries.ValidatePyPIPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate-limited")
	assert.NotContains(t, err.Error(), "not found")
}

// TestValidatePyPI_VersionNotFoundProbeUnclassified: the version 404s and the probe
// returns an unclassifiable status, so the validator falls back to a plain
// version-not-found message without claiming the package is present or absent.
func TestValidatePyPI_VersionNotFoundProbeUnclassified(t *testing.T) {
	ctx := context.Background()
	mock := newPyPIMock(http.StatusNotFound, "", http.StatusTeapot)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypePyPI, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	err := registries.ValidatePyPIPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "version '1.0.0' not found")
	assert.NotContains(t, err.Error(), "exists, but version")
}

// TestValidatePyPI_PositivePathMock: a version README carrying the exact mcp-name token validates.
func TestValidatePyPI_PositivePathMock(t *testing.T) {
	ctx := context.Background()
	const serverName = "io.github.test/demo"
	body := fmt.Sprintf(`{"info":{"description":"# Demo\n\nmcp-name: %s\n"}}`, serverName)
	mock := newPyPIMock(http.StatusOK, body, http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypePyPI, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "1.0.0"}
	err := registries.ValidatePyPIPackage(ctx, pkg, serverName)
	assert.NoError(t, err, "a version README containing the exact mcp-name token should validate")
}

func TestValidatePyPI_RealPackages(t *testing.T) {
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
			version:      "1.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for PyPI packages",
		},
		{
			name:         "empty package version should fail",
			packageName:  "mcp-server-example",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package version is required for PyPI packages",
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
			name:         "real package without MCP server name should fail",
			packageName:  "requests", // Popular package without MCP server name in keywords/description/URLs
			version:      "2.31.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "ownership validation failed",
		},
		{
			name:         "real package with different server name should fail",
			packageName:  "numpy", // Another popular package
			version:      "1.25.2",
			serverName:   "com.example/completely-different-name",
			expectError:  true,
			errorMessage: "ownership validation failed", // Will fail because numpy doesn't have this server name
		},
		{
			name:        "real package with server name in README should pass",
			packageName: "time-mcp-pypi",
			version:     "1.0.6",
			serverName:  "io.github.domdomegg/time-mcp-pypi",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypePyPI,
				Identifier:   tt.packageName,
				Version:      tt.version,
			}

			err := registries.ValidatePyPI(ctx, pkg, tt.serverName)

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
