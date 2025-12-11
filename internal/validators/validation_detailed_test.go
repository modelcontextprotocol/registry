package validators_test

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

const schemaPath = "schema"

func TestValidateServerJSON_CollectsAllErrors(t *testing.T) {
	// Create a server JSON with multiple validation errors
	serverJSON := &apiv0.ServerJSON{
		Name:        "invalid-name", // Invalid server name format
		Version:     "^1.0.0",       // Invalid version range
		Description: "Test server",
		Repository: &model.Repository{
			URL:    "not-a-valid-url", // Invalid repository URL
			Source: "github",
		},
		WebsiteURL: "ftp://invalid-scheme.com", // Invalid website URL scheme
		Packages: []model.Package{
			{
				RegistryType:    model.RegistryTypeOCI,
				RegistryBaseURL: "https://docker.io",
				Identifier:      "package with spaces", // Invalid package name
				Version:         "latest",              // Reserved version
				Transport: model.Transport{
					Type: model.TransportTypeStdio,
					URL:  "should-not-have-url", // Invalid stdio transport with URL
				},
				RuntimeArguments: []model.Argument{
					{
						Type: model.ArgumentTypeNamed,
						Name: "--port <port>", // Invalid argument name
					},
				},
			},
		},
		Remotes: []model.Transport{
			{
				Type: model.TransportTypeStdio, // Invalid remote transport type
				URL:  "",                       // Missing URL for remote
			},
		},
	}

	// Run detailed validation
	result := validators.ValidateServerJSON(serverJSON, validators.ValidationSchemaVersionAndSemantic)

	// Verify it's invalid
	assert.False(t, result.Valid)
	assert.Greater(t, len(result.Issues), 5, "Should have multiple validation issues")

	// Check that we have issues of different types and severities
	hasError := false
	hasSemantic := false

	for _, issue := range result.Issues {
		if issue.Severity == validators.ValidationIssueSeverityError {
			hasError = true
		}
		if issue.Type == validators.ValidationIssueTypeSemantic {
			hasSemantic = true
		}
	}

	assert.True(t, hasError, "Should have error severity issues")
	assert.True(t, hasSemantic, "Should have semantic type issues")

	// Verify specific issues exist
	issuePaths := make(map[string]bool)
	for _, issue := range result.Issues {
		issuePaths[issue.Path] = true
	}

	// Check for expected issue paths
	expectedPaths := []string{
		"name",
		"version",
		"repository.url",
		"websiteUrl",
		"packages[0].identifier",
		"packages[0].version",
		"packages[0].transport.url",
		"packages[0].runtimeArguments[0].name",
		"remotes[0].type",
		"remotes[0].url",
	}

	foundPaths := 0
	for _, expectedPath := range expectedPaths {
		if issuePaths[expectedPath] {
			foundPaths++
		}
	}

	assert.Greater(t, foundPaths, 5, "Should have issues at multiple JSON paths")
}

func TestValidateServerJSON_ValidServer(t *testing.T) {
	// Create a valid server JSON
	serverJSON := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example.test/valid-server",
		Version:     "1.0.0",
		Description: "A valid test server",
		Repository: &model.Repository{
			URL:    "https://github.com/example/valid-server",
			Source: "github",
		},
		WebsiteURL: "https://test.example.com",
		Packages: []model.Package{
			{
				RegistryType:    model.RegistryTypeOCI,
				RegistryBaseURL: "https://docker.io",
				Identifier:      "valid-package",
				Version:         "1.0.0",
				Transport: model.Transport{
					Type: model.TransportTypeStdio,
				},
			},
		},
	}

	// Run detailed validation
	result := validators.ValidateServerJSON(serverJSON, validators.ValidationSchemaVersionAndSemantic)

	// Verify it's valid
	assert.True(t, result.Valid)
	assert.Empty(t, result.Issues, "Should have no validation issues")
}

func TestValidateServerJSON_ContextPaths(t *testing.T) {
	// Create a server with nested validation errors to test context paths
	serverJSON := &apiv0.ServerJSON{
		Name:    "com.example.test/server",
		Version: "1.0.0",
		Packages: []model.Package{
			{
				RegistryType:    model.RegistryTypeOCI,
				RegistryBaseURL: "https://docker.io",
				Identifier:      "package-1",
				Version:         "latest", // Error in first package
				Transport: model.Transport{
					Type: model.TransportTypeStdio,
				},
			},
			{
				RegistryType:    model.RegistryTypeOCI,
				RegistryBaseURL: "https://docker.io",
				Identifier:      "package-2",
				Version:         "2.0.0",
				Transport: model.Transport{
					Type: model.TransportTypeStdio,
				},
				RuntimeArguments: []model.Argument{
					{
						Type: model.ArgumentTypeNamed,
						Name: "invalid name", // Error in second package's argument
					},
				},
			},
		},
	}

	// Run detailed validation
	result := validators.ValidateServerJSON(serverJSON, validators.ValidationSchemaVersionAndSemantic)

	// Verify we have issues at the correct paths
	issuePaths := make(map[string]bool)
	for _, issue := range result.Issues {
		issuePaths[issue.Path] = true
	}

	// Should have issues at specific nested paths
	assert.True(t, issuePaths["packages[0].version"], "Should have issue at packages[0].version")
	assert.True(t, issuePaths["packages[1].runtimeArguments[0].name"], "Should have issue at packages[1].runtimeArguments[0].name")
}

