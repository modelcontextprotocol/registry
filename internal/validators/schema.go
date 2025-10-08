package validators

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed schema/server.schema.json
var embeddedSchema []byte

// GetCurrentSchemaVersion extracts the $id field from the embedded schema
func GetCurrentSchemaVersion() (string, error) {
	var schema map[string]any
	if err := json.Unmarshal(embeddedSchema, &schema); err != nil {
		return "", fmt.Errorf("failed to parse embedded schema: %w", err)
	}

	id, ok := schema["$id"].(string)
	if !ok {
		return "", fmt.Errorf("embedded schema missing $id field")
	}

	return id, nil
}

// validateServerJSONSchema validates the server JSON against server.schema.json using jsonschema
func validateServerJSONSchema(serverJSON *apiv0.ServerJSON) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}

	// Use embedded schema - no file system access needed
	schemaData := embeddedSchema

	// Parse the schema
	var schema map[string]any
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

	var serverMap map[string]any
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
		var validationErr *jsonschema.ValidationError
		if errors.As(err, &validationErr) {
			// Process the validation error and its causes
			addValidationError(result, validationErr, schema)
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
func addValidationError(result *ValidationResult, validationErr *jsonschema.ValidationError, schema map[string]any) {
	// Use DetailedOutput to get the nested error details
	detailed := validationErr.DetailedOutput()

	// Process the detailed error structure

	addDetailedErrors(result, detailed, schema)
}

// addDetailedErrors recursively processes detailed validation errors
func addDetailedErrors(result *ValidationResult, detailed jsonschema.Detailed, schema map[string]any) {
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

		// Build the full resolved reference path
		reference := buildResolvedReference(detailed.KeywordLocation, detailed.AbsoluteKeywordLocation, schema)

		issue := NewValidationIssue(
			ValidationIssueTypeSchema,
			path,
			message,
			ValidationIssueSeverityError,
			reference, // cleaned schema rule path for deterministic mapping
		)
		result.AddIssue(issue)
	}

	// Process nested errors
	for _, nested := range detailed.Errors {
		addDetailedErrors(result, nested, schema)
	}
}

// buildResolvedReference extracts the resolved reference path by resolving $ref segments
func buildResolvedReference(keywordLocation, absoluteKeywordLocation string, schema map[string]any) string {
	if keywordLocation == "" || absoluteKeywordLocation == "" {
		return ""
	}

	// Clean up the absolute location by removing file:// prefix
	absolute := absoluteKeywordLocation
	if strings.HasPrefix(absolute, "file://") {
		absolute = strings.TrimPrefix(absolute, "file://")
		if idx := strings.Index(absolute, "#"); idx != -1 {
			absolute = absolute[idx:] // Keep only the #/path part
		}
	}

	// Parse the keyword location to understand the $ref chain
	keyword := strings.TrimPrefix(keywordLocation, "/")
	keywordParts := strings.Split(keyword, "/")

	// Build the path showing $ref resolution
	pathSegments := make([]string, 0)

	// Track the resolved path so far (starts empty, gets built up as we resolve $refs)
	resolvedPath := ""

	// Process each part of the keyword path
	for i, part := range keywordParts {
		if part == "" {
			continue // Skip empty parts
		}

		if part == "$ref" {
			// This is a $ref - we need to look up what it resolves to
			// For the first $ref, use the path from the root
			// For subsequent $refs, use the resolved path from the previous $ref plus the current segment
			var refPath string
			if resolvedPath == "" {
				// First $ref - use the path from the root
				refPath = strings.Join(keywordParts[:i+1], "/")
				refPath = "/" + refPath
			} else {
				// Subsequent $ref - use the resolved path plus the current segment
				refPath = resolvedPath + "/" + part
			}

			// Look up the $ref value in the schema
			refValue := resolveRefInSchema(schema, refPath)

			if refValue != "" {
				pathSegments = append(pathSegments, fmt.Sprintf("[%s]", refValue))
				// Update the resolved path for the next $ref
				resolvedPath = refValue
			} else {
				pathSegments = append(pathSegments, "[$ref]")
			}
		} else {
			// Regular path segment
			pathSegments = append(pathSegments, part)
			// Add this segment to the resolved path for the next $ref
			if resolvedPath != "" {
				resolvedPath = resolvedPath + "/" + part
			} else {
				resolvedPath = part
			}
		}
	}

	// Build the final reference string
	if len(pathSegments) > 0 {
		pathStr := strings.Join(pathSegments, "/")
		return fmt.Sprintf("%s from: %s", absolute, pathStr)
	}

	// Fallback: return the absolute location with context
	return absolute + " (from: " + keywordLocation + ")"
}

// resolveRefInSchema looks up a $ref value in the schema
func resolveRefInSchema(schema map[string]any, refPath string) string {
	// Handle the # prefix - it indicates the root of the schema JSON
	refPath = strings.TrimPrefix(refPath, "#")

	// Parse the JSON pointer path
	pathParts := strings.Split(strings.TrimPrefix(refPath, "/"), "/")

	// Navigate through the schema to find the $ref value
	var current any = schema
	for _, part := range pathParts {
		if part == "" {
			continue
		}

		if part == "$ref" {
			// We've reached the $ref, return its value
			if currentMap, ok := current.(map[string]any); ok {
				if refValue, ok := currentMap["$ref"].(string); ok {
					return refValue
				}
			}
			return ""
		}

		// Navigate to the next level
		// Check if this is an array index
		if index, err := strconv.Atoi(part); err == nil {
			// This is an array index - check if current element is an array
			if arr, ok := current.([]any); ok && index < len(arr) {
				current = arr[index]
			} else {
				// Current element is not an array or index out of bounds
				return ""
			}
		} else {
			// This is a map key
			if currentMap, ok := current.(map[string]any); ok {
				current = currentMap[part]
			} else {
				// Current element is not a map
				return ""
			}
		}
	}

	return ""
}
