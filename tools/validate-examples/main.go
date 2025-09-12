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

	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
)

const (
	// expectedExampleCount is the number of JSON examples we expect to find in generic-server-json.md
	// IMPORTANT: Only change this count if you have intentionally added or removed examples. This
	// check prevents accidental formatting changes from causing examples to be skipped during validation.
	expectedExampleCount = 13
)

func main() {
	log.SetFlags(0) // Remove timestamp from logs

	if err := runValidation(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func runValidation() error {
	basePath := filepath.Join("docs", "reference", "server-json")

	examplesPath := filepath.Join(basePath, "generic-server-json.md")
	schemaPath := filepath.Join(basePath, "server.schema.json")

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
		return fmt.Errorf("failed to compile server.schema.json: %w", err)
	}

	validatedCount := 0

	for i, example := range examples {
		log.Printf("Example %d:", i+1)

		if validateExample(example, baseSchema) {
			validatedCount++
		}

		log.Println()
	}

	if validatedCount != expectedExampleCount {
		return fmt.Errorf("validation failed: expected %d examples to pass all validations but only %d did",
			expectedExampleCount, validatedCount)
	}

	log.Printf("Successfully validated all %d examples!", validatedCount)
	return nil
}

func validateExample(ex example, baseSchema *jsonschema.Schema) bool {
	var data any
	if err := json.Unmarshal([]byte(ex.content), &data); err != nil {
		log.Printf("  ❌ Invalid JSON: %v", err)
		return false
	}

	// Extract server portion if this is a PublishRequest format
	serverData := data
	publishRequestValid := true
	if dataMap, ok := data.(map[string]any); ok {
		if server, exists := dataMap["server"]; exists {
			// This is a PublishRequest format - validate only expected properties exist
			for key := range dataMap {
				if key != "server" && key != "x-publisher" {
					log.Printf("  Invalid PublishRequest property: ❌ %s (only 'server' and optional 'x-publisher' are allowed)", key)
					publishRequestValid = false
				}
			}
			serverData = server
		}
	}

	baseValid := validateAgainstSchema(serverData, baseSchema, "server.schema.json")
	goValidatorValid := validateWithObjectValidator(serverData)

	// Only count as validated if all validations passed
	return publishRequestValid && baseValid && goValidatorValid
}

func validateAgainstSchema(data any, schema *jsonschema.Schema, schemaName string) bool {
	if err := schema.Validate(data); err != nil {
		log.Printf("  Validating against %s: ❌", schemaName)
		log.Printf("    Error: %v", err)
		return false
	}
	log.Printf("  Validating against %s: ✅", schemaName)
	return true
}

func validateWithObjectValidator(serverData any) bool {
	var serverDetail apiv0.ServerJSON
	serverDataBytes, err := json.Marshal(serverData)
	if err != nil {
		log.Printf("  Validating with Go Validator: ❌")
		log.Printf("    Error marshaling server data: %v", err)
		return false
	}

	if err := json.Unmarshal(serverDataBytes, &serverDetail); err != nil {
		log.Printf("  Validating with Go Validator: ❌")
		log.Printf("    Error unmarshaling to ServerDetail: %v", err)
		return false
	}

	if err := validators.ValidateServerJSON(&serverDetail); err != nil {
		log.Printf("  Validating with Go Validator: ❌")
		log.Printf("    Error: %v", err)
		return false
	}

	log.Printf("  Validating with Go Validator: ✅")
	return true
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
	re := regexp.MustCompile("(?s)```json\r?\n(.*?)\r?\n```")
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
		basePath := filepath.Join(filepath.Dir(path), "server.schema.json")
		baseData, err := os.ReadFile(basePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read base schema: %w", err)
		}

		// Add the base schema to the compiler with the expected URL
		if err := compiler.AddResource("https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json", bytes.NewReader(baseData)); err != nil {
			return nil, fmt.Errorf("failed to add base schema resource: %w", err)
		}
	}

	return compiler.Compile(path)
}
