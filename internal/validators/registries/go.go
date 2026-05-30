package registries

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

var (
	ErrMissingIdentifierForGo = errors.New("package identifier (the module path, e.g. github.com/owner/repo) is required for Go packages")
	ErrMissingVersionForGo    = errors.New("package version is required for Go packages")
)

const (
	goGitHubNamespacePrefix = "io.github."
	goGitHubModulePrefix    = "github.com/"
)

// ValidateGo validates that a Go module package is owned by the publisher and
// exists on the Go module proxy.
//
// Ownership: a Go module path is its own source location, so for the v1
// GitHub-namespace model a server published under "io.github.<owner>" must use
// a module path rooted at "github.com/<owner>/...". The registry already
// authenticated the publisher for that GitHub namespace, so matching the owner
// segment proves ownership without an mcpName-style marker (which would not be
// idiomatic in a go.mod). Non-GitHub module paths are intentionally left for a
// follow-up.
//
// Existence: the module version is verified against the Go module proxy
// (proxy.golang.org), the canonical source of truth.
func ValidateGo(ctx context.Context, pkg model.Package, serverName string) error {
	if pkg.RegistryBaseURL == "" {
		pkg.RegistryBaseURL = model.RegistryURLGoProxy
	}
	if pkg.Identifier == "" {
		return ErrMissingIdentifierForGo
	}
	if pkg.Version == "" {
		return ErrMissingVersionForGo
	}
	// Validate that MCPB-specific fields are not present
	if pkg.FileSHA256 != "" {
		return fmt.Errorf("go packages must not have 'fileSha256' field - this is only for MCPB packages")
	}
	// Validate that the registry base URL matches the Go module proxy exactly
	if pkg.RegistryBaseURL != model.RegistryURLGoProxy {
		return fmt.Errorf("registry type and base URL do not match: '%s' is not valid for registry type '%s'. Expected: %s",
			pkg.RegistryBaseURL, model.RegistryTypeGo, model.RegistryURLGoProxy)
	}

	if err := ValidateGoModuleOwnership(pkg.Identifier, serverName); err != nil {
		return err
	}

	return validateGoModuleExists(ctx, pkg.RegistryBaseURL, pkg.Identifier, pkg.Version)
}

// ValidateGoModuleOwnership checks that the module path is rooted at the GitHub
// owner that owns the server's io.github.* namespace. Comparison is
// case-insensitive because GitHub owners are case-insensitive.
func ValidateGoModuleOwnership(modulePath, serverName string) error {
	namespace, _, found := strings.Cut(serverName, "/")
	if !found || !strings.HasPrefix(namespace, goGitHubNamespacePrefix) {
		return fmt.Errorf("go packages are currently only supported for 'io.github.*' server namespaces; '%s' is not supported", serverName)
	}
	owner := strings.ToLower(strings.TrimPrefix(namespace, goGitHubNamespacePrefix))

	lowerModule := strings.ToLower(modulePath)
	if !strings.HasPrefix(lowerModule, goGitHubModulePrefix) {
		return fmt.Errorf("go module path '%s' must be hosted on github.com to be published under '%s'", modulePath, namespace)
	}
	moduleOwner, _, _ := strings.Cut(lowerModule[len(goGitHubModulePrefix):], "/")
	if moduleOwner == "" {
		return fmt.Errorf("go module path '%s' is missing an owner segment", modulePath)
	}
	if moduleOwner != owner {
		return fmt.Errorf("go module ownership validation failed: server '%s' is owned by GitHub user '%s', but module path '%s' belongs to '%s'", serverName, owner, modulePath, moduleOwner)
	}
	return nil
}

// validateGoModuleExists confirms the module version is available on the Go
// module proxy.
func validateGoModuleExists(ctx context.Context, baseURL, modulePath, version string) error {
	escapedModule, err := EscapeGoModulePath(modulePath)
	if err != nil {
		return fmt.Errorf("invalid Go module path '%s': %w", modulePath, err)
	}
	escapedVersion, err := EscapeGoModulePath(version)
	if err != nil {
		return fmt.Errorf("invalid Go module version '%s': %w", version, err)
	}

	requestURL := fmt.Sprintf("%s/%s/@v/%s.info", strings.TrimRight(baseURL, "/"), escapedModule, escapedVersion)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "MCP-Registry-Validator/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch module metadata from the Go module proxy: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound, http.StatusGone:
		return fmt.Errorf("go module '%s' version '%s' was not found on the Go module proxy. If you published it recently, allow a few minutes for the proxy to fetch it", modulePath, version)
	default:
		return fmt.Errorf("go module proxy returned unexpected status %d for '%s@%s'", resp.StatusCode, modulePath, version)
	}
}

// EscapeGoModulePath applies the goproxy case-encoding: every uppercase ASCII
// letter is replaced by '!' followed by its lowercase form. It rejects control
// characters and "."/".." path elements so a crafted identifier cannot escape
// the proxy path.
func EscapeGoModulePath(s string) (string, error) {
	if s == "" {
		return "", errors.New("empty path")
	}
	for _, segment := range strings.Split(s, "/") {
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("contains a %q path element", segment)
		}
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteByte('!')
			b.WriteRune(r + ('a' - 'A'))
		case r < 0x20 || r == 0x7f:
			return "", errors.New("contains a control character")
		default:
			b.WriteRune(r)
		}
	}
	return b.String(), nil
}
