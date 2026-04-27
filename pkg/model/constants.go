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
	RegistryURLGitHub = "https://github.com"
	RegistryURLGitLab = "https://gitlab.com"
	RegistryURLNPM    = "https://registry.npmjs.org"
	RegistryURLNuGet  = "https://api.nuget.org/v3/index.json"
	RegistryURLPyPI   = "https://pypi.org"
	RegistryURLQuay   = "https://quay.io"
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
	CurrentSchemaVersion = "2025-12-11"
	// CurrentSchemaURL is the full URL to the current schema
	CurrentSchemaURL = "https://static.modelcontextprotocol.io/schemas/" + CurrentSchemaVersion + "/server.schema.json"
)
