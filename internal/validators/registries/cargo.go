package registries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

var (
	ErrMissingIdentifierForCargo = errors.New("package identifier is required for Cargo packages")
	ErrMissingVersionForCargo    = errors.New("package version is required for Cargo packages")
)

// CargoReadmeMetaResponse is the structure returned by the crates.io readme metadata endpoint.
//
// With `Accept: application/json`, crates.io's /api/v1/crates/{name}/{version}/readme
// endpoint returns 200 OK with a JSON body containing a `url` field that points to the
// rendered README on the static CDN. (Without the Accept header, or via HEAD, the same
// endpoint emits a 302 redirect to the CDN URL — the validator uses the JSON path so
// that crates.io controls where the README lives.) Validators must follow the pointer
// to retrieve the actual README content.
type CargoReadmeMetaResponse struct {
	URL string `json:"url"`
}

// ValidateCargo validates that a Cargo (crates.io) package contains the correct MCP server name.
//
// Verification mechanism: the `mcp-name: <server-name>` token is searched for in the package's
// rendered README. This mirrors the PyPI validator's README-token approach (see ValidatePyPI),
// requiring no Cargo.toml parsing on the registry side. Crate authors add a single line
// `mcp-name: io.github.OWNER/REPO` to their README before publishing.
//
// Two-call retrieval pattern:
//  1. GET https://crates.io/api/v1/crates/{name}/{version}/readme
//     → 200 OK with JSON: {"url": "https://static.crates.io/readmes/.../...html"}
//  2. GET <url from step 1>
//     → 200 OK with rendered README HTML, or 403 if the crate/version is missing
//
// The two-call pattern stays on the documented crates.io API surface rather than relying
// on the CDN URL layout being stable.
func ValidateCargo(ctx context.Context, pkg model.Package, serverName string) error {
	// Set default registry base URL if empty
	if pkg.RegistryBaseURL == "" {
		pkg.RegistryBaseURL = model.RegistryURLCrates
	}

	if pkg.Identifier == "" {
		return ErrMissingIdentifierForCargo
	}

	if pkg.Version == "" {
		return ErrMissingVersionForCargo
	}

	// Validate that MCPB-specific fields are not present
	if pkg.FileSHA256 != "" {
		return fmt.Errorf("cargo packages must not have 'fileSha256' field - this is only for MCPB packages")
	}

	// Validate that the registry base URL matches crates.io exactly
	if pkg.RegistryBaseURL != model.RegistryURLCrates {
		return fmt.Errorf("registry type and base URL do not match: '%s' is not valid for registry type '%s'. Expected: %s",
			pkg.RegistryBaseURL, model.RegistryTypeCargo, model.RegistryURLCrates)
	}

	return validateCargoREADME(ctx, pkg, serverName)
}

// validateCargoREADME performs the two-call README fetch and the mcp-name token
// check. It is split out from ValidateCargo so that httptest-based tests can
// drive the HTTP pipeline against a mock server (exposed via export_test.go),
// bypassing the exact-baseURL guard that ValidateCargo enforces for callers.
func validateCargoREADME(ctx context.Context, pkg model.Package, serverName string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	// crates.io's crawler policy expects a non-generic User-Agent identifying the source.
	userAgent := "MCP-Registry-Validator/1.0 (https://registry.modelcontextprotocol.io)"

	// Step 1: fetch the README pointer from the documented API endpoint.
	metaURL := fmt.Sprintf("%s/api/v1/crates/%s/%s/readme",
		pkg.RegistryBaseURL,
		url.PathEscape(pkg.Identifier),
		url.PathEscape(pkg.Version))

	metaReq, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create crates.io metadata request: %w", err)
	}
	metaReq.Header.Set("User-Agent", userAgent)
	metaReq.Header.Set("Accept", "application/json")

	metaResp, err := client.Do(metaReq)
	if err != nil {
		return fmt.Errorf("failed to fetch package metadata from crates.io: %w", err)
	}
	defer metaResp.Body.Close()

	if metaResp.StatusCode != http.StatusOK {
		// 5xx from the metadata endpoint is upstream availability, not a missing crate.
		if metaResp.StatusCode >= 500 && metaResp.StatusCode < 600 {
			return fmt.Errorf("crates.io upstream error fetching metadata for cargo package '%s' (status: %d) — likely transient, retry later", pkg.Identifier, metaResp.StatusCode)
		}
		return fmt.Errorf("cargo package '%s' metadata fetch failed (status: %d)", pkg.Identifier, metaResp.StatusCode)
	}

	var meta CargoReadmeMetaResponse
	if err := json.NewDecoder(metaResp.Body).Decode(&meta); err != nil {
		return fmt.Errorf("failed to parse crates.io readme metadata: %w", err)
	}
	if meta.URL == "" {
		return fmt.Errorf("cargo package '%s' metadata response missing 'url' field", pkg.Identifier)
	}

	// Step 2: fetch the rendered README from the URL the API gave us.
	readmeReq, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to create crates.io readme request: %w", err)
	}
	readmeReq.Header.Set("User-Agent", userAgent)
	readmeReq.Header.Set("Accept", "text/html")

	readmeResp, err := client.Do(readmeReq)
	if err != nil {
		return fmt.Errorf("failed to fetch rendered README from crates.io: %w", err)
	}
	defer readmeResp.Body.Close()

	// Missing crates and missing versions surface as 403 from static.crates.io
	// (S3's default for missing keys), not 404. 5xx from the CDN is upstream
	// availability — surface it as transient so callers can distinguish retryable
	// failures from genuinely missing crates.
	if readmeResp.StatusCode != http.StatusOK {
		if readmeResp.StatusCode >= 500 && readmeResp.StatusCode < 600 {
			return fmt.Errorf("crates.io upstream error fetching README for cargo package '%s' version '%s' (status: %d) — likely transient, retry later", pkg.Identifier, pkg.Version, readmeResp.StatusCode)
		}
		return fmt.Errorf("cargo package '%s' version '%s' not found on crates.io (status: %d)", pkg.Identifier, pkg.Version, readmeResp.StatusCode)
	}

	body, err := io.ReadAll(readmeResp.Body)
	if err != nil {
		return fmt.Errorf("failed to read rendered README: %w", err)
	}

	// Search for the mcp-name: <server-name> token. The token contains no characters
	// that get HTML-escaped during README rendering (no <, >, &, ", '), so a direct
	// substring match against the rendered HTML is reliable.
	mcpNamePattern := "mcp-name: " + serverName
	if strings.Contains(string(body), mcpNamePattern) {
		return nil
	}

	return fmt.Errorf("cargo package '%s' ownership validation failed. The server name '%s' must appear as 'mcp-name: %s' in the package README", pkg.Identifier, serverName, serverName)
}
