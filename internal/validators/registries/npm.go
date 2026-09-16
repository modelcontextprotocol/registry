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
	ErrMissingIdentifierForNPM = errors.New("package identifier is required for NPM packages")
	ErrMissingVersionForNPM    = errors.New("package version is required for NPM packages")
)

// NPMPackageResponse represents the structure returned by the NPM registry API
type NPMPackageResponse struct {
	MCPName string `json:"mcpName"`
}

// ValidateNPM validates that an NPM package contains the correct MCP server name
func ValidateNPM(ctx context.Context, pkg model.Package, serverName string) error {
	// Set default registry base URL if empty
	if pkg.RegistryBaseURL == "" {
		pkg.RegistryBaseURL = model.RegistryURLNPM
	}

	if pkg.Identifier == "" {
		return ErrMissingIdentifierForNPM
	}

	// we need version to look up the package metadata
	// not providing version will return all the versions
	// and we won't be able to validate the mcpName field
	// against the server name
	if pkg.Version == "" {
		return ErrMissingVersionForNPM
	}

	// Validate that MCPB-specific fields are not present
	if pkg.FileSHA256 != "" {
		return fmt.Errorf("NPM packages must not have 'fileSha256' field")
	}

	// Validate that the registry base URL matches NPM exactly
	if pkg.RegistryBaseURL != model.RegistryURLNPM {
		return fmt.Errorf("registry type and base URL do not match: '%s' is not valid for registry type '%s'. Expected: %s",
			pkg.RegistryBaseURL, model.RegistryTypeNPM, model.RegistryURLNPM)
	}

	return validateNPMPackage(ctx, pkg, serverName)
}

// validateNPMPackage performs the version-metadata fetch and the mcpName check.
// It is split out from ValidateNPM so that httptest-based tests can drive the HTTP
// pipeline against a mock server (exposed via export_test.go), bypassing the
// exact-baseURL guard that ValidateNPM enforces for callers.
func validateNPMPackage(ctx context.Context, pkg model.Package, serverName string) error {
	client := &http.Client{Timeout: 10 * time.Second}

	requestURL := pkg.RegistryBaseURL + "/" + url.PathEscape(pkg.Identifier) + "/" + url.PathEscape(pkg.Version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch package metadata from NPM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return npmFetchStatusError(ctx, client, pkg, resp.StatusCode)
	}

	var npmResp NPMPackageResponse
	if err := json.NewDecoder(resp.Body).Decode(&npmResp); err != nil {
		return fmt.Errorf("failed to parse NPM package metadata: %w", err)
	}

	if npmResp.MCPName == "" {
		return fmt.Errorf("NPM package '%s' is missing required 'mcpName' field. Add this to your package.json: \"mcpName\": \"%s\"", pkg.Identifier, serverName)
	}

	if npmResp.MCPName != serverName {
		return fmt.Errorf("NPM package ownership validation failed. Expected mcpName '%s', got '%s'", serverName, npmResp.MCPName)
	}

	return nil
}

// npmPackageState is the outcome of probing the package-level NPM metadata
// endpoint, used to disambiguate a 404 from the version-specific endpoint.
type npmPackageState int

const (
	// npmPackageUnknown: the probe returned a status we can't classify.
	npmPackageUnknown npmPackageState = iota
	// npmPackageExists: the package exists (200) but the requested version does not.
	npmPackageExists
	// npmPackageMissing: the package itself does not exist (404).
	npmPackageMissing
	// npmPackageTransient: the probe failed for a retryable reason (network error,
	// 429, or 5xx). Existence is undetermined and the caller should not report
	// "not found".
	npmPackageTransient
)

// npmFetchStatusError maps a non-200 status from the version-specific metadata
// endpoint to a caller-actionable error. A 404 is delegated to npmVersion404Error
// for disambiguation. 429/5xx are transient, not "not found".
func npmFetchStatusError(ctx context.Context, client *http.Client, pkg model.Package, status int) error {
	switch {
	case status == http.StatusNotFound:
		return npmVersion404Error(ctx, client, pkg)
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("NPM rate-limited the metadata request for package '%s' (status: 429). Likely transient, retry later", pkg.Identifier)
	case status >= 500 && status < 600:
		return fmt.Errorf("NPM upstream error fetching metadata for package '%s' (status: %d). Likely transient, retry later", pkg.Identifier, status)
	default:
		return fmt.Errorf("NPM package '%s' metadata fetch failed (status: %d)", pkg.Identifier, status)
	}
}

// npmVersion404Error disambiguates a 404 from the version-specific endpoint: a
// genuinely-missing package versus a package that exists but whose requested
// version is absent (commonly because a freshly published release has not yet
// propagated). It probes the package-level endpoint so the publisher gets an
// actionable message rather than a blanket "not found".
func npmVersion404Error(ctx context.Context, client *http.Client, pkg model.Package) error {
	switch probeNPMPackage(ctx, client, pkg.RegistryBaseURL, pkg.Identifier) {
	case npmPackageExists:
		return fmt.Errorf("NPM package '%s' exists, but version '%s' was not found (status: 404). A newly published release can take a moment to appear on the registry. Wait and retry, or publish version '%s' before registering it", pkg.Identifier, pkg.Version, pkg.Version)
	case npmPackageMissing:
		return fmt.Errorf("NPM package '%s' not found (status: 404)", pkg.Identifier)
	case npmPackageTransient:
		return fmt.Errorf("NPM could not confirm package '%s' version '%s' (version status: 404, package check inconclusive). Likely transient, retry later", pkg.Identifier, pkg.Version)
	case npmPackageUnknown:
		// Probe returned an unclassifiable status, so fall through to the
		// best-effort message below.
	}
	return fmt.Errorf("NPM package '%s' version '%s' not found (status: 404)", pkg.Identifier, pkg.Version)
}

// probeNPMPackage checks whether a package exists on the NPM registry regardless
// of version, with a HEAD request to the package-level endpoint (/{name}). Only
// the status code is used, so HEAD avoids downloading the (large) packument.
func probeNPMPackage(ctx context.Context, client *http.Client, baseURL, identifier string) npmPackageState {
	// The probe only refines the 404 error message, so it must not extend the
	// validator's worst case by another full client timeout.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	probeURL := baseURL + "/" + url.PathEscape(identifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, probeURL, nil)
	if err != nil {
		return npmPackageUnknown
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return npmPackageTransient
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		return npmPackageExists
	case resp.StatusCode == http.StatusNotFound:
		return npmPackageMissing
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500 && resp.StatusCode < 600:
		return npmPackageTransient
	default:
		return npmPackageUnknown
	}
}
