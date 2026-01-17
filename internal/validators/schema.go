package validators

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed schemas/*.json
var schemaFS embed.FS

// extractVersionFromSchemaURL extracts the version identifier from a schema URL
// e.g., "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json" -> "2025-10-17"
// e.g., "https://static.modelcontextprotocol.io/schemas/draft/server.schema.json" -> "draft"
// Version identifier can contain: A-Z, a-z, 0-9, hyphen (-), underscore (_), tilde (~), and period (.)
func extractVersionFromSchemaURL(schemaURL string) (string, error) {
	// Pattern: /schemas/{identifier}/server.schema.json
	// Identifier allowed characters: A-Z, a-z, 0-9, -, _, ~, .
	re := regexp.MustCompile(`/schemas/([A-Za-z0-9_~.-]+)/server\.schema\.json`)
	matches := re.FindStringSubmatch(schemaURL)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid schema URL format: %s", schemaURL)
	}
	return matches[1], nil
}

// loadSchemaByVersion loads a schema file from the embedded filesystem by version
func loadSchemaByVersion(version string) ([]byte, error) {
	filename := fmt.Sprintf("schemas/%s.json", version)
	data, err := schemaFS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("schema version %s not found in embedded schemas: %w", version, err)
	}
	return data, nil
}

// GetCurrentSchemaVersion returns the current schema URL from constants
func GetCurrentSchemaVersion() (string, error) {
	return model.CurrentSchemaURL, nil
}

// validateServerJSONSchema validates the server JSON against the schema version specified in $schema using jsonschema
// Empty/missing schema always produces an error.
// If performValidation is true, performs full JSON Schema validation.
// If performValidation is false, only checks for empty schema (always an error) and handles non-current schemas per policy.
// nonCurrentPolicy determines how non-current (but valid) schema versions are handled when performValidation is true.
func validateServerJSONSchema(serverJSON *apiv0.ServerJSON, performValidation bool, nonCurrentPolicy SchemaVersionPolicy) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}
	ctx := &ValidationContext{}

	// Empty/missing schema is always an error
	if serverJSON.Schema == "" {
		issue := NewValidationIssue(
			ValidationIssueTypeSemantic,
			ctx.Field("schema").String(),
			"$schema field is required",
			ValidationIssueSeverityError,
			"schema-field-required",
		)
		result.AddIssue(issue)
		return result
	}

	// Extract version from the schema URL
	version, err := extractVersionFromSchemaURL(serverJSON.Schema)
	if err != nil {
		issue := NewValidationIssue(
			ValidationIssueTypeSchema,
			ctx.Field("schema").String(),
			fmt.Sprintf("failed to extract schema version from URL: %v", err),
			ValidationIssueSeverityError,
			"schema-version-extraction-error",
		)
		result.AddIssue(issue)
		return result
	}

	// Check if the schema version is the current one and handle based on policy
	currentSchemaURL, err := GetCurrentSchemaVersion()
	if err == nil && serverJSON.Schema != currentSchemaURL {
		// Extract current version for the message
		currentVersion, _ := extractVersionFromSchemaURL(currentSchemaURL)

		switch nonCurrentPolicy {
		case SchemaVersionPolicyError:
			issue := NewValidationIssue(
				ValidationIssueTypeSemantic,
				ctx.Field("schema").String(),
				fmt.Sprintf("schema version %s is not the current version (%s). Use the current schema version", version, currentVersion),
				ValidationIssueSeverityError,
				"schema-version-deprecated",
			)
			result.AddIssue(issue)
		case SchemaVersionPolicyWarn:
			issue := NewValidationIssue(
				ValidationIssueTypeSemantic,
				ctx.Field("schema").String(),
				fmt.Sprintf("schema version %s is not the current version (%s). Consider updating to the latest schema version", version, currentVersion),
				ValidationIssueSeverityWarning,
				"schema-version-deprecated",
			)
			result.AddIssue(issue)
		case SchemaVersionPolicyAllow:
			// No issue added - allow non-current schemas silently
		}
	}

	// Load the appropriate schema file to verify it exists (required for schema version validation)
	// This ensures that the specified schema version is available, even when not performing full validation
	schemaData, err := loadSchemaByVersion(version)
	if err != nil {
		issue := NewValidationIssue(
			ValidationIssueTypeSchema,
			ctx.Field("schema").String(),
			fmt.Sprintf("schema version %s not available: %v", version, err),
			ValidationIssueSeverityError,
			"schema-version-not-available",
		)
		result.AddIssue(issue)
		return result
	}

	// If not performing validation, return after performing schema version checks (done above)
	if !performValidation {
		return result
	}

	// Parse the schema
	var schema map[string]any
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		// If we can't parse the schema, return an error
		issue := NewValidationIssue(
			ValidationIssueTypeSchema,
			ctx.Field("schema").String(),
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

	// Get the schema $id for proper reference resolution
	// Schema files must have $id (required by JSON Schema spec and verified by sync process)
	// However, we check here in case a schema file exists but is malformed or missing $id
	schemaID, ok := schema["$id"].(string)
	if !ok {
		issue := NewValidationIssue(
			ValidationIssueTypeSchema,
			ctx.Field("schema").String(),
			fmt.Sprintf("schema file for version %s exists but is missing or has invalid $id field (required by JSON Schema spec)", version),
			ValidationIssueSeverityError,
			"schema-missing-id",
		)
		result.AddIssue(issue)
		return result
	}

	// Validate against schema using jsonschema library
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaID, bytes.NewReader(schemaData)); err != nil {
		// If we can't add the schema resource, return an error
		issue := NewValidationIssue(
			ValidationIssueTypeSchema,
			ctx.Field("schema").String(),
			fmt.Sprintf("failed to add schema resource: %v", err),
			ValidationIssueSeverityError,
			"schema-resource-error",
		)
		result.AddIssue(issue)
		return result
	}

	schemaInstance, err := compiler.Compile(schemaID)
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

// ConvertJSONPointerToBracketNotation converts a JSON Pointer path (RFC 6901) to bracket notation
// format to match the format used by semantic validation (ValidationContext).
// The transformation includes:
// 1. Remove leading slash from JSON Pointer format
// 2. Convert path separators from "/" to "."
// 3. Convert numeric array indices from dot notation to bracket notation
// Example: "/packages/0/transport" -> "packages[0].transport"
// Example: "/0/name" -> "[0].name"
// Example: "/packages/0/transport/1/url" -> "packages[0].transport[1].url"
func ConvertJSONPointerToBracketNotation(jsonPointer string) string {
	if jsonPointer == "" {
		return ""
	}

	// Step 1: Convert JSON Pointer to dot notation (remove leading slash, convert / to .)
	path := strings.TrimPrefix(jsonPointer, "/")
	path = strings.ReplaceAll(path, "/", ".")

	// Step 2: Convert dot notation array indices to bracket notation
	if path == "" {
		return ""
	}

	parts := strings.Split(path, ".")
	var result strings.Builder

	for i, part := range parts {
		// Check if part is a pure number (array index)
		if _, err := strconv.Atoi(part); err == nil {
			// It's a numeric index - use bracket notation
			result.WriteString(fmt.Sprintf("[%s]", part))
			// Add dot after bracket if next part exists and is a field name (not a number)
			if i < len(parts)-1 {
				nextPart := parts[i+1]
				if _, err := strconv.Atoi(nextPart); err != nil {
					// Next part is a field name, add dot separator
					result.WriteString(".")
				}
				// If next part is a number, no dot needed (brackets will connect: [0][1])
			}
		} else {
			// It's a field name
			// Add dot separator before field name if previous part was also a field name
			if i > 0 {
				prevPart := parts[i-1]
				if _, err := strconv.Atoi(prevPart); err != nil {
					// Previous was not a number (it's a field), need dot separator
					result.WriteString(".")
				}
				// If previous was a number, brackets already written, dot added after bracket above
			}
			result.WriteString(part)
		}
	}

	return result.String()
}

// addDetailedErrors recursively processes detailed validation errors
func addDetailedErrors(result *ValidationResult, detailed jsonschema.Detailed, schema map[string]any) {
	// Only process errors that have specific field paths and meaningful messages
	if detailed.InstanceLocation != "" && detailed.Error != "" {
		// Convert JSON Pointer format to bracket notation to match semantic validation format
		path := ConvertJSONPointerToBracketNotation(detailed.InstanceLocation)

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
