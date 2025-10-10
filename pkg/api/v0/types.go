package v0

import (
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// RegistryExtensions represents registry-generated metadata
type RegistryExtensions struct {
	Status      model.Status `json:"status"`
	PublishedAt time.Time    `json:"publishedAt"`
	UpdatedAt   time.Time    `json:"updatedAt,omitempty"`
	IsLatest    bool         `json:"isLatest"`
}

// ResponseMeta represents the top-level metadata in API responses
type ResponseMeta struct {
	Official *RegistryExtensions `json:"io.modelcontextprotocol.registry/official,omitempty"`
}

// ServerResponse represents the new API response format with separated metadata
type ServerResponse struct {
	Server ServerJSON   `json:"server"`
	Meta   ResponseMeta `json:"_meta"`
}

// ServerListResponse represents the paginated server list response
type ServerListResponse struct {
	Servers  []ServerResponse `json:"servers"`
	Metadata Metadata         `json:"metadata"`
}

// ServerMeta represents the structured metadata with known extension fields
type ServerMeta struct {
	PublisherProvided map[string]interface{} `json:"io.modelcontextprotocol.registry/publisher-provided,omitempty"`
}

// ServerJSON represents complete server information as defined in the MCP spec, with extension support
type ServerJSON struct {
	Schema      string            `json:"$schema" required:"true" minLength:"1"`
	Name        string            `json:"name" minLength:"1" maxLength:"200"`
	Description string            `json:"description" minLength:"1" maxLength:"100"`
	Title       string            `json:"title,omitempty" minLength:"1" maxLength:"100"`
	Repository  model.Repository  `json:"repository,omitempty"`
	Version     string            `json:"version"`
	WebsiteURL  string            `json:"websiteUrl,omitempty"`
	Icons       []model.Icon      `json:"icons,omitempty"`
	Packages    []model.Package   `json:"packages,omitempty"`
	Remotes     []model.Transport `json:"remotes,omitempty"`
	Meta        *ServerMeta       `json:"_meta,omitempty"`
}

// Metadata represents pagination metadata
type Metadata struct {
	NextCursor string `json:"nextCursor,omitempty"`
	Count      int    `json:"count"`
}
