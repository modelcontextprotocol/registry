package validators_test

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const serverSchemaPath = "../../docs/reference/server-json/draft/server.schema.json"

// schemaHelper provides utilities for extracting values from the JSON schema
type schemaHelper struct {
	t      *testing.T
	schema map[string]interface{}
}

func loadSchema(t *testing.T) *schemaHelper {
	t.Helper()
	data, err := os.ReadFile(serverSchemaPath)
	require.NoError(t, err, "Failed to read schema file")

	var schema map[string]interface{}
	err = json.Unmarshal(data, &schema)
	require.NoError(t, err, "Failed to parse schema JSON")

	return &schemaHelper{t: t, schema: schema}
}

// getDefinition returns a definition from the schema by name
func (s *schemaHelper) getDefinition(name string) map[string]interface{} {
	s.t.Helper()
	definitions := s.schema["definitions"].(map[string]interface{})
	def, ok := definitions[name].(map[string]interface{})
	require.True(s.t, ok, "Definition %q not found in schema", name)
	return def
}

// getPropertyPattern extracts a regex pattern from a definition's property
func (s *schemaHelper) getPropertyPattern(definitionName, propertyName string) string {
	s.t.Helper()
	def := s.getDefinition(definitionName)
	props := def["properties"].(map[string]interface{})
	prop, ok := props[propertyName].(map[string]interface{})
	require.True(s.t, ok, "Property %q not found in %s", propertyName, definitionName)
	pattern, ok := prop["pattern"].(string)
	require.True(s.t, ok, "Pattern not found for %s.%s", definitionName, propertyName)
	return pattern
}

// TestTransportURLPattern validates the URL pattern used by StreamableHttpTransport and SseTransport.
// URLs must start with http://, https://, or a template variable like {baseUrl}.
func TestTransportURLPattern(t *testing.T) {
	schema := loadSchema(t)

	streamablePattern := schema.getPropertyPattern("StreamableHttpTransport", "url")
	ssePattern := schema.getPropertyPattern("SseTransport", "url")

	// Verify both transport types use the same pattern
	assert.Equal(t, streamablePattern, ssePattern,
		"StreamableHttpTransport and SseTransport should use identical URL patterns")

	t.Logf("Pattern: %s", streamablePattern)

	re, err := regexp.Compile(streamablePattern)
	require.NoError(t, err, "Pattern should be valid regex")

	// Test cases that SHOULD match
	validCases := []string{
		// Standard URLs
		"https://api.example.com/mcp",
		"http://localhost:8080/sse",
		"https://example.com/path?query=value",
		"https://api.example.com/v1/mcp",
		// Template variables
		"{baseUrl}",
		"{baseUrl}/mcp",
		"{server_url}/api/v1",
		"{API_ENDPOINT}",
		"{a}",
		"{_private}/endpoint",
	}

	for _, tc := range validCases {
		assert.True(t, re.MatchString(tc), "Expected %q to match pattern", tc)
	}

	// Test cases that should NOT match
	invalidCases := []string{
		"ftp://example.com",       // wrong protocol
		"example.com",             // missing protocol or variable
		"/relative/path",          // relative path
		"{invalid-name}/path",     // hyphen in variable name
		"{123invalid}",            // variable starts with number
		"",                        // empty string
		"mailto:test@example.com", // wrong protocol
		"file:///path/to/file",    // wrong protocol
		"{}/empty",                // empty variable name
		"{{nested}}/path",         // nested braces
	}

	for _, tc := range invalidCases {
		assert.False(t, re.MatchString(tc), "Expected %q to NOT match pattern", tc)
	}
}
