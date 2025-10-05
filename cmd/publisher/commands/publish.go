package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func PublishCommand(args []string) error {
	// Check for server.json file
	serverFile := "server.json"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		serverFile = args[0]
	}

	// Read server.json
	serverData, err := os.ReadFile(serverFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("server.json not found. Run 'mcp-publisher init' to create one")
		}
		return fmt.Errorf("failed to read server.json: %w", err)
	}

	// Validate JSON
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(serverData, &serverJSON); err != nil {
		return fmt.Errorf("invalid server.json: %w", err)
	}

	// Check for deprecated schema and recommend migration
	// Allow empty schema (will use default) but reject old schemas
	if serverJSON.Schema != "" && !strings.Contains(serverJSON.Schema, model.CurrentSchemaVersion) {
		return fmt.Errorf(`deprecated schema detected: %s.

Migrate to the current schema format for new servers.

📋 Migration checklist: https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/CHANGELOG.md#migration-checklist-for-publishers
📖 Full changelog with examples: https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/CHANGELOG.md`, serverJSON.Schema)
	}

	// Load saved token
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	tokenPath := filepath.Join(homeDir, TokenFileName)
	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("not authenticated. Run 'mcp-publisher login <method>' first")
		}
		return fmt.Errorf("failed to read token: %w", err)
	}

	var tokenInfo map[string]string
	if err := json.Unmarshal(tokenData, &tokenInfo); err != nil {
		return fmt.Errorf("invalid token data: %w", err)
	}

	token := tokenInfo["token"]
	registryURL := tokenInfo["registry"]
	if registryURL == "" {
		registryURL = DefaultRegistryURL
	}

	// Publish to registry
	_, _ = fmt.Fprintf(os.Stdout, "Publishing to %s...\n", registryURL)
	response, err := publishToRegistry(registryURL, serverData, token)
	if err != nil {
		return fmt.Errorf("publish failed: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, "✓ Successfully published")
	_, _ = fmt.Fprintf(os.Stdout, "✓ Server %s version %s\n", response.Server.Name, response.Server.Version)

	return nil
}

func publishToRegistry(registryURL string, serverData []byte, token string) (*apiv0.ServerResponse, error) {
	// Parse the server JSON data
	var serverJSON apiv0.ServerJSON
	err := json.Unmarshal(serverData, &serverJSON)
	if err != nil {
		return nil, fmt.Errorf("error parsing server.json file: %w", err)
	}

	// Convert to JSON
	jsonData, err := json.Marshal(serverJSON)
	if err != nil {
		return nil, fmt.Errorf("error serializing request: %w", err)
	}

	// Ensure URL ends with the publish endpoint
	if !strings.HasSuffix(registryURL, "/") {
		registryURL += "/"
	}
	publishURL := registryURL + "v0/publish"

	// Create and send request
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, publishURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	var serverResponse apiv0.ServerResponse
	if err := json.Unmarshal(body, &serverResponse); err != nil {
		return nil, err
	}

	return &serverResponse, nil
}
