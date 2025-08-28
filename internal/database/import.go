package database

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// ReadSeedFile reads seed data from various sources:
// 1. Local file paths (*.json files) - expects extension wrapper format
// 2. Direct HTTP URLs to seed.json files - expects extension wrapper format
// 3. Registry root URLs (automatically appends /v0/servers and paginates)
// Only the extension wrapper format is supported (array of ServerResponse objects)
func ReadSeedFile(ctx context.Context, path string) ([]*apiv0.ServerRecord, error) {
	var data []byte
	var err error

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		// Handle HTTP URLs
		if strings.HasSuffix(path, "/v0/servers") || strings.Contains(path, "/v0/servers") {
			// This is a registry API endpoint - fetch paginated data
			return fetchFromRegistryAPI(ctx, path)
		}
		// This is a direct file URL
		data, err = fetchFromHTTP(ctx, path)
	} else {
		// Handle local file paths
		data, err = os.ReadFile(path)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to read seed data from %s: %w", path, err)
	}

	// Parse extension wrapper format (only supported format)
	var serverResponses []apiv0.ServerRecord
	if err := json.Unmarshal(data, &serverResponses); err != nil {
		return nil, fmt.Errorf("failed to parse seed data as extension wrapper format: %w", err)
	}

	if len(serverResponses) == 0 {
		return []*apiv0.ServerRecord{}, nil
	}

	// Validate servers and collect warnings instead of failing the whole batch
	var validRecords []*apiv0.ServerRecord
	var invalidServers []string
	var validationFailures []string

	for _, response := range serverResponses {
		if err := validators.ValidateServerJSON(&response.Server); err != nil {
			// Log warning and track invalid server instead of failing
			invalidServers = append(invalidServers, response.Server.Name)
			validationFailures = append(validationFailures, fmt.Sprintf("Server '%s': %v", response.Server.Name, err))
			log.Printf("Warning: Skipping invalid server '%s': %v", response.Server.Name, err)
			continue
		}

		// Convert valid ServerResponse to ServerRecord
		record := convertServerResponseToRecord(response)
		validRecords = append(validRecords, record)
	}

	// Print summary of validation results
	if len(invalidServers) > 0 {
		log.Printf("Import summary: %d valid servers imported, %d invalid servers skipped", len(validRecords), len(invalidServers))
		log.Printf("Invalid servers: %v", invalidServers)
		for _, failure := range validationFailures {
			log.Printf("  - %s", failure)
		}
	} else {
		log.Printf("Import summary: All %d servers imported successfully", len(validRecords))
	}

	return validRecords, nil
}

func fetchFromHTTP(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from HTTP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func fetchFromRegistryAPI(ctx context.Context, baseURL string) ([]*apiv0.ServerRecord, error) {
	var allRecords []*apiv0.ServerRecord
	cursor := ""

	for {
		url := baseURL
		if cursor != "" {
			if strings.Contains(url, "?") {
				url += "&cursor=" + cursor
			} else {
				url += "?cursor=" + cursor
			}
		}

		data, err := fetchFromHTTP(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page from registry API: %w", err)
		}

		var response struct {
			Servers  []apiv0.ServerRecord `json:"servers"`
			Metadata *struct {
				NextCursor string `json:"next_cursor,omitempty"`
			} `json:"metadata,omitempty"`
		}

		if err := json.Unmarshal(data, &response); err != nil {
			return nil, fmt.Errorf("failed to parse registry API response: %w", err)
		}

		// Convert and add servers
		for _, serverResponse := range response.Servers {
			record := convertServerResponseToRecord(serverResponse)
			allRecords = append(allRecords, record)
		}

		// Check if there's a next page
		if response.Metadata == nil || response.Metadata.NextCursor == "" {
			break
		}
		cursor = response.Metadata.NextCursor
	}

	return allRecords, nil
}

func convertServerResponseToRecord(response apiv0.ServerRecord) *apiv0.ServerRecord {
	// The registry extensions are already properly typed, so we can use them directly
	registryMetadata := response.XIOModelContextProtocolRegistry

	// Publisher extensions
	publisherExtensions := response.XPublisher
	if publisherExtensions == nil {
		publisherExtensions = make(map[string]interface{})
	}

	return &apiv0.ServerRecord{
		Server:                          response.Server,
		XIOModelContextProtocolRegistry: registryMetadata,
		XPublisher:                      publisherExtensions,
	}
}
