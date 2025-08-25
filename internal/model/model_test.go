//nolint:testpackage
package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractPublisherExtensions(t *testing.T) {
	tests := []struct {
		name     string
		request  PublishRequest
		expected map[string]interface{}
	}{
		{
			name: "nil publisher extensions",
			request: PublishRequest{
				Server: ServerDetail{Name: "test"},
			},
			expected: map[string]interface{}{},
		},
		{
			name: "empty publisher extensions",
			request: PublishRequest{
				Server:     ServerDetail{Name: "test"},
				XPublisher: map[string]interface{}{},
			},
			expected: map[string]interface{}{},
		},
		{
			name: "simple publisher extensions",
			request: PublishRequest{
				Server: ServerDetail{Name: "test"},
				XPublisher: map[string]interface{}{
					"build_info": map[string]interface{}{
						"version": "1.2.3",
						"commit":  "abc123",
					},
					"publisher_metadata": "test-publisher",
				},
			},
			expected: map[string]interface{}{
				"build_info": map[string]interface{}{
					"version": "1.2.3",
					"commit":  "abc123",
				},
				"publisher_metadata": "test-publisher",
			},
		},
		{
			name: "nested publisher extensions",
			request: PublishRequest{
				Server: ServerDetail{Name: "test"},
				XPublisher: map[string]interface{}{
					"ci": map[string]interface{}{
						"pipeline": map[string]interface{}{
							"id":     "12345",
							"branch": "main",
						},
						"artifacts": []string{"binary", "docs"},
					},
				},
			},
			expected: map[string]interface{}{
				"ci": map[string]interface{}{
					"pipeline": map[string]interface{}{
						"id":     "12345",
						"branch": "main",
					},
					"artifacts": []string{"binary", "docs"},
				},
			},
		},
		{
			name: "non-map publisher extensions (should be ignored)",
			request: PublishRequest{
				Server:     ServerDetail{Name: "test"},
				XPublisher: "invalid-string-data",
			},
			expected: map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractPublisherExtensions(tt.request)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPublisherExtensions_DoesNotDoubleNest(t *testing.T) {
	// This test specifically verifies the bug we fixed - no double nesting
	request := PublishRequest{
		Server: ServerDetail{Name: "test"},
		XPublisher: map[string]interface{}{
			"tool": "publisher-cli",
			"version": "1.0.0",
		},
	}

	result := ExtractPublisherExtensions(request)
	
	// Verify we get the data directly, not wrapped in another "x-publisher" key
	assert.Equal(t, "publisher-cli", result["tool"])
	assert.Equal(t, "1.0.0", result["version"])
	
	// Verify we don't have double nesting
	assert.NotContains(t, result, "x-publisher")
}

func TestRegistryMetadata_CreateRegistryExtensions(t *testing.T) {
	publishedTime := time.Date(2023, 12, 1, 10, 30, 0, 0, time.UTC)
	updatedTime := time.Date(2023, 12, 1, 11, 0, 0, 0, time.UTC)

	metadata := RegistryMetadata{
		ID:          "test-id-123",
		PublishedAt: publishedTime,
		UpdatedAt:   updatedTime,
		IsLatest:    true,
		ReleaseDate: "2023-12-01T10:30:00Z",
	}

	result := metadata.CreateRegistryExtensions()

	expected := map[string]interface{}{
		"x-io.modelcontextprotocol.registry": map[string]interface{}{
			"id":           "test-id-123",
			"published_at": publishedTime,
			"updated_at":   updatedTime,
			"is_latest":    true,
			"release_date": "2023-12-01T10:30:00Z",
		},
	}

	assert.Equal(t, expected, result)
}

func TestServerResponse_JSONSerialization(t *testing.T) {
	// Test that ServerResponse properly serializes to extension wrapper format
	publishedTime := time.Date(2023, 12, 1, 10, 30, 0, 0, time.UTC)
	
	response := ServerResponse{
		Server: ServerDetail{
			Name:        "test-server",
			Description: "A test server",
			Repository: Repository{
				URL:    "https://github.com/test/server",
				Source: "github",
				ID:     "test/server",
			},
			VersionDetail: VersionDetail{
				Version: "1.0.0",
			},
		},
		XIOModelContextProtocolRegistry: map[string]interface{}{
			"id":           "registry-id-123",
			"published_at": publishedTime.Format(time.RFC3339),
			"is_latest":    true,
		},
		XPublisher: map[string]interface{}{
			"build_tool": "ci-pipeline",
			"commit":     "abc123def",
		},
	}

	// Serialize to JSON
	jsonData, err := json.Marshal(response)
	require.NoError(t, err)

	// Parse back to verify structure
	var parsed map[string]interface{}
	err = json.Unmarshal(jsonData, &parsed)
	require.NoError(t, err)

	// Verify three-layer structure exists
	assert.Contains(t, parsed, "server")
	assert.Contains(t, parsed, "x-io.modelcontextprotocol.registry")
	assert.Contains(t, parsed, "x-publisher")

	// Verify server data
	serverData := parsed["server"].(map[string]interface{})
	assert.Equal(t, "test-server", serverData["name"])

	// Verify registry metadata
	registryData := parsed["x-io.modelcontextprotocol.registry"].(map[string]interface{})
	assert.Equal(t, "registry-id-123", registryData["id"])

	// Verify publisher extensions
	publisherData := parsed["x-publisher"].(map[string]interface{})
	assert.Equal(t, "ci-pipeline", publisherData["build_tool"])
	assert.Equal(t, "abc123def", publisherData["commit"])
}

func TestPublishRequest_WithPublisherExtensions(t *testing.T) {
	// Test that PublishRequest properly deserializes publisher extensions
	jsonData := `{
		"server": {
			"name": "test-server",
			"description": "Test server",
			"version_detail": {
				"version": "1.0.0"
			}
		},
		"x-publisher": {
			"tool": "publisher-cli",
			"metadata": {
				"build_date": "2023-12-01",
				"commit": "abc123"
			}
		}
	}`

	var request PublishRequest
	err := json.Unmarshal([]byte(jsonData), &request)
	require.NoError(t, err)

	// Verify server data
	assert.Equal(t, "test-server", request.Server.Name)
	assert.Equal(t, "Test server", request.Server.Description)

	// Verify publisher extensions
	require.NotNil(t, request.XPublisher)
	publisherMap := request.XPublisher.(map[string]interface{})
	assert.Equal(t, "publisher-cli", publisherMap["tool"])
	
	metadata := publisherMap["metadata"].(map[string]interface{})
	assert.Equal(t, "2023-12-01", metadata["build_date"])
	assert.Equal(t, "abc123", metadata["commit"])
}

func TestServerResponse_EmptyExtensions(t *testing.T) {
	// Test behavior with empty/nil extensions
	response := ServerResponse{
		Server: ServerDetail{
			Name:        "minimal-server",
			Description: "Minimal test",
		},
		XIOModelContextProtocolRegistry: map[string]interface{}{
			"id": "min-id",
		},
		XPublisher: nil,
	}

	jsonData, err := json.Marshal(response)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(jsonData, &parsed)
	require.NoError(t, err)

	// Should still have the structure, even with nil publisher extensions
	assert.Contains(t, parsed, "server")
	assert.Contains(t, parsed, "x-io.modelcontextprotocol.registry")
	
	// x-publisher should be null/nil in JSON when empty
	publisherValue, exists := parsed["x-publisher"]
	if exists {
		assert.Nil(t, publisherValue)
	}
}