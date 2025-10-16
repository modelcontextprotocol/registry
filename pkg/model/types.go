package model

type Status string

const (
	StatusActive     Status = "active"
	StatusDeprecated Status = "deprecated"
	StatusDeleted    Status = "deleted"
)

type Transport struct {
	Type    string          `json:"type" doc:"Transport type (stdio, streamable-http, or sse)" example:"stdio"`
	URL     string          `json:"url,omitempty" doc:"URL for streamable-http or sse transports" example:"https://api.example.com/mcp"`
	Headers []KeyValueInput `json:"headers,omitempty" doc:"HTTP headers for streamable-http or sse transports"`
}

// Package represents a package configuration.
// The RegistryType field determines which other fields are relevant:
//   - NPM:   RegistryType, Identifier (package name), Version, RegistryBaseURL (optional)
//   - PyPI:  RegistryType, Identifier (package name), Version, RegistryBaseURL (optional)
//   - NuGet: RegistryType, Identifier (package ID), Version, RegistryBaseURL (optional)
//   - OCI:   RegistryType, Identifier (full image reference like "ghcr.io/owner/repo:tag")
//   - MCPB:  RegistryType, Identifier (download URL), Version (optional), FileSHA256 (required)
type Package struct {
	// RegistryType indicates how to download packages (e.g., "npm", "pypi", "oci", "nuget", "mcpb")
	RegistryType string `json:"registryType" minLength:"1" doc:"Registry type indicating how to download packages (e.g., 'npm', 'pypi', 'oci', 'nuget', 'mcpb')" example:"npm"`
	// RegistryBaseURL is the base URL of the package registry (used by npm, pypi, nuget; not used by oci, mcpb)
	RegistryBaseURL string `json:"registryBaseUrl,omitempty" format:"uri" doc:"Base URL of the package registry" example:"https://registry.npmjs.org"`
	// Identifier is the package identifier:
	//   - For NPM/PyPI/NuGet: package name or ID
	//   - For OCI: full image reference (e.g., "ghcr.io/owner/repo:v1.0.0")
	//   - For MCPB: direct download URL
	Identifier string `json:"identifier" minLength:"1" doc:"Package identifier - either a package name (for registries) or URL (for direct downloads)" example:"@modelcontextprotocol/server-brave-search"`
	// Version is the package version (required for npm, pypi, nuget; optional for mcpb; not used by oci where version is in the identifier)
	Version string `json:"version,omitempty" minLength:"1" doc:"Package version. Must be a specific version. Version ranges are rejected (e.g., '^1.2.3', '~1.2.3', '>=1.2.3', '1.x', '1.*')." example:"1.0.2"`
	// FileSHA256 is the SHA-256 hash for integrity verification (required for mcpb, optional for others)
	FileSHA256 string `json:"fileSha256,omitempty" pattern:"^[a-f0-9]{64}$" doc:"SHA-256 hash of the package file for integrity verification. Required for MCPB packages and optional for other package types. Authors are responsible for generating correct SHA-256 hashes when creating server.json. If present, MCP clients must validate the downloaded file matches the hash before running packages to ensure file integrity." example:"fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce"`
	// RunTimeHint suggests the appropriate runtime for the package
	RunTimeHint string `json:"runtimeHint,omitempty" doc:"A hint to help clients determine the appropriate runtime for the package. This field should be provided when runtimeArguments are present." example:"npx"`
	// Transport is required and specifies the transport protocol configuration
	Transport Transport `json:"transport" doc:"Transport protocol configuration for the package"`
	// RuntimeArguments are passed to the package's runtime command (e.g., docker, npx)
	RuntimeArguments []Argument `json:"runtimeArguments,omitempty" doc:"A list of arguments to be passed to the package's runtime command (such as docker or npx). The runtimeHint field should be provided when runtimeArguments are present."`
	// PackageArguments are passed to the package's binary
	PackageArguments []Argument `json:"packageArguments,omitempty" doc:"A list of arguments to be passed to the package's binary."`
	// EnvironmentVariables are set when running the package
	EnvironmentVariables []KeyValueInput `json:"environmentVariables,omitempty" doc:"A mapping of environment variables to be set when running the package."`
}

