package validators

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// validateServerJSONSchema validates the server JSON against server.schema.json using jsonschema
func validateServerJSONSchema(serverJSON *apiv0.ServerJSON) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}

	// Load the schema file - find it relative to the binary's location
	schemaPath := "docs/reference/server-json/server.schema.json"

	// If running from bin/ directory, go up one level to find the schema
	if _, err := os.Stat(schemaPath); os.IsNotExist(err) {
		schemaPath = "../docs/reference/server-json/server.schema.json"
	}

	// Try to find the schema file relative to the current working directory
	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		// If we can't load the schema, return an error - schema validation is required
		issue := NewValidationIssue(
			ValidationIssueTypeSchema,
			"",
			fmt.Sprintf("failed to load schema file '%s': %v", schemaPath, err),
			ValidationIssueSeverityError,
			"schema-load-error",
		)
		result.AddIssue(issue)
		return result
	}

	// Parse the schema
	var schema map[string]interface{}
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		// If we can't parse the schema, return an error
		issue := NewValidationIssue(
			ValidationIssueTypeSchema,
			"",
			fmt.Sprintf("failed to parse schema file: %v", err),
			ValidationIssueSeverityError,
			"schema-parse-error",
		)
		result.AddIssue(issue)
		return result
	}

	// Convert the server JSON to a map for validation
	serverData, err := json.Marshal(serverJSON)
	if err != nil {
		issue := NewValidationIssue(
			ValidationIssueTypeJSON,
			"",
			fmt.Sprintf("failed to marshal server JSON for schema validation: %v", err),
			ValidationIssueSeverityError,
			"json-marshal-error",
		)
		result.AddIssue(issue)
		return result
	}

	var serverMap map[string]interface{}
	if err := json.Unmarshal(serverData, &serverMap); err != nil {
		issue := NewValidationIssue(
			ValidationIssueTypeJSON,
			"",
			fmt.Sprintf("failed to unmarshal server JSON for schema validation: %v", err),
			ValidationIssueSeverityError,
			"json-unmarshal-error",
		)
		result.AddIssue(issue)
		return result
	}

	// Validate against schema using jsonschema library
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("file:///server.schema.json", bytes.NewReader(schemaData)); err != nil {
		// If we can't add the schema resource, return an error
		issue := NewValidationIssue(
			ValidationIssueTypeSchema,
			"",
			fmt.Sprintf("failed to add schema resource: %v", err),
			ValidationIssueSeverityError,
			"schema-resource-error",
		)
		result.AddIssue(issue)
		return result
	}

	schemaInstance, err := compiler.Compile("file:///server.schema.json")
	if err != nil {
		// If we can't compile the schema, return an error
		issue := NewValidationIssue(
			ValidationIssueTypeSchema,
			"",
			fmt.Sprintf("failed to compile schema: %v", err),
			ValidationIssueSeverityError,
			"schema-compile-error",
		)
		result.AddIssue(issue)
		return result
	}

	// Perform validation
	if err := schemaInstance.Validate(serverMap); err != nil {
		// Convert validation error to our issue format
		if validationErr, ok := err.(*jsonschema.ValidationError); ok {
			// Process the validation error and its causes
			addValidationError(result, validationErr)
		} else {
			// Fallback for other error types
			issue := NewValidationIssue(
				ValidationIssueTypeSchema,
				"",
				fmt.Sprintf("schema validation failed: %v", err),
				ValidationIssueSeverityError,
				"schema-validation-error",
			)
			result.AddIssue(issue)
		}
	}

	return result
}

// addValidationError processes validation errors and extracts useful information
func addValidationError(result *ValidationResult, validationErr *jsonschema.ValidationError) {
	// Use DetailedOutput to get the nested error details
	detailed := validationErr.DetailedOutput()
	addDetailedErrors(result, detailed)
}

// addDetailedErrors recursively processes detailed validation errors
func addDetailedErrors(result *ValidationResult, detailed jsonschema.Detailed) {
	// Only process errors that have specific field paths and meaningful messages
	if detailed.InstanceLocation != "" && detailed.Error != "" {
		// Convert JSON Pointer to readable path (remove leading slash, convert / to .)
		path := strings.TrimPrefix(detailed.InstanceLocation, "/")
		path = strings.ReplaceAll(path, "/", ".")

		// Clean up the error message
		message := detailed.Error

		// Make messages more user-friendly
		if strings.Contains(message, "missing properties:") {
			message = strings.ReplaceAll(message, "missing properties:", "missing required fields:")
		}
		if strings.Contains(message, "is not valid") {
			message = strings.ReplaceAll(message, "is not valid", "has invalid format")
		}

		issue := NewValidationIssue(
			ValidationIssueTypeSchema,
			path,
			message,
			ValidationIssueSeverityError,
			"schema-validation",
		)
		result.AddIssue(issue)
	}

	// Process nested errors
	for _, nested := range detailed.Errors {
		addDetailedErrors(result, nested)
	}
}
