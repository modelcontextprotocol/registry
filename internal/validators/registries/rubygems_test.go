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

// newRubyGemsMock stands in for rubygems.org: it routes the version fetch
// (/api/v2/rubygems/{name}/versions/{version}.json) and the gem probe
// (/api/v1/versions/{name}.json) by path shape and returns the given statuses
// (versionBody used on 200).
func newRubyGemsMock(versionStatus int, versionBody string, gemStatus int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Pin the method per endpoint (GET fetch, HEAD probe) so a method
		// regression in the validator surfaces as a 405 instead of passing.
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/rubygems/"):
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
		case strings.HasPrefix(r.URL.Path, "/api/v1/versions/"):
			if r.Method != http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(gemStatus)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
}

// TestValidateRubyGems_PositivePathMock: a version response whose gemspec
// metadata carries the matching mcp_name validates.
func TestValidateRubyGems_PositivePathMock(t *testing.T) {
	ctx := context.Background()
	const serverName = "io.github.test/demo"
	body := fmt.Sprintf(`{"metadata":{"mcp_name":%q},"yanked":false}`, serverName)
	mock := newRubyGemsMock(http.StatusOK, body, http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeRubyGems, RegistryBaseURL: mock.URL, Identifier: "demo-gem", Version: "1.0.0"}
	err := registries.ValidateRubyGemsPackage(ctx, pkg, serverName)
	assert.NoError(t, err, "a version response with the matching mcp_name metadata should validate")
}

// TestValidateRubyGems_MissingMCPName: a gem without the mcp_name metadata key
// gets an actionable error telling the author what to add to the gemspec.
func TestValidateRubyGems_MissingMCPName(t *testing.T) {
	ctx := context.Background()
	body := `{"metadata":{"source_code_uri":"https://github.com/example/demo-gem"},"yanked":false}`
	mock := newRubyGemsMock(http.StatusOK, body, http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeRubyGems, RegistryBaseURL: mock.URL, Identifier: "demo-gem", Version: "1.0.0"}
	err := registries.ValidateRubyGemsPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing the required 'mcp_name' gemspec metadata")
	assert.Contains(t, err.Error(), `spec.metadata["mcp_name"] = "io.github.test/demo"`, "the error must show the exact gemspec line to add")
}

// TestValidateRubyGems_MCPNameMismatch: an mcp_name that names a different
// server fails ownership validation with expected/got.
func TestValidateRubyGems_MCPNameMismatch(t *testing.T) {
	ctx := context.Background()
	body := `{"metadata":{"mcp_name":"io.github.other/name"},"yanked":false}`
	mock := newRubyGemsMock(http.StatusOK, body, http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeRubyGems, RegistryBaseURL: mock.URL, Identifier: "demo-gem", Version: "1.0.0"}
	err := registries.ValidateRubyGemsPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Expected mcp_name 'io.github.test/demo', got 'io.github.other/name'")
}

// TestValidateRubyGems_YankedVersion: rubygems.org normally 404s yanked versions
// on the version endpoint, but a 200 carrying yanked=true must still be rejected.
func TestValidateRubyGems_YankedVersion(t *testing.T) {
	ctx := context.Background()
	body := `{"metadata":{"mcp_name":"io.github.test/demo"},"yanked":true}`
	mock := newRubyGemsMock(http.StatusOK, body, http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeRubyGems, RegistryBaseURL: mock.URL, Identifier: "demo-gem", Version: "1.0.0"}
	err := registries.ValidateRubyGemsPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "yanked")
}

// TestValidateRubyGems_VersionNotYetVisible: version 404s while the gem exists,
// so the error must report a missing version, not a missing gem.
func TestValidateRubyGems_VersionNotYetVisible(t *testing.T) {
	ctx := context.Background()
	mock := newRubyGemsMock(http.StatusNotFound, "", http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeRubyGems, RegistryBaseURL: mock.URL, Identifier: "demo-gem", Version: "9.9.9"}
	err := registries.ValidateRubyGemsPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exists, but version '9.9.9'", "gem-exists/version-missing must be distinguished from gem-missing")
}

// TestValidateRubyGems_GemMissing: both endpoints 404, so "not found" is correct.
func TestValidateRubyGems_GemMissing(t *testing.T) {
	ctx := context.Background()
	mock := newRubyGemsMock(http.StatusNotFound, "", http.StatusNotFound)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeRubyGems, RegistryBaseURL: mock.URL, Identifier: "demo-gem", Version: "1.0.0"}
	err := registries.ValidateRubyGemsPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.NotContains(t, err.Error(), "exists, but version", "a genuinely missing gem must not claim the version exists")
}

// TestValidateRubyGems_TransientUpstream: a 5xx on the version fetch must be
// retryable, not "not found".
func TestValidateRubyGems_TransientUpstream(t *testing.T) {
	ctx := context.Background()
	mock := newRubyGemsMock(http.StatusBadGateway, "", http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeRubyGems, RegistryBaseURL: mock.URL, Identifier: "demo-gem", Version: "1.0.0"}
	err := registries.ValidateRubyGemsPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transient")
	assert.NotContains(t, err.Error(), "not found", "transient upstream errors must not be reported as 'not found'")
}

