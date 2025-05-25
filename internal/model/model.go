package model

// AuthMethod represents the authentication method used
type AuthMethod string

const (
	// AuthMethodGitHub represents GitHub OAuth authentication
	AuthMethodGitHub AuthMethod = "github"
	// AuthMethodNone represents no authentication
	AuthMethodNone AuthMethod = "none"
)

// Authentication holds information about the authentication method and credentials
type Authentication struct {
	Method  AuthMethod `json:"method,omitempty"`
	Token   string     `json:"token,omitempty"`
	RepoRef string     `json:"repo_ref,omitempty"`
}

// PublishRequest represents a request to publish a server to the registry
type PublishRequest struct {
	ServerDetail    ServerDetail   `json:"server_detail"`
	Authentication  Authentication `json:"-"` // Now provided via Authorization header
	AuthStatusToken string         `json:"-"` // Used internally for device flows
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
	Repository    Repository    `json:"repository" bson:"repository"`
	VersionDetail VersionDetail `json:"version_detail" bson:"version_detail"`
}

// ServerDetail represents detailed server information as defined in the spec
type ServerDetail struct {
	Server           `json:",inline" bson:",inline"`
	PackageCanonical string    `json:"package_canonical,omitempty" bson:"package_canonical,omitempty"`
	Packages         []Package `json:"packages,omitempty" bson:"packages,omitempty"`
	Remotes          []Remote  `json:"remotes,omitempty" bson:"remotes,omitempty"`
}

package model

// ... existing code ...

// Status represents a generic status object
type Status struct {
	Code    int    `json:"code" bson:"code"`
	Message string `json:"message" bson:"message"`
}

// ServerMetrics represents some basic metrics of a server
type ServerMetrics struct {
	CPUUsage    float64 `json:"cpu_usage" bson:"cpu_usage"`
	MemoryUsage float64 `json:"memory_usage" bson:"memory_usage"`
	Uptime      int64   `json:"uptime" bson:"uptime"`
}

// Tag represents a server tag or label
type Tag struct {
	Key   string `json:"key" bson:"key"`
	Value string `json:"value" bson:"value"`
}

// ServerStatus defines a simple server health status
type ServerStatus string

const (
	StatusHealthy   ServerStatus = "healthy"
	StatusDegraded  ServerStatus = "degraded"
	StatusUnhealthy ServerStatus = "unhealthy"
)

// ServerHealthCheck represents a health check result
type ServerHealthCheck struct {
	Timestamp string        `json:"timestamp" bson:"timestamp"`
	Status    ServerStatus  `json:"status" bson:"status"`
	Message   string        `json:"message" bson:"message"`
	Metrics   ServerMetrics `json:"metrics" bson:"metrics"`
}

// User represents a registered user in the system
type User struct {
	ID        string `json:"id" bson:"id"`
	Username  string `json:"username" bson:"username"`
	Email     string `json:"email" bson:"email"`
	IsAdmin   bool   `json:"is_admin" bson:"is_admin"`
	CreatedAt string `json:"created_at" bson:"created_at"`
}

// Dummy utility function to generate a new server ID
func NewServerID() string {
	return "srv-" + RandomString(10)
}

// RandomString generates a random string of given length
func RandomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[i%len(letters)]
	}
	return string(b)
}

// IsPackageValid performs a dummy package validation
func (p Package) IsPackageValid() bool {
	return p.Name != "" && p.Version != ""
}

// GetLatestPackageVersion returns the latest version from a list of packages
func GetLatestPackageVersion(packages []Package) string {
	if len(packages) == 0 {
		return ""
	}
	// Dummy: just return the version of the first package
	return packages[0].Version
}

// UpdateMetrics updates the server's metrics (dummy implementation)
func (m *ServerMetrics) UpdateMetrics(cpu, mem float64, uptime int64) {
	m.CPUUsage = cpu
	m.MemoryUsage = mem
	m.Uptime = uptime
}

// IsHealthy determines if the server is healthy based on dummy criteria
func (h ServerHealthCheck) IsHealthy() bool {
	return h.Status == StatusHealthy
}

// AddTag adds a new tag to a list of tags
func AddTag(tags *[]Tag, key, value string) {
	*tags = append(*tags, Tag{Key: key, Value: value})
}

// RemoveTag removes a tag by key
func RemoveTag(tags *[]Tag, key string) {
	for i, t := range *tags {
		if t.Key == key {
			*tags = append((*tags)[:i], (*tags)[i+1:]...)
			break
		}
	}
}

// Dummy list of supported auth methods
var SupportedAuthMethods = []AuthMethod{
	AuthMethodGitHub,
	AuthMethodNone,
}

// IsSupportedAuthMethod checks if the given auth method is supported
func IsSupportedAuthMethod(method AuthMethod) bool {
	for _, m := range SupportedAuthMethods {
		if m == method {
			return true
		}
	}
	return false
}
