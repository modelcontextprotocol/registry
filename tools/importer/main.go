package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/registry/internal/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	mongoURI       string
	databaseName   string
	collectionName string
	seedFilePath   string
	dropCollection bool
)

func init() {
	flag.StringVar(&mongoURI, "uri", "mongodb://localhost:27017", "MongoDB connection URI")
	flag.StringVar(&databaseName, "db", "mcp_registry", "MongoDB database name")
	flag.StringVar(&collectionName, "collection", "servers", "MongoDB collection name")
	flag.StringVar(&seedFilePath, "seed", "", "Path to seed.json file (default: data/seed.json)")
	flag.BoolVar(&dropCollection, "drop", false, "Drop collection before importing")
	flag.Parse()

	// Set default seed file path if not provided
	if seedFilePath == "" {
		// Try to find the seed.json in the data directory
		seedFilePath = filepath.Join("data", "seed.json")
		if _, err := os.Stat(seedFilePath); os.IsNotExist(err) {
			// Try from the repository root
			repoRoot := filepath.Join("..", "..")
			seedFilePath = filepath.Join(repoRoot, "data", "seed.json")
		}
	}
}

func main() {
	// Create a context with timeout for the database operations
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Received termination signal. Cleaning up...")
		cancel()
		os.Exit(0)
	}()

	// Read the seed file
	seedData, err := readSeedFile(seedFilePath)
	if err != nil {
		log.Fatalf("Failed to read seed file: %v", err)
	}

	// Connect to MongoDB
	log.Printf("Connecting to MongoDB at %s", mongoURI)
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer func() {
		if err = client.Disconnect(ctx); err != nil {
			log.Fatalf("Failed to disconnect from MongoDB: %v", err)
		}
	}()

	// Ping MongoDB to verify the connection
	if err = client.Ping(ctx, nil); err != nil {
		log.Fatalf("Failed to ping MongoDB: %v", err)
	}
	log.Println("Successfully connected to MongoDB")

	// Get collection
	db := client.Database(databaseName)
	collection := db.Collection(collectionName)

	// Drop collection if requested
	if dropCollection {
		log.Printf("Dropping collection %s", collectionName)
		if err := collection.Drop(ctx); err != nil {
			log.Fatalf("Failed to drop collection: %v", err)
		}
	}

	// Import data
	importData(ctx, collection, seedData)
}

// readSeedFile reads and parses the seed.json file
func readSeedFile(path string) ([]model.ServerDetail, error) {
	log.Printf("Reading seed file from %s", path)

	// Read the file content
	fileContent, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	// Parse the JSON content
	var servers []model.ServerDetail
	if err := json.Unmarshal(fileContent, &servers); err != nil {
		// Try parsing as a raw JSON array and then convert to our model
		var rawData []map[string]interface{}
		if jsonErr := json.Unmarshal(fileContent, &rawData); jsonErr != nil {
			return nil, fmt.Errorf("failed to parse JSON: %v (original error: %v)", jsonErr, err)
		}

		// Convert raw data to model format
		servers = convertToServerDetails(rawData)
	}

	log.Printf("Found %d server entries in seed file", len(servers))
	return servers, nil
}

