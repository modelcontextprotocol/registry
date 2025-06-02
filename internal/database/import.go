package database

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/registry/internal/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ImportSeedFile populates the MongoDB database with initial data from a seed file.
func ImportSeedFile(mongo *MongoDB, seedFilePath string) error {
	// Set default seed file path if not provided
	if seedFilePath == "" {
		// Try to find the seed.json in the data directory
		seedFilePath = filepath.Join("data", "seed.json")
		if _, err := os.Stat(seedFilePath); os.IsNotExist(err) {
			return fmt.Errorf("seed file not found at %s", seedFilePath)
		}
	}

	// Create a context with timeout for the database operations
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Read the seed file
	seedData, err := readSeedFile(seedFilePath)
	if err != nil {
		return fmt.Errorf("failed to read seed file: %w", err)
	}

	collection := mongo.collection
	importData(ctx, collection, seedData)
	return nil
}

// readSeedFile reads and parses the seed.json file
func readSeedFile(path string) ([]model.ServerDetail, error) {
	log.Printf("Reading seed file from %s", path)

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

// importData imports the seed data into MongoDB
func importData(ctx context.Context, collection *mongo.Collection, servers []model.ServerDetail) {
	log.Printf("Importing %d servers into collection %s", len(servers), collection.Name())

	for i, server := range servers {
		if server.ID == "" || server.Name == "" {
			log.Printf("Skipping server %d: ID or Name is empty", i+1)
			continue
		}
		// Create filter based on server ID
		filter := bson.M{"id": server.ID}

		if server.VersionDetail.Version == "" {
			server.VersionDetail.Version = "0.0.1-seed"
			server.VersionDetail.ReleaseDate = time.Now().Format(time.RFC3339)
			server.VersionDetail.IsLatest = true
		}
		// Create update document
		update := bson.M{"$set": server}

		// Use upsert to create if not exists or update if exists
		opts := options.Update().SetUpsert(true)
		result, err := collection.UpdateOne(ctx, filter, update, opts)
		if err != nil {
			log.Printf("Error importing server %s: %v", server.ID, err)
			continue
		}

		switch {
		case result.UpsertedCount > 0:
			log.Printf("[%d/%d] Created server: %s", i+1, len(servers), server.Name)
		case result.ModifiedCount > 0:
			log.Printf("[%d/%d] Updated server: %s", i+1, len(servers), server.Name)
		default:
			log.Printf("[%d/%d] Server already up to date: %s", i+1, len(servers), server.Name)
		}
	}

	log.Println("Import completed successfully")
}
