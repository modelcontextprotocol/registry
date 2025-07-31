package model

// AuthMethod represents the authentication method used
type AuthMethod string

const (
	// AuthMethodGitHub represents GitHub OAuth authentication
	AuthMethodGitHub AuthMethod = "github"
	// AuthMethodNone represents no authentication
	AuthMethodNone AuthMethod = "none"
)

// ServerStatus represents the lifecycle status of a server
type ServerStatus string

const (
	// ServerStatusActive represents an actively maintained server (as asserted by the publisher)
	ServerStatusActive ServerStatus = "active"
	// ServerStatusDeprecated represents a server that is no longer actively maintained
	ServerStatusDeprecated ServerStatus = "deprecated"
)

// Authentication holds information about the authentication method and credentials
type Authentication struct {
	Method  AuthMethod `json:"method,omitempty"`
	Token   string     `json:"token,omitempty"`
	RepoRef string     `json:"repo_ref,omitempty"`
}

// PublishRequest represents a request to publish a server to the registry
type PublishRequest struct {
	ServerDetail    `json:",inline"`
	AuthStatusToken string `json:"-"` // Used internally for device flows
}

// Repository represents a source code repository as defined in the spec
type Repository struct {
	URL    string `json:"url" bson:"url"`
	Source string `json:"source" bson:"source"`
	ID     string `json:"id" bson:"id"`
}

// ServerList represents the response for listing servers as defined in the spec
type ServerList struct {
	Servers    []Server `json:"servers" bson:"servers"`
	Next       string   `json:"next,omitempty" bson:"next,omitempty"`
	TotalCount int      `json:"total_count" bson:"total_count"`
}

// create an enum for Format
type Format string

const (
	FormatString   Format = "string"
	FormatNumber   Format = "number"
	FormatBoolean  Format = "boolean"
	FormatFilePath Format = "file_path"
)

// UserInput represents a user input as defined in the spec
type Input struct {
	Description string           `json:"description,omitempty" bson:"description,omitempty"`
	IsRequired  bool             `json:"is_required,omitempty" bson:"is_required,omitempty"`
	Format      Format           `json:"format,omitempty" bson:"format,omitempty"`
	Value       string           `json:"value,omitempty" bson:"value,omitempty"`
	IsSecret    bool             `json:"is_secret,omitempty" bson:"is_secret,omitempty"`
	Default     string           `json:"default,omitempty" bson:"default,omitempty"`
	Choices     []string         `json:"choices,omitempty" bson:"choices,omitempty"`
	Template    string           `json:"template,omitempty" bson:"template,omitempty"`
	Properties  map[string]Input `json:"properties,omitempty" bson:"properties,omitempty"`
}

type InputWithVariables struct {
	Input     `json:",inline" bson:",inline"`
	Variables map[string]Input `json:"variables,omitempty" bson:"variables,omitempty"`
}

type KeyValueInput struct {
	InputWithVariables `json:",inline" bson:",inline"`
	Name               string `json:"name" bson:"name"`
}
type ArgumentType string

const (
	ArgumentTypePositional ArgumentType = "positional"
	ArgumentTypeNamed      ArgumentType = "named"
)

// RuntimeArgument defines a type that can be either a PositionalArgument or a NamedArgument
type Argument struct {
	InputWithVariables `json:",inline" bson:",inline"`
	Type               ArgumentType `json:"type" bson:"type"`
	Name               string       `json:"name,omitempty" bson:"name,omitempty"`
	IsRepeated         bool         `json:"is_repeated,omitempty" bson:"is_repeated,omitempty"`
	ValueHint          string       `json:"value_hint,omitempty" bson:"value_hint,omitempty"`
}

type Package struct {
	RegistryName         string          `json:"registry_name" bson:"registry_name"`
	Name                 string          `json:"name" bson:"name"`
	Version              string          `json:"version" bson:"version"`
	RunTimeHint          string          `json:"runtime_hint,omitempty" bson:"runtime_hint,omitempty"`
	RuntimeArguments     []Argument      `json:"runtime_arguments,omitempty" bson:"runtime_arguments,omitempty"`
	PackageArguments     []Argument      `json:"package_arguments,omitempty" bson:"package_arguments,omitempty"`
	EnvironmentVariables []KeyValueInput `json:"environment_variables,omitempty" bson:"environment_variables,omitempty"`
}

// Remote represents a remote connection endpoint
type Remote struct {
	TransportType string  `json:"transport_type" bson:"transport_type"`
	URL           string  `json:"url" bson:"url"`
	Headers       []Input `json:"headers,omitempty" bson:"headers,omitempty"`
}

// VersionDetail represents the version details of a server
type VersionDetail struct {
	Version     string `json:"version" bson:"version"`
	ReleaseDate string `json:"release_date" bson:"release_date"`
	IsLatest    bool   `json:"is_latest" bson:"is_latest"`
}

// Server represents a basic server information as defined in the spec
type Server struct {
	ID            string        `json:"id" bson:"id"`
	Name          string        `json:"name" bson:"name"`
	Description   string        `json:"description" bson:"description"`
	Status        ServerStatus  `json:"status,omitempty" bson:"status,omitempty"`
	Repository    Repository    `json:"repository" bson:"repository"`
	VersionDetail VersionDetail `json:"version_detail" bson:"version_detail"`
}

// ServerDetail represents detailed server information as defined in the spec
type ServerDetail struct {
	Server   `json:",inline" bson:",inline"`
	Packages []Package `json:"packages,omitempty" bson:"packages,omitempty"`
	Remotes  []Remote  `json:"remotes,omitempty" bson:"remotes,omitempty"`
}
