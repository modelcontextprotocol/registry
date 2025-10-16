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

	// Convert OpenAPI discriminators to JSON Schema if/then/else patterns first
	jsonSchema = convertDiscriminators(jsonSchema).(map[string]interface{})

	// Then replace all #/components/schemas/ references with #/definitions/
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

// convertDiscriminators converts OpenAPI discriminators to JSON Schema if/then/else patterns
func convertDiscriminators(obj interface{}) interface{} {
	switch v := obj.(type) {
	case map[string]interface{}:
		// Check if this object has a discriminator with oneOf
		if discriminator, hasDiscriminator := v["discriminator"].(map[string]interface{}); hasDiscriminator {
			if oneOf, hasOneOf := v["oneOf"].([]interface{}); hasOneOf {
				// Extract discriminator property name and mapping
				propertyName, _ := discriminator["propertyName"].(string)
				mapping, _ := discriminator["mapping"].(map[string]interface{})

				if propertyName != "" && mapping != nil && len(oneOf) > 0 {
					// Get description if present
					description, _ := v["description"].(string)

					// Build the allOf with if/then blocks for discriminated union
					result := buildDiscriminatedUnion(propertyName, mapping, oneOf, description)

					// Recursively convert discriminators in the result
					return convertDiscriminators(result)
				}
			}
		}

		// Recursively convert discriminators in nested objects
		result := make(map[string]interface{})
		for key, value := range v {
			result[key] = convertDiscriminators(value)
		}
		return result

	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = convertDiscriminators(item)
		}
		return result

	default:
		return obj
	}
}

// buildDiscriminatedUnion builds an allOf structure with separate if/then blocks for each discriminator value
func buildDiscriminatedUnion(propertyName string, mapping map[string]interface{}, oneOf []interface{}, description string) map[string]interface{} {
	// Build a sorted list of mapping entries by extracting from oneOf order
	mappingList := make([]struct{ key, ref string }, 0, len(oneOf))
	for _, item := range oneOf {
		if refMap, ok := item.(map[string]interface{}); ok {
			if ref, ok := refMap["$ref"].(string); ok {
				// Find the key in mapping that matches this ref
				for key, value := range mapping {
					if refValue, ok := value.(string); ok && refValue == ref {
						mappingList = append(mappingList, struct{ key, ref string }{key, ref})
						break
					}
				}
			}
		}
	}

	// Extract enum values in the same order
	enumValues := make([]interface{}, 0, len(mappingList))
	for _, item := range mappingList {
		enumValues = append(enumValues, item.key)
	}

	// Build allOf array with separate if/then for each type
	allOfItems := make([]interface{}, 0, len(mappingList))
	for _, item := range mappingList {
		allOfItems = append(allOfItems, map[string]interface{}{
			"if": map[string]interface{}{
				"properties": map[string]interface{}{
					propertyName: map[string]interface{}{
						"const": item.key,
					},
				},
			},
			"then": map[string]interface{}{
				"$ref": item.ref,
			},
		})
	}

	// Build result as regular map (will be alphabetically sorted by Go's json.Marshal)
	result := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			propertyName: map[string]interface{}{
				"type": "string",
				"enum": enumValues,
			},
		},
		"required": []interface{}{propertyName},
		"allOf":    allOfItems,
	}

	// Add description if present
	if description != "" {
		result["description"] = description
	}

	return result
}