func TestValidateServerJSON_RefResolution(t *testing.T) {
	// Create a server JSON with validation errors that will trigger $ref resolution
	serverJSON := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example.test/invalid-server",
		Version:     "1.0.0",
		Description: "Test server with validation errors",
		Repository: &model.Repository{
			URL:    "", // Empty URL should trigger format validation error in $ref'd Repository
			Source: "github",
		},
		Packages: []model.Package{
			{
				RegistryType:    model.RegistryTypeOCI,
				RegistryBaseURL: "https://docker.io",
				Identifier:      "test-package",
				Version:         "1.0.0",
				Transport: model.Transport{
					Type: model.TransportTypeSSE,
					URL:  "https://example.com",
				},
				PackageArguments: []model.Argument{
					{
						InputWithVariables: model.InputWithVariables{
							Input: model.Input{
								Format: "invalid-format", // This should trigger a validation error in the complex path
							},
						},
						Type: "named",
						Name: "test-arg",
					},
				},
			},
		},
	}

	// Run validation with schema validation enabled
	result := validators.ValidateServerJSON(serverJSON, validators.ValidationAll)

	// Check that we have validation errors
	assert.False(t, result.Valid, "Expected validation errors")
	assert.Greater(t, len(result.Issues), 0, "Expected at least one validation issue")

	// Check that we have schema validation issues with proper $ref resolution
	hasSchemaIssues := false
	for _, issue := range result.Issues {
		if issue.Type == validators.ValidationIssueTypeSchema {
			hasSchemaIssues = true
			// Check that there are no unresolved [$ref] segments
			assert.NotContains(t, issue.Reference, "[$ref]", "Found unresolved $ref segment in reference: %s", issue.Reference)

			// Check for exact resolved paths we expect
			if issue.Path == "repository.url" {
				expectedRef := "#/definitions/Repository/properties/url/format from: [#/definitions/ServerDetail]/properties/repository/[#/definitions/Repository]/properties/url/format"
				assert.Equal(t, expectedRef, issue.Reference, "Repository URL error should have exact resolved reference")
			}
			if issue.Path == "packages[0].packageArguments[0].format" {
				// The schema uses anyOf for Argument types, so it could match either PositionalArgument or NamedArgument
				// Just check that it contains the expected definitions
				assert.Contains(t, issue.Reference, "#/definitions/Input/properties/format/enum", "Should reference the Input format enum")
				assert.Contains(t, issue.Reference, "[#/definitions/InputWithVariables]", "Should reference InputWithVariables")
				assert.Contains(t, issue.Reference, "[#/definitions/Input]", "Should reference Input")
			}
		}
	}
	assert.True(t, hasSchemaIssues, "Expected schema validation issues with $ref resolution")

	// Check that we have issues at expected paths
	issuePaths := make(map[string]bool)
	for _, issue := range result.Issues {
		issuePaths[issue.Path] = true
	}

	// Should have issues at specific paths that trigger $ref resolution
	assert.True(t, issuePaths["repository.url"], "Should have issue at repository.url")
	assert.True(t, issuePaths["packages[0].packageArguments[0].format"], "Should have issue at packages[0].packageArguments[0].format")
}

func TestValidateServerJSON_EmptySchema(t *testing.T) {
	// Test that empty/missing schema produces an error
	serverJSON := &apiv0.ServerJSON{
		// Schema field intentionally omitted (empty string)
		Name:        "com.example.test/server",
		Version:     "1.0.0",
		Description: "Test server",
		Repository: &model.Repository{
			URL:    "https://github.com/example/server",
			Source: "github",
		},
	}

	result := validators.ValidateServerJSON(serverJSON, validators.ValidationAll)

	// Should be invalid due to missing schema
	assert.False(t, result.Valid, "Empty schema should cause validation failure")

	// Should have an error issue for missing schema
	hasSchemaError := false
	for _, issue := range result.Issues {
		if issue.Path == schemaPath && issue.Severity == validators.ValidationIssueSeverityError {
			if strings.Contains(issue.Message, "$schema field is required") {
				hasSchemaError = true
			}
		}
	}
	assert.True(t, hasSchemaError, "Should have error for missing $schema field")
}

