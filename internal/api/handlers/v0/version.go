package v0

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// VersionBody represents the version information
type VersionBody struct {
	Version   string `json:"version" example:"v1.0.0" doc:"Application version"`
	GitCommit string `json:"git_commit" example:"abc123d" doc:"Git commit SHA"`
	BuildTime string `json:"build_time" example:"2025-10-14T12:00:00Z" doc:"Build timestamp"`
}

// RegisterVersionEndpoint registers the version endpoint with a custom path prefix
func RegisterVersionEndpoint(api huma.API, pathPrefix string, versionInfo *VersionBody) {
	huma.Register(api, huma.Operation{
		OperationID: "get-version" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/version",
		Summary:     "Get version information",
		Description: "Returns the version, git commit, and build time of the registry application",
		Tags:        []string{"version"},
	}, func(_ context.Context, _ *struct{}) (*Response[VersionBody], error) {
		return &Response[VersionBody]{
			Body: *versionInfo,
		}, nil
	})
}
