package registries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

var (
	ErrMissingIdentifierForCargo = errors.New("package identifier is required for Cargo packages")
	ErrMissingVersionForCargo    = errors.New("package version is required for Cargo packages")
)

// cargoUserAgent identifies the validator to crates.io. crates.io's crawler
// policy expects a non-generic User-Agent with a contact URL; a bare UA may be
// rate-limited or blocked. (Distinct from the package-level userAgent constant
// used by the NuGet validator, which has no contact URL.)
const cargoUserAgent = "MCP-Registry-Validator/1.0 (https://registry.modelcontextprotocol.io)"

// cargoStaticHost is the CDN host crates.io serves rendered READMEs from.
const cargoStaticHost = "static.crates.io"

// maxCargoReadmeBytes caps how much of a rendered README we buffer, so a hostile
// or oversized response cannot exhaust validator memory.
const maxCargoReadmeBytes = 5 << 20 // 5 MiB

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

// cargoAllowedHosts returns the set of hosts the validator is permitted to talk
// to for a given base URL. For the real crates.io base this is crates.io (the API)
// plus static.crates.io (the rendered-README CDN). For any other base — only the
// httptest-driven tests, since the public ValidateCargo pins the base to
// crates.io — it is the base host itself, so mock servers keep working.
//
// This is the allowlist enforced both on the README pointer (step 2 URL) and on
// every redirect hop, so a metadata response or redirect cannot steer the
// validator at an internal or attacker-chosen host (SSRF).
func cargoAllowedHosts(baseURL string) map[string]struct{} {
	hosts := map[string]struct{}{}
	if u, err := url.Parse(baseURL); err == nil && u.Hostname() != "" {
		hosts[u.Hostname()] = struct{}{}
	}
	if baseURL == model.RegistryURLCrates {
		hosts[cargoStaticHost] = struct{}{}
	}
	return hosts
}

// newCargoHTTPClient builds the client used for all crates.io calls. The
// CheckRedirect policy pins every redirect hop to allowedHosts, so even though
// the initial URL is host-pinned, an upstream 3xx cannot redirect the validator
// to an unexpected host.
func newCargoHTTPClient(allowedHosts map[string]struct{}) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if _, ok := allowedHosts[req.URL.Hostname()]; !ok {
				return fmt.Errorf("refusing redirect to unexpected host %q", req.URL.Hostname())
			}
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			return nil
		},
	}
}

// cargoVersionExists checks whether a specific crate version exists on crates.io,
// used to disambiguate a 403 from the README CDN. static.crates.io (S3) returns
// 403 both for a genuinely-missing crate/version AND for a crate that exists but
// has no rendered README, so a 403 alone cannot tell a publisher which it is.
//
// Returns (exists, determined): determined is false if the existence endpoint
// itself was unreachable or returned an unexpected status, in which case the
// caller should fall back to a generic message rather than assert existence.
func cargoVersionExists(ctx context.Context, client *http.Client, baseURL, identifier, version string) (exists, determined bool) {
	versionURL := fmt.Sprintf("%s/api/v1/crates/%s/%s",
		baseURL, url.PathEscape(identifier), url.PathEscape(version))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, versionURL, nil)
	if err != nil {
		return false, false
	}
	req.Header.Set("User-Agent", cargoUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, true
	case http.StatusNotFound:
		return false, true
	default:
		return false, false
	}
}

// cargoMetadataStatusError maps a non-200 status from the metadata endpoint to a
// caller-actionable error, distinguishing transient upstream conditions (429,
// 5xx) from a genuine fetch failure.
func cargoMetadataStatusError(identifier string, status int) error {
	switch {
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("crates.io rate-limited the metadata request for cargo package '%s' (status: 429) — likely transient, retry later", identifier)
	case status >= 500 && status < 600:
		return fmt.Errorf("crates.io upstream error fetching metadata for cargo package '%s' (status: %d) — likely transient, retry later", identifier, status)
	default:
		return fmt.Errorf("cargo package '%s' metadata fetch failed (status: %d)", identifier, status)
	}
}

// cargoReadmeStatusError maps a non-200 status from the README CDN to a
// caller-actionable error. 429/5xx are transient; 403 is disambiguated (see
// cargoReadme403Error) because static.crates.io returns it both for a missing
// crate/version and for a crate that has no rendered README.
func cargoReadmeStatusError(ctx context.Context, client *http.Client, pkg model.Package, serverName string, status int) error {
	switch {
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("crates.io rate-limited the README fetch for cargo package '%s' version '%s' (status: 429) — likely transient, retry later", pkg.Identifier, pkg.Version)
	case status >= 500 && status < 600:
		return fmt.Errorf("crates.io upstream error fetching README for cargo package '%s' version '%s' (status: %d) — likely transient, retry later", pkg.Identifier, pkg.Version, status)
	case status == http.StatusForbidden:
		return cargoReadme403Error(ctx, client, pkg, serverName)
	default:
		return fmt.Errorf("cargo package '%s' version '%s' README fetch failed (status: %d)", pkg.Identifier, pkg.Version, status)
	}
}

