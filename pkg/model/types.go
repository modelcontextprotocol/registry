package model

// ServerStatus represents the lifecycle status of a server
type ServerStatus string

const (
	ServerStatusActive     ServerStatus = "active"
	ServerStatusDeprecated ServerStatus = "deprecated"
	ServerStatusDeleted    ServerStatus = "deleted"
)