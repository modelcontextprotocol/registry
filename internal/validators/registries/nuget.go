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
	"sync"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

var (
	ErrMissingIdentifierForNuget = errors.New("package identifier is required for NuGet packages")
	ErrMissingVersionForNuget    = errors.New("package version is required for NuGet packages")
)

const userAgent = "MCP-Registry-Validator/1.0"

// maxNuGetReadmeBytes caps how much of a package README we buffer, so a hostile
// or oversized response cannot exhaust validator memory. Mirrors the cargo
// validator's maxCargoReadmeBytes.
const maxNuGetReadmeBytes = 5 << 20 // 5 MiB

type cachedServiceIndex struct {
	index     *serviceIndex
	expiresAt time.Time
}

var (
	serviceIndexCache = make(map[string]*cachedServiceIndex)
	cacheMu           sync.RWMutex
	cacheDuration     = 1 * time.Hour
)

type serviceIndexResource struct {
	ID   string `json:"@id"`
	Type string `json:"@type"`
}

type serviceIndex struct {
	Resources []serviceIndexResource `json:"resources"`
}

type packageContentIndex struct {
	Versions []string `json:"versions"`
}

type ReadmeState int

const (
	ValidReadme ReadmeState = iota
	InvalidReadme
	// GluedReadme: the literal token is present but glued to a trailing character
	// (so the boundary-anchored match rejected it as a prefix of a longer name).
	GluedReadme
	NoReadme
)

type PackageExistenceState int

const (
	PackageAndVersionExist PackageExistenceState = iota
	PackageExistsVersionMissing
	PackageIDNotFound
)

// ValidateNuGet validates that a NuGet package contains the correct MCP server name
func ValidateNuGet(ctx context.Context, pkg model.Package, serverName string) error {
	err := validateAndNormalizeBaseURL(&pkg)
	if err != nil {
		return err
	}

	if pkg.Version == "" {
		return ErrMissingVersionForNuget
	}

	client := newNuGetHTTPClient(pkg.RegistryBaseURL)

	// Fetch the service serviceIndex
	serviceIndex, err := fetchAndCacheServiceIndex(ctx, client, pkg.RegistryBaseURL)
	if err != nil {
		return err
	}

	lowerID := strings.ToLower(pkg.Identifier)
	lowerVersion := strings.ToLower(pkg.Version)

	// remove any SemVer 2.0.0 build metadata suffix (e.g., +abc)
	if i := strings.Index(lowerVersion, "+"); i >= 0 {
		lowerVersion = lowerVersion[:i]
	}

	status, err := validateReadme(ctx, serverName, lowerID, lowerVersion, client, serviceIndex)
	if err != nil {
		return err
	}

	switch status {
	case ValidReadme:
		return nil
	case InvalidReadme:
		return fmt.Errorf("NuGet package '%s' ownership validation for version %s failed. The server name '%s' must appear as 'mcp-name: %s' in the package README. Add it to your README and publish a new package version", pkg.Identifier, pkg.Version, serverName, serverName)
	case GluedReadme:
		return fmt.Errorf("NuGet package '%s' ownership validation for version %s failed: found 'mcp-name: %s' in the README, but it is immediately followed by another character rather than a boundary. The token must be followed by a space, newline, an HTML tag, or a comment close ('-->') — put it on its own line and publish a new package version", pkg.Identifier, pkg.Version, serverName)
	case NoReadme:
		// Continue to check if package exists
	default:
		return fmt.Errorf("unexpected readme state: %d", status)
	}

	existenceState, err := validatePackageExists(ctx, lowerID, lowerVersion, client, serviceIndex)
	if err != nil {
		return err
	}

	switch existenceState {
	case PackageIDNotFound:
		return fmt.Errorf("NuGet package '%s' does not exist in the registry. If you recently published the package for the first time, wait for validation to complete", pkg.Identifier)
	case PackageExistsVersionMissing:
		return fmt.Errorf("NuGet package '%s' exists but version %s does not exist in the registry. If you recently published the version, wait for validation to complete", pkg.Identifier, pkg.Version)
	case PackageAndVersionExist:
		return fmt.Errorf("NuGet package '%s' ownership validation for version %s failed because it does not have an embedded README. Add one to your package and publish a new version", pkg.Identifier, pkg.Version)
	default:
		return fmt.Errorf("unexpected package existence state: %d", existenceState)
	}
}

