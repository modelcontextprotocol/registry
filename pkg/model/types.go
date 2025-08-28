package model

// Status represents the lifecycle status of a server
type Status string

const (
	StatusActive     Status = "active"
	StatusDeprecated Status = "deprecated"
	StatusDeleted    Status = "deleted"
)

// ServerJSON represents complete server information as defined in the MCP spec (pure, no registry metadata)
type ServerJSON struct {
	Schema        string        `json:"$schema,omitempty" bson:"$schema,omitempty"`
	Name          string        `json:"name" minLength:"1" maxLength:"200" bson:"name"`
	Description   string        `json:"description" minLength:"1" maxLength:"100" bson:"description"`
	Status        Status        `json:"status,omitempty" minLength:"1" bson:"status,omitempty"`
	Repository    Repository    `json:"repository,omitempty" bson:"repository"`
	VersionDetail VersionDetail `json:"version_detail" bson:"version_detail"`
	Packages      []Package     `json:"packages,omitempty" bson:"packages,omitempty"`
	Remotes       []Remote      `json:"remotes,omitempty" bson:"remotes,omitempty"`
}

// Package represents a package configuration
type Package struct {
	// RegistryType indicates how to download packages (e.g., "npm", "pypi", "docker-hub", "mcpb")
	RegistryType string `json:"registry_type,omitempty" bson:"registry_type,omitempty"`
	// RegistryBaseURL is the base URL of the package registry
	RegistryBaseURL string `json:"registry_base_url,omitempty" bson:"registry_base_url,omitempty"`
	// Identifier is the package identifier - either a package name (for registries) or URL (for direct downloads)
	Identifier           string          `json:"identifier,omitempty" bson:"identifier,omitempty"`
	Version              string          `json:"version,omitempty" bson:"version,omitempty"`
	FileSHA256           string          `json:"file_sha256,omitempty" bson:"file_sha256,omitempty"`
	RunTimeHint          string          `json:"runtime_hint,omitempty" bson:"runtime_hint,omitempty"`
	RuntimeArguments     []Argument      `json:"runtime_arguments,omitempty" bson:"runtime_arguments,omitempty"`
	PackageArguments     []Argument      `json:"package_arguments,omitempty" bson:"package_arguments,omitempty"`
	EnvironmentVariables []KeyValueInput `json:"environment_variables,omitempty" bson:"environment_variables,omitempty"`
}

// Remote represents a remote connection endpoint
type Remote struct {
	TransportType string          `json:"transport_type" bson:"transport_type"`
	URL           string          `json:"url" format:"uri" bson:"url"`
	Headers       []KeyValueInput `json:"headers,omitempty" bson:"headers,omitempty"`
}

// Repository represents a source code repository as defined in the spec
type Repository struct {
	URL    string `json:"url" bson:"url"`
	Source string `json:"source" bson:"source"`
	ID     string `json:"id,omitempty" bson:"id,omitempty"`
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
	Description string   `json:"description,omitempty" bson:"description,omitempty"`
	IsRequired  bool     `json:"is_required,omitempty" bson:"is_required,omitempty"`
	Format      Format   `json:"format,omitempty" bson:"format,omitempty"`
	Value       string   `json:"value,omitempty" bson:"value,omitempty"`
	IsSecret    bool     `json:"is_secret,omitempty" bson:"is_secret,omitempty"`
	Default     string   `json:"default,omitempty" bson:"default,omitempty"`
	Choices     []string `json:"choices,omitempty" bson:"choices,omitempty"`
}

// InputWithVariables represents an input that can contain variables
type InputWithVariables struct {
	Input     `json:",inline" bson:",inline"`
	Variables map[string]Input `json:"variables,omitempty" bson:"variables,omitempty"`
}

// KeyValueInput represents a named input with variables
type KeyValueInput struct {
	InputWithVariables `json:",inline" bson:",inline"`
	Name               string `json:"name" bson:"name"`
}

// ArgumentType represents the type of argument
type ArgumentType string

const (
	ArgumentTypePositional ArgumentType = "positional"
	ArgumentTypeNamed      ArgumentType = "named"
)

// Argument defines a type that can be either a PositionalArgument or a NamedArgument
type Argument struct {
	InputWithVariables `json:",inline" bson:",inline"`
	Type               ArgumentType `json:"type" bson:"type"`
	Name               string       `json:"name,omitempty" bson:"name,omitempty"`
	IsRepeated         bool         `json:"is_repeated,omitempty" bson:"is_repeated,omitempty"`
	ValueHint          string       `json:"value_hint,omitempty" bson:"value_hint,omitempty"`
}

// VersionDetail represents the version details of a server (pure MCP spec, no registry metadata)
type VersionDetail struct {
	Version string `json:"version" bson:"version"`
}
