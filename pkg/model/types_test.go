package model_test

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestRepository_JSONMarshal_OmitsEmptyFields(t *testing.T) {
	tests := []struct {
		name     string
		repo     model.Repository
		expected string
	}{
		{
			name:     "empty repository",
			repo:     model.Repository{},
			expected: `{}`,
		},
		{
			name: "repository with only URL",
			repo: model.Repository{
				URL: "https://github.com/owner/repo",
			},
			expected: `{"url":"https://github.com/owner/repo"}`,
		},
		{
			name: "repository with only source",
			repo: model.Repository{
				Source: "github",
			},
			expected: `{"source":"github"}`,
		},
		{
			name: "repository with all fields",
			repo: model.Repository{
				URL:       "https://github.com/owner/repo",
				Source:    "github",
				ID:        "owner/repo",
				Subfolder: "src",
			},
			expected: `{"url":"https://github.com/owner/repo","source":"github","id":"owner/repo","subfolder":"src"}`,
		},
		{
			name: "repository with empty strings",
			repo: model.Repository{
				URL:    "",
				Source: "",
			},
			expected: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.repo)
			if err != nil {
				t.Fatalf("Failed to marshal repository: %v", err)
			}

			actual := string(data)
			if actual != tt.expected {
				t.Errorf("Expected JSON %s, got %s", tt.expected, actual)
			}
		})
	}
}

func TestRepository_JSONUnmarshal_HandlesEmptyFields(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected model.Repository
	}{
		{
			name:     "empty JSON object",
			json:     `{}`,
			expected: model.Repository{},
		},
		{
			name: "JSON with only URL",
			json: `{"url":"https://github.com/owner/repo"}`,
			expected: model.Repository{
				URL: "https://github.com/owner/repo",
			},
		},
		{
			name: "JSON with all fields",
			json: `{"url":"https://github.com/owner/repo","source":"github","id":"owner/repo","subfolder":"src"}`,
			expected: model.Repository{
				URL:       "https://github.com/owner/repo",
				Source:    "github",
				ID:        "owner/repo",
				Subfolder: "src",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var repo model.Repository
			err := json.Unmarshal([]byte(tt.json), &repo)
			if err != nil {
				t.Fatalf("Failed to unmarshal JSON: %v", err)
			}

			if repo != tt.expected {
				t.Errorf("Expected repository %+v, got %+v", tt.expected, repo)
			}
		})
	}
}