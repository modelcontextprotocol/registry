package v0

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
)

// normalizeDomain extracts and cleans the domain from a URL or domain string
// It removes protocols, paths, and query parameters, returning just the hostname
func normalizeDomain(domain string) (string, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "", errors.New("domain cannot be empty")
	}

	// Try parsing as URL first (handles cases with protocol)
	if u, err := url.Parse(domain); err == nil && u.Host != "" {
		return strings.ToLower(u.Host), nil
	}

	// If no protocol, try adding one and parsing again
	if !strings.Contains(domain, "://") {
		if u, err := url.Parse("https://" + domain); err == nil && u.Host != "" {
			return strings.ToLower(u.Host), nil
		}
	}

	// If we get here, the input is not a valid domain/URL
	return "", errors.New("invalid domain format")
}

// DomainClaimRequest represents the request body for domain claiming
type DomainClaimRequest struct {
	Domain string `json:"domain"`
}

// DomainStatusRequest represents the request body for domain status checking
type DomainStatusRequest struct {
	Domain string `json:"domain"`
}

// DomainClaimResponse represents the response for domain claim operations
type DomainClaimResponse struct {
	Domain           string `json:"domain"`            // Original domain from request
	NormalizedDomain string `json:"normalized_domain"` // Cleaned domain (TLD + subdomains)
	Token            string `json:"token"`
	CreatedAt        string `json:"created_at"`
}

// DomainStatusResponse represents the response for domain verification status
type DomainStatusResponse struct {
	Domain string `json:"domain"`
	Status string `json:"status"` // "verified" or "unverified"
}

// ClaimDomainHandler handles requests to claim a domain for verification
func ClaimDomainHandler(registry service.RegistryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only allow POST method
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse request body
		var req DomainClaimRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, "Invalid request payload: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Validate required fields
		if req.Domain == "" {
			http.Error(w, "domain is required", http.StatusBadRequest)
			return
		}

		// Normalize the domain (remove protocol, path, etc.)
		normalizedDomain, err := normalizeDomain(req.Domain)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Generate and store the verification token for the normalized domain
		verificationToken, err := registry.ClaimDomain(normalizedDomain)
		if err != nil {
			http.Error(w, "Failed to claim domain: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Prepare response
		response := DomainClaimResponse{
			Domain:           req.Domain,
			NormalizedDomain: normalizedDomain,
			Token:            verificationToken.Token,
			CreatedAt:        verificationToken.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

// GetDomainStatusHandler handles requests to get domain verification status
func GetDomainStatusHandler(registry service.RegistryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET method
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get domain from query parameter
		domain := r.URL.Query().Get("domain")
		if domain == "" {
			http.Error(w, "domain query parameter is required", http.StatusBadRequest)
			return
		}

		// Normalize the domain (remove protocol, path, etc.)
		normalizedDomain, err := normalizeDomain(domain)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Get the domain verification status using normalized domain
		verificationTokens, err := registry.GetDomainVerificationStatus(normalizedDomain)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				http.Error(w, "Domain not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Failed to retrieve domain status: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Determine status
		status := "unverified"
		if verificationTokens.VerifiedToken != nil {
			status = "verified"
		}

		// Prepare response with normalized domain
		response := DomainStatusResponse{
			Domain: normalizedDomain,
			Status: status,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}
