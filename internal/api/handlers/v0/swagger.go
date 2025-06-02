// Package v0 contains API handlers for version 0 of the API
package v0

import (
	"net/http"
	"os"
	"path/filepath"

	_ "github.com/swaggo/files" // Swagger files needed for embedding
	httpSwagger "github.com/swaggo/http-swagger"
)

// SwaggerHandler returns a handler that serves the Swagger UI
func SwaggerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// When accessed directly, redirect to the UI path
		if r.URL.Path == "/v0/swagger" {
			http.Redirect(w, r, "/v0/swagger/", http.StatusFound)
			return
		}

		// Serve the Swagger UI
		handler := httpSwagger.Handler(
			httpSwagger.URL("/v0/swagger/doc.json"), // The URL to the generated Swagger JSON
			httpSwagger.DeepLinking(true),
		)

		// Handle other Swagger UI paths
		handler.ServeHTTP(w, r)
	}
}

// SwaggerJSONHandler serves the Swagger specification as JSON
func SwaggerJSONHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Find the project root directory
		workDir, err := os.Getwd()
		if err != nil {
			http.Error(w, "Unable to determine working directory", http.StatusInternalServerError)
			return
		}

		// Path to the swagger YAML file
		swaggerFilePath := filepath.Join(workDir, "internal", "docs", "swagger.yaml")

		// Serve the file directly with the correct content type
		http.ServeFile(w, r, swaggerFilePath)
	}
}
