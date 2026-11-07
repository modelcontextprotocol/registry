package commands_test

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func TestPublishCommand_PrintsSubmittedIdentityOnFailure(t *testing.T) {
	server := SetupMockRegistryServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "duplicate version", http.StatusBadRequest)
	}, nil)
	SetupTestToken(t, server.URL, "test-token")
	CreateTestServerJSON(t, apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "io.github.BargLabs/cejel",
		Description: "A test server",
		Version:     "0.4.5",
	})

	output, err := os.CreateTemp(t.TempDir(), "stdout")
	require.NoError(t, err)
	originalStdout := os.Stdout
	os.Stdout = output
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = output.Close()
	})

	err = commands.PublishCommand(nil)
	require.ErrorContains(t, err, "duplicate version")
	_, err = output.Seek(0, io.SeekStart)
	require.NoError(t, err)
	data, err := io.ReadAll(output)
	require.NoError(t, err)
	assert.Equal(t, "Publishing io.github.BargLabs/cejel@0.4.5 to "+server.URL+"...\n", string(data))
}

func TestPublishCommand_PreservesNonASCIIDescription(t *testing.T) {
	server := SetupMockRegistryServer(t,
		func(w http.ResponseWriter, r *http.Request) {
			var req apiv0.ServerJSON
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			assert.Equal(t, "Tools for agents — safely", req.Description)

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(apiv0.ServerResponse{
				Server: apiv0.ServerJSON{
					Name:    req.Name,
					Version: req.Version,
				},
			})
		},
		nil,
	)
	SetupTestToken(t, server.URL, "test-token")

	CreateTestServerJSON(t, apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "Tools for agents — safely",
		Version:     "1.0.0",
	})

	require.NoError(t, commands.PublishCommand([]string{}))
}

