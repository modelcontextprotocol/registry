package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

func ValidateCommand(args []string) error {
	// Parse arguments
	serverFile := "server.json"

	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			serverFile = arg
		}
	}

	// Read server file
	serverData, err := os.ReadFile(serverFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s not found, please check the file path", serverFile)
		}
		return fmt.Errorf("failed to read %s: %w", serverFile, err)
	}

	// Validate JSON
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(serverData, &serverJSON); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// Check for deprecated schema and recommend migration
	// Allow empty schema (will use default) but reject old schemas
	if serverJSON.Schema != "" {
		// Get current schema version from embedded schema
		currentSchema, err := validators.GetCurrentSchemaVersion()
		if err != nil {
			// Should never happen (schema is embedded)
			return fmt.Errorf("failed to get current schema version: %w", err)
		}

		if serverJSON.Schema != currentSchema {
			return fmt.Errorf(`deprecated schema detected: %s.

Expected current schema: %s

Migrate to the current schema format for new servers.

📋 Migration checklist: https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/CHANGELOG.md#migration-checklist-for-publishers
📖 Full changelog with examples: https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/CHANGELOG.md`, serverJSON.Schema, currentSchema)
		}
	}

	// Run detailed validation (this is the whole point of the validate command)
	// Include schema validation for comprehensive validation
	result := validators.ValidateServerJSONExhaustive(&serverJSON, true)

	if result.Valid {
		_, _ = fmt.Fprintln(os.Stdout, "✅ server.json is valid")
		return nil
	}

	// Print all issues
	_, _ = fmt.Fprintf(os.Stdout, "❌ Validation failed with %d issue(s):\n\n", len(result.Issues))
	for i, issue := range result.Issues {
		_, _ = fmt.Fprintf(os.Stdout, "%d. [%s] %s (%s)\n", i+1, issue.Severity, issue.Path, issue.Type)
		_, _ = fmt.Fprintf(os.Stdout, "   %s\n", issue.Message)
		if issue.Reference != "" {
			_, _ = fmt.Fprintf(os.Stdout, "   Reference: %s\n", issue.Reference)
		}
		_, _ = fmt.Fprintln(os.Stdout)
	}

	return fmt.Errorf("validation failed")
}