// TestValidateRubyGems_VersionEndpointRateLimited: a 429 on the version fetch is
// reported as rate-limited/transient.
func TestValidateRubyGems_VersionEndpointRateLimited(t *testing.T) {
	ctx := context.Background()
	mock := newRubyGemsMock(http.StatusTooManyRequests, "", http.StatusOK)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeRubyGems, RegistryBaseURL: mock.URL, Identifier: "demo-gem", Version: "1.0.0"}
	err := registries.ValidateRubyGemsPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate-limited")
	assert.NotContains(t, err.Error(), "not found")
}

// TestValidateRubyGems_VersionNotFoundProbeInconclusive: version 404 plus a
// transient probe (503) leaves existence undetermined, so the validator must not
// say "not found".
func TestValidateRubyGems_VersionNotFoundProbeInconclusive(t *testing.T) {
	ctx := context.Background()
	mock := newRubyGemsMock(http.StatusNotFound, "", http.StatusServiceUnavailable)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeRubyGems, RegistryBaseURL: mock.URL, Identifier: "demo-gem", Version: "1.0.0"}
	err := registries.ValidateRubyGemsPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transient")
	assert.NotContains(t, err.Error(), "not found", "an inconclusive probe must not assert the gem is missing")
}

// TestValidateRubyGems_VersionNotFoundProbeUnclassified: the version 404s and
// the probe returns an unclassifiable status, so the validator falls back to a
// plain version-not-found message.
func TestValidateRubyGems_VersionNotFoundProbeUnclassified(t *testing.T) {
	ctx := context.Background()
	mock := newRubyGemsMock(http.StatusNotFound, "", http.StatusTeapot)
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeRubyGems, RegistryBaseURL: mock.URL, Identifier: "demo-gem", Version: "1.0.0"}
	err := registries.ValidateRubyGemsPackage(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "version '1.0.0' not found")
	assert.NotContains(t, err.Error(), "exists, but version")
}

// TestValidateRubyGems_ProbeDeadlineBounded: a hung probe must be cut off by the
// probe's own short deadline instead of riding out the client's full 10s
// timeout, and the cutoff must read as inconclusive rather than "not found".
func TestValidateRubyGems_ProbeDeadlineBounded(t *testing.T) {
	ctx := context.Background()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v2/rubygems/") { // version fetch
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Gem probe: hang until the client gives up.
		<-r.Context().Done()
	}))
	defer mock.Close()

	pkg := model.Package{RegistryType: model.RegistryTypeRubyGems, RegistryBaseURL: mock.URL, Identifier: "demo-gem", Version: "1.0.0"}
	start := time.Now()
	err := registries.ValidateRubyGemsPackage(ctx, pkg, "io.github.test/demo")
	elapsed := time.Since(start)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transient", "a probe cut off by its deadline is inconclusive, not 'not found'")
	assert.Less(t, elapsed, 8*time.Second, "a hung probe must be bounded by the probe deadline, not the client timeout")
}

// TestValidateRubyGems_FileSHA256Rejected: the MCPB-only fileSha256 field is
// rejected before any network access.
func TestValidateRubyGems_FileSHA256Rejected(t *testing.T) {
	ctx := context.Background()

	pkg := model.Package{
		RegistryType: model.RegistryTypeRubyGems,
		Identifier:   "demo-gem",
		Version:      "1.0.0",
		FileSHA256:   "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce",
	}
	err := registries.ValidateRubyGems(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must not have 'fileSha256'")
}

// TestValidateRubyGems_BaseURLMismatch: only https://rubygems.org is accepted as
// the registry base URL, and the guard fires before any network access.
func TestValidateRubyGems_BaseURLMismatch(t *testing.T) {
	ctx := context.Background()

	pkg := model.Package{
		RegistryType:    model.RegistryTypeRubyGems,
		RegistryBaseURL: "https://example.com",
		Identifier:      "demo-gem",
		Version:         "1.0.0",
	}
	err := registries.ValidateRubyGems(ctx, pkg, "io.github.test/demo")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "registry type and base URL do not match")
}

func TestValidateRubyGems_RealPackages(t *testing.T) {
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
			errorMessage: "package identifier is required for RubyGems packages",
		},
		{
			name:         "empty package version should fail",
			packageName:  "test-gem",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package version is required for RubyGems packages",
		},
		{
			name:         "both empty identifier and version should fail with identifier error first",
			packageName:  "",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for RubyGems packages",
		},
		{
			name:         "non-existent gem should fail",
			packageName:  generateRandomPackageName(),
			version:      "1.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "real gem without mcp_name should fail",
			packageName:  "rubocop", // Popular gem without mcp_name metadata
			version:      "1.80.2",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing the required 'mcp_name' gemspec metadata",
		},
		{
			name:         "real gem without mcp_name should fail",
			packageName:  "rake", // Another popular gem
			version:      "13.2.1",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing the required 'mcp_name' gemspec metadata",
		},
		{
			name:         "yanked version of an existing gem should report missing version, not missing gem",
			packageName:  "mimemagic", // 0.3.0 was yanked in 2021
			version:      "0.3.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "exists, but version '0.3.0'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeRubyGems,
				Identifier:   tt.packageName,
				Version:      tt.version,
			}

			err := registries.ValidateRubyGems(ctx, pkg, tt.serverName)

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
