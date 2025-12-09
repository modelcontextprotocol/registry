// validate-examples validates JSON examples in documentation files
// against both schema.json and Go validators.
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

type validationTarget struct {
	path          string
	requireSchema bool
	expectedCount *int
}

func main() {
	log.SetFlags(0) // Remove timestamp from logs

	if err := runValidation(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func runValidation() error {
	// Define what we validate and how
	expectedServerJSONCount := 12
	targets := []validationTarget{
		{
			path:          filepath.Join("docs", "reference", "server-json", "generic-server-json.md"),
			requireSchema: false,
			expectedCount: &expectedServerJSONCount,
		},
		{
			path:          filepath.Join("docs", "modelcontextprotocol-io", "package-types.mdx"),
			requireSchema: true,
			expectedCount: nil, // No count validation for guide
		},
		{
			path:          filepath.Join("docs", "modelcontextprotocol-io", "quickstart.mdx"),
			requireSchema: true,
			expectedCount: nil, // No count validation for guide
		},
		{
			path:          filepath.Join("docs", "modelcontextprotocol-io", "remote-servers.mdx"),
			requireSchema: true,
			expectedCount: nil, // No count validation for guide
		},
	}

	schemaPath := filepath.Join("docs", "reference", "server-json", "server.schema.json")
	baseSchema, err := compileSchema(schemaPath)
	if err != nil {
		return fmt.Errorf("failed to compile server.schema.json: %w", err)
	}

	for _, target := range targets {
		if err := validateFile(target, baseSchema); err != nil {
			return err
		}
		log.Println()
	}

	log.Println("All validations passed!")
	return nil
}

func validateFile(target validationTarget, baseSchema *jsonschema.Schema) error {
	examples, err := extractExamples(target.path, target.requireSchema)
	if err != nil {
		return fmt.Errorf("failed to extract examples from %s: %w", target.path, err)
	}

	log.Printf("Validating %s: found %d examples\n", target.path, len(examples))

	if target.expectedCount != nil && len(examples) != *target.expectedCount {
		return fmt.Errorf("expected %d examples in %s but found %d - if this is intentional, update expectedCount in tools/validate-examples/main.go",
			*target.expectedCount, target.path, len(examples))
	}

	if len(examples) == 0 {
		log.Println("  No examples to validate")
		return nil
	}

	log.Println()

	validatedCount := 0
	for i, example := range examples {
		log.Printf("  Example %d (line %d):", i+1, example.line)

		if validateExample(example, baseSchema) {
			validatedCount++
		}

		log.Println()
	}

	if validatedCount != len(examples) {
		return fmt.Errorf("validation failed for %s: expected %d examples to pass but only %d did",
			target.path, len(examples), validatedCount)
	}

	return nil
}

func validateExample(ex example, baseSchema *jsonschema.Schema) bool {
	var data any
	if err := json.Unmarshal([]byte(ex.content), &data); err != nil {
		log.Printf("    ❌ Invalid JSON: %v", err)
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
					log.Printf("    Invalid PublishRequest property: ❌ %s (only 'server' and optional 'x-publisher' are allowed)", key)
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
		log.Printf("    Validating against %s: ❌", schemaName)
		log.Printf("      Error: %v", err)
		return false
	}
	log.Printf("    Validating against %s: ✅", schemaName)
	return true
}

func validateWithObjectValidator(serverData any) bool {
	var serverDetail apiv0.ServerJSON
	serverDataBytes, err := json.Marshal(serverData)
	if err != nil {
		log.Printf("    Validating with Go Validator: ❌")
		log.Printf("      Error marshaling server data: %v", err)
		return false
	}

	if err := json.Unmarshal(serverDataBytes, &serverDetail); err != nil {
		log.Printf("    Validating with Go Validator: ❌")
		log.Printf("      Error unmarshaling to ServerDetail: %v", err)
		return false
	}

	if err := validators.ValidateServerJSON(&serverDetail); err != nil {
		log.Printf("    Validating with Go Validator: ❌")
		log.Printf("      Error: %v", err)
		return false
	}

	log.Printf("    Validating with Go Validator: ✅")
	return true
}

type example struct {
	content string
	line    int
}

func extractExamples(path string, requireSchema bool) ([]example, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)

	// Regex to match JSON code blocks in markdown
	re := regexp.MustCompile("(?s)```json(?: [^\r\n]+)?\r?\n(.*?)\r?\n```")
	matches := re.FindAllStringSubmatchIndex(content, -1)

	var examples []example
	for _, match := range matches {
		if len(match) < 4 {
			return nil, fmt.Errorf("invalid match - expected at least 4 indices but got %d", len(match))
		}
		start, end := match[2], match[3]
		jsonContent := content[start:end]

		// Filter by $schema if required
		if requireSchema && !strings.Contains(jsonContent, "$schema") {
			continue
		}

		line := 1 + strings.Count(content[:start], "\n")
		examples = append(examples, example{
			content: jsonContent,
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
		if err := compiler.AddResource("https://static.modelcontextprotocol.io/schemas/2025-09-16/server.schema.json", bytes.NewReader(baseData)); err != nil {
			return nil, fmt.Errorf("failed to add base schema resource: %w", err)
		}
	}

	return compiler.Compile(path)
}
