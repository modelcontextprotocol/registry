package v0

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed ui_index.html
var embedUI string

const (
	defaultAPIBasePlaceholder = "PLACEHOLDER"
	defaultAPIBaseValueKey    = "__DEFAULT_API_BASE_VALUE__"
	defaultAPIBasePlaceholderKey = "__DEFAULT_API_BASE_PLACEHOLDER__"
)

// GetUIHTML returns the embedded HTML for the UI with injected API base URL configuration.
// The defaultBaseURL is injected into the JavaScript context to allow the frontend to
// access a custom registry URL set via environment variable. If no custom URL is provided,
// the frontend will fall back to the default production registry.
func GetUIHTML(defaultBaseURL string) string {
	// Marshal the URL to get a JSON-encoded string for safe JavaScript injection
	jsonEncoded, err := json.Marshal(defaultBaseURL)
	if err != nil {
		// Fallback to empty string if marshaling fails (should be very rare for strings)
		jsonEncoded = []byte(`""`)
	}

	// Trim the surrounding quotes from JSON encoding to get the raw URL value
	value := strings.Trim(string(jsonEncoded), `"`)

	// Inject the actual URL value into the UI
	html := strings.ReplaceAll(embedUI, defaultAPIBaseValueKey, value)

	// Inject the placeholder sentinel value for frontend comparison logic
	html = strings.ReplaceAll(html, defaultAPIBasePlaceholderKey, defaultAPIBasePlaceholder)

	return html
}
