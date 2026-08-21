package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var camelCaseServerJSON = []byte(`{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
  "name": "com.example/test-server",
  "description": "A test server",
  "version": "1.0.0",
  "packages": [
    {
      "registryType": "npm",
      "identifier": "@example/test-server",
      "version": "1.0.0",
      "transport": {
        "type": "stdio"
      },
      "environmentVariables": [
        {
          "name": "API_KEY",
          "description": "API key",
          "isRequired": true,
          "isSecret": true
        }
      ]
    }
  ]
}`)

func assertCamelCaseRequestBody(t *testing.T, body []byte) {
	t.Helper()

	assert.JSONEq(t, string(camelCaseServerJSON), string(body))
	assert.Contains(t, string(body), `"registryType"`)
	assert.Contains(t, string(body), `"environmentVariables"`)
	assert.Contains(t, string(body), `"isRequired"`)
	assert.Contains(t, string(body), `"isSecret"`)
	assert.NotContains(t, string(body), `"registry_type"`)
	assert.NotContains(t, string(body), `"environment_variables"`)
	assert.NotContains(t, string(body), `"is_required"`)
	assert.NotContains(t, string(body), `"is_secret"`)
}

func TestPublishToRegistry_PreservesOriginalJSONFieldCasing(t *testing.T) {
	var requestBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v0/publish", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		var err error
		requestBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		assertCamelCaseRequestBody(t, requestBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"server":{"$schema":"` + model.CurrentSchemaURL + `","name":"com.example/test-server","description":"A test server","version":"1.0.0"}}`))
	}))
	defer server.Close()

	resp, statusCode, err := publishToRegistry(server.URL, camelCaseServerJSON, "test-token")
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, statusCode)
	require.NotNil(t, resp)
	assertCamelCaseRequestBody(t, requestBody)
}

func TestValidateViaAPI_PreservesOriginalJSONFieldCasing(t *testing.T) {
	var requestBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v0/validate", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var err error
		requestBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		assertCamelCaseRequestBody(t, requestBody)

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(validators.ValidationResult{Valid: true}))
	}))
	defer server.Close()

	result, err := validateViaAPI(server.URL, camelCaseServerJSON)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.True(t, strings.Contains(string(requestBody), `"registryType"`))
}
