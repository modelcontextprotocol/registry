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

// Package represents a package configuration
type Package struct {
	// RegistryType indicates how to download packages (e.g., "npm", "pypi", "oci", "mcpb")
	RegistryType string `json:"registry_type" minLength:"1"`
	// RegistryBaseURL is the base URL of the package registry
	RegistryBaseURL string `json:"registry_base_url,omitempty"`
	// Identifier is the package identifier - either a package name (for registries) or URL (for direct downloads)
	Identifier           string          `json:"identifier" minLength:"1"`
	Version              string          `json:"version" minLength:"1"`
	FileSHA256           string          `json:"file_sha256,omitempty"`
	RunTimeHint          string          `json:"runtime_hint,omitempty"`
	Transport            Transport       `json:"transport,omitempty"`
	RuntimeArguments     []Argument      `json:"runtime_arguments,omitempty"`
	PackageArguments     []Argument      `json:"package_arguments,omitempty"`
	EnvironmentVariables []KeyValueInput `json:"environment_variables,omitempty"`
}

// Repository represents a source code repository as defined in the spec
type Repository struct {
	URL    string `json:"url"`
	Source string `json:"source"`
	ID     string `json:"id,omitempty"`
}

// Format represents the input format type
type Format string

const (
	FormatString   Format = "string"
	FormatNumber   Format = "number"
	FormatBoolean  Format = "boolean"
	FormatFilePath Format = "file_path"
)

// Input represents a configuration input
type Input struct {
	Description string   `json:"description,omitempty"`
	IsRequired  bool     `json:"is_required,omitempty"`
	Format      Format   `json:"format,omitempty"`
	Value       string   `json:"value,omitempty"`
	IsSecret    bool     `json:"is_secret,omitempty"`
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
	IsRepeated         bool         `json:"is_repeated,omitempty"`
	ValueHint          string       `json:"value_hint,omitempty"`
}

// VersionDetail represents the version details of a server (pure MCP spec, no registry metadata)
type VersionDetail struct {
	Version string `json:"version" minLength:"1"`
}
