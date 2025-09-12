package v0_test

import (
	"encoding/json"
	"testing"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestServerJSON_OmitsEmptyRepository(t *testing.T) {
	tests := []struct {
		name     string
		server   apiv0.ServerJSON
		wantRepo bool // whether repository field should be in the JSON
	}{
		{
			name: "server with empty repository",
			server: apiv0.ServerJSON{
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Repository:  model.Repository{}, // empty repository
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "https://example.com/mcp",
					},
				},
			},
			wantRepo: false,
		},
		{
			name: "server with repository containing empty strings",
			server: apiv0.ServerJSON{
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Repository: model.Repository{
					URL:    "",
					Source: "",
				},
				Remotes: []model.Transport{
					{
						Type: "streamable-http",
						URL:  "https://example.com/mcp",
					},
				},
			},
			wantRepo: false,
		},
		{
			name: "server with valid repository",
			server: apiv0.ServerJSON{
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
				Repository: model.Repository{
					URL:    "https://github.com/owner/repo",
					Source: "github",
				},
			},
			wantRepo: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.server)
			if err != nil {
				t.Fatalf("Failed to marshal server: %v", err)
			}

			var result map[string]interface{}
			err = json.Unmarshal(data, &result)
			if err != nil {
				t.Fatalf("Failed to unmarshal JSON: %v", err)
			}

			_, hasRepo := result["repository"]
			if hasRepo != tt.wantRepo {
				if tt.wantRepo {
					t.Errorf("Expected repository field to be present in JSON, but it was missing")
				} else {
					t.Errorf("Expected repository field to be omitted from JSON, but it was present: %s", string(data))
				}
			}
		})
	}
}

func TestServerJSON_RemoteOnlyServer(t *testing.T) {
	// This test specifically addresses issue #463
	server := apiv0.ServerJSON{
		Name:        "com.example/remote-server",
		Description: "A remote-only MCP server",
		Version:     "1.0.0",
		Remotes: []model.Transport{
			{
				Type: "streamable-http",
				URL:  "https://api.example.com/mcp",
			},
		},
		// No repository field set - should be omitted from JSON
	}

	data, err := json.Marshal(server)
	if err != nil {
		t.Fatalf("Failed to marshal server: %v", err)
	}

	jsonStr := string(data)
	
	// Check that the JSON doesn't contain a repository field
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if _, hasRepo := result["repository"]; hasRepo {
		t.Errorf("Remote-only server should not have repository field in JSON output.\nGot: %s", jsonStr)
	}
}