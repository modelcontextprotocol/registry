package v0_test

import (
	"strings"
	"testing"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
)

func TestGetUIHTML_NotEmpty(t *testing.T) {
	html := v0.GetUIHTML()
	if html == "" {
		t.Fatal("GetUIHTML returned empty string")
	}
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Error("UI HTML missing DOCTYPE declaration")
	}
	if !strings.Contains(html, "Official MCP Registry") {
		t.Error("UI HTML missing expected page title text")
	}
}

// TestGetUIHTML_IconSupport guards the icon-rendering wiring added for
// modelcontextprotocol/registry#784. If a future refactor removes the icon
// picker or the lazy-loading attribute, this test fails so the regression
// surfaces in CI rather than after release.
func TestGetUIHTML_IconSupport(t *testing.T) {
	html := v0.GetUIHTML()

	checks := map[string]string{
		"pickIconSrc helper":                "function pickIconSrc",
		"renderIconTile helper":             "function renderIconTile",
		"lazy-loaded <img> tag":             `loading="lazy"`,
		"server-icon-fallback":              "server-icon-fallback",
		"https-only icon URL":               "^https:",
		"explicit icon dimensions (width)":  `width="40"`,
		"explicit icon dimensions (height)": `height="40"`,
	}

	for name, marker := range checks {
		if !strings.Contains(html, marker) {
			t.Errorf("UI HTML missing %s (expected substring %q)", name, marker)
		}
	}
}
