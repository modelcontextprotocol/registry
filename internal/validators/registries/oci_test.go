package registries

import (
	"context"
	"os"
	"testing"

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
	err := ValidateOCI(context.Background(), pkg, "io.github.owner/repo")
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
	token := lookupOCIAuthToken(model.RegistryURLGHCR)
	if token != "secret123" {
		t.Fatalf("expected token from %s env var, got %q", hostVar, token)
	}
}

func TestLookupOCIAuthTokenMapping(t *testing.T) {
	os.Setenv("MCP_REGISTRY_OCI_REGISTRY_AUTH", "ghcr.io=abc123,docker.io=def456")
	defer os.Unsetenv("MCP_REGISTRY_OCI_REGISTRY_AUTH")
	token := lookupOCIAuthToken(model.RegistryURLGHCR)
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
