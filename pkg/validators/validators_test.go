package validators_test

import (
	"errors"
	"testing"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/modelcontextprotocol/registry/pkg/validators"
)

// TestValidateServerJSON_PublicAPI exercises the public facade end to end: a
// well-formed server.json passes, and a malformed one is reported as invalid.
func TestValidateServerJSON_PublicAPI(t *testing.T) {
	valid := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Repository: &model.Repository{
			URL:    "https://github.com/owner/repo",
			Source: "github",
		},
		Version: "1.0.0",
	}

	result := validators.ValidateServerJSON(valid, validators.ValidationSchemaVersionAndSemantic)
	if result == nil {
		t.Fatal("expected a non-nil validation result")
	}
	if !result.Valid {
		t.Fatalf("expected known-good server.json to be valid, got issues: %+v", result.Issues)
	}

	// A top-level version range is rejected by semantic validation.
	invalid := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Repository: &model.Repository{
			URL:    "https://github.com/owner/repo",
			Source: "github",
		},
		Version: "^1.2.3",
	}

	result = validators.ValidateServerJSON(invalid, validators.ValidationSchemaVersionAndSemantic)
	if result.Valid {
		t.Fatal("expected known-bad server.json (version range) to be invalid")
	}
	if err := result.FirstError(); err == nil {
		t.Fatal("expected FirstError to return an error for an invalid result")
	}
}

// TestSentinelErrorsAreMatchable confirms the re-exported sentinel errors are the
// same values as the internal ones, so consumers can use errors.Is instead of
// matching on error strings.
func TestSentinelErrorsAreMatchable(t *testing.T) {
	if !errors.Is(validators.ErrVersionLooksLikeRange, validators.ErrVersionLooksLikeRange) {
		t.Fatal("sentinel error should match itself via errors.Is")
	}
	if validators.ErrVersionLooksLikeRange.Error() == "" {
		t.Fatal("expected re-exported sentinel to carry a message")
	}
}
