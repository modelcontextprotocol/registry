package validators_test

import (
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateServerJSONDetailed_CollectsAllErrors(t *testing.T) {
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
	result := validators.ValidateServerJSONDetailed(serverJSON, false)

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

func TestValidateServerJSONDetailed_ValidServer(t *testing.T) {
	// Create a valid server JSON
	serverJSON := &apiv0.ServerJSON{
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
	result := validators.ValidateServerJSONDetailed(serverJSON, false)

	// Verify it's valid
	assert.True(t, result.Valid)
	assert.Empty(t, result.Issues, "Should have no validation issues")
}

func TestValidateServerJSONDetailed_ContextPaths(t *testing.T) {
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
	result := validators.ValidateServerJSONDetailed(serverJSON, false)

	// Verify we have issues at the correct paths
	issuePaths := make(map[string]bool)
	for _, issue := range result.Issues {
		issuePaths[issue.Path] = true
	}

	// Should have issues at specific nested paths
	assert.True(t, issuePaths["packages[0].version"], "Should have issue at packages[0].version")
	assert.True(t, issuePaths["packages[1].runtimeArguments[0].name"], "Should have issue at packages[1].runtimeArguments[0].name")
}
