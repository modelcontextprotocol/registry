// Package v0 contains API handlers for version 0 of the API
package v0

import (
	"encoding/json"
	"net/http"

	"github.com/modelcontextprotocol/registry/internal/config"
)

type HealthResponse struct {
	Status         string `json:"status"`
	GitHubClientID string `json:"github_client_id"`
}

// HealthHandler returns a handler for health check endpoint
func HealthHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(HealthResponse{
			Status:         "ok",
			GitHubClientID: cfg.GithubClientID,
		}); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
	}
}
