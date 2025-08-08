// validate-schemas validates that schema.json and registry-schema.json
// are valid JSON Schema documents.
//
// For more information, see docs/server-json/README.md
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
)

func main() {
	log.SetFlags(0) // Remove timestamp from logs

	if err := runValidation(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func runValidation() error {
	basePath := filepath.Join("docs", "server-json")

	schemas := []struct {
		name string
		path string
	}{
		{"schema.json", filepath.Join(basePath, "schema.json")},
		{"registry-schema.json", filepath.Join(basePath, "registry-schema.json")},
	}

	expectedSchemaCount := len(schemas)
	validatedCount := 0

	for _, schemaFile := range schemas {
		log.Printf("Validating %s...", schemaFile.name)

		if err := validateSchema(schemaFile.path); err != nil {
			log.Printf("  ❌ Invalid: %v", err)
		} else {
			log.Printf("  ✅ Valid JSON Schema")
			validatedCount++
		}
	}

	if validatedCount != expectedSchemaCount {
		return fmt.Errorf("validation failed: expected to validate %d schemas but only %d passed",
			expectedSchemaCount, validatedCount)
	}

	log.Printf("\nSuccessfully validated all %d schemas!", validatedCount)
	return nil
}

func validateSchema(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	var schemaData any
	if err := json.Unmarshal(data, &schemaData); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7

	// For registry-schema.json, we need to register the base schema it references
	if strings.Contains(path, "registry-schema.json") {
		basePath := filepath.Join(filepath.Dir(path), "schema.json")
		baseData, err := os.ReadFile(basePath)
		if err != nil {
			return fmt.Errorf("failed to read base schema: %w", err)
		}

		// Add the base schema to the compiler with the expected URL
		if err := compiler.AddResource("https://modelcontextprotocol.io/schemas/draft/2025-07-09/server.json", bytes.NewReader(baseData)); err != nil {
			return fmt.Errorf("failed to add base schema resource: %w", err)
		}
	}

	if _, err := compiler.Compile(path); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}

	return nil
}
