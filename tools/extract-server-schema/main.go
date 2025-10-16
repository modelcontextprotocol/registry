package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	openAPIPath     = "docs/reference/api/openapi.yaml"
	schemaOutputDir = "docs/reference/server-json"
)

func main() {
	var check bool
	flag.BoolVar(&check, "check", false, "Check if schema is in sync (exit 1 if not)")
	flag.Parse()

	// Read OpenAPI spec
	openapiData, err := os.ReadFile(openAPIPath)
	if err != nil {
		log.Fatalf("Failed to read OpenAPI spec: %v", err)
	}

	// Parse YAML
	var openapi map[string]interface{}
	if err := yaml.Unmarshal(openapiData, &openapi); err != nil {
		log.Fatalf("Failed to parse OpenAPI YAML: %v", err)
	}

	// Extract version from info section
	info, ok := openapi["info"].(map[string]interface{})
	if !ok {
		log.Fatal("Missing 'info' in OpenAPI spec")
	}
	version, ok := info["version"].(string)
	if !ok {
		log.Fatal("Missing 'info.version' in OpenAPI spec")
	}

	// Extract components/schemas
	components, ok := openapi["components"].(map[string]interface{})
	if !ok {
		log.Fatal("Missing 'components' in OpenAPI spec")
	}

	schemas, ok := components["schemas"].(map[string]interface{})
	if !ok {
		log.Fatal("Missing 'components/schemas' in OpenAPI spec")
	}

	// Extract ServerDetail
	serverDetail, ok := schemas["ServerDetail"].(map[string]interface{})
	if !ok {
		log.Fatal("Missing 'ServerDetail' schema in OpenAPI spec")
	}

	// Auto-discover all schemas referenced by ServerDetail
	referencedSchemas := make(map[string]bool)
	findReferencedSchemas(serverDetail, referencedSchemas)

	// Build definitions by recursively collecting all referenced schemas
	definitions := make(map[string]interface{})
	definitions["ServerDetail"] = serverDetail

	// Keep discovering until we've found all transitively referenced schemas
	for {
		added := false
		for schemaName := range referencedSchemas {
			if _, exists := definitions[schemaName]; !exists {
				schema, ok := schemas[schemaName]
				if !ok {
					log.Fatalf("Referenced schema '%s' not found in OpenAPI spec", schemaName)
				}
				definitions[schemaName] = schema
				// Find schemas referenced by this newly added schema
				findReferencedSchemas(schema, referencedSchemas)
				added = true
			}
		}
		if !added {
			break
		}
	}

	// Build the JSON Schema document with dynamic version from OpenAPI spec
	schemaID := fmt.Sprintf("https://static.modelcontextprotocol.io/schemas/%s/server.schema.json", version)
	jsonSchema := map[string]interface{}{
		"$comment":    "This file is auto-generated from docs/reference/api/openapi.yaml. Do not edit manually. Run 'make generate-schema' to update.",
		"$schema":     "http://json-schema.org/draft-07/schema#",
		"$id":         schemaID,
		"title":       "server.json defining a Model Context Protocol (MCP) server",
		"$ref":        "#/definitions/ServerDetail",
		"definitions": definitions,
	}

	// Replace all #/components/schemas/ references with #/definitions/
	jsonSchema = replaceComponentRefs(jsonSchema).(map[string]interface{})

	// Convert to JSON
	jsonData, err := json.MarshalIndent(jsonSchema, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal JSON schema: %v", err)
	}

	// Append newline at end
	jsonStr := string(jsonData) + "\n"

	outputPath := schemaOutputDir + "/server.schema.json"

	if check {
		// Check mode: compare with existing file
		existingData, err := os.ReadFile(outputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading existing schema: %v\n", err)
			os.Exit(1)
		}

		if string(existingData) != jsonStr {
			fmt.Fprintf(os.Stderr, "ERROR: server.schema.json is out of sync with openapi.yaml\n")
			fmt.Fprintf(os.Stderr, "Run 'make generate-schema' to update it.\n")
			os.Exit(1)
		}

		log.Println("✓ server.schema.json is in sync with openapi.yaml")
		return
	}

	// Write mode: update the file
	if err := os.WriteFile(outputPath, []byte(jsonStr), 0644); err != nil { //nolint:gosec // This is a documentation file that should be world-readable
		log.Fatalf("Failed to write schema file: %v", err)
	}

	log.Printf("✓ Generated %s from %s\n", outputPath, openAPIPath)
}

// findReferencedSchemas recursively finds all schema names referenced via $ref
func findReferencedSchemas(obj interface{}, found map[string]bool) {
	switch v := obj.(type) {
	case map[string]interface{}:
		for key, value := range v {
			if key == "$ref" {
				if ref, ok := value.(string); ok {
					// Extract schema name from #/components/schemas/SchemaName
					if strings.HasPrefix(ref, "#/components/schemas/") {
						schemaName := strings.TrimPrefix(ref, "#/components/schemas/")
						found[schemaName] = true
					}
				}
			} else {
				findReferencedSchemas(value, found)
			}
		}
	case []interface{}:
		for _, item := range v {
			findReferencedSchemas(item, found)
		}
	}
}

// replaceComponentRefs recursively replaces #/components/schemas/ with #/definitions/
func replaceComponentRefs(obj interface{}) interface{} {
	switch v := obj.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, value := range v {
			if key == "$ref" {
				if ref, ok := value.(string); ok {
					// Replace the reference path
					result[key] = strings.ReplaceAll(ref, "#/components/schemas/", "#/definitions/")
				} else {
					result[key] = value
				}
			} else {
				result[key] = replaceComponentRefs(value)
			}
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = replaceComponentRefs(item)
		}
		return result
	default:
		return obj
	}
}
