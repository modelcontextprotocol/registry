package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// StatusUpdateRequest represents the request body for status update endpoints
type StatusUpdateRequest struct {
	Status        string  `json:"status"`
	StatusMessage *string `json:"statusMessage,omitempty"`
}

// AllVersionsStatusResponse represents the response from the all-versions status endpoint
type AllVersionsStatusResponse struct {
	UpdatedCount int `json:"updatedCount"`
}

// VersionInfo holds version and status for display
type VersionInfo struct {
	Version string
	Status  string
}

// ServerResponseMeta represents the _meta field in API responses
type ServerResponseMeta struct {
	Official *struct {
		Status string `json:"status"`
	} `json:"io.modelcontextprotocol.registry/official,omitempty"`
}

// SingleServerResponse represents the response from a single server version endpoint
type SingleServerResponse struct {
	Server struct {
		Version string `json:"version"`
	} `json:"server"`
	Meta ServerResponseMeta `json:"_meta"`
}

// ServerListResponse represents the response from the versions list endpoint
type ServerListResponse struct {
	Servers []SingleServerResponse `json:"servers"`
}

func StatusCommand(args []string) error {
	// Parse command flags
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	status := fs.String("status", "", "New status: active, deprecated, or deleted (required)")
	message := fs.String("message", "", "Optional status message explaining the change")
	allVersions := fs.Bool("all-versions", false, "Apply status change to all versions of the server")
	yes := fs.Bool("yes", false, "Skip confirmation prompt for bulk operations")
	fs.BoolVar(yes, "y", false, "Skip confirmation prompt for bulk operations (shorthand)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Validate required arguments
	if *status == "" {
		return errors.New("--status flag is required (active, deprecated, or deleted)")
	}

	// Validate status value
	validStatuses := map[string]bool{"active": true, "deprecated": true, "deleted": true}
	if !validStatuses[*status] {
		return fmt.Errorf("invalid status '%s'. Must be one of: active, deprecated, deleted", *status)
	}

	// Get server name from positional args
	remainingArgs := fs.Args()
	if len(remainingArgs) < 1 {
		return errors.New("server name is required\n\nUsage: mcp-publisher status --status <active|deprecated|deleted> [flags] <server-name> [version]")
	}

	serverName := remainingArgs[0]
	var version string

	// Get version if provided (required unless --all-versions is set)
	if !*allVersions {
		if len(remainingArgs) < 2 {
			return errors.New("version is required unless --all-versions flag is set\n\nUsage: mcp-publisher status --status <active|deprecated|deleted> [flags] <server-name> <version>")
		}
		version = remainingArgs[1]
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

	// Update status
	if *allVersions {
		return updateAllVersionsStatus(registryURL, serverName, *status, *message, token, *yes)
	}
	return updateVersionStatus(registryURL, serverName, version, *status, *message, token)
}

func updateVersionStatus(registryURL, serverName, version, status, statusMessage, token string) error {
	// Fetch current status to show "from → to"
	currentStatus, err := fetchVersionStatus(registryURL, serverName, version, token)
	if err != nil {
		return fmt.Errorf("failed to fetch current status: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stdout, "Updating %s version %s: %s → %s\n", serverName, version, currentStatus, status)

	if err := updateServerStatus(registryURL, serverName, version, status, statusMessage, token); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, "✓ Successfully updated status")
	return nil
}

func updateAllVersionsStatus(registryURL, serverName, status, statusMessage, token string, skipConfirm bool) error {
	if !strings.HasSuffix(registryURL, "/") {
		registryURL += "/"
	}

	// Fetch all versions to show current statuses and get count for confirmation
	versions, err := fetchAllVersionsStatus(registryURL, serverName, token)
	if err != nil {
		return fmt.Errorf("failed to fetch current versions: %w", err)
	}

	if len(versions) == 0 {
		return errors.New("no versions found for this server")
	}

	// Show what will be updated
	_, _ = fmt.Fprintf(os.Stdout, "This will update %d version(s) of %s:\n", len(versions), serverName)
	for _, v := range versions {
		_, _ = fmt.Fprintf(os.Stdout, "  %s: %s → %s\n", v.Version, v.Status, status)
	}

	// Prompt for confirmation unless -y/--yes was provided
	if !skipConfirm {
		_, _ = fmt.Fprint(os.Stdout, "Continue? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			return errors.New("operation cancelled")
		}
	}

	// Build the request body
	requestBody := StatusUpdateRequest{
		Status: status,
	}
	if statusMessage != "" {
		requestBody.StatusMessage = &statusMessage
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("error serializing request: %w", err)
	}

	// URL encode the server name
	encodedServerName := url.PathEscape(serverName)
	statusURL := registryURL + "v0/servers/" + encodedServerName + "/status"

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	// Parse response to get updated count
	var response AllVersionsStatusResponse
	if err := json.Unmarshal(body, &response); err != nil {
		// If we can't parse the response, just report success
		_, _ = fmt.Fprintln(os.Stdout, "✓ Successfully updated all versions")
		return nil
	}

	_, _ = fmt.Fprintf(os.Stdout, "✓ Successfully updated %d version(s)\n", response.UpdatedCount)
	return nil
}

func updateServerStatus(registryURL, serverName, version, status, statusMessage, token string) error {
	if !strings.HasSuffix(registryURL, "/") {
		registryURL += "/"
	}

	// Build the request body
	requestBody := StatusUpdateRequest{
		Status: status,
	}
	if statusMessage != "" {
		requestBody.StatusMessage = &statusMessage
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("error serializing request: %w", err)
	}

	// URL encode the server name and version
	encodedServerName := url.PathEscape(serverName)
	encodedVersion := url.PathEscape(version)
	statusURL := registryURL + "v0/servers/" + encodedServerName + "/versions/" + encodedVersion + "/status"

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	return nil
}

func fetchVersionStatus(registryURL, serverName, version, token string) (string, error) {
	if !strings.HasSuffix(registryURL, "/") {
		registryURL += "/"
	}

	encodedServerName := url.PathEscape(serverName)
	encodedVersion := url.PathEscape(version)
	fetchURL := registryURL + "v0/servers/" + encodedServerName + "/versions/" + encodedVersion + "?include_deleted=true"

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fetchURL, nil)
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	// Parse the response to extract status
	var response SingleServerResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("error parsing response: %w", err)
	}

	if response.Meta.Official == nil {
		return "", errors.New("server response missing status information")
	}

	return response.Meta.Official.Status, nil
}

func fetchAllVersionsStatus(registryURL, serverName, token string) ([]VersionInfo, error) {
	if !strings.HasSuffix(registryURL, "/") {
		registryURL += "/"
	}

	encodedServerName := url.PathEscape(serverName)
	fetchURL := registryURL + "v0/servers/" + encodedServerName + "/versions?include_deleted=true"

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	var response ServerListResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("error parsing response: %w", err)
	}

	var versions []VersionInfo
	for _, s := range response.Servers {
		status := "unknown"
		if s.Meta.Official != nil {
			status = s.Meta.Official.Status
		}
		versions = append(versions, VersionInfo{
			Version: s.Server.Version,
			Status:  status,
		})
	}

	return versions, nil
}