func TestPublishCommand_RejectsInvalidUTF8ServerJSON(t *testing.T) {
	createRawServerJSON(t, []byte{
		'{', '"', '$', 's', 'c', 'h', 'e', 'm', 'a', '"', ':', '"', '"', ',',
		'"', 'n', 'a', 'm', 'e', '"', ':', '"', 'c', 'o', 'm', '.', 'e', 'x', 'a', 'm', 'p', 'l', 'e', '/', 't', 'e', 's', 't', '"', ',',
		'"', 'd', 'e', 's', 'c', 'r', 'i', 'p', 't', 'i', 'o', 'n', '"', ':', '"', 'b', 'a', 'd', ' ', 0xe2, '"', ',',
		'"', 'v', 'e', 'r', 's', 'i', 'o', 'n', '"', ':', '"', '1', '.', '0', '.', '0', '"', '}',
	})

	err := commands.PublishCommand([]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UTF-8")
}

func TestPublishCommand_RejectsUnpairedSurrogateEscape(t *testing.T) {
	createRawServerJSON(t, []byte(`{"$schema":"","name":"com.example/test","description":"bad \udc94","version":"1.0.0"}`))

	err := commands.PublishCommand([]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unpaired UTF-16 surrogate")
	assert.Contains(t, err.Error(), "$.description")
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

func createRawServerJSON(t *testing.T, data []byte) {
	t.Helper()

	tempDir := t.TempDir()
	serverFile := filepath.Join(tempDir, "server.json")
	require.NoError(t, os.WriteFile(serverFile, data, 0600))

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	require.NoError(t, os.Chdir(tempDir))
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

// registryValidationBody is the body the registry returns while npm has the
// package but not yet the version. TestRegistryValidationBodyIsReal pins it to the
// real validator output.
const registryValidationBody = `{"title":"Bad Request","status":400,"detail":"Failed to publish server","errors":[{"message":"registry validation failed for package 0 (@example/server): NPM package '@example/server' exists, but version '1.0.0' was not found (status: 404). A newly published release can take a moment to appear on the registry. Wait and retry, or publish version '1.0.0' before registering it"}]}`

// missingMCPNameBody carries the same wrapper but a failure that retrying cannot fix.
const missingMCPNameBody = `{"title":"Bad Request","status":400,"detail":"Failed to publish server","errors":[{"message":"registry validation failed for package 0 (@example/server): NPM package '@example/server' is missing required 'mcpName' field. Add this to your package.json: \"mcpName\": \"com.example/test-server\""}]}`

// fakeRetryClock advances a virtual clock instead of sleeping.
type fakeRetryClock struct {
	now    time.Time
	sleeps []time.Duration
}

func installFakeRetryClock(t *testing.T) *fakeRetryClock {
	t.Helper()
	c := &fakeRetryClock{now: time.Unix(0, 0)}
	restore := commands.SetPublishRetryClock(func(d time.Duration) {
		c.sleeps = append(c.sleeps, d)
		c.now = c.now.Add(d)
	}, func() time.Time { return c.now })
	t.Cleanup(restore)
	return c
}

func TestPublishCommand_WaitRetriesRegistryValidation(t *testing.T) {
	clock := installFakeRetryClock(t)
	publishCallCount := 0

	server := SetupMockRegistryServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			publishCallCount++
			if publishCallCount < 3 {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(registryValidationBody))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(apiv0.ServerResponse{
				Server: apiv0.ServerJSON{Name: "com.example/test-server", Version: "1.0.0"},
			})
		},
		nil,
	)

	SetupTestToken(t, server.URL, "test-token")
	CreateTestServerJSON(t, apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	})

	err := commands.PublishCommand([]string{"--wait", "1m"})

	require.NoError(t, err)
	assert.Equal(t, 3, publishCallCount, "publish endpoint should be retried until success")
	assert.Equal(t, []time.Duration{5 * time.Second, 10 * time.Second}, clock.sleeps)
}

func TestPublishCommand_WaitDoesNotRetryOther400(t *testing.T) {
	clock := installFakeRetryClock(t)
	publishCallCount := 0

	server := SetupMockRegistryServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			publishCallCount++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"title":"Bad Request","status":400,"detail":"Failed to publish server","errors":[{"message":"version 1.0.0 already exists"}]}`))
		},
		nil,
	)

	SetupTestToken(t, server.URL, "test-token")
	CreateTestServerJSON(t, apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	})

	err := commands.PublishCommand([]string{"--wait", "1m"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish failed")
	assert.Contains(t, err.Error(), "already exists")
	assert.Equal(t, 1, publishCallCount, "non-registry-validation errors must fail on the first attempt")
	assert.Empty(t, clock.sleeps)
}

func TestPublishCommand_WaitDeadlineFallsThroughToError(t *testing.T) {
	clock := installFakeRetryClock(t)
	publishCallCount := 0

	server := SetupMockRegistryServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			publishCallCount++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(registryValidationBody))
		},
		nil,
	)

	SetupTestToken(t, server.URL, "test-token")
	CreateTestServerJSON(t, apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	})

	// 12s deadline: attempt at 0s, sleep 5s, attempt at 5s, sleep clamped to 7s,
	// attempt at 12s, deadline reached -> error.
	err := commands.PublishCommand([]string{"--wait", "12s"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish failed")
	assert.Contains(t, err.Error(), "registry validation failed")
	assert.Equal(t, 3, publishCallCount, "attempts must stop at the deadline")
	assert.Equal(t, []time.Duration{5 * time.Second, 7 * time.Second}, clock.sleeps)
}

func TestPublishCommand_WaitBackoffCapsAt30s(t *testing.T) {
	clock := installFakeRetryClock(t)

	server := SetupMockRegistryServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(registryValidationBody))
		},
		nil,
	)

	SetupTestToken(t, server.URL, "test-token")
	CreateTestServerJSON(t, apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	})

	err := commands.PublishCommand([]string{"--wait", "2m"})

	require.Error(t, err)
	// 5 + 10 + 20 + 30 + 30 + 25 (clamped) = 120s
	assert.Equal(t, []time.Duration{
		5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second, 30 * time.Second, 25 * time.Second,
	}, clock.sleeps)
}

func TestPublishCommand_WithoutWaitDoesNotRetry(t *testing.T) {
	clock := installFakeRetryClock(t)
	publishCallCount := 0

	server := SetupMockRegistryServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			publishCallCount++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(registryValidationBody))
		},
		nil,
	)

	SetupTestToken(t, server.URL, "test-token")
	CreateTestServerJSON(t, apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	})

	err := commands.PublishCommand(nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "registry validation failed")
	assert.Equal(t, 1, publishCallCount)
	assert.Empty(t, clock.sleeps)
}

func TestPublishCommand_WaitRejectsBadDuration(t *testing.T) {
	publishCallCount := 0
	server := SetupMockRegistryServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			publishCallCount++
			w.WriteHeader(http.StatusCreated)
		},
		nil,
	)
	SetupTestToken(t, server.URL, "test-token")
	CreateTestServerJSON(t, apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	})

	err := commands.PublishCommand([]string{"--wait", "soon"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--wait")
	assert.Contains(t, err.Error(), "soon")
	assert.Equal(t, 0, publishCallCount, "nothing should be sent when the flag is invalid")
}

func TestPublishCommand_WaitWithPositionalFile(t *testing.T) {
	installFakeRetryClock(t)
	publishCallCount := 0
	server := SetupMockRegistryServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			publishCallCount++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(apiv0.ServerResponse{
				Server: apiv0.ServerJSON{Name: "com.example/test-server", Version: "1.0.0"},
			})
		},
		nil,
	)
	SetupTestToken(t, server.URL, "test-token")
	data, err := json.Marshal(apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	})
	require.NoError(t, err)
	custom := filepath.Join(t.TempDir(), "custom.json")
	require.NoError(t, os.WriteFile(custom, data, 0600))

	err = commands.PublishCommand([]string{"--wait", "1m", custom})

	require.NoError(t, err)
	assert.Equal(t, 1, publishCallCount)
}

func setupWaitPublish(t *testing.T, token string, handler http.HandlerFunc) {
	t.Helper()
	server := SetupMockRegistryServer(t, handler, nil)
	SetupTestToken(t, server.URL, token)
	CreateTestServerJSON(t, apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	})
}

func writePublished(w http.ResponseWriter) {
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(apiv0.ServerResponse{
		Server: apiv0.ServerJSON{Name: "com.example/test-server", Version: "1.0.0"},
	})
}

func writeBadRequest(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte(body))
}

// captureStdout redirects os.Stdout for the rest of the test and returns a
// function that reads what was written so far.
func captureStdout(t *testing.T) func() string {
	t.Helper()
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	require.NoError(t, err)
	original := os.Stdout
	os.Stdout = output
	t.Cleanup(func() {
		os.Stdout = original
		_ = output.Close()
	})
	return func() string {
		data, err := os.ReadFile(output.Name())
		require.NoError(t, err)
		return string(data)
	}
}

// fakeJWT builds an unsigned token whose payload carries the given exp claim.
func fakeJWT(exp int64) string {
	encode := base64.RawURLEncoding.EncodeToString
	return encode([]byte(`{"alg":"EdDSA"}`)) + "." + encode(fmt.Appendf(nil, `{"exp":%d}`, exp)) + ".signature"
}

func TestPublishCommand_WaitPrintsServerMessageOnEveryRetry(t *testing.T) {
	installFakeRetryClock(t)
	stdout := captureStdout(t)
	calls := 0
	setupWaitPublish(t, "test-token", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			writeBadRequest(w, registryValidationBody)
			return
		}
		writePublished(w)
	})

	require.NoError(t, commands.PublishCommand([]string{"--wait", "1m"}))

	assert.Equal(t, 2, strings.Count(stdout(), "exists, but version '1.0.0' was not found (status: 404)"))
}

func TestPublishCommand_WaitDoesNotRetryMarkerWithMissingMCPName(t *testing.T) {
	clock := installFakeRetryClock(t)
	calls := 0
	setupWaitPublish(t, "test-token", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		writeBadRequest(w, missingMCPNameBody)
	})

	err := commands.PublishCommand([]string{"--wait", "1m"})

	require.ErrorContains(t, err, "missing required 'mcpName'")
	assert.Equal(t, 1, calls, "a missing mcpName must fail on the first attempt")
	assert.Empty(t, clock.sleeps)
}

func TestPublishCommand_WaitRejectsNegativeDuration(t *testing.T) {
	calls := 0
	setupWaitPublish(t, "test-token", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		writePublished(w)
	})

	err := commands.PublishCommand([]string{"--wait", "-5s"})

	require.ErrorContains(t, err, "invalid --wait duration")
	assert.Equal(t, 0, calls)
}

func TestPublishCommand_WaitZeroIsASingleAttempt(t *testing.T) {
	clock := installFakeRetryClock(t)
	stdout := captureStdout(t)
	calls := 0
	setupWaitPublish(t, fakeJWT(1), func(w http.ResponseWriter, _ *http.Request) {
		calls++
		writeBadRequest(w, registryValidationBody)
	})

	err := commands.PublishCommand([]string{"--wait", "0"})

	require.ErrorContains(t, err, "exists, but version")
	assert.Equal(t, 1, calls)
	assert.Empty(t, clock.sleeps)
	assert.NotContains(t, stdout(), "Warning")
}

func TestPublishCommand_TokenExpiryDuringWait(t *testing.T) {
	clock := installFakeRetryClock(t)
	calls := 0
	setupWaitPublish(t, "test-token", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			writeBadRequest(w, registryValidationBody)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401,"detail":"Invalid or expired Registry JWT token"}`))
	})

	err := commands.PublishCommand([]string{"--wait", "1m"})

	require.ErrorContains(t, err, "registry token expired during --wait")
	assert.Contains(t, err.Error(), "exists, but version '1.0.0' was not found", "the npm cause must survive the 401")
	assert.Equal(t, 2, calls)
	assert.Len(t, clock.sleeps, 1)
}

