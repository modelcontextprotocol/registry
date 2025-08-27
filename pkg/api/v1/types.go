package v1

import (
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// RegistryExtensions represents registry-generated metadata
type RegistryExtensions struct {
	ID          string    `json:"id" bson:"_id"`
	PublishedAt time.Time `json:"published_at" bson:"published_at"`
	UpdatedAt   time.Time `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	IsLatest    bool      `json:"is_latest" bson:"is_latest"`
	ReleaseDate string    `json:"release_date" bson:"release_date"`
}

// CreateRegistryExtensions generates the x-io.modelcontextprotocol.registry extension from registry metadata
func (rm *RegistryExtensions) CreateRegistryExtensions() map[string]interface{} {
	return map[string]interface{}{
		"x-io.modelcontextprotocol.registry": map[string]interface{}{
			"id":           rm.ID,
			"published_at": rm.PublishedAt,
			"updated_at":   rm.UpdatedAt,
			"is_latest":    rm.IsLatest,
			"release_date": rm.ReleaseDate,
		},
	}
}

// ServerRecord represents the complete storage model that separates server.json from registry metadata
type ServerRecord struct {
	ServerJSON          model.ServerJSON       `json:"server" bson:"server"`                             // Pure MCP server.json
	RegistryExtensions  RegistryExtensions     `json:"registry_extensions" bson:"registry_extensions"`   // Registry-generated data
	PublisherExtensions map[string]interface{} `json:"publisher_extensions" bson:"publisher_extensions"` // x-publisher extensions
}

// ServerResponse represents the API response format with wrapper and extensions
type ServerResponse struct {
	Server                          model.ServerJSON `json:"server"`
	XIOModelContextProtocolRegistry interface{}      `json:"x-io.modelcontextprotocol.registry,omitempty"`
	XPublisher                      interface{}      `json:"x-publisher,omitempty"`
}

// ServerListResponse represents the paginated server list response
type ServerListResponse struct {
	Servers  []ServerResponse `json:"servers"`
	Metadata *Metadata        `json:"metadata,omitempty"`
}

// PublishRequest represents the API request format for publishing servers
type PublishRequest struct {
	Server     model.ServerJSON `json:"server"`
	XPublisher interface{}      `json:"x-publisher,omitempty"`
}

// Metadata represents pagination metadata
type Metadata struct {
	NextCursor string `json:"next_cursor,omitempty"`
	Count      int    `json:"count,omitempty"`
	Total      int    `json:"total,omitempty"`
}

// ToServerResponse converts a ServerRecord to API response format
func (sr *ServerRecord) ToServerResponse() ServerResponse {
	response := ServerResponse{
		Server: sr.ServerJSON,
	}

	// Add registry metadata extension
	response.XIOModelContextProtocolRegistry = sr.RegistryExtensions.CreateRegistryExtensions()["x-io.modelcontextprotocol.registry"]

	// Add publisher extensions directly
	if len(sr.PublisherExtensions) > 0 {
		response.XPublisher = sr.PublisherExtensions
	}

	return response
}
