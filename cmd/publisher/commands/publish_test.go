package commands_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/registry/cmd/publisher/commands"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishCommand_Success(t *testing.T) {
	// Setup mock server that returns success
	server := SetupMockRegistryServer(t, nil, nil)

	// Setup token
	SetupTestToken(t, server.URL, "test-token")

	// Create valid server.json
	serverJSON := apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	}
	CreateTestServerJSON(t, serverJSON)

	// Run publish command
	err := commands.PublishCommand([]string{})

	// Should succeed
	assert.NoError(t, err)
}

func TestPublishCommand_422ValidationFlow(t *testing.T) {
	validateCallCount := 0
	publishCallCount := 0

	// Setup mock server
	server := SetupMockRegistryServer(t,
		// Publish handler: return 422 for invalid schema
		func(w http.ResponseWriter, _ *http.Request) {
			publishCallCount++
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"Failed to publish server, invalid schema: call /validate for details"}`))
		},
		// Validate handler: return validation errors
		func(w http.ResponseWriter, r *http.Request) {
			validateCallCount++
			w.Header().Set("Content-Type", "application/json")

			body, _ := io.ReadAll(r.Body)
			var req apiv0.ServerJSON
			_ = json.Unmarshal(body, &req)

			// Return validation result with deprecated schema error
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

	// Setup token
	SetupTestToken(t, server.URL, "test-token")

	// Create server.json with deprecated schema
	serverJSON := apiv0.ServerJSON{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json",
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	}
	CreateTestServerJSON(t, serverJSON)

	// Run publish command
	err := commands.PublishCommand([]string{})

	// Should fail with validation error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema version 2025-07-09")
	assert.Contains(t, err.Error(), "Migration checklist:")
	assert.Contains(t, err.Error(), "Full changelog with examples:")

	// Verify both endpoints were called
	assert.Equal(t, 1, publishCallCount, "publish endpoint should be called once")
	assert.Equal(t, 1, validateCallCount, "validate endpoint should be called once after 422")
}

func TestPublishCommand_422WithMultipleIssues(t *testing.T) {
	validateCallCount := 0

	// Setup mock server
	server := SetupMockRegistryServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"Failed to publish server, invalid schema"}`))
		},
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
					{
						Type:      validators.ValidationIssueTypeSchema,
						Path:      "name",
						Message:   "name is required",
						Severity:  validators.ValidationIssueSeverityError,
						Reference: "schema-field-required",
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
		Version:     "^1.0.0", // Invalid version range
	}
	CreateTestServerJSON(t, serverJSON)

	err := commands.PublishCommand([]string{})

	require.Error(t, err)
	assert.Equal(t, 1, validateCallCount, "validate endpoint should be called")
}

func TestPublishCommand_NoToken(t *testing.T) {
	// Don't create a token file
	serverJSON := apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	}
	CreateTestServerJSON(t, serverJSON)

	err := commands.PublishCommand([]string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestPublishCommand_Non422Error(t *testing.T) {
	publishCallCount := 0

	server := SetupMockRegistryServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			publishCallCount++
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Unauthorized"}`))
		},
		nil, // No validate handler needed
	)

	SetupTestToken(t, server.URL, "invalid-token")

	serverJSON := apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	}
	CreateTestServerJSON(t, serverJSON)

	err := commands.PublishCommand([]string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish failed")
	assert.Equal(t, 1, publishCallCount, "publish endpoint should be called")
}

