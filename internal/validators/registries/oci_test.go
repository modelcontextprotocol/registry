package registries_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateOCI_RegistryAllowlist(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		identifier  string
		expectError bool
		errorMsg    string
	}{
		// Allowed registries - use real public images that exist
		// These should fail with "missing required annotation" (no MCP label)
		// NOT with "unsupported registry", "does not exist", or "is private" errors
		{
			name:        "Docker Hub should be allowed",
			identifier:  "docker.io/library/alpine:latest",
			expectError: true,
			errorMsg:    "missing required annotation",
		},
		{
			name:        "Docker Hub without explicit registry should default and be allowed",
			identifier:  "library/hello-world:latest",
			expectError: true,
			errorMsg:    "missing required annotation",
		},
		{
			name:        "GHCR should be allowed",
			identifier:  "ghcr.io/containerbase/base:latest",
			expectError: true,
			errorMsg:    "missing required annotation",
		},
		{
			name:        "Artifact Registry regional should be allowed",
			identifier:  "us-central1-docker.pkg.dev/database-toolbox/toolbox/toolbox:latest",
			expectError: true,
			errorMsg:    "missing required annotation",
		},
		{
			name:        "Artifact Registry multi-region should be allowed",
			identifier:  "us-docker.pkg.dev/berglas/berglas/berglas:latest",
			expectError: true,
			errorMsg:    "missing required annotation",
		},
		{
			name:        "MCR should be allowed",
			identifier:  "mcr.microsoft.com/dotnet/aspire-dashboard:9.5.0",
			expectError: true,
			errorMsg:    "missing required annotation",
		},
		{
			name:        "ACR should be allowed",
			identifier:  "azurearcjumpstart.azurecr.io/hello-arc:latest",
			expectError: true,
			errorMsg:    "missing required annotation",
		},

		// Disallowed registries
		{
			name:        "GCR should be rejected",
			identifier:  "gcr.io/test/image:latest",
			expectError: true,
			errorMsg:    "unsupported OCI registry",
		},
		{
			name:        "Quay.io should be rejected",
			identifier:  "quay.io/test/image:latest",
			expectError: true,
			errorMsg:    "unsupported OCI registry",
		},
		{
			name:        "ECR Public should be rejected",
			identifier:  "public.ecr.aws/test/image:latest",
			expectError: true,
			errorMsg:    "unsupported OCI registry",
		},
		{
			name:        "GitLab registry should be rejected",
			identifier:  "registry.gitlab.com/test/image:latest",
			expectError: true,
			errorMsg:    "unsupported OCI registry",
		},
		{
			name:        "Custom registry should be rejected",
			identifier:  "custom-registry.com/test/image:latest",
			expectError: true,
			errorMsg:    "unsupported OCI registry",
		},
		{
			name:        "Harbor registry should be rejected",
			identifier:  "harbor.example.com/test/image:latest",
			expectError: true,
			errorMsg:    "unsupported OCI registry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   tt.identifier,
			}

			err := registries.ValidateOCI(ctx, pkg, "com.example/test")

			if tt.expectError {
				assert.Error(t, err)
				// Should contain the specific error message
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateOCI_RejectsOldFormat(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		pkg          model.Package
		errorMessage string
	}{
		{
			name: "OCI package with registryBaseUrl should be rejected",
			pkg: model.Package{
				RegistryType:    model.RegistryTypeOCI,
				RegistryBaseURL: "https://docker.io",
				Identifier:      "docker.io/test/image:latest",
			},
			errorMessage: "OCI packages must not have 'registryBaseUrl' field",
		},
		{
			name: "OCI package with version field should be rejected",
			pkg: model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   "docker.io/test/image:latest",
				Version:      "1.0.0",
			},
			errorMessage: "OCI packages must not have 'version' field",
		},
		{
			name: "OCI package with fileSha256 field should be rejected",
			pkg: model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   "docker.io/test/image:latest",
				FileSHA256:   "abcd1234",
			},
			errorMessage: "OCI packages must not have 'fileSha256' field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registries.ValidateOCI(ctx, tt.pkg, "com.example/test")

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMessage)
		})
	}
}

func TestValidateOCI_InvalidReferences(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		identifier string
	}{
		{
			name:       "invalid characters in reference",
			identifier: "docker.io/test/image:INVALID SPACE",
		},
		{
			name:       "malformed reference",
			identifier: "not-a-valid-reference::::",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   tt.identifier,
			}

			err := registries.ValidateOCI(ctx, pkg, "com.example/test")
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid OCI reference")
		})
	}
}

func TestValidateOCI_EmptyIdentifier(t *testing.T) {
	ctx := context.Background()

	pkg := model.Package{
		RegistryType: model.RegistryTypeOCI,
		Identifier:   "",
	}

	err := registries.ValidateOCI(ctx, pkg, "com.example/test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "package identifier is required")
}

func TestValidateOCI_SuccessfulValidation(t *testing.T) {
	ctx := context.Background()

	// Test with a real MCP server image that has the correct label
	pkg := model.Package{
		RegistryType: model.RegistryTypeOCI,
		Identifier:   "ghcr.io/github/github-mcp-server:latest",
	}

	err := registries.ValidateOCI(ctx, pkg, "io.github.github/github-mcp-server")
	assert.NoError(t, err)
}

func TestValidateOCI_LabelMismatch(t *testing.T) {
	ctx := context.Background()

	// Test with a real MCP server image but wrong expected server name
	// This should fail because the label doesn't match
	pkg := model.Package{
		RegistryType: model.RegistryTypeOCI,
		Identifier:   "ghcr.io/github/github-mcp-server:latest",
	}

	err := registries.ValidateOCI(ctx, pkg, "io.github.github/github-mcp-server-mismatch")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ownership validation failed")
	assert.Contains(t, err.Error(), "Expected annotation")
}
