package v0

import (
	"encoding/json"
	
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// MarshalJSON implements custom JSON marshaling for ServerJSON to properly handle empty repositories
func (s ServerJSON) MarshalJSON() ([]byte, error) {
	// Check if repository is empty (all fields are zero values)
	isEmptyRepo := s.Repository.URL == "" && s.Repository.Source == "" && 
		s.Repository.ID == "" && s.Repository.Subfolder == ""
	
	// Create an alias type to avoid infinite recursion
	type Alias ServerJSON
	
	if isEmptyRepo {
		// Create a version without the repository field
		type ServerJSONNoRepo struct {
			Schema        string              `json:"$schema,omitempty"`
			Name          string              `json:"name"`
			Description   string              `json:"description"`
			Status        model.Status        `json:"status,omitempty"`
			Version       string              `json:"version"`
			WebsiteURL    string              `json:"website_url,omitempty"`
			Packages      []model.Package     `json:"packages,omitempty"`
			Remotes       []model.Transport   `json:"remotes,omitempty"`
			Meta          *ServerMeta         `json:"_meta,omitempty"`
		}
		
		noRepo := ServerJSONNoRepo{
			Schema:      s.Schema,
			Name:        s.Name,
			Description: s.Description,
			Status:      s.Status,
			Version:     s.Version,
			WebsiteURL:  s.WebsiteURL,
			Packages:    s.Packages,
			Remotes:     s.Remotes,
			Meta:        s.Meta,
		}
		
		return json.Marshal(noRepo)
	}
	
	// If repository is not empty, use default marshaling
	return json.Marshal(Alias(s))
}