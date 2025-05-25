// Package v0 contains API handlers for version 0 of the API
package v0

import (
	"encoding/json"
	"net/http"

	"github.com/modelcontextprotocol/registry/internal/config"
)

type HealthResponse struct {
	Status         string `json:"status"`
	GitHubClientId string `json:"github_client_id"`
}

// HealthHandler returns a handler for health check endpoint
func HealthHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{
			Status:         "ok",
			GitHubClientId: cfg.GithubClientID,
		})
	}
}
