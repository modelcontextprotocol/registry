// Package v0 contains API handlers for version 0 of the API
package v0

import (
	"encoding/json"
	"net/http"
)

// HealthHandler returns a handler for health check endpoint
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}