func TestPublishCommand_401OnFirstAttemptStaysAnAuthError(t *testing.T) {
	installFakeRetryClock(t)
	setupWaitPublish(t, "test-token", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401,"detail":"Invalid or expired Registry JWT token"}`))
	})

	err := commands.PublishCommand([]string{"--wait", "1m"})

	require.ErrorContains(t, err, "status 401")
	assert.NotContains(t, err.Error(), "during --wait")
}

func TestPublishCommand_WaitWarnsWhenDeadlineRunsPastToken(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wait     string
		wantWarn bool
	}{
		{name: "deadline past exp", token: fakeJWT(240), wait: "5m", wantWarn: true},
		{name: "deadline before exp", token: fakeJWT(300), wait: "4m"},
		{name: "not a JWT", token: "test-token", wait: "5m"},
		{name: "two segments", token: "a.b", wait: "5m"},
		{name: "payload not base64", token: "a.!!!.c", wait: "5m"},
		{name: "payload not JSON", token: "a." + base64.RawURLEncoding.EncodeToString([]byte("nope")) + ".c", wait: "5m"},
		{name: "exp not a number", token: "a." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":"soon"}`)) + ".c", wait: "5m"},
		{name: "no exp", token: "a." + base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + ".c", wait: "5m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installFakeRetryClock(t)
			stdout := captureStdout(t)
			setupWaitPublish(t, tt.token, func(w http.ResponseWriter, _ *http.Request) { writePublished(w) })

			require.NoError(t, commands.PublishCommand([]string{"--wait", tt.wait}))

			if tt.wantWarn {
				assert.Contains(t, stdout(), "Warning: the registry token expires at 1970-01-01T00:04:00Z")
			} else {
				assert.NotContains(t, stdout(), "Warning")
			}
		})
	}
}

