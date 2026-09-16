package validators_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// draftSchemaPath is the in-repo draft schema, the source of truth for unreleased
// schema changes such as postInstallInstructions.
const draftSchemaPath = "../../docs/reference/server-json/draft/server.schema.json"

// compileDraftSchema loads and compiles the in-repo draft server.json schema so
// tests can validate documents against the unreleased schema definition.
func compileDraftSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()

	absPath, err := filepath.Abs(draftSchemaPath)
	require.NoError(t, err, "resolve draft schema path")

	schemaData, err := os.ReadFile(absPath)
	require.NoError(t, err, "read draft schema file")

	schemaID := "https://raw.githubusercontent.com/modelcontextprotocol/registry/main/docs/reference/server-json/draft/server.schema.json"

	compiler := jsonschema.NewCompiler()
	require.NoError(t, compiler.AddResource(schemaID, strings.NewReader(string(schemaData))), "add draft schema resource")

	schema, err := compiler.Compile(schemaID)
	require.NoError(t, err, "compile draft schema")
	return schema
}

// validateAgainstDraft validates a raw server.json document against the draft schema
// and returns any validation error.
func validateAgainstDraft(t *testing.T, schema *jsonschema.Schema, doc string) error {
	t.Helper()

	var instance interface{}
	require.NoError(t, json.Unmarshal([]byte(doc), &instance), "unmarshal server.json document")
	return schema.Validate(instance)
}

const baseServerWithPackagePrefix = `{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
  "name": "io.github.aniongithub/mind-map",
  "description": "A mind-map MCP server that doubles as a system service",
  "version": "1.0.0",
  "packages": [`

const baseServerSuffix = `]
}`

// TestDraftSchema_PostInstallInstructions_Accepted ensures a package carrying
// postInstallInstructions validates cleanly against the draft schema.
func TestDraftSchema_PostInstallInstructions_Accepted(t *testing.T) {
	schema := compileDraftSchema(t)

	doc := baseServerWithPackagePrefix + `{
		"registryType": "mcpb",
		"identifier": "https://github.com/aniongithub/mind-map/releases/download/v1.0.0/mind-map.mcpb",
		"version": "1.0.0",
		"fileSha256": "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce",
		"transport": { "type": "stdio" },
		"postInstallInstructions": [
			{
				"description": "Install as a system service for the web UI",
				"command": "mind-map service install --addr 127.0.0.1:4242",
				"documentation": "https://github.com/aniongithub/mind-map#service-management",
				"optional": true
			}
		]
	}` + baseServerSuffix

	err := validateAgainstDraft(t, schema, doc)
	assert.NoError(t, err, "server.json with postInstallInstructions should be valid")
}

// TestDraftSchema_PostInstallInstructions_OptionalWhenAbsent ensures the field
// remains optional: a package without it must still validate.
func TestDraftSchema_PostInstallInstructions_OptionalWhenAbsent(t *testing.T) {
	schema := compileDraftSchema(t)

	doc := baseServerWithPackagePrefix + `{
		"registryType": "npm",
		"identifier": "@example/server",
		"version": "1.0.0",
		"transport": { "type": "stdio" }
	}` + baseServerSuffix

	err := validateAgainstDraft(t, schema, doc)
	assert.NoError(t, err, "server.json without postInstallInstructions should remain valid")
}

// TestDraftSchema_PostInstallInstructions_MinimalDescriptionOnly ensures only the
// required description field is needed for a valid instruction.
func TestDraftSchema_PostInstallInstructions_MinimalDescriptionOnly(t *testing.T) {
	schema := compileDraftSchema(t)

	doc := baseServerWithPackagePrefix + `{
		"registryType": "npm",
		"identifier": "@example/server",
		"version": "1.0.0",
		"transport": { "type": "stdio" },
		"postInstallInstructions": [
			{ "description": "Restart your editor to load the server" }
		]
	}` + baseServerSuffix

	err := validateAgainstDraft(t, schema, doc)
	assert.NoError(t, err, "instruction with only a description should be valid")
}

// TestDraftSchema_PostInstallInstructions_RequiresDescription ensures an
// instruction missing the required description field is rejected.
func TestDraftSchema_PostInstallInstructions_RequiresDescription(t *testing.T) {
	schema := compileDraftSchema(t)

	doc := baseServerWithPackagePrefix + `{
		"registryType": "npm",
		"identifier": "@example/server",
		"version": "1.0.0",
		"transport": { "type": "stdio" },
		"postInstallInstructions": [
			{ "command": "do something" }
		]
	}` + baseServerSuffix

	err := validateAgainstDraft(t, schema, doc)
	assert.Error(t, err, "instruction without description should be rejected")
}

// TestDraftSchema_PostInstallInstructions_DocumentationMustBeURI ensures the
// documentation field is validated as a URI.
func TestDraftSchema_PostInstallInstructions_DocumentationMustBeURI(t *testing.T) {
	schema := compileDraftSchema(t)

	doc := baseServerWithPackagePrefix + `{
		"registryType": "npm",
		"identifier": "@example/server",
		"version": "1.0.0",
		"transport": { "type": "stdio" },
		"postInstallInstructions": [
			{ "description": "See docs", "documentation": "not a uri with spaces" }
		]
	}` + baseServerSuffix

	err := validateAgainstDraft(t, schema, doc)
	assert.Error(t, err, "non-URI documentation should be rejected")
}
