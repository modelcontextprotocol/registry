package commands_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/modelcontextprotocol/registry/cmd/publisher/commands"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCommand_Success(t *testing.T) {
	validateCallCount := 0

	server := SetupMockRegistryServer(t,
		nil, // No publish handler
		func(w http.ResponseWriter, _ *http.Request) {
			validateCallCount++
			w.Header().Set("Content-Type", "application/json")

			result := validators.ValidationResult{
				Valid:  true,
				Issues: []validators.ValidationIssue{},
			}

			_ = json.NewEncoder(w).Encode(result)
		},
	)

	SetupTestToken(t, server.URL, "test-token")

	serverJSON := apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	}
	CreateTestServerJSON(t, serverJSON)

	err := commands.ValidateCommand([]string{})

	assert.NoError(t, err)
	assert.Equal(t, 1, validateCallCount, "validate endpoint should be called")
}

func TestValidateCommand_WithErrors(t *testing.T) {
	validateCallCount := 0

	server := SetupMockRegistryServer(t,
		nil,
		func(w http.ResponseWriter, _ *http.Request) {
			validateCallCount++
			w.Header().Set("Content-Type", "application/json")

			result := validators.ValidationResult{
				Valid: false,
				Issues: []validators.ValidationIssue{
					{
						Type:      validators.ValidationIssueTypeSemantic,
						Path:      "version",
						Message:   "version must be a specific version, not a range",
						Severity:  validators.ValidationIssueSeverityError,
						Reference: "semantic-version-range",
					},
				},
			}

			_ = json.NewEncoder(w).Encode(result)
		},
	)

	SetupTestToken(t, server.URL, "test-token")

	serverJSON := apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "^1.0.0", // Invalid
	}
	CreateTestServerJSON(t, serverJSON)

	err := commands.ValidateCommand([]string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
	assert.Equal(t, 1, validateCallCount, "validate endpoint should be called")
}

func TestValidateCommand_DeprecatedSchema(t *testing.T) {
	server := SetupMockRegistryServer(t,
		nil,
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			body, _ := io.ReadAll(r.Body)
			var req apiv0.ServerJSON
			_ = json.Unmarshal(body, &req)

			result := validators.ValidationResult{
				Valid: false,
				Issues: []validators.ValidationIssue{
					{
						Type:      validators.ValidationIssueTypeSemantic,
						Path:      "schema",
						Message:   "schema version 2025-07-09 is not the current version",
						Severity:  validators.ValidationIssueSeverityWarning,
						Reference: "schema-version-deprecated",
					},
				},
			}

			_ = json.NewEncoder(w).Encode(result)
		},
	)

	SetupTestToken(t, server.URL, "test-token")

	serverJSON := apiv0.ServerJSON{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json",
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	}
	CreateTestServerJSON(t, serverJSON)

	err := commands.ValidateCommand([]string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema version 2025-07-09")
	assert.Contains(t, err.Error(), "Migration checklist:")
}

func TestValidateCommand_NoServerFile(t *testing.T) {
	server := SetupMockRegistryServer(t, nil, nil)
	SetupTestToken(t, server.URL, "test-token")

	// Don't create server.json
	tempDir, err := os.MkdirTemp("", "mcp-publisher-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalDir) }()

	_ = os.Chdir(tempDir)

	err = commands.ValidateCommand([]string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestValidateCommand_InvalidJSON(t *testing.T) {
	server := SetupMockRegistryServer(t, nil, nil)
	SetupTestToken(t, server.URL, "test-token")

	tempDir, err := os.MkdirTemp("", "mcp-publisher-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalDir) }()

	_ = os.Chdir(tempDir)

	// Create invalid JSON file
	err = os.WriteFile("server.json", []byte("{ invalid json }"), 0600)
	require.NoError(t, err)

	err = commands.ValidateCommand([]string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}
