package registries_test

import (
	"context"
	"os"
	"testing"

	registries "github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// This test focuses on validating preliminary logic for ghcr base URL acceptance and token lookup.
// Network calls are not performed (we use an obviously invalid identifier so validation should fail
// before HTTP fetch when base URL mismatch would have happened previously). We check that the
// error does not complain about unsupported base URL when using ghcr.io.
func TestValidateOCI_GHCRBaseURLAccepted(t *testing.T) {
	pkg := model.Package{
		RegistryType:    model.RegistryTypeOCI,
		RegistryBaseURL: model.RegistryURLGHCR,
		Identifier:      "owner/repo", // intentionally simple; subsequent HTTP will fail
		Version:         "latest",
	}
	err := registries.ValidateOCI(context.Background(), pkg, "io.github.owner/repo")
	if err == nil {
		t.Skip("Network access allowed? Unexpected success; skipping as test environment may have real image")
	}
	// Ensure error is not about unsupported base URL
	if contains(err.Error(), "not valid for registry type") {
		t.Fatalf("expected ghcr.io base URL to be accepted, got error: %v", err)
	}
}

func TestLookupOCIAuthTokenEnvSpecific(t *testing.T) {
	hostVar := "MCP_REGISTRY_OCI_TOKEN_GHCR_IO"
	os.Setenv(hostVar, "secret123")
	defer os.Unsetenv(hostVar)
	token := registries.LookupOCIAuthTokenForTest(model.RegistryURLGHCR)
	if token != "secret123" {
		t.Fatalf("expected token from %s env var, got %q", hostVar, token)
	}
}

func TestLookupOCIAuthTokenMapping(t *testing.T) {
	os.Setenv("MCP_REGISTRY_OCI_REGISTRY_AUTH", "ghcr.io=abc123,docker.io=def456")
	defer os.Unsetenv("MCP_REGISTRY_OCI_REGISTRY_AUTH")
	token := registries.LookupOCIAuthTokenForTest(model.RegistryURLGHCR)
	if token != "abc123" {
		t.Fatalf("expected token 'abc123' from mapping, got %q", token)
	}
}

// contains is a tiny helper to avoid importing strings (keep scope minimal for test file)
func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && index(haystack, needle) >= 0)
}

// index is naive substring search to avoid extra imports for this small test
func index(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestValidateOCI_UnsupportedRegistry(t *testing.T) {
	ctx := context.Background()

	pkg := model.Package{
		RegistryType:    model.RegistryTypeOCI,
		RegistryBaseURL: "https://unsupported-registry.com",
		Identifier:      "test/image",
		Version:         "latest",
	}

	err := registries.ValidateOCI(ctx, pkg, "com.example/test")
	if err == nil {
		t.Fatalf("expected error for unsupported registry base URL, got nil")
	}
	if !contains(err.Error(), "registry type and base URL do not match") {
		t.Fatalf("expected mismatch error, got: %v", err)
	}
}

func TestValidateOCI_SupportedRegistries(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		registryURL string
		expected    bool
	}{
		{
			name:        "Docker Hub should be supported",
			registryURL: model.RegistryURLDocker,
			expected:    true,
		},
		{
			name:        "GHCR should be supported",
			registryURL: model.RegistryURLGHCR,
			expected:    true,
		},
		{
			name:        "Unsupported registry should fail",
			registryURL: "https://quay.io",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType:    model.RegistryTypeOCI,
				RegistryBaseURL: tt.registryURL,
				Identifier:      "test/image",
				Version:         "latest",
			}

			err := registries.ValidateOCI(ctx, pkg, "com.example/test")
			if tt.expected {
				// Should not fail immediately on registry validation
				// (may fail later due to network/image not found, but not due to unsupported registry)
				if err != nil && contains(err.Error(), "registry type and base URL do not match") {
					t.Fatalf("did not expect mismatch error for supported registry, got: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error for unsupported registry, got nil")
				}
				if !contains(err.Error(), "registry type and base URL do not match") {
					t.Fatalf("expected mismatch error, got: %v", err)
				}
			}
		})
	}
}
