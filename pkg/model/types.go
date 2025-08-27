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