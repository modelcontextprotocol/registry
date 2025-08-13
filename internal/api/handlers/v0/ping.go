package v0

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// PingOutput represents the ping response
type PingOutput struct {
	Body struct {
		Pong bool `json:"pong" example:"true" doc:"Ping response"`
	}
}

// RegisterPingEndpoint registers the ping endpoint
func RegisterPingEndpoint(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "ping",
		Method:      http.MethodGet,
		Path:        "/v0/ping",
		Summary:     "Ping",
		Description: "Simple ping endpoint",
		Tags:        []string{"ping"},
	}, func(_ context.Context, _ *struct{}) (*PingOutput, error) {
		resp := &PingOutput{}
		resp.Body.Pong = true
		return resp, nil
	})
}
