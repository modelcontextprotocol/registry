// Package v0 contains API handlers for version 0 of the API
package v0

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/modelcontextprotocol/registry/internal/service"
)

// PublishHandler handles requests to publish new server details to the registry
func PublishHandler(registry service.RegistryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only allow POST method
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Parse request body into ServerDetail struct
		var serverDetail model.ServerDetail
		err = json.Unmarshal(body, &serverDetail)
		if err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		// Validate required fields
		if serverDetail.Name == "" {
			http.Error(w, "Name is required", http.StatusBadRequest)
			return
		}

		// version is required
		if serverDetail.VersionDetail.Version == "" {
			http.Error(w, "Version is required", http.StatusBadRequest)
			return
		}

		// Call the publish method on the registry service
		err = registry.Publish(&serverDetail)
		if err != nil {
			http.Error(w, "Failed to publish server details: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Server publication successful",
			"id":      serverDetail.ID,
		})
	}
}
