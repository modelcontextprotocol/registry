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
	"regexp"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const (
	// expectedExampleCount is the number of JSON examples we expect to find in examples.md
	// IMPORTANT: Only change this count if you have intentionally added or removed examples
	// from the examples.md file. This check prevents accidental formatting changes from
	// causing examples to be skipped during validation.
	expectedExampleCount = 9
)

func main() {
	log.SetFlags(0) // Remove timestamp from logs

	if err := runValidation(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func runValidation() error {
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

	validatedCount := 0
	for i, example := range examples {
		log.Printf("Example %d:", i+1)

		var data any
		if err := json.Unmarshal([]byte(example.content), &data); err != nil {
			log.Printf("  ❌ Invalid JSON: %v", err)
			continue
		}

		baseValid := false
		registryValid := false

		if err := baseSchema.Validate(data); err != nil {
			log.Printf("  Validating against schema.json: ❌")
			log.Printf("    Error: %v", err)
		} else {
			log.Printf("  Validating against schema.json: ✅")
			baseValid = true
		}

		if err := registrySchema.Validate(data); err != nil {
			log.Printf("  Validating against registry-schema.json: ❌")
			log.Printf("    Error: %v", err)
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

	if validatedCount != expectedExampleCount {
		return fmt.Errorf("validation failed: expected %d examples to pass both validations but only %d did",
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

	// Regex to match JSON code blocks in markdown
	// Captures everything between ```json and ```
	re := regexp.MustCompile("(?s)```json\n(.*?)\n```")
	matches := re.FindAllStringSubmatchIndex(content, -1)

	var examples []example
	for _, match := range matches {
		if len(match) < 4 {
			// should never happen
			return nil, fmt.Errorf("invalid match - expected at least 4 indices but got %d", len(match))
		}
		start, end := match[2], match[3]
		// line numbers start at 1
		line := 1 + strings.Count(content[:start], "\n")
		examples = append(examples, example{
			content: content[start:end],
			line:    line,
		})
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
