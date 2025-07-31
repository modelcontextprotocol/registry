package v0

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/modelcontextprotocol/registry/internal/service"
	"golang.org/x/net/html"
)

// TokenGenerateRequest represents the request body for token generation
type TokenGenerateRequest struct {
	ServerID string `json:"server_id"`
}

// TokenResponse represents the response for token operations
type TokenResponse struct {
	Token     string `json:"token"`
	CreatedAt string `json:"created_at"`
	ServerID  string `json:"server_id"`
}

// GenerateVerificationTokenHandler handles requests to generate verification tokens
func GenerateVerificationTokenHandler(registry service.RegistryService, authService auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only allow POST method
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse request body
		var req TokenGenerateRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, "Invalid request payload: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Validate required fields
		if req.ServerID == "" {
			http.Error(w, "server_id is required", http.StatusBadRequest)
			return
		}

		// Check if the server exists
		_, err = registry.GetByID(req.ServerID)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				http.Error(w, "Server not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Failed to verify server existence: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Get auth token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header is required", http.StatusUnauthorized)
			return
		}

		// Handle bearer token format
		token := authHeader
		if len(authHeader) > 7 && strings.ToUpper(authHeader[:7]) == "BEARER " {
			token = authHeader[7:]
		}

		// Determine authentication method based on server ID prefix
		var authMethod model.AuthMethod
		switch {
		case strings.HasPrefix(req.ServerID, "io.github"):
			authMethod = model.AuthMethodGitHub
		default:
			authMethod = model.AuthMethodNone
		}

		serverName := html.EscapeString(req.ServerID)

		// Setup authentication info
		a := model.Authentication{
			Method:  authMethod,
			Token:   token,
			RepoRef: serverName,
		}

		valid, err := authService.ValidateAuth(r.Context(), a)
		if err != nil {
			if errors.Is(err, auth.ErrAuthRequired) {
				http.Error(w, "Authentication is required for token generation", http.StatusUnauthorized)
				return
			}
			http.Error(w, "Authentication failed: "+err.Error(), http.StatusUnauthorized)
			return
		}

		if !valid {
			http.Error(w, "Invalid authentication credentials", http.StatusUnauthorized)
			return
		}

		// Generate the verification token
		verificationToken, err := registry.GenerateVerificationToken(req.ServerID)
		if err != nil {
			http.Error(w, "Failed to generate verification token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Prepare response
		response := TokenResponse{
			Token:     verificationToken.Token,
			CreatedAt: verificationToken.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			ServerID:  req.ServerID,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

// GetVerificationTokenHandler handles requests to retrieve verification tokens
func GetVerificationTokenHandler(registry service.RegistryService, authService auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET method
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract server ID from URL path
		path := strings.TrimPrefix(r.URL.Path, "/v0/verification/")
		serverID := strings.Split(path, "/")[0]

		if serverID == "" {
			http.Error(w, "server_id is required", http.StatusBadRequest)
			return
		}

		// Check if the server exists
		_, err := registry.GetByID(serverID)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				http.Error(w, "Server not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Failed to verify server existence: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Get auth token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header is required", http.StatusUnauthorized)
			return
		}

		// Handle bearer token format
		token := authHeader
		if len(authHeader) > 7 && strings.ToUpper(authHeader[:7]) == "BEARER " {
			token = authHeader[7:]
		}

		// Determine authentication method based on server ID prefix
		var authMethod model.AuthMethod
		switch {
		case strings.HasPrefix(serverID, "io.github"):
			authMethod = model.AuthMethodGitHub
		default:
			authMethod = model.AuthMethodNone
		}

		serverName := html.EscapeString(serverID)

		// Setup authentication info
		a := model.Authentication{
			Method:  authMethod,
			Token:   token,
			RepoRef: serverName,
		}

		valid, err := authService.ValidateAuth(r.Context(), a)
		if err != nil {
			if errors.Is(err, auth.ErrAuthRequired) {
				http.Error(w, "Authentication is required for token retrieval", http.StatusUnauthorized)
				return
			}
			http.Error(w, "Authentication failed: "+err.Error(), http.StatusUnauthorized)
			return
		}

		if !valid {
			http.Error(w, "Invalid authentication credentials", http.StatusUnauthorized)
			return
		}

		// Get the verification token
		verificationToken, err := registry.GetVerificationToken(serverID)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				http.Error(w, "Verification token not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Failed to retrieve verification token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Prepare response
		response := TokenResponse{
			Token:     verificationToken.Token,
			CreatedAt: verificationToken.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			ServerID:  serverID,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}
