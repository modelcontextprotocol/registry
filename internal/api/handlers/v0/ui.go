package v0

import (
	_ "embed"
	"strings"
)

//go:embed ui_index.html
var embedUI string

// GetUIHTML returns the embedded HTML for the UI with the given base path
// substituted into browser-side links and navigation.
func GetUIHTML(basePath string) string {
	return strings.ReplaceAll(embedUI, "{{UI_BASE_PATH}}", basePath)
}
