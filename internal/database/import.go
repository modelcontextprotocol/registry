package database

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/registry/internal/model"
)

// ReadSeedFile reads and parses the seed.json file - exported for use by all database implementations
func ReadSeedFile(path string) ([]model.ServerDetail, error) {
	log.Printf("Reading seed file from %s", path)

	// Set default seed file path if not provided
	if path == "" {
		// Try to find the seed.json in the data directory
		path = filepath.Join("data", "seed.json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return nil, fmt.Errorf("seed file not found at %s", path)
		}
	}

	// Read the file content
	fileContent, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Parse the JSON content
	var servers []model.ServerDetail
	if err := json.Unmarshal(fileContent, &servers); err != nil {
		// Try parsing as a raw JSON array and then convert to our model
		var rawData []map[string]interface{}
		if jsonErr := json.Unmarshal(fileContent, &rawData); jsonErr != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w (original error: %w)", jsonErr, err)
		}
	}

	log.Printf("Found %d server entries in seed file", len(servers))
	return servers, nil
}
