package registries

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// PyPIPackageResponse represents the structure returned by the PyPI JSON API
type PyPIPackageResponse struct {
	Info struct {
		ProjectUrls map[string]string `json:"project_urls"`
	} `json:"info"`
}

// ValidatePyPI validates that a PyPI package contains the correct MCP server name
func ValidatePyPI(ctx context.Context, pkg model.Package, serverName string) error {
	baseURL := pkg.RegistryBaseURL
	if baseURL == "" {
		baseURL = model.RegistryURLPyPI
	}

	client := &http.Client{Timeout: 10 * time.Second}
	
	url := fmt.Sprintf("%s/pypi/%s/json", baseURL, pkg.Identifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("User-Agent", "MCP-Registry-Validator/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch package metadata from PyPI: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PyPI package '%s' not found (status: %d)", pkg.Identifier, resp.StatusCode)
	}

	var pypiResp PyPIPackageResponse
	if err := json.NewDecoder(resp.Body).Decode(&pypiResp); err != nil {
		return fmt.Errorf("failed to parse PyPI package metadata: %w", err)
	}

	mcpName, exists := pypiResp.Info.ProjectUrls["MCP"]
	if !exists {
		return fmt.Errorf("PyPI package '%s' is missing required 'MCP' project URL. Add this to your pyproject.toml: [project.urls] MCP = \"%s\"", pkg.Identifier, serverName)
	}

	if mcpName != serverName {
		return fmt.Errorf("PyPI package ownership validation failed. Expected MCP project URL '%s', got '%s'", serverName, mcpName)
	}

	return nil
}