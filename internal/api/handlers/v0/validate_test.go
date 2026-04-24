package v0_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type issueStruct struct {
	Type      string `json:"type"`
	Path      string `json:"path"`
	Message   string `json:"message"`
	Severity  string `json:"severity"`
	Reference string `json:"reference"`
}

func TestValidateEndpoint(t *testing.T) {
	// Create a new ServeMux and Huma API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	// Register the endpoint
	v0.RegisterValidateEndpoint(api, "/v0")

	testCases := []struct {
		name           string
		serverJSON     apiv0.ServerJSON
		expectedValid  bool
		expectedStatus int
		description    string
		validateIssues func(t *testing.T, issues []issueStruct)
	}{
		{
			name: "valid server json",
			serverJSON: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "1.0.0",
			},
			expectedValid:  true,
			expectedStatus: http.StatusOK,
			description:    "Should return valid for a properly formatted server JSON",
			validateIssues: func(t *testing.T, issues []issueStruct) {
				t.Helper()
				assert.Empty(t, issues, "Valid server JSON should have no issues")
			},
		},
		{
			name: "version range should be invalid",
			serverJSON: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "^1.0.0", // Version range, not allowed
			},
			expectedValid:  false,
			expectedStatus: http.StatusOK,
			description:    "Should return invalid when version is a range instead of specific version",
			validateIssues: func(t *testing.T, issues []issueStruct) {
				t.Helper()
				require.Greater(t, len(issues), 0, "Should have at least one issue")
				issue := issues[0]
				assert.Equal(t, "semantic", issue.Type, "Issue type should be semantic")
				assert.Equal(t, "version", issue.Path, "Issue path should be 'version'")
				assert.Equal(t, "error", issue.Severity, "Issue severity should be error")
				assert.NotEmpty(t, issue.Message, "Issue message should not be empty")
				assert.Contains(t, issue.Message, "^1.0.0", "Issue message should contain the version range")
				assert.NotEmpty(t, issue.Reference, "Issue reference should not be empty")
			},
		},
		{
			name: "latest version should be invalid",
			serverJSON: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/test-server",
				Description: "A test server",
				Version:     "latest", // "latest" is reserved
			},
			expectedValid:  false,
			expectedStatus: http.StatusOK,
			description:    "Should return invalid when version is 'latest' (reserved word)",
			validateIssues: func(t *testing.T, issues []issueStruct) {
				t.Helper()
				require.Greater(t, len(issues), 0, "Should have at least one issue")
				issue := issues[0]
				assert.Equal(t, "semantic", issue.Type, "Issue type should be semantic")
				assert.Equal(t, "version", issue.Path, "Issue path should be 'version'")
				assert.Equal(t, "error", issue.Severity, "Issue severity should be error")
				assert.NotEmpty(t, issue.Message, "Issue message should not be empty")
				assert.Contains(t, strings.ToLower(issue.Message), "latest", "Issue message should mention 'latest'")
				assert.NotEmpty(t, issue.Reference, "Issue reference should not be empty")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create request body
			bodyBytes, err := json.Marshal(tc.serverJSON)
			require.NoError(t, err)

			// Create request
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/v0/validate", bytes.NewBuffer(bodyBytes))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			// Perform request
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			// Assert status code
			assert.Equal(t, tc.expectedStatus, rr.Code, "%s: expected status %d, got %d", tc.description, tc.expectedStatus, rr.Code)

			// Parse response - Huma returns ValidationResult directly
			var result struct {
				Valid  bool          `json:"valid"`
				Issues []issueStruct `json:"issues"`
			}
			err = json.Unmarshal(rr.Body.Bytes(), &result)
			require.NoError(t, err, "Failed to parse response: %s", rr.Body.String())

			// Debug output for failed tests
			if tc.expectedValid != result.Valid {
				t.Logf("Response body: %s", rr.Body.String())
				t.Logf("Validation issues: %+v", result.Issues)
			}

			// Always log response for first test case to show structure
			if tc.name == "valid server json" {
				var prettyJSON bytes.Buffer
				if err := json.Indent(&prettyJSON, rr.Body.Bytes(), "", "  "); err == nil {
					t.Logf("Example valid response:\n%s", prettyJSON.String())
				}
			}
			if tc.name == "version range should be invalid" {
				var prettyJSON bytes.Buffer
				if err := json.Indent(&prettyJSON, rr.Body.Bytes(), "", "  "); err == nil {
					t.Logf("Example invalid response:\n%s", prettyJSON.String())
				}
			}

			// Assert validity
			assert.Equal(t, tc.expectedValid, result.Valid, "%s: expected valid=%v, got %v. Issues: %+v", tc.description, tc.expectedValid, result.Valid, result.Issues)

			// Validate issues structure and content
			if tc.validateIssues != nil {
				tc.validateIssues(t, result.Issues)
			} else if !tc.expectedValid {
				// Default validation: if expected invalid, verify there are issues
				assert.Greater(t, len(result.Issues), 0, "%s: expected validation issues but got none. Response: %s", tc.description, rr.Body.String())
				// Validate all issues have required fields
				for i, issue := range result.Issues {
					assert.NotEmpty(t, issue.Type, "Issue %d: type should not be empty", i)
					assert.NotEmpty(t, issue.Severity, "Issue %d: severity should not be empty", i)
					assert.NotEmpty(t, issue.Message, "Issue %d: message should not be empty", i)
				}
			}
		})
	}
}