func TestPublishCommand_RejectsFlagAfterPath(t *testing.T) {
	calls := 0
	setupWaitPublish(t, "test-token", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		writePublished(w)
	})

	err := commands.PublishCommand([]string{"server.json", "--wait", "1m"})

	require.ErrorContains(t, err, "flags must come before the path")
	assert.Equal(t, 0, calls)
}

func TestPublishCommand_RejectsUnknownFlag(t *testing.T) {
	calls := 0
	setupWaitPublish(t, "test-token", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		writePublished(w)
	})

	err := commands.PublishCommand([]string{"--registry", "https://example.com"})

	require.ErrorContains(t, err, "flag provided but not defined")
	assert.Equal(t, 0, calls)
}

func TestPublishCommand_HelpAfterWaitReturnsErrHelp(t *testing.T) {
	calls := 0
	setupWaitPublish(t, "test-token", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		writePublished(w)
	})

	err := commands.PublishCommand([]string{"--wait", "1m", "--help"})

	require.ErrorIs(t, err, flag.ErrHelp)
	assert.Equal(t, 0, calls)
}

// hangUntilClientGives blocks like a stalled registry until the client drops the
// connection, so server.Close in test cleanup does not wait for it. The server only
// notices the disconnect once the request body has been read.
func hangUntilClientGives(r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	select {
	case <-r.Context().Done():
	case <-time.After(30 * time.Second):
	}
}

func TestPublishCommand_WaitDeadlineDuringHungRequest(t *testing.T) {
	t.Cleanup(commands.SetPublishRequestGrace(50 * time.Millisecond))
	setupWaitPublish(t, "test-token", func(_ http.ResponseWriter, r *http.Request) {
		hangUntilClientGives(r)
	})

	start := time.Now()
	err := commands.PublishCommand([]string{"--wait", "50ms"})

	require.ErrorContains(t, err, "--wait deadline passed during a request")
	assert.Less(t, time.Since(start), 10*time.Second, "a hung request must not overrun --wait")
}

func TestPublishCommand_WaitDeadlineDuringHungRetryKeepsValidationCause(t *testing.T) {
	installFakeRetryClock(t)
	t.Cleanup(commands.SetPublishRequestGrace(50 * time.Millisecond))
	var calls atomic.Int32
	setupWaitPublish(t, "test-token", func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			writeBadRequest(w, registryValidationBody)
			return
		}
		hangUntilClientGives(r)
	})

	// The fake clock jumps 5s on the first sleep, leaving 50ms plus the grace.
	err := commands.PublishCommand([]string{"--wait", "5050ms"})

	require.ErrorContains(t, err, "--wait deadline passed during a request")
	assert.Contains(t, err.Error(), "exists, but version '1.0.0' was not found")
	assert.Equal(t, int32(2), calls.Load())
}

func TestPublishCommand_WithoutWaitHasNoRequestDeadline(t *testing.T) {
	t.Cleanup(commands.SetPublishRequestGrace(time.Nanosecond))
	setupWaitPublish(t, "test-token", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		writePublished(w)
	})

	require.NoError(t, commands.PublishCommand(nil))
}
