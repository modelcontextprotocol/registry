package v0

import (
	_ "embed"
)

//go:embed ui_index.html
var embedUI string

// GetUIHTML returns the embedded HTML for the UI
func GetUIHTML() string {
	return embedUI
}
