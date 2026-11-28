package registries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

var (
	ErrMissingIdentifierForRubyGems = errors.New("package identifier is required for RubyGems packages")
	ErrMissingVersionForRubyGems    = errors.New("package version is required for RubyGems packages")
)

// rubyGemsMCPNameKey is the gemspec metadata key that carries the MCP server
// name. Gem authors set it in their gemspec:
//
//	spec.metadata["mcp_name"] = "io.github.OWNER/REPO"
//
// The key follows the snake_case convention of gemspec metadata
// (e.g. "source_code_uri", "changelog_uri").
const rubyGemsMCPNameKey = "mcp_name"

// RubyGemsVersionResponse represents the structure returned by the RubyGems.org
// version-specific API (/api/v2/rubygems/{name}/versions/{version}.json). The
// metadata hash carries the gemspec metadata, which is where the mcp_name
// ownership token lives. Yanked versions 404 on this endpoint, but the yanked
// flag is honored defensively in case one is still served.
//
// When a version is published for several platforms and no platform parameter
// is given, rubygems.org picks which build to return, and it is not necessarily
// the "ruby" one. The platform is left unspecified on purpose so that versions
// published only as platform-specific builds can be registered; authors must
// therefore set mcp_name in every platform build of a version.
type RubyGemsVersionResponse struct {
	Metadata map[string]string `json:"metadata"`
	Yanked   bool              `json:"yanked"`
}

// ValidateRubyGems validates that a RubyGems (rubygems.org) package contains the
// correct MCP server name.
//
// Verification mechanism: the version-specific rubygems.org API returns the
// gemspec metadata hash, so a single call covers existence, version, and the
// mcp_name ownership check. This mirrors the npm validator's manifest-field
// approach (mcpName in package.json) rather than the README-token approach used
// for PyPI/NuGet/cargo: rubygems.org has no README API, and gemspec metadata is
// a first-class, author-controlled field.
func ValidateRubyGems(ctx context.Context, pkg model.Package, serverName string) error {
	// Set default registry base URL if empty
	if pkg.RegistryBaseURL == "" {
		pkg.RegistryBaseURL = model.RegistryURLRubyGems
	}

	if pkg.Identifier == "" {
		return ErrMissingIdentifierForRubyGems
	}

	// The version-specific endpoint is the only one that exposes per-version
	// gemspec metadata, so a version is required to validate mcp_name.
	if pkg.Version == "" {
		return ErrMissingVersionForRubyGems
	}

	// Validate that MCPB-specific fields are not present
	if pkg.FileSHA256 != "" {
		return fmt.Errorf("RubyGems packages must not have 'fileSha256' field - this is only for MCPB packages")
	}

	// Validate that the registry base URL matches rubygems.org exactly
	if pkg.RegistryBaseURL != model.RegistryURLRubyGems {
		return fmt.Errorf("registry type and base URL do not match: '%s' is not valid for registry type '%s'. Expected: %s",
			pkg.RegistryBaseURL, model.RegistryTypeRubyGems, model.RegistryURLRubyGems)
	}

	return validateRubyGemsPackage(ctx, pkg, serverName)
}

// validateRubyGemsPackage performs the version-metadata fetch and the mcp_name
// check. It is split out from ValidateRubyGems so that httptest-based tests can
// drive the HTTP pipeline against a mock server (exposed via export_test.go),
// bypassing the exact-baseURL guard that ValidateRubyGems enforces for callers.
func validateRubyGemsPackage(ctx context.Context, pkg model.Package, serverName string) error {
	client := &http.Client{Timeout: 10 * time.Second}

	requestURL := fmt.Sprintf("%s/api/v2/rubygems/%s/versions/%s.json",
		pkg.RegistryBaseURL, url.PathEscape(pkg.Identifier), url.PathEscape(pkg.Version))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch package metadata from RubyGems.org: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return rubyGemsFetchStatusError(ctx, client, pkg, resp.StatusCode)
	}

	var gemResp RubyGemsVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&gemResp); err != nil {
		return fmt.Errorf("failed to parse RubyGems package metadata: %w", err)
	}

	if gemResp.Yanked {
		return fmt.Errorf("RubyGems package '%s' version '%s' has been yanked and cannot be registered", pkg.Identifier, pkg.Version)
	}

	mcpName := gemResp.Metadata[rubyGemsMCPNameKey]
	if mcpName == "" {
		return fmt.Errorf("RubyGems package '%s' is missing the required 'mcp_name' gemspec metadata. Add this to your gemspec and publish a new version: spec.metadata[\"mcp_name\"] = \"%s\"", pkg.Identifier, serverName)
	}

	if mcpName != serverName {
		return fmt.Errorf("RubyGems package ownership validation failed. Expected mcp_name '%s', got '%s'", serverName, mcpName)
	}

	return nil
}

