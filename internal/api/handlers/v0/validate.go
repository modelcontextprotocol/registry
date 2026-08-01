package v0

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// ValidateServerInput represents the input for validating a server JSON
type ValidateServerInput struct {
	Body apiv0.ServerJSON `body:""`
}

// RegisterValidateEndpoint registers the validate endpoint with a custom path prefix
func RegisterValidateEndpoint(api huma.API, pathPrefix string) {
	huma.Register(api, huma.Operation{
		OperationID: "validate-server" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/validate",
		Summary:     "Validate MCP server JSON",
		Description: "Validate a server.json file without publishing it to the registry",
		Tags:        []string{"validate"},
	}, func(_ context.Context, input *ValidateServerInput) (*Response[validators.ValidationResult], error) {
		// Perform comprehensive validation (schema version, full schema validation, and semantic)
		result := validators.ValidateServerJSON(&input.Body, validators.ValidationAll)

		// Return validation result (always 200 OK, validity indicated in result.Valid)
		return &Response[validators.ValidationResult]{
			Body: *result,
		}, nil
	})
}
