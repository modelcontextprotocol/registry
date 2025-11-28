package model

// Registry Types - supported package registry types
const (
	RegistryTypeNPM   = "npm"
	RegistryTypePyPI  = "pypi"
	RegistryTypeOCI   = "oci"
	RegistryTypeNuGet = "nuget"
	RegistryTypeMCPB  = "mcpb"
)

// Registry Base URLs - supported package registry base URLs
const (
	RegistryURLNPM    = "https://registry.npmjs.org"
	RegistryURLPyPI   = "https://pypi.org"
	RegistryURLNuGet  = "https://api.nuget.org"
	RegistryURLGitHub = "https://github.com"
	RegistryURLGitLab = "https://gitlab.com"
)

// Transport Types - supported remote transport protocols
const (
	TransportTypeStreamableHTTP = "streamable-http"
	TransportTypeSSE            = "sse"
	TransportTypeStdio          = "stdio"
)

// Runtime Hints - supported package runtime hints
const (
	RuntimeHintNPX    = "npx"
	RuntimeHintUVX    = "uvx"
	RuntimeHintDocker = "docker"
	RuntimeHintDNX    = "dnx"
)

// Schema versions
const (
	// CurrentSchemaVersion is the current supported schema version date
	CurrentSchemaVersion = "2025-10-17"
	// CurrentSchemaURL is the full URL to the current schema
	CurrentSchemaURL = "https://static.modelcontextprotocol.io/schemas/" + CurrentSchemaVersion + "/server.schema.json"
	// SchemaURLPrefix is the common prefix for all schema URLs
	SchemaURLPrefix = "https://static.modelcontextprotocol.io/schemas/"
	// SchemaURLSuffix is the common suffix for all schema URLs
	SchemaURLSuffix = "/server.schema.json"
)

// ValidSchemaVersions is the list of all accepted schema versions.
// This is used by validators to accept any valid schema version (not just the current one).
// TODO: Consider only supporting more recent schema versions.
var ValidSchemaVersions = []string{
	"2025-10-17",
	"2025-10-11",
	"2025-09-29",
	"2025-09-16",
	"2025-07-09",
}

// IsValidSchemaURL checks if the given schema URL is a valid MCP server schema URL.
// It must match the pattern: https://static.modelcontextprotocol.io/schemas/{version}/server.schema.json
// where {version} is one of the valid schema versions.
func IsValidSchemaURL(schemaURL string) bool {
	for _, version := range ValidSchemaVersions {
		expectedURL := SchemaURLPrefix + version + SchemaURLSuffix
		if schemaURL == expectedURL {
			return true
		}
	}
	return false
}

// ValidSchemaURLs returns the list of all valid schema URLs.
func ValidSchemaURLs() []string {
	urls := make([]string, len(ValidSchemaVersions))
	for i, version := range ValidSchemaVersions {
		urls[i] = SchemaURLPrefix + version + SchemaURLSuffix
	}
	return urls
}