func validateAndNormalizeBaseURL(pkg *model.Package) error {
	if pkg.RegistryBaseURL == "" {
		pkg.RegistryBaseURL = model.RegistryURLNuGet
	}

	if pkg.Identifier == "" {
		return ErrMissingIdentifierForNuget
	}

	// Validate that MCPB-specific fields are not present
	if pkg.FileSHA256 != "" {
		return fmt.Errorf("NuGet packages must not have 'fileSha256' field - this is only for MCPB packages")
	}

	// Validate that the registry base URL matches NuGet exactly
	if pkg.RegistryBaseURL != model.RegistryURLNuGet {
		return fmt.Errorf("registry type and base URL do not match: '%s' is not valid for registry type '%s'. Expected: %s",
			pkg.RegistryBaseURL, model.RegistryTypeNuGet, model.RegistryURLNuGet)
	}

	return nil
}

// nugetRedirectAllowed reports whether a redirect to target is permitted, given
// the originating request. The target host must match the origin's host;
// additionally, for the real NuGet base, the scheme must not change from the
// origin's (which is https) and the port must stay default — so a redirect
// cannot downgrade the scheme or steer the validator at a non-standard port on
// an otherwise-trusted host. For test (httptest) bases only the host is checked,
// so mocks on http/loopback keep working.
//
// This is the NuGet analogue of cargoURLAllowed. NuGet has no separate README
// CDN host (the README URL comes from the trusted service-index template and is
// served from the same host), so the allowed host is the origin's own host
// rather than a static allow-list.
func nugetRedirectAllowed(target, origin *url.URL, baseURL string) bool {
	if target.Hostname() != origin.Hostname() {
		return false
	}
	if baseURL == model.RegistryURLNuGet {
		if target.Scheme != origin.Scheme {
			return false
		}
		if p := target.Port(); p != "" && p != "443" {
			return false
		}
	}
	return true
}

// newNuGetHTTPClient builds the client used for all NuGet calls (service index,
// README, and package index). Its CheckRedirect pins every redirect hop to the
// originating request's host (see nugetRedirectAllowed), so an upstream 3xx
// cannot steer the validator at an internal or attacker-chosen host (SSRF).
// NuGet serves these resources directly without cross-host redirects, so this is
// behaviour-preserving for real packages.
func newNuGetHTTPClient(baseURL string) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			// via is non-empty whenever net/http invokes CheckRedirect, so
			// via[0] is the originating request.
			if !nugetRedirectAllowed(req.URL, via[0].URL, baseURL) {
				return fmt.Errorf("refusing redirect to unexpected URL %q", req.URL.Redacted())
			}
			return nil
		},
	}
}

func fetchAndCacheServiceIndex(ctx context.Context, client *http.Client, baseURL string) (*serviceIndex, error) {
	cacheMu.RLock()
	if cached, exists := serviceIndexCache[baseURL]; exists {
		if time.Now().Before(cached.expiresAt) {
			cacheMu.RUnlock()
			return cached.index, nil
		}
	}
	cacheMu.RUnlock()

	cacheMu.Lock()
	defer cacheMu.Unlock()

	if cached, exists := serviceIndexCache[baseURL]; exists {
		if time.Now().Before(cached.expiresAt) {
			return cached.index, nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create NuGet service index request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch NuGet service index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("NuGet service index returned status %d", resp.StatusCode)
	}

	var index serviceIndex
	if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
		return nil, fmt.Errorf("failed to parse NuGet service index: %w", err)
	}

	serviceIndexCache[baseURL] = &cachedServiceIndex{
		index:     &index,
		expiresAt: time.Now().Add(cacheDuration),
	}

	return &index, nil
}

