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

	// Read server.json
	serverData, err := os.ReadFile(serverFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("server.json not found. Run 'mcp-publisher init' to create one")
		}
		return fmt.Errorf("failed to read server.json: %w", err)
	}

	// Validate JSON
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(serverData, &serverJSON); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// Check for deprecated schema and recommend migration
	// Allow empty schema (will use default) but reject old schemas
	if serverJSON.Schema != "" && !strings.Contains(serverJSON.Schema, "2025-09-29") {
		return fmt.Errorf(`deprecated schema detected: %s.

Migrate to the current schema format for new servers.

📋 Migration checklist: https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/CHANGELOG.md#migration-checklist-for-publishers
📖 Full changelog with examples: https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/CHANGELOG.md`, serverJSON.Schema)
	}

	// Run detailed validation (this is the whole point of the validate command)
	// Include schema validation for comprehensive validation
	result := validators.ValidateServerJSONDetailed(&serverJSON, true)

	if result.Valid {
		fmt.Println("✅ server.json is valid")
		return nil
	}

	// Print all issues
	fmt.Printf("❌ Validation failed with %d issue(s):\n\n", len(result.Issues))
	for i, issue := range result.Issues {
		fmt.Printf("%d. [%s] %s (%s)\n", i+1, issue.Severity, issue.Path, issue.Type)
		fmt.Printf("   %s\n", issue.Message)
		fmt.Printf("   Rule: %s\n", issue.Rule)
		fmt.Println()
	}

	return fmt.Errorf("validation failed")
}
