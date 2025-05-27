// Package v0 contains API handlers for version 0 of the API
package v0

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/model"
)

// StartAuthHandler handles requests to start an authentication flow
func StartAuthHandler(authService auth.Service) http.HandlerFunc {
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

		// Parse request body into AuthRequest struct
		var authReq struct {
			Method  string `json:"method"`
			RepoRef string `json:"repo_ref"`
		}
		err = json.Unmarshal(body, &authReq)
		if err != nil {
			http.Error(w, "Invalid request payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Validate required fields
		if authReq.Method == "" {
			http.Error(w, "Auth method is required", http.StatusBadRequest)
			return
		}

		// Convert string method to enum type
		var method model.AuthMethod
		switch authReq.Method {
		case "github":
			method = model.AuthMethodGitHub
		default:
			http.Error(w, "Unsupported authentication method", http.StatusBadRequest)
			return
		}

		// Start auth flow
		flowInfo, statusToken, err := authService.StartAuthFlow(r.Context(), method, authReq.RepoRef)
		if err != nil {
			http.Error(w, "Failed to start auth flow: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Return successful response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"flow_info":    flowInfo,
			"status_token": statusToken,
			"expires_in":   300, // 5 minutes
		}); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

// CheckAuthStatusHandler handles requests to check the status of an authentication flow
func CheckAuthStatusHandler(authService auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET method
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get status token from query parameter
		statusToken := r.URL.Query().Get("token")
		if statusToken == "" {
			http.Error(w, "Status token is required", http.StatusBadRequest)
			return
		}

		// Check auth status
		token, err := authService.CheckAuthStatus(r.Context(), statusToken)
		if err != nil {
			if err.Error() == "pending" {
				// Auth is still pending
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if err := json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "pending",
				}); err != nil {
					http.Error(w, "Failed to encode response", http.StatusInternalServerError)
					return
				}
				return
			}

			// Other error
			http.Error(w, "Failed to check auth status: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Authentication completed successfully
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "complete",
			"token":  token,
		}); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}
