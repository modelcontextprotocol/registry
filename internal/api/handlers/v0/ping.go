// Package v0 contains API handlers for version 0 of the API
package v0

import (
	"encoding/json"
	"net/http"

	"github.com/modelcontextprotocol/registry/internal/config"
)

// PingHandler returns a handler for the ping endpoint that returns build version
func PingHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		response := map[string]string{
			"status":  "ok",
			"version": cfg.Version,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
	}
}
