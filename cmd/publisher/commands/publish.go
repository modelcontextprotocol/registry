package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

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
	tokenPath, err := tokenFilePath()
	if err != nil {
		return err
	}

	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return notAuthenticatedError()
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

	// Validate JSON encoding before sending to prevent publishing
	// corrupted data on systems with non-UTF-8 codepage environments.
	if err := validatePublishData(jsonData); err != nil {
		return nil, 0, fmt.Errorf("publish data validation failed: %w", err)
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

// validatePublishData checks that JSON data is clean before sending to the registry.
// This catches encoding corruption that can occur on Windows systems when running
// under Git Bash / MSYS with a non-UTF-8 ANSI codepage (e.g. zh-CN CP-936/GBK).
//
// The check validates:
//  1. Data is valid UTF-8
//  2. No JSON-escaped lone surrogates (\udc00-\udfff) appear in the data
func validatePublishData(data []byte) error {
	// Check for valid UTF-8 encoding
	if !utf8.Valid(data) {
		return fmt.Errorf("server.json contains invalid UTF-8 encoding - this may indicate system encoding corruption. Try running mcp-publisher from PowerShell or CMD instead of Git Bash")
	}

	// Check for JSON-escaped lone surrogates (e.g. \udc94, \uddff).
	// These indicate bytes that were misinterpreted through the system codepage
	// instead of being treated as raw UTF-8.
	//
	// We look for the escape prefix \udc-\udf in the marshaled JSON output.
	// Lone surrogates (U+DC00-U+DFFF) are never valid content in server metadata
	// and always indicate encoding corruption.
	lower := bytes.ToLower(data)
	if bytes.Contains(lower, []byte(`\udc`)) ||
		bytes.Contains(lower, []byte(`\udd`)) ||
		bytes.Contains(lower, []byte(`\ude`)) ||
		bytes.Contains(lower, []byte(`\udf`)) {
		return fmt.Errorf("server.json contains encoding corruption (lone surrogate characters detected) - this may be caused by non-UTF-8 system codepage. Try running mcp-publisher from PowerShell or CMD instead of Git Bash")
	}

	return nil
}
