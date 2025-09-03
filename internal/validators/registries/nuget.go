package registries

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// NuSpecMetadata represents the metadata section of a .nuspec file
type NuSpecMetadata struct {
	XMLName xml.Name `xml:"metadata"`
	MCPName string   `xml:"mcp-name"`
}

// NuSpecPackage represents the root element of a .nuspec file
type NuSpecPackage struct {
	XMLName  xml.Name       `xml:"package"`
	Metadata NuSpecMetadata `xml:"metadata"`
}

// ValidateNuGet validates that a NuGet package contains the correct MCP server name
func ValidateNuGet(ctx context.Context, pkg model.Package, serverName string) error {
	baseURL := pkg.RegistryBaseURL
	if baseURL == "" {
		baseURL = model.RegistryURLNuGet
	}

	client := &http.Client{Timeout: 10 * time.Second}

	lowerID := strings.ToLower(pkg.Identifier)
	lowerVersion := strings.ToLower(pkg.Version)
	if lowerVersion == "" {
		return fmt.Errorf("NuGet package validation requires a specific version, but none was provided")
	}

	url := fmt.Sprintf("%s/v3-flatcontainer/%s/%s/%s.nuspec", baseURL, lowerID, lowerVersion, lowerID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "MCP-Registry-Validator/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch .nuspec from NuGet: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("NuGet package '%s' version '%s' not found (status: %d)", pkg.Identifier, pkg.Version, resp.StatusCode)
	}

	var nuspec NuSpecPackage
	if err := xml.NewDecoder(resp.Body).Decode(&nuspec); err != nil {
		return fmt.Errorf("failed to parse .nuspec metadata: %w", err)
	}

	if nuspec.Metadata.MCPName == "" {
		return fmt.Errorf("NuGet package '%s' is missing required '<mcp-name>' element. Add this to your .nuspec: <mcp-name>%s</mcp-name>", pkg.Identifier, serverName)
	}

	if nuspec.Metadata.MCPName != serverName {
		return fmt.Errorf("NuGet package ownership validation failed. Expected mcp-name '%s', got '%s'", serverName, nuspec.Metadata.MCPName)
	}

	return nil
}
