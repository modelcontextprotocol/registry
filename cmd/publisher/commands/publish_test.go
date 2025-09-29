package commands_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/registry/cmd/publisher/commands"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestPublishCommand_DeprecatedSchema(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "mcp-publisher-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current dir: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp dir: %v", err)
	}

	tests := []struct {
		name        string
		schema      string
		expectError bool
		errorSubstr string
	}{
		{
			name:        "deprecated 2025-07-09 schema should show warning",
			schema:      "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json",
			expectError: true,
			errorSubstr: "deprecated schema detected",
		},
		{
			name:        "current 2025-09-29 schema should pass validation",
			schema:      "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
			expectError: false,
		},
		{
			name:        "empty schema should pass validation",
			schema:      "",
			expectError: false,
		},
		{
			name:        "custom schema without 2025-07-09 should pass validation",
			schema:      "https://example.com/custom.schema.json",
			expectError: true,
			errorSubstr: "deprecated schema detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server.json with specific schema
			serverJSON := apiv0.ServerJSON{
				Schema:      tt.schema,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Repository: model.Repository{
					URL:    "https://github.com/example/test",
					Source: "github",
				},
				Packages: []model.Package{
					{
						RegistryType:    model.RegistryTypeNPM,
						RegistryBaseURL: model.RegistryURLNPM,
						Identifier:      "@example/test-server",
						Version:         "1.0.0",
						Transport: model.Transport{
							Type: model.TransportTypeStdio,
						},
					},
				},
			}

			jsonData, err := json.MarshalIndent(serverJSON, "", "  ")
			if err != nil {
				t.Fatalf("Failed to marshal test JSON: %v", err)
			}

			// Write server.json to temp directory
			serverFile := filepath.Join(tempDir, "server.json")
			if err := os.WriteFile(serverFile, jsonData, 0o600); err != nil {
				t.Fatalf("Failed to write server.json: %v", err)
			}

			err = commands.PublishCommand([]string{})

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for deprecated schema, but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errorSubstr) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorSubstr, err)
				}
				// Check that the error contains the migration links
				if !strings.Contains(err.Error(), "Migration checklist:") {
					t.Errorf("Expected error to contain migration checklist link")
				}
				if !strings.Contains(err.Error(), "Full changelog with examples:") {
					t.Errorf("Expected error to contain changelog link")
				}
			} else {
				// For non-deprecated schemas, we expect the command to fail at auth step, not schema validation
				if err != nil && strings.Contains(err.Error(), "deprecated schema detected") {
					t.Errorf("Unexpected deprecated schema error for schema '%s': %v", tt.schema, err)
				}

				// We expect auth errors for valid schemas since we don't have a token
				if err != nil && !strings.Contains(err.Error(), "not authenticated") && !strings.Contains(err.Error(), "failed to read token") {
					t.Logf("Expected auth error for valid schema, got: %v", err)
				}
			}

			// Clean up for next test
			os.Remove(serverFile)
		})
	}
}
