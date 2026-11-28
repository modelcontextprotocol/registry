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
	return newNPMMockWithPackument(versionStatus, versionBody, packageStatus,
		`{"versions":{"1.0.0":{}}}`)
}

// newNPMMockWithPackument is newNPMMock plus control over the packument body that
// the package-level endpoint returns on a full GET, which the validator reads to
// tell a live package from an unpublished tombstone.
func newNPMMockWithPackument(versionStatus int, versionBody string, packageStatus int, packument string) *httptest.Server {
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
		case 1: // /{name}  (package-existence probe, then tombstone check)
			switch r.Method {
			case http.MethodHead:
				w.WriteHeader(packageStatus)
			case http.MethodGet:
				if packageStatus != http.StatusOK {
					w.WriteHeader(packageStatus)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, packument)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
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

// TestValidateNPM_DistTagIsNotAVersion: npm answers a dist-tag with the release it
// points at, so an entry naming the tag must be refused and told the real version.
func TestValidateNPM_DistTagIsNotAVersion(t *testing.T) {
	ctx := context.Background()
	const serverName = "io.github.test/demo"
	body := fmt.Sprintf(`{"mcpName":%q,"version":"2.0.0-rc.1"}`, serverName)
	mock := newNPMMock(http.StatusOK, body, http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: mock.URL, Identifier: "demo-pkg", Version: "next"}
	err := registries.ValidateNPMPackage(ctx, pkg, serverName)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "dist-tag")
		assert.Contains(t, err.Error(), "2.0.0-rc.1")
	}

	pkg.Version = "2.0.0-rc.1"
	assert.NoError(t, registries.ValidateNPMPackage(ctx, pkg, serverName), "the release itself validates")
}

func TestValidateNPM_RealPackages(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeNPM,
				Identifier:   tt.packageName,
				Version:      tt.version,
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

// TestValidateNPM_UnpublishedTombstoneIsMissingPackage is the #1862 regression:
// npm answers 200 on both GET and HEAD for an unpublished unscoped package, with
// `time.unpublished` set and no versions. The validator used to read that 200 as
// "package exists, version missing" and told the publisher to publish the version.
func TestValidateNPM_UnpublishedTombstoneIsMissingPackage(t *testing.T) {
	ctx := context.Background()
	mock := newNPMMockWithPackument(http.StatusNotFound, "", http.StatusOK,
		`{"time":{"unpublished":{"time":"2026-08-05T17:24:17.672Z","versions":["0.1.0","0.1.1"]}},"versions":{}}`)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: mock.URL,
		Identifier: "ace-context-mcp", Version: "0.1.1"}
	err := registries.ValidateNPMPackage(ctx, pkg, "io.github.test/demo")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.NotContains(t, err.Error(), "exists, but version")
}

// An empty versions map with no explicit tombstone timestamp is also a package
// that cannot be installed; treat it the same way.
func TestValidateNPM_EmptyVersionsIsMissingPackage(t *testing.T) {
	ctx := context.Background()
	mock := newNPMMockWithPackument(http.StatusNotFound, "", http.StatusOK, `{"versions":{}}`)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: mock.URL,
		Identifier: "gone-mcp", Version: "1.0.0"}
	err := registries.ValidateNPMPackage(ctx, pkg, "io.github.test/demo")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.NotContains(t, err.Error(), "exists, but version")
}

// A live package with a missing version must keep the actionable message.
func TestValidateNPM_LivePackageMissingVersionStillSaysVersion(t *testing.T) {
	ctx := context.Background()
	mock := newNPMMockWithPackument(http.StatusNotFound, "", http.StatusOK,
		`{"time":{"modified":"2026-11-01T00:00:00.000Z"},"versions":{"2.0.0":{}}}`)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: mock.URL,
		Identifier: "live-mcp", Version: "9.9.9"}
	err := registries.ValidateNPMPackage(ctx, pkg, "io.github.test/demo")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exists, but version")
}
