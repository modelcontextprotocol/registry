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
	RegistryURLNuGet  = "https://api.nuget.org/v3/index.json"
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

// Architecture - OS and architecture combinations for packages
const (
	ArchLinuxAMD64   = "linux-amd64"
	ArchLinuxARM64   = "linux-arm64"
	ArchDarwinAMD64  = "darwin-amd64"
	ArchDarwinARM64  = "darwin-arm64"
	ArchWindowsAMD64 = "windows-amd64"
	ArchWindowsARM64 = "windows-arm64"
)

// Schema versions
const (
	// CurrentSchemaVersion is the current supported schema version date
	CurrentSchemaVersion = "2025-12-11"
	// CurrentSchemaURL is the full URL to the current schema
	CurrentSchemaURL = "https://static.modelcontextprotocol.io/schemas/" + CurrentSchemaVersion + "/server.schema.json"
)
