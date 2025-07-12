// validate-schemas validates that schema.json and registry-schema.json
// are valid JSON Schema documents.
//
// For more information, see docs/server-json/README.md
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/spf13/cobra"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "validate-schemas",
		Short: "Validate JSON schema files",
		Long:  "Validates that schema.json and registry-schema.json are valid JSON Schema documents",
		RunE:  runValidation,
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runValidation(cmd *cobra.Command, args []string) error {
	basePath := filepath.Join("docs", "server-json")
	
	schemas := []struct {
		name string
		path string
	}{
		{"schema.json", filepath.Join(basePath, "schema.json")},
		{"registry-schema.json", filepath.Join(basePath, "registry-schema.json")},
	}

	expectedSchemaCount := len(schemas)
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7

	allValid := true
	validatedCount := 0
	
	for _, schemaFile := range schemas {
		fmt.Printf("Validating %s...\n", schemaFile.name)
		
		if err := validateSchema(compiler, schemaFile.path); err != nil {
			fmt.Printf("  ❌ Invalid: %v\n", err)
			allValid = false
		} else {
			fmt.Printf("  ✅ Valid JSON Schema\n")
			validatedCount++
		}
	}

	if !allValid {
		return fmt.Errorf("one or more schemas are invalid")
	}

	if validatedCount != expectedSchemaCount {
		return fmt.Errorf("validation count mismatch: expected to validate %d schemas but only %d passed",
			expectedSchemaCount, validatedCount)
	}

	fmt.Printf("\nSuccessfully validated all %d schemas!\n", validatedCount)
	return nil
}

func validateSchema(compiler *jsonschema.Compiler, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	var schemaData interface{}
	if err := json.Unmarshal(data, &schemaData); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// For registry-schema.json, we need to register the base schema it references
	if strings.Contains(path, "registry-schema.json") {
		basePath := filepath.Join(filepath.Dir(path), "schema.json")
		baseData, err := os.ReadFile(basePath)
		if err != nil {
			return fmt.Errorf("failed to read base schema: %w", err)
		}
		
		// Add the base schema to the compiler with the expected URL
		compiler.AddResource("https://modelcontextprotocol.io/schemas/draft/2025-07-09/server.json", bytes.NewReader(baseData))
	}

	if _, err := compiler.Compile(path); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}

	return nil
}