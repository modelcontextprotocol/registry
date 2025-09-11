package v0

import (
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// RegistryExtensions represents registry-generated metadata
type RegistryExtensions struct {
	ID          string    `json:"id"`
	PublishedAt time.Time `json:"published_at"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
	IsLatest    bool      `json:"is_latest"`
}

// ServerListResponse represents the paginated server list response
type ServerListResponse struct {
	Servers  []ServerJSON `json:"servers"`
	Metadata Metadata     `json:"metadata"`
}

// ServerMeta represents the structured metadata with known extension fields
type ServerMeta struct {
	Official         *RegistryExtensions    `json:"io.modelcontextprotocol.registry/official,omitempty"`
	PublisherProvided map[string]interface{} `json:"io.modelcontextprotocol.registry/publisher-provided,omitempty"`
}

// ServerJSON represents complete server information as defined in the MCP spec, with extension support
type ServerJSON struct {
	Schema        string              `json:"$schema,omitempty"`
	Name          string              `json:"name" minLength:"1" maxLength:"200"`
	Description   string              `json:"description" minLength:"1" maxLength:"100"`
	Status        model.Status        `json:"status,omitempty" minLength:"1"`
	Repository    model.Repository    `json:"repository,omitempty"`
	Version       string              `json:"version"`
	WebsiteURL    string              `json:"website_url,omitempty"`
	Packages      []model.Package     `json:"packages,omitempty"`
	Remotes       []model.Transport   `json:"remotes,omitempty"`
	Meta          *ServerMeta         `json:"_meta,omitempty"`
}

// Metadata represents pagination metadata
type Metadata struct {
	NextCursor string `json:"next_cursor,omitempty"`
	Count      int    `json:"count"`
}

func (s *ServerJSON) GetID() string {
	if s.Meta != nil && s.Meta.Official != nil {
		return s.Meta.Official.ID
	}
	return ""
}
