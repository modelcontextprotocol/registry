// validate-examples validates JSON examples in docs/server-json/examples.md
// against both schema.json and registry-schema.json.
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

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/spf13/cobra"
)

const (
	// expectedExampleCount is the number of JSON examples we expect to find in examples.md
	// IMPORTANT: Only change this count if you have intentionally added or removed examples
	// from the examples.md file. This check prevents accidental formatting changes from
	// causing examples to be skipped during validation.
	expectedExampleCount = 7
)

func main() {
	log.SetFlags(0) // Remove timestamp from logs
	
	var rootCmd = &cobra.Command{
		Use:   "validate-examples",
		Short: "Validate examples in examples.md",
		Long:  "Validates that all JSON examples in examples.md conform to both schema.json and registry-schema.json",
		RunE:  runValidation,
	}

	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func runValidation(_ *cobra.Command, _ []string) error {
	basePath := filepath.Join("docs", "server-json")

	examplesPath := filepath.Join(basePath, "examples.md")
	schemaPath := filepath.Join(basePath, "schema.json")
	registrySchemaPath := filepath.Join(basePath, "registry-schema.json")

	examples, err := extractExamples(examplesPath)
	if err != nil {
		return fmt.Errorf("failed to extract examples: %w", err)
	}

	log.Printf("Found %d examples in examples.md\n", len(examples))

	if len(examples) != expectedExampleCount {
		return fmt.Errorf("expected %d examples but found %d - if this is intentional, update expectedExampleCount in %s",
			expectedExampleCount, len(examples), "tools/validate-examples/main.go")
	}

	log.Println()

	baseSchema, err := compileSchema(schemaPath)
	if err != nil {
		return fmt.Errorf("failed to compile schema.json: %w", err)
	}

	registrySchema, err := compileSchema(registrySchemaPath)
	if err != nil {
		return fmt.Errorf("failed to compile registry-schema.json: %w", err)
	}

	allValid := true
	validatedCount := 0
	for i, example := range examples {
		log.Printf("Example %d:", i+1)

		var data interface{}
		if err := json.Unmarshal([]byte(example.content), &data); err != nil {
			log.Printf("  ❌ Invalid JSON: %v", err)
			allValid = false
			continue
		}

		baseValid := false
		registryValid := false

		if err := baseSchema.Validate(data); err != nil {
			log.Printf("  Validating against schema.json: ❌")
			log.Printf("    Error: %v", err)
			allValid = false
		} else {
			log.Printf("  Validating against schema.json: ✅")
			baseValid = true
		}

		if err := registrySchema.Validate(data); err != nil {
			log.Printf("  Validating against registry-schema.json: ❌")
			log.Printf("    Error: %v", err)
			allValid = false
		} else {
			log.Printf("  Validating against registry-schema.json: ✅")
			registryValid = true
		}

		// Only count as validated if both schemas passed
		if baseValid && registryValid {
			validatedCount++
		}

		log.Println()
	}

	if !allValid {
		return fmt.Errorf("one or more examples failed validation")
	}

	if validatedCount != expectedExampleCount {
		return fmt.Errorf("validation count mismatch: expected to validate %d examples but only %d passed both validations",
			expectedExampleCount, validatedCount)
	}

	log.Printf("Successfully validated all %d examples!", validatedCount)
	return nil
}

type example struct {
	content string
	line    int
}

func extractExamples(path string) ([]example, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	var examples []example
	inCodeBlock := false
	var currentExample strings.Builder
	var startLine int

	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "```json"):
			inCodeBlock = true
			startLine = i + 1
			currentExample.Reset()
		case inCodeBlock && strings.HasPrefix(line, "```"):
			inCodeBlock = false
			if currentExample.Len() > 0 {
				examples = append(examples, example{
					content: currentExample.String(),
					line:    startLine,
				})
			}
		case inCodeBlock:
			if currentExample.Len() > 0 {
				currentExample.WriteString("\n")
			}
			currentExample.WriteString(line)
		}
	}

	return examples, nil
}

func compileSchema(path string) (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7

	// For registry-schema.json, we need to register the base schema it references
	if strings.Contains(path, "registry-schema.json") {
		basePath := filepath.Join(filepath.Dir(path), "schema.json")
		baseData, err := os.ReadFile(basePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read base schema: %w", err)
		}

		// Add the base schema to the compiler with the expected URL
		if err := compiler.AddResource("https://modelcontextprotocol.io/schemas/draft/2025-07-09/server.json", bytes.NewReader(baseData)); err != nil {
			return nil, fmt.Errorf("failed to add base schema resource: %w", err)
		}
	}

	return compiler.Compile(path)
}