func TestValidateServerJSON_NonCurrentSchema_Warning(t *testing.T) {
	// Test that non-current (but valid) schema produces a warning, not an error
	serverJSON := &apiv0.ServerJSON{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json", // Older but valid schema
		Name:        "com.example.test/server",
		Version:     "1.0.0",
		Description: "Test server",
		Repository: &model.Repository{
			URL:    "https://github.com/example/server",
			Source: "github",
		},
	}

	result := validators.ValidateServerJSON(serverJSON, validators.ValidationAll)

	// Should be valid (warnings don't make it invalid)
	assert.True(t, result.Valid, "Non-current schema should produce warning but still be valid")

	// Should have a warning issue for non-current schema
	hasSchemaWarning := false
	for _, issue := range result.Issues {
		if issue.Path == schemaPath && issue.Severity == validators.ValidationIssueSeverityWarning {
			if strings.Contains(issue.Message, "not the current version") || strings.Contains(issue.Message, "Consider updating") {
				hasSchemaWarning = true
			}
		}
	}
	assert.True(t, hasSchemaWarning, "Should have warning for non-current schema version")
}

func TestValidateServerJSON_InvalidSchema_Error(t *testing.T) {
	// Test that invalid/non-existent schema produces an error
	serverJSON := &apiv0.ServerJSON{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-01-27/server.schema.json", // Non-existent version
		Name:        "com.example.test/server",
		Version:     "1.0.0",
		Description: "Test server",
		Repository: &model.Repository{
			URL:    "https://github.com/example/server",
			Source: "github",
		},
	}

	result := validators.ValidateServerJSON(serverJSON, validators.ValidationAll)

	// Should be invalid due to schema not available
	assert.False(t, result.Valid, "Invalid schema version should cause validation failure")

	// Should have an error issue for schema not available
	hasSchemaError := false
	for _, issue := range result.Issues {
		if issue.Path == schemaPath && issue.Severity == validators.ValidationIssueSeverityError {
			if strings.Contains(issue.Message, "not available") || strings.Contains(issue.Message, "not found") {
				hasSchemaError = true
			}
		}
	}
	assert.True(t, hasSchemaError, "Should have error for invalid/non-existent schema version")
}

func TestValidateServerJSON_NonCurrentSchema_Policies(t *testing.T) {
	// Test all three policies for non-current schema handling
	serverJSON := &apiv0.ServerJSON{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json", // Older but valid schema
		Name:        "com.example.test/server",
		Version:     "1.0.0",
		Description: "Test server",
		Repository: &model.Repository{
			URL:    "https://github.com/example/server",
			Source: "github",
		},
	}

	tests := []struct {
		name             string
		policy           validators.SchemaVersionPolicy
		expectValid      bool
		expectWarning    bool
		expectError      bool
		expectIssueCount int
	}{
		{
			name:             "Allow policy - no warning or error",
			policy:           validators.SchemaVersionPolicyAllow,
			expectValid:      true,
			expectWarning:    false,
			expectError:      false,
			expectIssueCount: 0,
		},
		{
			name:             "Warn policy - warning but still valid",
			policy:           validators.SchemaVersionPolicyWarn,
			expectValid:      true,
			expectWarning:    true,
			expectError:      false,
			expectIssueCount: 1,
		},
		{
			name:             "Error policy - error and invalid",
			policy:           validators.SchemaVersionPolicyError,
			expectValid:      false,
			expectWarning:    false,
			expectError:      true,
			expectIssueCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := validators.ValidationOptions{
				ValidateSchema:         true,
				ValidateSemantic:       true,
				NonCurrentSchemaPolicy: tt.policy,
			}
			result := validators.ValidateServerJSON(serverJSON, opts)

			assert.Equal(t, tt.expectValid, result.Valid, "Validation result should match expected")

			hasWarning := false
			hasError := false
			schemaWarnings := 0
			schemaErrors := 0

			for _, issue := range result.Issues {
				if issue.Path == schemaPath {
					if issue.Severity == validators.ValidationIssueSeverityWarning {
						hasWarning = true
						schemaWarnings++
						if !strings.Contains(issue.Message, "not the current version") {
							t.Errorf("Warning message should mention 'not the current version', got: %s", issue.Message)
						}
					}
					if issue.Severity == validators.ValidationIssueSeverityError {
						if strings.Contains(issue.Message, "not the current version") {
							hasError = true
							schemaErrors++
						}
					}
				}
			}

			assert.Equal(t, tt.expectWarning, hasWarning, "Warning presence should match expected")
			assert.Equal(t, tt.expectError, hasError, "Error presence should match expected")

			// Count schema-related issues (excluding other validation issues)
			schemaIssueCount := 0
			for _, issue := range result.Issues {
				if issue.Path == schemaPath && (strings.Contains(issue.Message, "not the current version") || strings.Contains(issue.Message, "current version")) {
					schemaIssueCount++
				}
			}
			assert.Equal(t, tt.expectIssueCount, schemaIssueCount, "Schema issue count should match expected")
		})
	}
}
