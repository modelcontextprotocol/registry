package registries

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

var (
	ErrMissingIdentifierForMCPB = fmt.Errorf("package identifier is required for MCPB packages")
	ErrMissingFileSHA256ForMCPB = fmt.Errorf("must include a fileSha256 hash for integrity verification")
)

func ValidateMCPB(ctx context.Context, pkg model.Package, _ string) error {
	// MCPB packages must include a file hash for integrity verification
	if pkg.FileSHA256 == "" {
		return ErrMissingFileSHA256ForMCPB
	}

	if pkg.Identifier == "" {
		return ErrMissingIdentifierForMCPB
	}

	// Validate that old format fields are not present
	// MCPB packages use URL in identifier, version is embedded in the URL
	if pkg.Version != "" {
		return fmt.Errorf("MCPB packages must not have 'version' field - version should be embedded in the download URL")
	}
	if pkg.RegistryBaseURL != "" {
		return fmt.Errorf("MCPB packages must not have 'registryBaseUrl' field - use the full download URL in 'identifier' instead")
	}

	err := validateMCPBUrl(pkg.Identifier)
	if err != nil {
		return err
	}

	// Parse the URL to validate format
	url, err := url.Parse(pkg.Identifier)
	if err != nil {
		return fmt.Errorf("invalid MCPB package URL: %w", err)
	}
	if url.Scheme != "https" {
		return fmt.Errorf("invalid MCPB package URL, must use HTTPS: %s", pkg.Identifier)
	}

	// Check that the URL contains 'mcp' somewhere (case-insensitive)
	if !strings.Contains(strings.ToLower(pkg.Identifier), "mcp") {
		return fmt.Errorf("MCPB package URL must contain 'mcp': %s", pkg.Identifier)
	}

	// Verify the file exists and is publicly accessible
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, pkg.Identifier, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "MCP-Registry-Validator/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to verify MCPB package accessibility: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MCPB package '%s' is not publicly accessible (status: %d)", pkg.Identifier, resp.StatusCode)
	}

	return nil
}

func validateMCPBUrl(fullURL string) error {
	parsedURL, err := url.Parse(fullURL)
	if err != nil {
		return fmt.Errorf("invalid MCPB package URL: %w", err)
	}

	host := strings.ToLower(parsedURL.Host)
	allowedHosts := []string{
		"github.com",
		"www.github.com",
		"gitlab.com",
		"www.gitlab.com",
	}

	isAllowed := false
	for _, allowed := range allowedHosts {
		if host == allowed {
			isAllowed = true
			break
		}
	}

	if !isAllowed {
		return fmt.Errorf("MCPB packages must be hosted on allowlisted providers (GitHub or GitLab). Host '%s' is not allowed", host)
	}

	// Validate URL path is a proper release URL with strict structure validation
	path := parsedURL.Path
	switch host {
	case "github.com", "www.github.com":
		// GitHub release URLs must match: /owner/repo/releases/download/tag/filename
		if !isValidGitHubReleaseURL(path) {
			return fmt.Errorf("GitHub MCPB packages must be release assets following the pattern '/owner/repo/releases/download/tag/filename'")
		}
	case "gitlab.com", "www.gitlab.com":
		// GitLab release URLs must match specific patterns
		if !isValidGitLabReleaseURL(path) {
			return fmt.Errorf("GitLab MCPB packages must be release assets following patterns '/owner/repo/-/releases/tag/downloads/filename' or '/owner/repo/-/package_files/id/download'")
		}
	}

	return nil
}

// isValidGitHubReleaseURL validates that a path follows the GitHub release asset pattern
// Pattern: /owner/repo/releases/download/tag/filename
func isValidGitHubReleaseURL(path string) bool {
	// GitHub release URL pattern: /owner/repo/releases/download/tag/filename
	// - owner: username or organization (1-39 chars, alphanumeric + hyphens, no consecutive hyphens)
	// - repo: repository name (similar rules to owner)
	// - tag: release tag (can contain various characters but not empty)
	// - filename: asset filename (not empty)
	pattern := `^/([a-zA-Z0-9]([a-zA-Z0-9\-]{0,37}[a-zA-Z0-9])?)/([a-zA-Z0-9._\-]+)/releases/download/([^/]+)/([^/]+)$`
	matched, _ := regexp.MatchString(pattern, path)
	return matched
}

// isValidGitLabReleaseURL validates that a path follows GitLab release asset patterns
func isValidGitLabReleaseURL(path string) bool {
	// GitLab release URL patterns:
	// 1. /owner/repo/-/releases/tag/downloads/filename
	// 2. /owner/repo/-/package_files/id/download
	// 3. /group/subgroup/repo/-/releases/tag/downloads/filename (nested groups)

	// The key insight is that GitLab URLs have "/-/" as a delimiter that separates the
	// project path from the GitLab-specific routes. Everything before "/-/" is the project path.

	// Pattern 1: Release downloads with /-/releases/tag/downloads/filename
	releasePattern := `^/([a-zA-Z0-9._\-]+(?:/[a-zA-Z0-9._\-]+)*)/-/releases/([^/]+)/downloads/([^/]+)$`
	if matched, _ := regexp.MatchString(releasePattern, path); matched {
		return true
	}

	// Pattern 2: Package files with /-/package_files/id/download
	packagePattern := `^/([a-zA-Z0-9._\-]+(?:/[a-zA-Z0-9._\-]+)*)/-/package_files/([0-9]+)/download$`
	if matched, _ := regexp.MatchString(packagePattern, path); matched {
		return true
	}

	return false
}