// cargoReadme403Error disambiguates a 403 from static.crates.io (S3's default
// for a missing key): a genuinely-missing crate/version versus a crate that
// exists but has no rendered README. It probes the crate-version metadata
// endpoint so the publisher gets an actionable message rather than a blanket
// "not found".
func cargoReadme403Error(ctx context.Context, client *http.Client, pkg model.Package, serverName string) error {
	exists, determined := cargoVersionExists(ctx, client, pkg.RegistryBaseURL, pkg.Identifier, pkg.Version)
	switch {
	case determined && exists:
		return fmt.Errorf("cargo package '%s' version '%s' exists on crates.io but has no rendered README. Add a README containing 'mcp-name: %s' and publish a new version", pkg.Identifier, pkg.Version, serverName)
	case determined && !exists:
		return fmt.Errorf("cargo package '%s' version '%s' not found on crates.io", pkg.Identifier, pkg.Version)
	default:
		return fmt.Errorf("cargo package '%s' version '%s' not found on crates.io (status: 403)", pkg.Identifier, pkg.Version)
	}
}

// validateCargoREADME performs the two-call README fetch and the mcp-name token
// check. It is split out from ValidateCargo so that httptest-based tests can
// drive the HTTP pipeline against a mock server (exposed via export_test.go),
// bypassing the exact-baseURL guard that ValidateCargo enforces for callers.
func validateCargoREADME(ctx context.Context, pkg model.Package, serverName string) error {
	allowedHosts := cargoAllowedHosts(pkg.RegistryBaseURL)
	client := newCargoHTTPClient(allowedHosts)

	// Step 1: fetch the README pointer from the documented API endpoint.
	metaURL := fmt.Sprintf("%s/api/v1/crates/%s/%s/readme",
		pkg.RegistryBaseURL,
		url.PathEscape(pkg.Identifier),
		url.PathEscape(pkg.Version))

	metaReq, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create crates.io metadata request: %w", err)
	}
	metaReq.Header.Set("User-Agent", cargoUserAgent)
	metaReq.Header.Set("Accept", "application/json")

	metaResp, err := client.Do(metaReq)
	if err != nil {
		return fmt.Errorf("failed to fetch package metadata from crates.io: %w", err)
	}
	defer metaResp.Body.Close()

	if metaResp.StatusCode != http.StatusOK {
		return cargoMetadataStatusError(pkg.Identifier, metaResp.StatusCode)
	}

	var meta CargoReadmeMetaResponse
	if err := json.NewDecoder(metaResp.Body).Decode(&meta); err != nil {
		return fmt.Errorf("failed to parse crates.io readme metadata: %w", err)
	}
	if meta.URL == "" {
		return fmt.Errorf("cargo package '%s' metadata response missing 'url' field", pkg.Identifier)
	}

	// Pin the README pointer to an allowed host before fetching it, so a metadata
	// response cannot steer the validator at an internal or attacker-chosen host.
	readmeParsed, err := url.Parse(meta.URL)
	if err != nil || readmeParsed.Hostname() == "" {
		return fmt.Errorf("cargo package '%s': crates.io returned an unparseable README URL", pkg.Identifier)
	}
	if _, ok := allowedHosts[readmeParsed.Hostname()]; !ok {
		return fmt.Errorf("cargo package '%s': crates.io returned a README URL on unexpected host %q — refusing to fetch", pkg.Identifier, readmeParsed.Hostname())
	}

	// Step 2: fetch the rendered README from the (now host-validated) URL.
	readmeReq, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to create crates.io readme request: %w", err)
	}
	readmeReq.Header.Set("User-Agent", cargoUserAgent)
	readmeReq.Header.Set("Accept", "text/html")

	readmeResp, err := client.Do(readmeReq)
	if err != nil {
		return fmt.Errorf("failed to fetch rendered README from crates.io: %w", err)
	}
	defer readmeResp.Body.Close()

	if readmeResp.StatusCode != http.StatusOK {
		return cargoReadmeStatusError(ctx, client, pkg, serverName, readmeResp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(readmeResp.Body, maxCargoReadmeBytes))
	if err != nil {
		return fmt.Errorf("failed to read rendered README: %w", err)
	}

	// Search for the mcp-name: <server-name> ownership token. The token contains no
	// characters that get HTML-escaped during README rendering (no <, >, &, ", '),
	// so matching against the rendered HTML is reliable; containsMCPNameToken
	// additionally requires a trailing boundary to avoid prefix confusion.
	if containsMCPNameToken(string(body), serverName) {
		return nil
	}

	return fmt.Errorf("cargo package '%s' ownership validation failed. The server name '%s' must appear as 'mcp-name: %s' in the package README", pkg.Identifier, serverName, serverName)
}