func getReadmeURLTemplate(index *serviceIndex) (string, error) {
	for _, resource := range index.Resources {
		if resource.Type == "ReadmeUriTemplate/6.13.0" {
			return resource.ID, nil
		}
	}

	return "", fmt.Errorf("ReadmeUriTemplate/6.13.0 not found in service index")
}

func validateReadme(ctx context.Context, serverName, lowerID, lowerVersion string, client *http.Client, index *serviceIndex) (ReadmeState, error) {
	readmeURLTemplate, err := getReadmeURLTemplate(index)
	if err != nil {
		return NoReadme, fmt.Errorf("failed to get README URL template: %w", err)
	}

	// Replace placeholders in the template. PathEscape both the id and version
	// so a publisher cannot smuggle "/" / ".." through the template into a
	// fetch against an unrelated package's README.
	readmeURL := strings.ReplaceAll(readmeURLTemplate, "{lower_id}", url.PathEscape(lowerID))
	readmeURL = strings.ReplaceAll(readmeURL, "{lower_version}", url.PathEscape(lowerVersion))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, readmeURL, nil)
	if err != nil {
		return NoReadme, fmt.Errorf("failed to create NuGet README request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return NoReadme, fmt.Errorf("failed to fetch NuGet README: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		// Check README content. Bound the read so a hostile or oversized
		// response cannot exhaust validator memory.
		readmeBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxNuGetReadmeBytes))
		if err != nil {
			return NoReadme, fmt.Errorf("failed to read NuGet README content: %w", err)
		}

		readmeContent := string(readmeBytes)

		// Check for the mcp-name: <server-name> ownership token (boundary-anchored
		// to avoid prefix confusion — see containsMCPNameToken).
		if containsMCPNameToken(readmeContent, serverName) {
			return ValidReadme, nil
		}
		if _, glued := mcpNameTokenGluedTrailing(readmeContent, serverName); glued {
			return GluedReadme, nil
		}

		return InvalidReadme, nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return NoReadme, nil
	}

	return InvalidReadme, fmt.Errorf("NuGet README request returned status %d", resp.StatusCode)
}

func getPackageContentBaseURL(index *serviceIndex) (string, error) {
	for _, resource := range index.Resources {
		if resource.Type == "PackageBaseAddress/3.0.0" {
			return resource.ID, nil
		}
	}

	return "", fmt.Errorf("PackageBaseAddress/3.0.0 not found in service index")
}

func validatePackageExists(ctx context.Context, lowerID, lowerVersion string, client *http.Client, index *serviceIndex) (PackageExistenceState, error) {
	packageBaseURL, err := getPackageContentBaseURL(index)
	if err != nil {
		return PackageIDNotFound, fmt.Errorf("failed to get Package Base URL: %w", err)
	}

	// Fetch the package content index to check if package ID and version exist.
	// PathEscape so an identifier that smuggles "/" or ".." cannot redirect
	// the metadata fetch to a different package than the one being claimed.
	indexURL := fmt.Sprintf("%s/%s/index.json", strings.TrimRight(packageBaseURL, "/"), url.PathEscape(lowerID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return PackageIDNotFound, fmt.Errorf("failed to create NuGet package index request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return PackageIDNotFound, fmt.Errorf("failed to fetch NuGet package index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return PackageIDNotFound, nil // Package ID does not exist
	}

	if resp.StatusCode != http.StatusOK {
		return PackageIDNotFound, fmt.Errorf("NuGet package index returned status %d", resp.StatusCode)
	}

	var contentIndex packageContentIndex
	if err := json.NewDecoder(resp.Body).Decode(&contentIndex); err != nil {
		return PackageIDNotFound, fmt.Errorf("failed to parse NuGet package index: %w", err)
	}

	// Check if the version exists in the versions list
	for _, v := range contentIndex.Versions {
		if strings.EqualFold(v, lowerVersion) {
			return PackageAndVersionExist, nil
		}
	}

	return PackageExistsVersionMissing, nil
}
