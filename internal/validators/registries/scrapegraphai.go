package registries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

var (
	ErrMissingIdentifierForScrapeGraphAI = errors.New("package identifier is required for ScrapeGraphAI packages")
	ErrMissingVersionForScrapeGraphAI   = errors.New("package version is required for ScrapeGraphAI packages")
)

// ScrapeGraphAIPackageResponse represents the structure returned by the PyPI JSON API
// (ScrapeGraphAI packages are distributed via PyPI)
type ScrapeGraphAIPackageResponse struct {
	Info struct {
		Description string `json:"description"`
	} `json:"info"`
}

// NPMPackageResponse represents the structure returned by the NPM registry API
// (ScrapeGraphAI packages on Smithery are distributed via npm)
type NPMPackageResponse struct {
	MCPName string `json:"mcpName"`
}

// ValidateScrapeGraphAI validates that a ScrapeGraphAI package contains the correct MCP server name
// ScrapeGraphAI packages can be distributed via PyPI or via Smithery (which uses npm)
func ValidateScrapeGraphAI(ctx context.Context, pkg model.Package, serverName string) error {
	// Set default registry base URL if empty (use PyPI since ScrapeGraphAI is on PyPI)
	if pkg.RegistryBaseURL == "" {
		pkg.RegistryBaseURL = model.RegistryURLPyPI
	}

	if pkg.Identifier == "" {
		return ErrMissingIdentifierForScrapeGraphAI
	}

	if pkg.Version == "" {
		return ErrMissingVersionForScrapeGraphAI
	}

	// Validate that MCPB-specific fields are not present
	if pkg.FileSHA256 != "" {
		return fmt.Errorf("ScrapeGraphAI packages must not have 'fileSha256' field - this is only for MCPB packages")
	}

	// Validate registry base URL - support both PyPI and Smithery (which uses npm)
	if pkg.RegistryBaseURL != model.RegistryURLPyPI && pkg.RegistryBaseURL != model.RegistryURLSmithery && pkg.RegistryBaseURL != model.RegistryURLNPM {
		return fmt.Errorf("registry type and base URL do not match: '%s' is not valid for registry type '%s'. Supported URLs: %s, %s, %s",
			pkg.RegistryBaseURL, model.RegistryTypeScrapeGraphAI, model.RegistryURLPyPI, model.RegistryURLSmithery, model.RegistryURLNPM)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// If using Smithery or npm, validate via npm API (Smithery uses npm as backend)
	if pkg.RegistryBaseURL == model.RegistryURLSmithery || pkg.RegistryBaseURL == model.RegistryURLNPM {
		return validateScrapeGraphAIViaNPM(ctx, client, pkg, serverName)
	}

	// Otherwise, validate via PyPI API
	return validateScrapeGraphAIViaPyPI(ctx, client, pkg, serverName)
}

// validateScrapeGraphAIViaPyPI validates ScrapeGraphAI packages distributed via PyPI
func validateScrapeGraphAIViaPyPI(ctx context.Context, client *http.Client, pkg model.Package, serverName string) error {
	requestURL := fmt.Sprintf("%s/pypi/%s/%s/json", pkg.RegistryBaseURL, pkg.Identifier, pkg.Version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "MCP-Registry-Validator/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch package metadata from PyPI for ScrapeGraphAI package: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ScrapeGraphAI package '%s' not found on PyPI (status: %d)", pkg.Identifier, resp.StatusCode)
	}

	var scrapeGraphAIResp ScrapeGraphAIPackageResponse
	if err := json.NewDecoder(resp.Body).Decode(&scrapeGraphAIResp); err != nil {
		return fmt.Errorf("failed to parse ScrapeGraphAI package metadata: %w", err)
	}

	// Check description (README) content
	description := scrapeGraphAIResp.Info.Description

	// Check for mcp-name: format (more specific)
	mcpNamePattern := "mcp-name: " + serverName
	if strings.Contains(description, mcpNamePattern) {
		return nil // Found as mcp-name: format
	}

	return fmt.Errorf("ScrapeGraphAI package '%s' ownership validation failed. The server name '%s' must appear as 'mcp-name: %s' in the package README", pkg.Identifier, serverName, serverName)
}

// validateScrapeGraphAIViaNPM validates ScrapeGraphAI packages distributed via npm (Smithery uses npm)
func validateScrapeGraphAIViaNPM(ctx context.Context, client *http.Client, pkg model.Package, serverName string) error {
	// Use npm registry for validation (Smithery packages are on npm)
	registryURL := model.RegistryURLNPM
	if pkg.RegistryBaseURL == model.RegistryURLNPM {
		registryURL = pkg.RegistryBaseURL
	}

	requestURL := registryURL + "/" + url.PathEscape(pkg.Identifier) + "/" + url.PathEscape(pkg.Version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "MCP-Registry-Validator/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch package metadata from npm for ScrapeGraphAI package: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ScrapeGraphAI package '%s' not found on npm/Smithery (status: %d)", pkg.Identifier, resp.StatusCode)
	}

	var npmResp NPMPackageResponse
	if err := json.NewDecoder(resp.Body).Decode(&npmResp); err != nil {
		return fmt.Errorf("failed to parse ScrapeGraphAI package metadata from npm: %w", err)
	}

	if npmResp.MCPName == "" {
		return fmt.Errorf("ScrapeGraphAI package '%s' is missing required 'mcpName' field. Add this to your package.json: \"mcpName\": \"%s\"", pkg.Identifier, serverName)
	}

	if npmResp.MCPName != serverName {
		return fmt.Errorf("ScrapeGraphAI package ownership validation failed. Expected mcpName '%s', got '%s'", serverName, npmResp.MCPName)
	}

	return nil
}