// convertToServerDetails converts raw JSON data to ServerDetail structs
func convertToServerDetails(rawData []map[string]interface{}) []model.ServerDetail {
	servers := make([]model.ServerDetail, 0, len(rawData))

	for _, data := range rawData {
		server := model.ServerDetail{}

		// Extract basic fields
		if id, ok := data["id"].(string); ok {
			server.ID = id
		}
		if name, ok := data["name"].(string); ok {
			server.Name = name
		}
		if desc, ok := data["description"].(string); ok {
			server.Description = desc
		}
		if version, ok := data["version"].(string); ok {
			server.Version = version
		}
		if canonical, ok := data["registrycanonical"].(string); ok {
			server.RegistryCanonical = canonical
		}

		// Extract repository
		if repo, ok := data["repository"].(map[string]interface{}); ok {
			server.Repository = model.Repository{
				URL:       getStringValue(repo, "url"),
				SubFolder: getStringValue(repo, "subfolder"),
				Branch:    getStringValue(repo, "branch"),
				Commit:    getStringValue(repo, "commit"),
			}
		}

		// Extract registries
		if registries, ok := data["registries"].([]interface{}); ok {
			for _, reg := range registries {
				if regMap, ok := reg.(map[string]interface{}); ok {
					registry := model.Registries{
						Name:        getStringValue(regMap, "name"),
						PackageName: getStringValue(regMap, "packagename"),
						License:     getStringValue(regMap, "license"),
					}

					// Handle command arguments if present
					if cmd, ok := regMap["command_arguments"].(map[string]interface{}); ok {
						commandArgs := model.Command{}

						// Extract sub commands
						if subCmds, ok := cmd["sub_commands"].([]interface{}); ok {
							for _, sc := range subCmds {
								if scMap, ok := sc.(map[string]interface{}); ok {
									subCmd := model.SubCommand{
										Name:        getStringValue(scMap, "name"),
										Description: getStringValue(scMap, "description"),
									}

									// Extract named arguments in sub commands
									if namedArgs, ok := scMap["named_arguments"].([]interface{}); ok {
										for _, na := range namedArgs {
											if naMap, ok := na.(map[string]interface{}); ok {
												namedArg := extractNamedArgument(naMap)
												subCmd.NamedArguments = append(subCmd.NamedArguments, namedArg)
											}
										}
									}

									commandArgs.SubCommands = append(commandArgs.SubCommands, subCmd)
								}
							}
						}

						// Extract positional arguments
						if posArgs, ok := cmd["positional_arguments"].([]interface{}); ok {
							for _, pa := range posArgs {
								if paMap, ok := pa.(map[string]interface{}); ok {
									posArg := model.PositionalArgument{}

									if pos, ok := paMap["position"].(float64); ok {
										posArg.Position = int(pos)
									}

									if arg, ok := paMap["argument"].(map[string]interface{}); ok {
										posArg.Argument = extractArgument(arg)
									}

									commandArgs.PositionalArguments = append(commandArgs.PositionalArguments, posArg)
								}
							}
						}

						// Extract environment variables
						if envVars, ok := cmd["environment_variables"].([]interface{}); ok {
							for _, ev := range envVars {
								if evMap, ok := ev.(map[string]interface{}); ok {
									envVar := model.EnvironmentVariable{
										Name:        getStringValue(evMap, "name"),
										Description: getStringValue(evMap, "description"),
									}

									if required, ok := evMap["required"].(bool); ok {
										envVar.Required = required
									}

									commandArgs.EnvironmentVariables = append(commandArgs.EnvironmentVariables, envVar)
								}
							}
						}

						// Extract named arguments
						if namedArgs, ok := cmd["named_arguments"].([]interface{}); ok {
							for _, na := range namedArgs {
								if naMap, ok := na.(map[string]interface{}); ok {
									namedArg := extractNamedArgument(naMap)
									commandArgs.NamedArguments = append(commandArgs.NamedArguments, namedArg)
								}
							}
						}

						registry.CommandArguments = commandArgs
					}

					server.Registries = append(server.Registries, registry)
				}
			}
		}

		// Extract remotes
		if remotes, ok := data["remotes"].([]interface{}); ok {
			for _, rem := range remotes {
				if remMap, ok := rem.(map[string]interface{}); ok {
					remote := model.Remotes{
						TransportType: getStringValue(remMap, "transport_type"),
						Url:           getStringValue(remMap, "url"),
					}
					server.Remotes = append(server.Remotes, remote)
				}
			}
		}

		servers = append(servers, server)
	}

	return servers
}

// extractArgument extracts an Argument struct from a map
func extractArgument(data map[string]interface{}) model.Argument {
	arg := model.Argument{
		Name:         getStringValue(data, "name"),
		Description:  getStringValue(data, "description"),
		DefaultValue: getStringValue(data, "default_value"),
	}

	// Extract boolean fields
	if isRequired, ok := data["is_required"].(bool); ok {
		arg.IsRequired = isRequired
	}
	if isEditable, ok := data["is_editable"].(bool); ok {
		arg.IsEditable = isEditable
	}
	if isRepeatable, ok := data["is_repeatable"].(bool); ok {
		arg.IsRepeatable = isRepeatable
	}

	// Extract string array for choices
	if choices, ok := data["choices"].([]interface{}); ok {
		for _, choice := range choices {
			if strChoice, ok := choice.(string); ok {
				arg.Choices = append(arg.Choices, strChoice)
			}
		}
	}

	return arg
}

// extractNamedArgument extracts a NamedArguments struct from a map
func extractNamedArgument(data map[string]interface{}) model.NamedArguments {
	namedArg := model.NamedArguments{
		ShortFlag: getStringValue(data, "short_flag"),
		LongFlag:  getStringValue(data, "long_flag"),
	}

	if requiresValue, ok := data["requires_value"].(bool); ok {
		namedArg.RequiresValue = requiresValue
	}

	if arg, ok := data["argument"].(map[string]interface{}); ok {
		namedArg.Argument = extractArgument(arg)
	}

	return namedArg
}

// getStringValue safely extracts a string value from a map
func getStringValue(data map[string]interface{}, key string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	return ""
}

// importData imports the seed data into MongoDB
func importData(ctx context.Context, collection *mongo.Collection, servers []model.ServerDetail) {
	log.Printf("Importing %d servers into collection %s", len(servers), collection.Name())

	for i, server := range servers {
		// Create filter based on server ID
		filter := bson.M{"id": server.ID}

		// Create update document
		update := bson.M{"$set": server}

		// Use upsert to create if not exists or update if exists
		opts := options.Update().SetUpsert(true)
		result, err := collection.UpdateOne(ctx, filter, update, opts)
		if err != nil {
			log.Printf("Error importing server %s: %v", server.ID, err)
			continue
		}

		if result.UpsertedCount > 0 {
			log.Printf("[%d/%d] Created server: %s", i+1, len(servers), server.Name)
		} else if result.ModifiedCount > 0 {
			log.Printf("[%d/%d] Updated server: %s", i+1, len(servers), server.Name)
		} else {
			log.Printf("[%d/%d] Server already up to date: %s", i+1, len(servers), server.Name)
		}
	}

	log.Println("Import completed successfully")
}
