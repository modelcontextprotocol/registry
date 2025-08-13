// Package v0 contains API handlers for version 0 of the API
package v0

import (
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger"
)

// SwaggerHandler returns a handler that serves the Swagger UI
func SwaggerHandler() http.HandlerFunc {
	return httpSwagger.WrapHandler
}
