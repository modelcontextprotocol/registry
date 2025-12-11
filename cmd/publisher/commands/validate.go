package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// printSchemaValidationErrors prints nicely formatted error messages for schema validation issues
// (empty schema or non-current schema) with migration guidance to stdout.
// Returns true if any schema errors/warnings were printed.
func printSchemaValidationErrors(result *validators.ValidationResult, serverJSON *apiv0.ServerJSON) bool {
	currentSchemaURL := model.CurrentSchemaURL
	migrationURL := "https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/CHANGELOG.md"
	checklistURL := migrationURL + "#migration-checklist-for-publishers"

	printed := false

	for _, issue := range result.Issues {
		switch issue.Reference {
		case "schema-field-required":
			// Empty/missing schema
			_, _ = fmt.Fprintf(os.Stdout, "$schema field is required.\n")
			_, _ = fmt.Fprintln(os.Stdout)
			_, _ = fmt.Fprintf(os.Stdout, "Expected current schema: %s\n", currentSchemaURL)
			_, _ = fmt.Fprintln(os.Stdout)
			_, _ = fmt.Fprintln(os.Stdout, "Run 'mcp-publisher init' to create a new server.json with the correct schema, or update your existing server.json file.")
			_, _ = fmt.Fprintln(os.Stdout)
			_, _ = fmt.Fprintf(os.Stdout, "📋 Migration checklist: %s\n", checklistURL)
			_, _ = fmt.Fprintf(os.Stdout, "📖 Full changelog with examples: %s\n", migrationURL)
			_, _ = fmt.Fprintln(os.Stdout)
			printed = true
			return printed // Only one schema error at a time

		case "schema-version-deprecated":
			// Non-current schema
			if issue.Severity == validators.ValidationIssueSeverityWarning {
				// Warning format (for validate command)
				_, _ = fmt.Fprintf(os.Stdout, "⚠️  Deprecated schema detected: %s\n", serverJSON.Schema)
			} else {
				// Error format (for publish command)
				_, _ = fmt.Fprintf(os.Stdout, "deprecated schema detected: %s.\n", serverJSON.Schema)
			}
			_, _ = fmt.Fprintln(os.Stdout)
			_, _ = fmt.Fprintf(os.Stdout, "Expected current schema: %s\n", currentSchemaURL)
			_, _ = fmt.Fprintln(os.Stdout)
			_, _ = fmt.Fprintln(os.Stdout, "Migrate to the current schema format for new servers.")
			_, _ = fmt.Fprintln(os.Stdout)
			_, _ = fmt.Fprintf(os.Stdout, "📋 Migration checklist: %s\n", checklistURL)
			_, _ = fmt.Fprintf(os.Stdout, "📖 Full changelog with examples: %s\n", migrationURL)
			_, _ = fmt.Fprintln(os.Stdout)
			printed = true
			return printed // Only one schema error at a time
		}
	}

	return printed
}

// runValidationAndPrintIssues validates the server JSON, prints schema validation errors, and prints all issues.
// Validation failures are always printed (for both validate and publish commands).
// Returns the validation result and whether schema errors were printed.
func runValidationAndPrintIssues(serverJSON *apiv0.ServerJSON, opts validators.ValidationOptions) (*validators.ValidationResult, bool) {
	result := validators.ValidateServerJSON(serverJSON, opts)

	// Print schema validation errors/warnings with friendly messages
	schemaPrinted := printSchemaValidationErrors(result, serverJSON)

	if result.Valid {
		return result, schemaPrinted
	}

	// Print all issues
	_, _ = fmt.Fprintf(os.Stdout, "❌ Validation failed with %d issue(s):\n", len(result.Issues))
	_, _ = fmt.Fprintln(os.Stdout)

	// Track which schema issues we've already printed to avoid duplicates
	issueNum := 1

	for _, issue := range result.Issues {
		// Skip schema issues that were already printed
		if (issue.Reference == "schema-field-required" || issue.Reference == "schema-version-deprecated") && schemaPrinted {
			continue
		}

		// Print other issues normally
		_, _ = fmt.Fprintf(os.Stdout, "%d. [%s] %s (%s)\n", issueNum, issue.Severity, issue.Path, issue.Type)
		_, _ = fmt.Fprintf(os.Stdout, "   %s\n", issue.Message)
		if issue.Reference != "" {
			_, _ = fmt.Fprintf(os.Stdout, "   Reference: %s\n", issue.Reference)
		}
		_, _ = fmt.Fprintln(os.Stdout)
		issueNum++
	}

	return result, schemaPrinted
}

func ValidateCommand(args []string) error {
	// Parse arguments
	serverFile := "server.json"

	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			_, _ = fmt.Fprintln(os.Stdout, "Usage: mcp-publisher validate [file]")
			_, _ = fmt.Fprintln(os.Stdout)
			_, _ = fmt.Fprintln(os.Stdout, "Validate a server.json file without publishing.")
			_, _ = fmt.Fprintln(os.Stdout)
			_, _ = fmt.Fprintln(os.Stdout, "Arguments:")
			_, _ = fmt.Fprintln(os.Stdout, "  file    Path to server.json file (default: ./server.json)")
			_, _ = fmt.Fprintln(os.Stdout)
			_, _ = fmt.Fprintln(os.Stdout, "The validate command performs exhaustive validation, reporting all issues at once.")
			_, _ = fmt.Fprintln(os.Stdout, "It validates JSON syntax, schema compliance, and semantic rules.")
			return nil
		}
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

	// Run detailed validation (this is the whole point of the validate command)
	// Include schema validation for comprehensive validation
	// Warn about non-current schemas (don't error, just inform)
	result, _ := runValidationAndPrintIssues(&serverJSON, validators.ValidationAll)

	if result.Valid {
		_, _ = fmt.Fprintln(os.Stdout, "✅ server.json is valid")
		return nil
	}

	return fmt.Errorf("validation failed")
}
