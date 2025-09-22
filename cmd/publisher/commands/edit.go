package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// EditCommand handles editing properties of a published server
func EditCommand(args []string) error {
	if len(args) < 1 {
		return errors.New("server ID required\n\nUsage: mcp-publisher edit <server-id> [flags]\n\nFlags:\n  --status=<status>    Update server status (active, deprecated, deleted)")
	}

	serverID := args[0]

	// Parse flags
	editFlags := flag.NewFlagSet("edit", flag.ExitOnError)
	var status string
	editFlags.StringVar(&status, "status", "", "Update server status (active, deprecated, deleted)")

	if err := editFlags.Parse(args[1:]); err != nil {
		return err
	}

	// Check that at least one flag is provided
	if status == "" {
		return errors.New("no changes specified\n\nUsage: mcp-publisher edit <server-id> [flags]\n\nFlags:\n  --status=<status>    Update server status (active, deprecated, deleted)")
	}

	// Validate status value if provided
	if status != "" {
		validStatuses := []string{"active", "deprecated", "deleted"}
		isValidStatus := false
		for _, validStatus := range validStatuses {
			if status == validStatus {
				isValidStatus = true
				break
			}
		}
		if !isValidStatus {
			return fmt.Errorf("invalid status '%s'. Valid statuses: %s", status, strings.Join(validStatuses, ", "))
		}
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

	// Update server properties
	if status != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Updating server %s status to '%s'...\n", serverID, status)
		err = updateServerStatus(registryURL, serverID, status, token)
		if err != nil {
			return fmt.Errorf("status update failed: %w", err)
		}
		_, _ = fmt.Fprintln(os.Stdout, "✓ Successfully updated server status")
	}

	return nil
}

// UpdateStatusRequest represents the request body for updating server status
type UpdateStatusRequest struct {
	Status string `json:"status"`
}

func updateServerStatus(registryURL, serverID, status, token string) error {
	// Prepare request body
	requestBody := UpdateStatusRequest{
		Status: status,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("error serializing request: %w", err)
	}

	// Ensure URL ends with slash
	if !strings.HasSuffix(registryURL, "/") {
		registryURL += "/"
	}
	statusURL := registryURL + "v0/servers/" + serverID + "/status"

	// Create and send request
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPatch, statusURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	return nil
}