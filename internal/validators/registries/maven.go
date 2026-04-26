package registries

import (
	"context"
	"encoding/xml"
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
	ErrMissingIdentifierForMaven = errors.New("package identifier is required for Maven packages")
	ErrMissingVersionForMaven    = errors.New("package version is required for Maven packages")
	ErrInvalidMavenIdentifier    = errors.New("maven package identifier must be in 'groupId:artifactId' form (e.g., 'org.example:my-mcp-server')")
)

// mavenPOM represents the subset of Maven POM XML fields used to validate
// MCP server ownership. Only the fields we read are declared.
type mavenPOM struct {
	XMLName     xml.Name `xml:"project"`
	GroupID     string   `xml:"groupId"`
	ArtifactID  string   `xml:"artifactId"`
	Description string   `xml:"description"`
	Properties  struct {
		// Maven properties is a free-form element; we look for an
		// `mcpName` property as the most explicit ownership signal.
		MCPName string `xml:"mcpName"`
	} `xml:"properties"`
	// Parent POM may set groupId for the project; we do not currently
	// follow parent inheritance because the published POM should declare
	// its own groupId for ownership purposes.
}

// ValidateMaven validates that a Maven Central package contains the correct
// MCP server name. Identifier is "groupId:artifactId" (e.g., "org.example:my-mcp-server")
// and Version must be a concrete release (no version ranges).
//
// Ownership is verified by fetching the published POM at:
//
//	{baseURL}/{groupPath}/{artifactId}/{version}/{artifactId}-{version}.pom
//
// and looking for the server name as either:
//  1. an `<mcpName>` Maven property, or
//  2. an `mcp-name: <serverName>` line in the POM `<description>`.
//
// Maven Central already validates `groupId` domain ownership at publish time,
// so a matching declaration in the POM is sufficient to bind a package to an
// MCP server name in the same namespace.
func ValidateMaven(ctx context.Context, pkg model.Package, serverName string) error {
	if pkg.RegistryBaseURL == "" {
		pkg.RegistryBaseURL = model.RegistryURLMaven
	}

	if pkg.Identifier == "" {
		return ErrMissingIdentifierForMaven
	}
	if pkg.Version == "" {
		return ErrMissingVersionForMaven
	}

	// Maven packages must not carry MCPB-only fields.
	if pkg.FileSHA256 != "" {
		return fmt.Errorf("maven packages must not have 'fileSha256' field - this is only for MCPB packages")
	}

	// Maven Central is the only allowed base URL for the official registry.
	if pkg.RegistryBaseURL != model.RegistryURLMaven {
		return fmt.Errorf("registry type and base URL do not match: '%s' is not valid for registry type '%s'. Expected: %s",
			pkg.RegistryBaseURL, model.RegistryTypeMaven, model.RegistryURLMaven)
	}

	return validateMavenAgainst(ctx, pkg.RegistryBaseURL, pkg, serverName)
}

// validateMavenAgainst is the common validation routine, parameterized on
// the base URL so tests can point it at an httptest server without going
// through the Maven Central allowlist check.
func validateMavenAgainst(ctx context.Context, baseURL string, pkg model.Package, serverName string) error {
	if pkg.Identifier == "" {
		return ErrMissingIdentifierForMaven
	}
	if pkg.Version == "" {
		return ErrMissingVersionForMaven
	}

	groupID, artifactID, err := parseMavenIdentifier(pkg.Identifier)
	if err != nil {
		return err
	}

	pom, err := fetchMavenPOM(ctx, baseURL, groupID, artifactID, pkg.Version)
	if err != nil {
		return err
	}

	// Sanity-check that the POM matches the declared coordinates. This catches
	// publishing mistakes and proxy/mirror misconfiguration.
	if pom.GroupID != "" && pom.GroupID != groupID {
		return fmt.Errorf("maven POM groupId '%s' does not match identifier groupId '%s'", pom.GroupID, groupID)
	}
	if pom.ArtifactID != "" && pom.ArtifactID != artifactID {
		return fmt.Errorf("maven POM artifactId '%s' does not match identifier artifactId '%s'", pom.ArtifactID, artifactID)
	}

	// Preferred: explicit `<mcpName>` property in the POM.
	if pom.Properties.MCPName != "" {
		if pom.Properties.MCPName != serverName {
			return fmt.Errorf("maven package ownership validation failed. Expected mcpName '%s', got '%s'", serverName, pom.Properties.MCPName)
		}
		return nil
	}

	// Fallback: `mcp-name: <serverName>` in the POM description (mirrors the PyPI/NuGet pattern).
	mcpNamePattern := "mcp-name: " + serverName
	if strings.Contains(pom.Description, mcpNamePattern) {
		return nil
	}

	return fmt.Errorf(
		"maven package '%s:%s' ownership validation failed. Add either an `<mcpName>%s</mcpName>` property to the published POM or an 'mcp-name: %s' line to the POM `<description>`",
		groupID, artifactID, serverName, serverName,
	)
}

// parseMavenIdentifier splits a "groupId:artifactId" coordinate. Both parts
// must be non-empty; we deliberately reject the three-part "g:a:v" form so
// version always lives in the dedicated Version field (consistent with how
// other JVM-aware MCP clients consume coordinates).
func parseMavenIdentifier(identifier string) (groupID, artifactID string, err error) {
	parts := strings.Split(identifier, ":")
	if len(parts) != 2 {
		return "", "", ErrInvalidMavenIdentifier
	}
	groupID, artifactID = parts[0], parts[1]
	if groupID == "" || artifactID == "" {
		return "", "", ErrInvalidMavenIdentifier
	}
	return groupID, artifactID, nil
}

// fetchMavenPOM downloads and parses the published POM for the coordinate.
func fetchMavenPOM(ctx context.Context, baseURL, groupID, artifactID, version string) (*mavenPOM, error) {
	pomURL := buildPOMURL(baseURL, groupID, artifactID, version)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pomURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Maven POM request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/xml")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Maven POM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("maven package '%s:%s' version '%s' not found at %s", groupID, artifactID, version, pomURL)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("maven POM request returned status %d", resp.StatusCode)
	}

	// Cap the body so a hostile mirror can't exhaust memory; published POMs
	// are typically only a few KB.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read Maven POM: %w", err)
	}

	var pom mavenPOM
	if err := xml.Unmarshal(body, &pom); err != nil {
		return nil, fmt.Errorf("failed to parse Maven POM: %w", err)
	}
	return &pom, nil
}

// buildPOMURL constructs the canonical POM URL on the Maven repository
// layout: groupId dots become path slashes.
func buildPOMURL(baseURL, groupID, artifactID, version string) string {
	groupPath := strings.ReplaceAll(groupID, ".", "/")
	// PathEscape each segment to keep e.g. unusual artifact ids safe.
	return strings.TrimRight(baseURL, "/") + "/" +
		groupPath + "/" +
		url.PathEscape(artifactID) + "/" +
		url.PathEscape(version) + "/" +
		url.PathEscape(artifactID) + "-" + url.PathEscape(version) + ".pom"
}
