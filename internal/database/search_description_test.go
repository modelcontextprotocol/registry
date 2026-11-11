package database_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/require"
)

// Search must match terms found only in a server's description (issue #1807):
// discovery by functionality depends on the same substring semantics that
// name matching already provides.
func TestSearchMatchesDescriptionOnlyTerms(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	now := time.Now()
	fixture := []struct{ name, description string }{
		{"com.example/weather-tool", "Provides weather forecasts and severe weather alerts"},
		{"com.example/alpha-notes", "Takes structured meeting notes and action items"},
		{"com.example/coverage-tracker", "Reports 100% test coverage summaries"},
		{"com.example/path-helper", "Handles and/or normalizes request paths"},
	}
	for _, f := range fixture {
		_, err := db.CreateServer(ctx, nil,
			&apiv0.ServerJSON{Name: f.name, Version: "1.0.0", Description: f.description},
			&apiv0.RegistryExtensions{Status: model.StatusActive, PublishedAt: now, UpdatedAt: now, StatusChangedAt: now, IsLatest: true})
		require.NoError(t, err)
	}

	search := func(t *testing.T, term string) []string {
		t.Helper()
		var names []string
		cursor := ""
		for page := 0; page < 20; page++ {
			rows, next, err := db.ListServers(ctx, nil, &database.ServerFilter{SubstringName: &term}, cursor, 2)
			require.NoError(t, err)
			for _, r := range rows {
				names = append(names, r.Server.Name)
			}
			if next == "" {
				break
			}
			require.NotEqual(t, cursor, next)
			cursor = next
		}
		return names
	}

	cases := []struct {
		term string
		want []string
	}{
		// Terms present only in the description: the reported symptom.
		{"forecasts", []string{"com.example/weather-tool"}},
		{"action items", []string{"com.example/alpha-notes"}},
		{"request paths", []string{"com.example/path-helper"}},
		// Case-insensitive partial matching, same as name matching.
		{"FoReCaSt", []string{"com.example/weather-tool"}},
		// Literal substring semantics: LIKE metacharacters must not act wild.
		{"100%", []string{"com.example/coverage-tracker"}},
		{"100_", nil},
		// Terms spanning '/' match descriptions literally even though name
		// matching interprets them as namespace-qualified names.
		{"and/or", []string{"com.example/path-helper"}},
		// Name matching still works alongside description matching.
		{"alpha", []string{"com.example/alpha-notes"}},
		{"weather", []string{"com.example/weather-tool"}},
		// Unrelated terms stay excluded.
		{"blockchain", nil},
	}
	for _, tc := range cases {
		t.Run(tc.term, func(t *testing.T) {
			require.Equal(t, tc.want, search(t, tc.term))
		})
	}
}

// Mixed name and description matches share one ordered, duplicate-free
// pagination stream like ordinary listings.
func TestSearchDescriptionMatchesPaginateWithNameMatches(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	now := time.Now()
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("com.example/desc-page-%02d", i)
		description := "Contains the unique sentinel term somewhere in text"
		if i%2 == 1 {
			// Odd rows match only through their names, not their descriptions.
			name = fmt.Sprintf("com.example/sentinel-name-%02d", i)
			description = "Nothing relevant here"
		}
		_, err := db.CreateServer(ctx, nil,
			&apiv0.ServerJSON{Name: name, Version: "1.0.0", Description: description},
			&apiv0.RegistryExtensions{Status: model.StatusActive, PublishedAt: now, UpdatedAt: now, StatusChangedAt: now, IsLatest: true})
		require.NoError(t, err)
	}

	term := "sentinel"
	want := []string{
		"com.example/desc-page-00:1.0.0",
		"com.example/desc-page-02:1.0.0",
		"com.example/sentinel-name-01:1.0.0",
		"com.example/sentinel-name-03:1.0.0",
	}
	var got []string
	cursor := ""
	for page := 0; page < 20; page++ {
		rows, next, err := db.ListServers(ctx, nil, &database.ServerFilter{SubstringName: &term}, cursor, 1)
		require.NoError(t, err)
		for _, r := range rows {
			got = append(got, r.Server.Name+":"+r.Server.Version)
		}
		if next == "" {
			break
		}
		require.NotEqual(t, cursor, next)
		cursor = next
	}
	require.Equal(t, want, got, "ordered pagination over name and description matches")
	require.Equal(t, len(want), len(got), "no duplicate rows across pages")
}

// A description term must not be excluded by the name-namespace fast path
// that narrows '/'-spanning terms to namespace suffixes.
func TestSearchDescriptionSurvivesNamespaceNarrowing(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	now := time.Now()
	_, err := db.CreateServer(ctx, nil,
		&apiv0.ServerJSON{Name: "com.example/unrelated-name", Version: "1.0.0", Description: "Supports io.github.tools/backup workflows"},
		&apiv0.RegistryExtensions{Status: model.StatusActive, PublishedAt: now, UpdatedAt: now, StatusChangedAt: now, IsLatest: true})
	require.NoError(t, err)

	term := "tools/backup"
	rows, next, err := db.ListServers(ctx, nil, &database.ServerFilter{SubstringName: &term}, "", 10)
	require.NoError(t, err)
	require.Empty(t, next)
	require.Len(t, rows, 1)
	require.True(t, strings.Contains(rows[0].Server.Description, "tools/backup"))
}
