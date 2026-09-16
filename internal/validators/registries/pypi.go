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
	ErrMissingIdentifierForPyPI = errors.New("package identifier is required for PyPI packages")
	ErrMissingVersionForPyPi    = errors.New("package version is required for PyPI packages")
)

// PyPIPackageResponse represents the structure returned by the PyPI JSON API
type PyPIPackageResponse struct {
	Info struct {
		Description string `json:"description"`
	} `json:"info"`
}

// ValidatePyPI validates that a PyPI package contains the correct MCP server name
func ValidatePyPI(ctx context.Context, pkg model.Package, serverName string) error {
	// Set default registry base URL if empty
	if pkg.RegistryBaseURL == "" {
		pkg.RegistryBaseURL = model.RegistryURLPyPI
	}

	if pkg.Identifier == "" {
		return ErrMissingIdentifierForPyPI
	}

	if pkg.Version == "" {
		return ErrMissingVersionForPyPi
	}

	// Validate that MCPB-specific fields are not present
	if pkg.FileSHA256 != "" {
		return fmt.Errorf("PyPI packages must not have 'fileSha256' field - this is only for MCPB packages")
	}

	// Validate that the registry base URL matches PyPI exactly
	if pkg.RegistryBaseURL != model.RegistryURLPyPI {
		return fmt.Errorf("registry type and base URL do not match: '%s' is not valid for registry type '%s'. Expected: %s",
			pkg.RegistryBaseURL, model.RegistryTypePyPI, model.RegistryURLPyPI)
	}

	return validatePyPIPackage(ctx, pkg, serverName)
}

// validatePyPIPackage performs the version-metadata fetch and the mcp-name token
// check. It is split out from ValidatePyPI so that httptest-based tests can drive
// the HTTP pipeline against a mock server (exposed via export_test.go), bypassing
// the exact-baseURL guard that ValidatePyPI enforces for callers.
func validatePyPIPackage(ctx context.Context, pkg model.Package, serverName string) error {
	client := &http.Client{Timeout: 10 * time.Second}

	// PathEscape so an identifier that smuggles "/" or ".." cannot redirect
	// the metadata fetch to a different package than the one being claimed.
	fetchURL := fmt.Sprintf("%s/pypi/%s/%s/json",
		pkg.RegistryBaseURL,
		url.PathEscape(pkg.Identifier),
		url.PathEscape(pkg.Version))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch package metadata from PyPI: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return pypiFetchStatusError(ctx, client, pkg, resp.StatusCode)
	}

	var pypiResp PyPIPackageResponse
	if err := json.NewDecoder(resp.Body).Decode(&pypiResp); err != nil {
		return fmt.Errorf("failed to parse PyPI package metadata: %w", err)
	}

	// Check description (README) content
	description := pypiResp.Info.Description

	// Check for the mcp-name: <server-name> ownership token (boundary-anchored to
	// avoid prefix confusion — see containsMCPNameToken).
	if containsMCPNameToken(description, serverName) {
		return nil
	}

	// If the token IS present but glued to a trailing character, say so — otherwise
	// the publisher sees "must appear as mcp-name: X" while looking at exactly that.
	if trailing, glued := mcpNameTokenGluedTrailing(description, serverName); glued {
		return fmt.Errorf("PyPI package '%s' ownership validation failed: found 'mcp-name: %s' in the README, but it is immediately followed by %q rather than a boundary. The token must be followed by a space, newline, an HTML tag, or a comment close ('-->') — put it on its own line and republish", pkg.Identifier, serverName, trailing)
	}

	return fmt.Errorf("PyPI package '%s' ownership validation failed. The server name '%s' must appear as 'mcp-name: %s' in the package README", pkg.Identifier, serverName, serverName)
}

// pypiPackageState is the outcome of probing the package-level PyPI metadata
// endpoint, used to disambiguate a 404 from the version-specific endpoint.
type pypiPackageState int

const (
	// pypiPackageUnknown: the probe returned a status we can't classify.
	pypiPackageUnknown pypiPackageState = iota
	// pypiPackageExists: the package exists (200) but the requested version does not.
	pypiPackageExists
	// pypiPackageMissing: the package itself does not exist (404).
	pypiPackageMissing
	// pypiPackageTransient: the probe failed for a retryable reason (network error,
	// 429, or 5xx). Existence is undetermined and the caller should not report
	// "not found".
	pypiPackageTransient
)

// pypiFetchStatusError maps a non-200 status from the version-specific metadata
// endpoint to a caller-actionable error. A 404 is delegated to pypiVersion404Error
// for disambiguation. 429/5xx are transient, not "not found".
func pypiFetchStatusError(ctx context.Context, client *http.Client, pkg model.Package, status int) error {
	switch {
	case status == http.StatusNotFound:
		return pypiVersion404Error(ctx, client, pkg)
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("PyPI rate-limited the metadata request for package '%s' (status: 429). Likely transient, retry later", pkg.Identifier)
	case status >= 500 && status < 600:
		return fmt.Errorf("PyPI upstream error fetching metadata for package '%s' (status: %d). Likely transient, retry later", pkg.Identifier, status)
	default:
		return fmt.Errorf("PyPI package '%s' metadata fetch failed (status: %d)", pkg.Identifier, status)
	}
}

// pypiVersion404Error disambiguates a 404 from the version-specific endpoint: a
// genuinely-missing package versus a package that exists but whose requested
// version is absent (commonly because a freshly published release has not yet
// propagated). It probes the package-level endpoint so the publisher gets an
// actionable message rather than a blanket "not found".
func pypiVersion404Error(ctx context.Context, client *http.Client, pkg model.Package) error {
	switch probePyPIPackage(ctx, client, pkg.RegistryBaseURL, pkg.Identifier) {
	case pypiPackageExists:
		return fmt.Errorf("PyPI package '%s' exists, but version '%s' was not found (status: 404). A newly published release can take a moment to appear on PyPI. Wait and retry, or publish version '%s' before registering it", pkg.Identifier, pkg.Version, pkg.Version)
	case pypiPackageMissing:
		return fmt.Errorf("PyPI package '%s' not found (status: 404)", pkg.Identifier)
	case pypiPackageTransient:
		return fmt.Errorf("PyPI could not confirm package '%s' version '%s' (version status: 404, package check inconclusive). Likely transient, retry later", pkg.Identifier, pkg.Version)
	case pypiPackageUnknown:
		// Probe returned an unclassifiable status, so fall through to the
		// best-effort message below.
	}
	return fmt.Errorf("PyPI package '%s' version '%s' not found (status: 404)", pkg.Identifier, pkg.Version)
}

// probePyPIPackage checks whether a package exists on PyPI regardless of version,
// with a HEAD request to the package-level endpoint (/pypi/{name}/json). Only the
// status code is used, so HEAD avoids downloading the (large) all-versions body.
func probePyPIPackage(ctx context.Context, client *http.Client, baseURL, identifier string) pypiPackageState {
	// The probe only refines the 404 error message, so it must not extend the
	// validator's worst case by another full client timeout.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	probeURL := fmt.Sprintf("%s/pypi/%s/json", baseURL, url.PathEscape(identifier))
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, probeURL, nil)
	if err != nil {
		return pypiPackageUnknown
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return pypiPackageTransient
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		return pypiPackageExists
	case resp.StatusCode == http.StatusNotFound:
		return pypiPackageMissing
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500 && resp.StatusCode < 600:
		return pypiPackageTransient
	default:
		return pypiPackageUnknown
	}
}
