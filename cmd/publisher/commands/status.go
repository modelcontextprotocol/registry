package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
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

type StatusFlags struct {
	Status      string
	Message     string
	AllVersions bool
	SkipConfirm bool
}

var StatusFlg StatusFlags

func init() {
	mcpPublisherCmd.AddCommand(statusCmd)
	statusCmd.Flags().StringVarP(&StatusFlg.Status, "status", "s", "", "New status: active, deprecated, or deleted (required)")
	statusCmd.Flags().StringVarP(&StatusFlg.Message, "message", "m", "", "Optional status message explaining the change")
	statusCmd.Flags().BoolVar(&StatusFlg.AllVersions, "all-versions", false, "Apply status change to all versions of the server")
	statusCmd.Flags().BoolVarP(&StatusFlg.SkipConfirm, "yes", "y", false, "Skip confirmation prompt for bulk operations")
}

var statusCmd = &cobra.Command{
	Use:   "status --status <active|deprecated|deleted> [flags] <server-name> [version]",
	Short: "Update the status of a server version",
	Args: func(_ *cobra.Command, args []string) error {
		if len(args) < 1 {
			return errors.New("server name is required")
		}
		return nil
	},
	Long: `Arguments:
  server-name   Full server name (e.g., io.github.user/my-server)
  version       Server version to update (required unless --all-versions is set)`,
	Example: `  # Deprecate a specific version
  mcp-publisher status --status deprecated --message "Please upgrade to 2.0.0"
  io.github.user/my-server 1.0.0
  # Delete a version with security issues
  mcp-publisher status --status deleted --message "Critical security vulnerability"
  io.github.user/my-server 1.0.0
  # Restore a version to active
  mcp-publisher status --status active io.github.user/my-server 1.0.0
  # Deprecate all versions
  mcp-publisher status --status deprecated --all-versions --message "Project archived"
  io.github.user/my-server`,
	RunE: RunStatusCommand,
}

var RunStatusCommand = func(_ *cobra.Command, args []string) error {
	// Validate required arguments
	if StatusFlg.Status == "" {
		return errors.New("--status flag is required (active, deprecated, or deleted)")
	}

	// Validate status value
	validStatuses := map[string]bool{"active": true, "deprecated": true, "deleted": true}
	if !validStatuses[StatusFlg.Status] {
		return fmt.Errorf("invalid status '%s'. Must be one of: active, deprecated, deleted", StatusFlg.Status)
	}

	serverName := args[0]
	var version string

	// Get version if provided (required unless --all-versions is set)
	if !StatusFlg.AllVersions {
		if len(args) < 2 {
			return errors.New("version is required unless --all-versions flag is set\n\nUsage: mcp-publisher status --status <active|deprecated|deleted> [flags] <server-name> <version>")
		}
		version = args[1]
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
	if StatusFlg.AllVersions {
		return updateAllVersionsStatus(registryURL, serverName, StatusFlg.Status, StatusFlg.Message, token, StatusFlg.SkipConfirm)
	}
	return updateVersionStatus(registryURL, serverName, version, StatusFlg.Status, StatusFlg.Message, token)
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
