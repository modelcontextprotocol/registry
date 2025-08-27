package model

// ServerStatus represents the lifecycle status of a server
type ServerStatus string

const (
	ServerStatusActive     ServerStatus = "active"
	ServerStatusDeprecated ServerStatus = "deprecated"
	ServerStatusDeleted    ServerStatus = "deleted"
)

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