type Repository struct {
	URL       string `json:"url" format:"uri" doc:"Repository URL for browsing source code. Should support both web browsing and git clone operations." example:"https://github.com/modelcontextprotocol/servers"`
	Source    string `json:"source" doc:"Repository hosting service identifier. Used by registries to determine validation and API access methods." example:"github"`
	ID        string `json:"id,omitempty" doc:"Repository identifier from the hosting service (e.g., GitHub repo ID). Owned and determined by the source forge. Should remain stable across repository renames and may be used to detect repository resurrection attacks - if a repository is deleted and recreated, the ID should change. For GitHub, use: gh api repos/<owner>/<repo> --jq '.id'" example:"b94b5f7e-c7c6-d760-2c78-a5e9b8a5b8c9"`
	Subfolder string `json:"subfolder,omitempty" doc:"Optional relative path from repository root to the server location within a monorepo or nested package structure. Must be a clean relative path." example:"src/everything"`
}

type Format string

const (
	FormatString   Format = "string"
	FormatNumber   Format = "number"
	FormatBoolean  Format = "boolean"
	FormatFilePath Format = "filepath"
)

type Input struct {
	Description string   `json:"description,omitempty" doc:"A description of the input, which clients can use to provide context to the user."`
	IsRequired  bool     `json:"isRequired,omitempty" doc:"Whether the input is required"`
	Format      Format   `json:"format,omitempty" enum:"string,number,boolean,filepath" doc:"Specifies the input format. Supported values include filepath, which should be interpreted as a file on the user's filesystem."`
	Value       string   `json:"value,omitempty" doc:"The value for the input. If this is not set, the user may be prompted to provide a value. Identifiers wrapped in {curly_braces} will be replaced with the corresponding properties from the input variables map."`
	IsSecret    bool     `json:"isSecret,omitempty" doc:"Indicates whether the input is a secret value (e.g., password, token). If true, clients should handle the value securely."`
	Default     string   `json:"default,omitempty" doc:"The default value for the input. This should be a valid value for the input. If you want to provide input examples or guidance, use the placeholder field instead."`
	Placeholder string   `json:"placeholder,omitempty" doc:"A placeholder for the input to be displaying during configuration. This is used to provide examples or guidance about the expected form or content of the input."`
	Choices     []string `json:"choices,omitempty" doc:"A list of possible values for the input. If provided, the user must select one of these values."`
}

type InputWithVariables struct {
	Input     `json:",inline"`
	Variables map[string]Input `json:"variables,omitempty" doc:"A map of variable names to their values. Keys in the input value that are wrapped in {curly_braces} will be replaced with the corresponding variable values."`
}

type KeyValueInput struct {
	InputWithVariables `json:",inline"`
	Name               string `json:"name" doc:"Name of the header or environment variable." example:"SOME_VARIABLE"`
}

type ArgumentType string

const (
	ArgumentTypePositional ArgumentType = "positional"
	ArgumentTypeNamed      ArgumentType = "named"
)

type Argument struct {
	InputWithVariables `json:",inline"`
	Type               ArgumentType `json:"type" doc:"Argument type: 'positional' or 'named'" example:"positional"`
	Name               string       `json:"name,omitempty" doc:"The flag name (for named arguments), including any leading dashes. Empty for positional arguments." example:"--port"`
	ValueHint          string       `json:"valueHint,omitempty" doc:"An identifier for positional arguments. Used in transport URL variable substitution." example:"file_path"`
	IsRepeated         bool         `json:"isRepeated,omitempty" doc:"Whether the argument can be repeated multiple times."`
}

type Icon struct {
	Src      string   `json:"src" required:"true" format:"uri" maxLength:"255" doc:"A standard URI pointing to an icon resource. Must be an HTTPS URL. Consumers SHOULD take steps to ensure URLs serving icons are from the same domain as the server or a trusted domain. Consumers SHOULD take appropriate precautions when consuming SVGs as they can contain executable JavaScript." example:"https://example.com/icon.png"`
	MimeType *string  `json:"mimeType,omitempty" enum:"image/png,image/jpeg,image/jpg,image/svg+xml,image/webp" doc:"Optional MIME type override if the source MIME type is missing or generic. Must be one of: image/png, image/jpeg, image/jpg, image/svg+xml, image/webp." example:"image/png"`
	Sizes    []string `json:"sizes,omitempty" doc:"Optional array of strings that specify sizes at which the icon can be used. Each string should be in WxH format (e.g., '48x48', '96x96') or 'any' for scalable formats like SVG. If not provided, the client should assume that the icon can be used at any size." items.pattern:"^(\\d+x\\d+|any)$"`
	Theme    *string  `json:"theme,omitempty" enum:"light,dark" doc:"Optional specifier for the theme this icon is designed for. 'light' indicates the icon is designed to be used with a light background, and 'dark' indicates the icon is designed to be used with a dark background. If not provided, the client should assume the icon can be used with any theme."`
}
