package registries

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

var (
	ErrMissingIdentifierForNuget = errors.New("package identifier is required for NuGet packages")
	ErrMissingVersionForNuget    = errors.New("package version is required for NuGet packages")
)

// ValidateNuGet validates that a NuGet package contains the correct MCP server name
func ValidateNuGet(ctx context.Context, pkg model.Package, serverName string) error {
	// Set default registry base URL if empty
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

	client := &http.Client{Timeout: 10 * time.Second}

	lowerID := strings.ToLower(pkg.Identifier)
	lowerVersion := strings.ToLower(pkg.Version)
	if lowerVersion == "" {
		return ErrMissingVersionForNuget
	}

	// Try to get README from the package
	readmeURL := fmt.Sprintf("%s/v3-flatcontainer/%s/%s/readme", pkg.RegistryBaseURL, lowerID, lowerVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, readmeURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "MCP-Registry-Validator/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch README from NuGet: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		// Check README content
		readmeBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read README content: %w", err)
		}

		readmeContent := string(readmeBytes)

		// Check for mcp-name: format (more specific)
		mcpNamePattern := "mcp-name: " + serverName
		if strings.Contains(readmeContent, mcpNamePattern) {
			return nil // Found as mcp-name: format
		}
	}

	return fmt.Errorf("NuGet package '%s' ownership validation failed. The server name '%s' must appear as 'mcp-name: %s' in the package README. Add it to your package README", pkg.Identifier, serverName, serverName)
}