func TestPublishCommand_DeprecatedSchema(t *testing.T) {
	tests := []struct {
		name           string
		schema         string
		publishStatus  int
		validationOpts func(req apiv0.ServerJSON) validators.ValidationResult
		expectError    bool
		errorSubstr    string
		checkLinks     bool
	}{
		{
			name:          "deprecated 2025-07-09 schema should show warning",
			schema:        "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json",
			publishStatus: http.StatusUnprocessableEntity,
			validationOpts: func(_ apiv0.ServerJSON) validators.ValidationResult {
				return validators.ValidationResult{
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
			},
			expectError: true,
			errorSubstr: "schema version 2025-07-09",
			checkLinks:  true,
		},
		{
			name:          "current 2025-12-11 schema should pass validation",
			schema:        "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			publishStatus: http.StatusCreated,
			validationOpts: func(_ apiv0.ServerJSON) validators.ValidationResult {
				// Should not be called since publish succeeds
				return validators.ValidationResult{Valid: true}
			},
			expectError: false,
		},
		{
			name:          "empty schema should fail validation",
			schema:        "",
			publishStatus: http.StatusUnprocessableEntity,
			validationOpts: func(_ apiv0.ServerJSON) validators.ValidationResult {
				return validators.ValidationResult{
					Valid: false,
					Issues: []validators.ValidationIssue{
						{
							Type:      validators.ValidationIssueTypeSemantic,
							Path:      "schema",
							Message:   "$schema field is required",
							Severity:  validators.ValidationIssueSeverityError,
							Reference: "schema-field-required",
						},
					},
				}
			},
			expectError: true,
			errorSubstr: "$schema field is required",
			checkLinks:  true,
		},
		{
			name:          "custom schema without valid version should fail validation",
			schema:        "https://example.com/custom.schema.json",
			publishStatus: http.StatusUnprocessableEntity,
			validationOpts: func(_ apiv0.ServerJSON) validators.ValidationResult {
				return validators.ValidationResult{
					Valid: false,
					Issues: []validators.ValidationIssue{
						{
							Type:      validators.ValidationIssueTypeSchema,
							Path:      "schema",
							Message:   "failed to extract schema version from URL",
							Severity:  validators.ValidationIssueSeverityError,
							Reference: "schema-version-extraction-error",
						},
					},
				}
			},
			expectError: true,
			errorSubstr: "failed to extract schema version from URL",
			checkLinks:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validateCallCount := 0
			publishCallCount := 0

			// Setup mock server
			server := SetupMockRegistryServer(t,
				// Publish handler
				func(w http.ResponseWriter, _ *http.Request) {
					publishCallCount++
					if tt.publishStatus == http.StatusCreated {
						w.WriteHeader(http.StatusCreated)
						response := apiv0.ServerResponse{
							Server: apiv0.ServerJSON{
								Name:    "com.example/test-server",
								Version: "1.0.0",
							},
						}
						_ = json.NewEncoder(w).Encode(response)
					} else {
						w.WriteHeader(tt.publishStatus)
						_, _ = w.Write([]byte(`{"message":"Failed to publish server, invalid schema: call /validate for details"}`))
					}
				},
				// Validate handler (only called on 422)
				func(w http.ResponseWriter, r *http.Request) {
					validateCallCount++
					w.Header().Set("Content-Type", "application/json")

					body, _ := io.ReadAll(r.Body)
					var req apiv0.ServerJSON
					_ = json.Unmarshal(body, &req)

					result := tt.validationOpts(req)
					_ = json.NewEncoder(w).Encode(result)
				},
			)

			SetupTestToken(t, server.URL, "test-token")

			// Create server.json with specific schema
			serverJSON := apiv0.ServerJSON{
				Schema:      tt.schema,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
			}
			CreateTestServerJSON(t, serverJSON)

			err := commands.PublishCommand([]string{})

			if tt.expectError {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
				if tt.errorSubstr != "" {
					assert.Contains(t, err.Error(), tt.errorSubstr, "Error should contain expected substring")
				}
				if tt.checkLinks {
					assert.Contains(t, err.Error(), "Migration checklist:", "Error should contain migration checklist link")
					assert.Contains(t, err.Error(), "Full changelog with examples:", "Error should contain changelog link")
				}
				if tt.publishStatus == http.StatusUnprocessableEntity {
					assert.Equal(t, 1, publishCallCount, "publish endpoint should be called once")
					assert.Equal(t, 1, validateCallCount, "validate endpoint should be called after 422")
				}
			} else {
				assert.NoError(t, err, "Expected success for test case: %s", tt.name)
				assert.Equal(t, 1, publishCallCount, "publish endpoint should be called once")
				assert.Equal(t, 0, validateCallCount, "validate endpoint should not be called on success")
			}
		})
	}
}