// rubyGemsGemState is the outcome of probing the gem-level RubyGems.org
// endpoint, used to disambiguate a 404 from the version-specific endpoint.
type rubyGemsGemState int

const (
	// rubyGemsGemUnknown: the probe returned a status we can't classify.
	rubyGemsGemUnknown rubyGemsGemState = iota
	// rubyGemsGemExists: the gem exists (200) but the requested version does not.
	rubyGemsGemExists
	// rubyGemsGemMissing: the gem itself does not exist (404).
	rubyGemsGemMissing
	// rubyGemsGemTransient: the probe failed for a retryable reason (network
	// error, 429, or 5xx). Existence is undetermined and the caller should not
	// report "not found".
	rubyGemsGemTransient
)

// rubyGemsFetchStatusError maps a non-200 status from the version-specific
// endpoint to a caller-actionable error. A 404 is delegated to
// rubyGemsVersion404Error for disambiguation. 429/5xx are transient, not "not found".
func rubyGemsFetchStatusError(ctx context.Context, client *http.Client, pkg model.Package, status int) error {
	switch {
	case status == http.StatusNotFound:
		return rubyGemsVersion404Error(ctx, client, pkg)
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("RubyGems.org rate-limited the metadata request for package '%s' (status: 429). Likely transient, retry later", pkg.Identifier)
	case status >= 500 && status < 600:
		return fmt.Errorf("RubyGems.org upstream error fetching metadata for package '%s' (status: %d). Likely transient, retry later", pkg.Identifier, status)
	default:
		return fmt.Errorf("RubyGems package '%s' metadata fetch failed (status: %d)", pkg.Identifier, status)
	}
}

// rubyGemsVersion404Error disambiguates a 404 from the version-specific
// endpoint: a genuinely-missing gem versus a gem that exists but whose requested
// version is absent. The latter has two common causes: a freshly published
// release that has not propagated yet, or a yanked version (this endpoint 404s
// yanked versions). It probes the gem-level endpoint so the publisher gets an
// actionable message rather than a blanket "not found".
func rubyGemsVersion404Error(ctx context.Context, client *http.Client, pkg model.Package) error {
	switch probeRubyGemsGem(ctx, client, pkg.RegistryBaseURL, pkg.Identifier) {
	case rubyGemsGemExists:
		return fmt.Errorf("RubyGems package '%s' exists, but version '%s' was not found (status: 404). A newly published release can take a moment to appear, and yanked versions are not served. Wait and retry, or publish version '%s' before registering it", pkg.Identifier, pkg.Version, pkg.Version)
	case rubyGemsGemMissing:
		return fmt.Errorf("RubyGems package '%s' not found (status: 404)", pkg.Identifier)
	case rubyGemsGemTransient:
		return fmt.Errorf("RubyGems.org could not confirm package '%s' version '%s' (version status: 404, package check inconclusive). Likely transient, retry later", pkg.Identifier, pkg.Version)
	case rubyGemsGemUnknown:
		// Probe returned an unclassifiable status, so fall through to the
		// best-effort message below.
	}
	return fmt.Errorf("RubyGems package '%s' version '%s' not found (status: 404)", pkg.Identifier, pkg.Version)
}

// probeRubyGemsGem checks whether a gem exists on RubyGems.org regardless of
// version, with a HEAD request to the gem-level endpoint
// (/api/v1/versions/{name}.json). Only the status code is used, so HEAD avoids
// downloading the (potentially large) full version list.
func probeRubyGemsGem(ctx context.Context, client *http.Client, baseURL, identifier string) rubyGemsGemState {
	// The probe only refines the 404 error message, so it must not extend the
	// validator's worst case by another full client timeout.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	probeURL := fmt.Sprintf("%s/api/v1/versions/%s.json", baseURL, url.PathEscape(identifier))
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, probeURL, nil)
	if err != nil {
		return rubyGemsGemUnknown
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return rubyGemsGemTransient
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		return rubyGemsGemExists
	case resp.StatusCode == http.StatusNotFound:
		return rubyGemsGemMissing
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500 && resp.StatusCode < 600:
		return rubyGemsGemTransient
	default:
		return rubyGemsGemUnknown
	}
}
