package validators_test

import (
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateServerJSONExhaustive_CollectsAllErrors(t *testing.T) {
	// Create a server JSON with multiple validation errors
	serverJSON := &apiv0.ServerJSON{
		Name:        "invalid-name", // Invalid server name format
		Version:     "^1.0.0",       // Invalid version range
		Description: "Test server",
		Repository: model.Repository{
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
	result := validators.ValidateServerJSONExhaustive(serverJSON, false)

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

func TestValidateServerJSONExhaustive_ValidServer(t *testing.T) {
	// Create a valid server JSON
	serverJSON := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example.test/valid-server",
		Version:     "1.0.0",
		Description: "A valid test server",
		Repository: model.Repository{
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
	result := validators.ValidateServerJSONExhaustive(serverJSON, false)

	// Verify it's valid
	assert.True(t, result.Valid)
	assert.Empty(t, result.Issues, "Should have no validation issues")
}

func TestValidateServerJSONExhaustive_ContextPaths(t *testing.T) {
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
	result := validators.ValidateServerJSONExhaustive(serverJSON, false)

	// Verify we have issues at the correct paths
	issuePaths := make(map[string]bool)
	for _, issue := range result.Issues {
		issuePaths[issue.Path] = true
	}

	// Should have issues at specific nested paths
	assert.True(t, issuePaths["packages[0].version"], "Should have issue at packages[0].version")
	assert.True(t, issuePaths["packages[1].runtimeArguments[0].name"], "Should have issue at packages[1].runtimeArguments[0].name")
}

func TestValidateServerJSONExhaustive_RefResolution(t *testing.T) {
	// Create a server JSON with validation errors that will trigger $ref resolution
	serverJSON := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example.test/invalid-server",
		Version:     "1.0.0",
		Description: "Test server with validation errors",
		Repository: model.Repository{
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
	result := validators.ValidateServerJSONExhaustive(serverJSON, true)

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
			if issue.Path == "packages.0.packageArguments.0.format" {
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
	assert.True(t, issuePaths["packages.0.packageArguments.0.format"], "Should have issue at packages.0.packageArguments.0.format")
}
