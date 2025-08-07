package database

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/registry/internal/model"
)

// ReadSeedFile reads seed data from various sources:
// 1. Local file paths (*.json files)
// 2. Direct HTTP URLs to seed.json files
// 3. Registry root URLs (automatically appends /v0/servers and paginates)
func ReadSeedFile(ctx context.Context, path string) ([]model.ServerDetail, error) {
	log.Printf("Reading seed data from %s", path)

	// Set default seed file path if not provided
	if path == "" {
		// Try to find the seed.json in the data directory
		path = filepath.Join("data", "seed.json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return nil, fmt.Errorf("seed file not found at %s", path)
		}
	}

	// Check if path is an HTTP URL
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		// Determine if this is a direct seed file URL or a registry root URL
		if strings.HasSuffix(path, ".json") || strings.Contains(path, "seed.json") {
			// Direct seed file URL - read directly
			fileContent, err := readFromHTTP(ctx, path)
			if err != nil {
				return nil, fmt.Errorf("failed to read from HTTP URL: %w", err)
			}
			return parseSeedJSON(fileContent)
		}
		// Registry root URL - paginate through /v0/servers endpoint
		return readFromRegistryWithContext(ctx, path)
	}
	// Read from local file
	fileContent, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return parseSeedJSON(fileContent)
}

// readFromHTTP reads content from an HTTP URL with timeout
func readFromHTTP(ctx context.Context, url string) ([]byte, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, nil
}

// parseSeedJSON parses JSON content into ServerDetail objects
func parseSeedJSON(fileContent []byte) ([]model.ServerDetail, error) {
	var servers []model.ServerDetail
	if err := json.Unmarshal(fileContent, &servers); err != nil {
		// Try parsing as a raw JSON array and then convert to our model
		var rawData []map[string]any
		if jsonErr := json.Unmarshal(fileContent, &rawData); jsonErr != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w (original error: %w)", jsonErr, err)
		}
	}

	log.Printf("Found %d server entries in seed data", len(servers))
	return servers, nil
}

// PaginatedResponse represents the paginated response from /v0/servers endpoint
// PaginatedResponse represents the structure of a paginated response from /v0/servers endpoint
type PaginatedResponse struct {
	Data     []model.Server `json:"servers"`
	Metadata Metadata       `json:"metadata,omitempty"`
}

// Metadata contains pagination metadata
type Metadata struct {
	NextCursor string `json:"next_cursor,omitempty"`
	Count      int    `json:"count,omitempty"`
	Total      int    `json:"total,omitempty"`
}

// readFromRegistryWithContext reads all servers from a registry by paginating through /v0/servers endpoint
func readFromRegistryWithContext(ctx context.Context, registryURL string) ([]model.ServerDetail, error) {
	log.Printf("Reading from registry: %s", registryURL)

	// Ensure the URL doesn't have a trailing slash
	registryURL = strings.TrimSuffix(registryURL, "/")

	var allServers []model.ServerDetail
	cursor := ""
	pageCount := 0

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	for {
		pageCount++

		// Add delay between requests as requested (10 seconds by default)
		// Can be overridden by SEED_IMPORT_DELAY environment variable for testing
		if pageCount > 1 { // Don't delay before the first request
			delay := 10 * time.Second
			if delayStr := os.Getenv("SEED_IMPORT_DELAY"); delayStr != "" {
				if parsedDelay, err := time.ParseDuration(delayStr); err == nil {
					delay = parsedDelay
				}
			}
			if delay > 0 {
				log.Printf("Waiting %v before fetching page %d...", delay, pageCount)
				time.Sleep(delay)
			}
		}

		log.Printf("Fetching page %d from registry", pageCount)

		// Build the URL for this page
		serverURL := registryURL + "/v0/servers"
		if cursor != "" {
			// Add cursor parameter for pagination
			parsed, err := url.Parse(serverURL)
			if err != nil {
				return nil, fmt.Errorf("failed to parse registry URL: %w", err)
			}
			query := parsed.Query()
			query.Set("cursor", cursor)
			query.Set("limit", "100") // Use maximum limit for efficiency
			parsed.RawQuery = query.Encode()
			serverURL = parsed.String()
		} else {
			// First page - use max limit
			serverURL += "?limit=100"
		}

		// Fetch the page
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request for %s: %w", serverURL, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch servers from %s: %w", serverURL, err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP request to %s failed with status %d: %s", serverURL, resp.StatusCode, resp.Status)
		}

		// Read and parse the response
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response body from %s: %w", serverURL, err)
		}

		var pageResponse PaginatedResponse
		if err := json.Unmarshal(body, &pageResponse); err != nil {
			return nil, fmt.Errorf("failed to parse servers response from %s: %w", serverURL, err)
		}

		log.Printf("Retrieved %d servers from page %d", len(pageResponse.Data), pageCount)

		// For each server in this page, get the detailed information
		for _, server := range pageResponse.Data {
			// Build URL for server detail
			detailURL := registryURL + "/v0/servers/" + server.ID

			detailReq, err := http.NewRequestWithContext(ctx, http.MethodGet, detailURL, nil)
			if err != nil {
				log.Printf("Warning: failed to create request for server %s: %v", server.ID, err)
				// Fall back to basic server information
				serverDetail := model.ServerDetail{
					Server: server,
				}
				allServers = append(allServers, serverDetail)
				continue
			}

			detailResp, err := client.Do(detailReq)
			if err != nil {
				log.Printf("Warning: failed to fetch details for server %s: %v", server.ID, err)
				// Fall back to basic server information
				serverDetail := model.ServerDetail{
					Server: server,
				}
				allServers = append(allServers, serverDetail)
				continue
			}

			if detailResp.StatusCode != http.StatusOK {
				log.Printf("Warning: failed to fetch details for server %s (status %d)", server.ID, detailResp.StatusCode)
				detailResp.Body.Close()
				// Fall back to basic server information
				serverDetail := model.ServerDetail{
					Server: server,
				}
				allServers = append(allServers, serverDetail)
				continue
			}

			detailBody, err := io.ReadAll(detailResp.Body)
			detailResp.Body.Close()
			if err != nil {
				log.Printf("Warning: failed to read detail response for server %s: %v", server.ID, err)
				// Fall back to basic server information
				serverDetail := model.ServerDetail{
					Server: server,
				}
				allServers = append(allServers, serverDetail)
				continue
			}

			var serverDetail model.ServerDetail
			if err := json.Unmarshal(detailBody, &serverDetail); err != nil {
				log.Printf("Warning: failed to parse detail response for server %s: %v", server.ID, err)
				// Fall back to basic server information
				serverDetail = model.ServerDetail{
					Server: server,
				}
			}

			allServers = append(allServers, serverDetail)
		}

		// Check if there are more pages
		if pageResponse.Metadata.NextCursor == "" {
			log.Printf("Reached end of pagination after %d pages", pageCount)
			break
		}

		cursor = pageResponse.Metadata.NextCursor
	}

	log.Printf("Successfully retrieved %d servers from registry %s", len(allServers), registryURL)
	return allServers, nil
}
