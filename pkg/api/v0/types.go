package v0

import (
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// RegistryExtensions represents registry-generated metadata
type RegistryExtensions struct {
	ServerID    string       `json:"serverId"`  // Consistent ID across all versions of a server
	VersionID   string       `json:"versionId"` // Unique ID for this specific version
	Status      model.Status `json:"status"`    // Server status (moved from server.json)
	PublishedAt time.Time    `json:"publishedAt"`
	UpdatedAt   time.Time    `json:"updatedAt,omitempty"`
	IsLatest    bool         `json:"isLatest"`
}

// ResponseMeta represents top-level metadata for API responses
type ResponseMeta struct {
	Official *RegistryExtensions `json:"io.modelcontextprotocol.registry/official,omitempty"`
}

// ServerResponse represents the new API response structure with separated concerns
type ServerResponse struct {
	Server ServerJSON   `json:"server"`
	Meta   ResponseMeta `json:"_meta"`
}

// ServerListResponse represents the paginated server list response
type ServerListResponse struct {
	Servers  []ServerResponse `json:"servers"`
	Metadata Metadata         `json:"metadata"`
}

// ServerMeta represents the structured metadata for publisher-provided extensions only
type ServerMeta struct {
	PublisherProvided map[string]interface{} `json:"io.modelcontextprotocol.registry/publisher-provided,omitempty"`
}

// ServerJSON represents the immutable server.json as defined in the MCP spec (status moved to registry metadata)
type ServerJSON struct {
	Schema      string            `json:"$schema,omitempty"`
	Name        string            `json:"name" minLength:"1" maxLength:"200"`
	Description string            `json:"description" minLength:"1" maxLength:"100"`
	Repository  model.Repository  `json:"repository,omitempty"`
	Version     string            `json:"version"`
	WebsiteURL  string            `json:"websiteUrl,omitempty"`
	Packages    []model.Package   `json:"packages,omitempty"`
	Remotes     []model.Transport `json:"remotes,omitempty"`
	Meta        *ServerMeta       `json:"_meta,omitempty"` // Publisher-provided metadata only
}

// Metadata represents pagination metadata
type Metadata struct {
	NextCursor string `json:"next_cursor,omitempty"`
	Count      int    `json:"count"`
}

// Helper methods moved to ServerResponse since ServerJSON no longer contains official metadata
func (s *ServerResponse) GetServerID() string {
	if s.Meta.Official != nil {
		return s.Meta.Official.ServerID
	}
	return ""
}

func (s *ServerResponse) GetVersionID() string {
	if s.Meta.Official != nil {
		return s.Meta.Official.VersionID
	}
	return ""
}

func (s *ServerResponse) GetStatus() model.Status {
	if s.Meta.Official != nil {
		return s.Meta.Official.Status
	}
	return ""
}
