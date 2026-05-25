package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/cors"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/api/router"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/service"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

// NulByteValidationMiddleware rejects requests containing NUL bytes in URL path or query parameters.
// This prevents PostgreSQL encoding errors (SQLSTATE 22021) and returns a proper 400 Bad Request.
// Checks for both literal NUL bytes (\x00) and URL-encoded form (%00).
func NulByteValidationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check URL path for literal NUL bytes or URL-encoded %00
		// Path needs %00 check because handlers call url.PathUnescape() which would decode it
		if containsNulByte(r.URL.Path) {
			writeErrorResponse(w, http.StatusBadRequest, "Invalid request: URL path contains null bytes")
			return
		}

		// Check raw query string for literal NUL bytes or URL-encoded %00
		if containsNulByte(r.URL.RawQuery) {
			writeErrorResponse(w, http.StatusBadRequest, "Invalid request: query parameters contain null bytes")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// writeErrorResponse writes a JSON error response using huma's ErrorModel format
// for consistency with the rest of the API.
func writeErrorResponse(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	errModel := &huma.ErrorModel{
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	}
	_ = json.NewEncoder(w).Encode(errModel)
}

// containsNulByte checks if a string contains a NUL byte, either as a literal \x00
// or URL-encoded as %00.
func containsNulByte(s string) bool {
	// Check for literal NUL byte
	if strings.ContainsRune(s, '\x00') {
		return true
	}
	// Check for URL-encoded NUL byte (%00)
	// Using Contains directly since %00 has no case variation (both hex digits are 0)
	return strings.Contains(s, "%00")
}

// TrailingSlashMiddleware redirects requests with trailing slashes to their canonical form
func TrailingSlashMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only redirect if the path is not "/" and ends with a "/"
		if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
			// Remove the trailing slash from the path
			newPath := strings.TrimSuffix(r.URL.Path, "/")
			
			// Construct redirect URL safely - only use path and query to prevent open redirect
			redirectURL := newPath
			if r.URL.RawQuery != "" {
				redirectURL += "?" + r.URL.RawQuery
			}

			// Use 308 Permanent Redirect to preserve the request method
			http.Redirect(w, r, redirectURL, http.StatusPermanentRedirect)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SecurityHeadersMiddleware adds security-related HTTP headers
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HSTS: Enforce HTTPS for 1 year, including subdomains
		// Only set if the request is over HTTPS or in production
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		// Content Security Policy: Restrict resource loading to prevent XSS
		// Note: This is a baseline policy. Adjust based on specific application needs.
		csp := "default-src 'self'; " +
			"script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com; " +
			"style-src 'self' 'unsafe-inline'; " +
			"img-src 'self' data: https:; " +
			"font-src 'self' data:; " +
			"connect-src 'self' https://registry.modelcontextprotocol.io https://staging.registry.modelcontextprotocol.io; " +
			"frame-ancestors 'self'; " +
			"base-uri 'self'; " +
			"form-action 'self'"
		w.Header().Set("Content-Security-Policy", csp)

		// X-Content-Type-Options: Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// X-Frame-Options: Prevent clickjacking
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")

		// Referrer-Policy: Control referrer information
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		next.ServeHTTP(w, r)
	})
}

// Server represents the HTTP server
type Server struct {
	config   *config.Config
	registry service.RegistryService
	humaAPI  huma.API
	server   *http.Server
}

// NewServer creates a new HTTP server
func NewServer(cfg *config.Config, registryService service.RegistryService, metrics *telemetry.Metrics, versionInfo *v0.VersionBody) *Server {
	// Create HTTP mux and Huma API
	mux := http.NewServeMux()

	api := router.NewHumaAPI(cfg, registryService, mux, metrics, versionInfo)

	// Configure CORS with permissive settings for public API
	corsHandler := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowedHeaders:   []string{"*"},
		ExposedHeaders:   []string{"Content-Type", "Content-Length"},
		AllowCredentials: false, // Must be false when AllowedOrigins is "*"
		MaxAge:           86400, // 24 hours
	})

	// Wrap the mux with middleware stack
	// Order: NulByteValidation -> TrailingSlash -> SecurityHeaders -> CORS -> Mux
	handler := NulByteValidationMiddleware(
		TrailingSlashMiddleware(
			SecurityHeadersMiddleware(
				corsHandler.Handler(mux))))

	server := &Server{
		config:   cfg,
		registry: registryService,
		humaAPI:  api,
		server: &http.Server{
			Addr:              cfg.ServerAddress,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}

	return server
}

// Start begins listening for incoming HTTP requests
func (s *Server) Start() error {
	log.Printf("HTTP server starting on %s", s.config.ServerAddress)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
