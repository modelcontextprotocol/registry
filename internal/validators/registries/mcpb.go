package registries

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

func ValidateMCPB(ctx context.Context, pkg model.Package, _ string) error {
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
