package v0

import (
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

type RegistryExtensions struct {
	Status          model.Status `json:"status" enum:"active,deprecated,deleted" doc:"Server lifecycle status"`
	StatusChangedAt time.Time    `json:"statusChangedAt" format:"date-time" doc:"Timestamp when the server status was last changed"`
	StatusMessage   *string      `json:"statusMessage,omitempty" doc:"Optional message explaining status change (e.g., deprecation reason, migration guidance)"`
	PublishedAt     time.Time    `json:"publishedAt" format:"date-time" doc:"Timestamp when the server was first published to the registry"`
	UpdatedAt       time.Time    `json:"updatedAt,omitempty" format:"date-time" doc:"Timestamp when the server entry was last updated"`
	IsLatest        bool         `json:"isLatest" doc:"Whether this is the latest version of the server"`
}

type ResponseMeta struct {
	Official *RegistryExtensions `json:"io.modelcontextprotocol.registry/official,omitempty" doc:"Official MCP registry metadata"`
}

type ServerResponse struct {
	Server ServerJSON   `json:"server" doc:"Server configuration and metadata"`
	Meta   ResponseMeta `json:"_meta" doc:"Registry-managed metadata"`
}

type ServerListResponse struct {
	// encoded contains immutable, normalized wire records for HTTP-only listing.
	// Internal database/service operations continue to use typed ServerResponse.
	encoded  []EncodedServerRecord
	Servers  []ServerResponse `json:"servers" doc:"List of server entries"`
	Metadata Metadata         `json:"metadata" doc:"Pagination metadata"`
}

type ServerMeta struct {
	PublisherProvided map[string]interface{} `json:"io.modelcontextprotocol.registry/publisher-provided,omitempty" doc:"Publisher-provided metadata for downstream registries"`
}

type ServerJSON struct {
	Schema      string            `json:"$schema" required:"true" minLength:"1" format:"uri" doc:"JSON Schema URI for this server.json format" example:"https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json"`
	Name        string            `json:"name" minLength:"3" maxLength:"200" pattern:"^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$" doc:"Server name in reverse-DNS format. Must contain exactly one forward slash separating namespace from server name." example:"io.github.user/weather"`
	Description string            `json:"description" minLength:"1" maxLength:"100" doc:"Clear human-readable explanation of server functionality." example:"MCP server providing weather data and forecasts via OpenWeatherMap API"`
	Title       string            `json:"title,omitempty" minLength:"1" maxLength:"100" doc:"Optional human-readable title or display name for the MCP server." example:"Weather API"`
	Repository  *model.Repository `json:"repository,omitempty" doc:"Optional repository metadata for the MCP server source code."`
	Version     string            `json:"version" minLength:"1" maxLength:"255" doc:"Version string for this server. SHOULD follow semantic versioning." example:"1.0.2"`
	WebsiteURL  string            `json:"websiteUrl,omitempty" format:"uri" doc:"Optional URL to the server's homepage, documentation, or project website." example:"https://modelcontextprotocol.io/examples"`
	Icons       []model.Icon      `json:"icons,omitempty" doc:"Optional set of sized icons that the client can display in a user interface."`
	Packages    []model.Package   `json:"packages,omitempty" doc:"Array of package configurations"`
	Remotes     []model.Transport `json:"remotes,omitempty" doc:"Array of remote configurations"`
	Meta        *ServerMeta       `json:"_meta,omitempty" doc:"Extension metadata using reverse DNS namespacing for vendor-specific data"`
}

type Metadata struct {
	NextCursor string `json:"nextCursor,omitempty" doc:"Pagination cursor for retrieving the next page of results. Use this exact value in the cursor query parameter of your next request."`
	Count      int    `json:"count" doc:"Number of items in current page"`
}

// NewEncodedServerList creates a serialization-only listing. Each record must
// contain normalized ServerJSON bytes and current metadata. It takes ownership of
// records; callers must not mutate them after construction.
func NewEncodedServerList(records []EncodedServerRecord, metadata Metadata) ServerListResponse {
	if records == nil {
		records = make([]EncodedServerRecord, 0)
	}
	return ServerListResponse{encoded: records, Metadata: metadata}
}

func (s ServerListResponse) MarshalJSON() ([]byte, error) {
	if s.encoded == nil {
		type plain ServerListResponse
		return json.Marshal(plain(s))
	}
	// Payloads were normalized with json.Marshal before caching. Metadata is
	// freshly encoded here; append avoids copying every large payload through
	// intermediate record and slice encoders. The outer encoder validates JSON.
	capacity := 64
	for _, record := range s.encoded {
		capacity += len(record.Server) + 384
	}
	result := make([]byte, 0, capacity)
	result = append(result, `{"servers":[`...)
	for i, record := range s.encoded {
		if i > 0 {
			result = append(result, ',')
		}
		meta, err := json.Marshal(record.Meta)
		if err != nil {
			return nil, err
		}
		result = append(result, `{"server":`...)
		result = append(result, record.Server...)
		result = append(result, `,"_meta":`...)
		result = append(result, meta...)
		result = append(result, '}')
	}
	metadata, err := json.Marshal(s.Metadata)
	if err != nil {
		return nil, err
	}
	result = append(result, `],"metadata":`...)
	result = append(result, metadata...)
	return append(result, '}'), nil
}

// EncodedServerRecord contains a normalized server payload and fresh metadata.
// It is used only to construct a serialization-only ServerListResponse.
type EncodedServerRecord struct {
	Server json.RawMessage
	Meta   ResponseMeta
}
