package v0_test

import (
	"strings"
	"testing"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/stretchr/testify/require"
)

func TestUIHTML_SearchPrecedesRecentlyUpdated(t *testing.T) {
	html := v0.GetUIHTML()

	searchPos := strings.Index(html, `id="search"`)
	latestOnlyPos := strings.Index(html, `id="latest-only"`)
	recentPos := strings.Index(html, `id="recently-updated"`)

	require.NotEqual(t, -1, searchPos, "id=\"search\" must be present in UI HTML")
	require.NotEqual(t, -1, latestOnlyPos, "id=\"latest-only\" must be present in UI HTML")
	require.NotEqual(t, -1, recentPos, "id=\"recently-updated\" must be present in UI HTML")
	require.Less(t, searchPos, recentPos, "search control must appear before recently-updated section")
	require.Less(t, latestOnlyPos, recentPos, "filter control must appear before recently-updated section")
}
