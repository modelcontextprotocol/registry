package model

// Status represents the lifecycle status of a server
type Status string

const (
	StatusActive     Status = "active"
	StatusDeprecated Status = "deprecated"
	StatusDeleted    Status = "deleted"
)

// Transport represents transport configuration with optional URL templating
type Transport struct {
	Type    string          `json:"type"`
	URL     string          `json:"url,omitempty"`
	Headers []KeyValueInput `json:"headers,omitempty"`
}

// Package represents a package configuration.
// The RegistryType field determines which other fields are relevant:
//   - NPM:   RegistryType, Identifier (package name), Version, RegistryBaseURL (optional)
//   - PyPI:  RegistryType, Identifier (package name), Version, RegistryBaseURL (optional)
//   - NuGet: RegistryType, Identifier (package ID), Version, RegistryBaseURL (optional)
//   - OCI:   RegistryType, Identifier (full image reference like "ghcr.io/owner/repo:tag")
//   - MCPB:  RegistryType, Identifier (download URL), FileSHA256 (required)
type Package struct {
	// RegistryType indicates how to download packages (e.g., "npm", "pypi", "oci", "nuget", "mcpb")
	RegistryType string `json:"registryType" minLength:"1"`
	// RegistryBaseURL is the base URL of the package registry (used by npm, pypi, nuget; not used by oci, mcpb)
	RegistryBaseURL string `json:"registryBaseUrl,omitempty"`
	// Identifier is the package identifier:
	//   - For NPM/PyPI/NuGet: package name or ID
	//   - For OCI: full image reference (e.g., "ghcr.io/owner/repo:v1.0.0")
	//   - For MCPB: direct download URL
	Identifier string `json:"identifier" minLength:"1"`
	// Version is the package version (used by npm, pypi, nuget; not used by oci, mcpb where version is in the identifier)
	Version string `json:"version,omitempty" minLength:"1"`
	// FileSHA256 is the SHA-256 hash for integrity verification (required for mcpb, optional for others)
	FileSHA256 string `json:"fileSha256,omitempty"`
	// RunTimeHint suggests the appropriate runtime for the package
	RunTimeHint string `json:"runtimeHint,omitempty"`
	// Transport is required and specifies the transport protocol configuration
	Transport Transport `json:"transport"`
	// RuntimeArguments are passed to the package's runtime command (e.g., docker, npx)
	RuntimeArguments []Argument `json:"runtimeArguments,omitempty"`
	// PackageArguments are passed to the package's binary
	PackageArguments []Argument `json:"packageArguments,omitempty"`
	// EnvironmentVariables are set when running the package
	EnvironmentVariables []KeyValueInput `json:"environmentVariables,omitempty"`
}

// Repository represents a source code repository as defined in the spec
type Repository struct {
	URL       string `json:"url"`
	Source    string `json:"source"`
	ID        string `json:"id,omitempty"`
	Subfolder string `json:"subfolder,omitempty"`
}

// Format represents the input format type
type Format string

const (
	FormatString   Format = "string"
	FormatNumber   Format = "number"
	FormatBoolean  Format = "boolean"
	FormatFilePath Format = "filepath"
)

// Input represents a configuration input
type Input struct {
	Description string   `json:"description,omitempty"`
	IsRequired  bool     `json:"isRequired,omitempty"`
	Format      Format   `json:"format,omitempty"`
	Value       string   `json:"value,omitempty"`
	IsSecret    bool     `json:"isSecret,omitempty"`
	Default     string   `json:"default,omitempty"`
	Choices     []string `json:"choices,omitempty"`
}

// InputWithVariables represents an input that can contain variables
type InputWithVariables struct {
	Input     `json:",inline"`
	Variables map[string]Input `json:"variables,omitempty"`
}

// KeyValueInput represents a named input with variables
type KeyValueInput struct {
	InputWithVariables `json:",inline"`
	Name               string `json:"name"`
}

// ArgumentType represents the type of argument
type ArgumentType string

const (
	ArgumentTypePositional ArgumentType = "positional"
	ArgumentTypeNamed      ArgumentType = "named"
)

// Argument defines a type that can be either a PositionalArgument or a NamedArgument
type Argument struct {
	InputWithVariables `json:",inline"`
	Type               ArgumentType `json:"type"`
	Name               string       `json:"name,omitempty"`
	IsRepeated         bool         `json:"isRepeated,omitempty"`
	ValueHint          string       `json:"valueHint,omitempty"`
}

// Icon represents an optionally-sized icon that can be displayed in a user interface
type Icon struct {
	// Src is a standard URI pointing to an icon resource (HTTPS URL only for registry)
	Src string `json:"src" required:"true" format:"uri" maxLength:"255"`
	// MimeType is an optional MIME type override if the source MIME type is missing or generic
	MimeType *string `json:"mimeType,omitempty" enum:"image/png,image/jpeg,image/jpg,image/svg+xml,image/webp"`
	// Sizes is an optional array of strings that specify sizes at which the icon can be used
	// Each string should be in WxH format (e.g., "48x48", "96x96") or "any" for scalable formats
	Sizes []string `json:"sizes,omitempty" pattern:"^(\\d+x\\d+|any)$"`
	// Theme is an optional specifier for the theme this icon is designed for
	// "light" indicates the icon is designed for light backgrounds
	// "dark" indicates the icon is designed for dark backgrounds
	Theme *string `json:"theme,omitempty" enum:"light,dark"`
}
