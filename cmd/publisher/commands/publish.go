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
	response, statusCode, err := publishToRegistry(registryURL, serverData, token)
	if err != nil {
		// If publish failed with 422, call validate endpoint to show detailed errors
		if statusCode == http.StatusUnprocessableEntity {
			_, _ = fmt.Fprintln(os.Stdout, "Validation failed. Checking detailed validation errors...")
			_, _ = fmt.Fprintln(os.Stdout)

			// Call validate endpoint (same as validate command does)
			result, validateErr := validateViaAPI(registryURL, serverData)
			if validateErr != nil {
				// If validate also fails, return original publish error
				return fmt.Errorf("publish failed: %w", err)
			}

			// Print validation results using shared formatting logic
			formattedErrorMsg := printValidationIssues(result, &serverJSON)

			if !result.Valid {
				// Return error with formatted message if available
				if formattedErrorMsg != "" {
					return fmt.Errorf("%s", formattedErrorMsg)
				}
				return fmt.Errorf("validation failed")
			}
		}

		// For non-422 errors, return the original error
		return fmt.Errorf("publish failed: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, "✓ Successfully published")
	_, _ = fmt.Fprintf(os.Stdout, "✓ Server %s version %s\n", response.Server.Name, response.Server.Version)

	return nil
}

func publishToRegistry(registryURL string, serverData []byte, token string) (*apiv0.ServerResponse, int, error) {
	// Parse the server JSON data
	var serverJSON apiv0.ServerJSON
	err := json.Unmarshal(serverData, &serverJSON)
	if err != nil {
		return nil, 0, fmt.Errorf("error parsing server.json file: %w", err)
	}

	// Convert to JSON
	jsonData, err := json.Marshal(serverJSON)
	if err != nil {
		return nil, 0, fmt.Errorf("error serializing request: %w", err)
	}

	// Ensure URL ends with the publish endpoint
	if !strings.HasSuffix(registryURL, "/") {
		registryURL += "/"
	}
	publishURL := registryURL + "v0/publish"

	// Create and send request
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, publishURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, 0, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	var serverResponse apiv0.ServerResponse
	if err := json.Unmarshal(body, &serverResponse); err != nil {
		return nil, resp.StatusCode, err
	}

	return &serverResponse, resp.StatusCode, nil
}
