package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func ptr[T any](v T) *T { return &v }

// TestBuildFilterConditions_SearchMatchesNameOrDescription covers the
// SubstringName/SubstringDescription combination directly against the SQL
// fragments buildFilterConditions produces. This is white-box on purpose:
// ListServers-level tests in postgres_test.go require a live PostgreSQL
// instance and can only assert on results, not on the generated WHERE
// clause. The behavior under test here is specifically that the ?search=
// query param (which the handler maps to both SubstringName and
// SubstringDescription set to the same term) must be OR'd together - every
// other pair of filter fields in ServerFilter is combined with AND, so this
// is the one case buildFilterConditions has to special-case to avoid
// silently requiring a match on both name and description at once.
func TestBuildFilterConditions_SearchMatchesNameOrDescription(t *testing.T) {
	tests := []struct {
		name           string
		filter         *ServerFilter
		wantConditions []string
		wantArgs       []any
	}{
		{
			name:           "nil filter produces no conditions",
			filter:         nil,
			wantConditions: nil,
			wantArgs:       nil,
		},
		{
			name: "SubstringName and SubstringDescription both set (the ?search= case) are OR'd",
			filter: &ServerFilter{
				SubstringName:        ptr("weather"),
				SubstringDescription: ptr("weather"),
			},
			wantConditions: []string{
				"(server_name ILIKE $1 ESCAPE '\\' OR value ->> 'description' ILIKE $2 ESCAPE '\\')",
				"status != 'deleted'",
			},
			wantArgs: []any{"%weather%", "%weather%"},
		},
		{
			name: "SubstringName alone still matches only the name column",
			filter: &ServerFilter{
				SubstringName: ptr("weather"),
			},
			wantConditions: []string{
				"server_name ILIKE $1 ESCAPE '\\'",
				"status != 'deleted'",
			},
			wantArgs: []any{"%weather%"},
		},
		{
			name: "SubstringDescription alone matches only the JSONB description field",
			filter: &ServerFilter{
				SubstringDescription: ptr("weather"),
			},
			wantConditions: []string{
				"value ->> 'description' ILIKE $1 ESCAPE '\\'",
				"status != 'deleted'",
			},
			wantArgs: []any{"%weather%"},
		},
		{
			name: "LIKE metacharacters in the search term are escaped on both sides of the OR",
			filter: &ServerFilter{
				SubstringName:        ptr("100%_free"),
				SubstringDescription: ptr("100%_free"),
			},
			wantConditions: []string{
				"(server_name ILIKE $1 ESCAPE '\\' OR value ->> 'description' ILIKE $2 ESCAPE '\\')",
				"status != 'deleted'",
			},
			wantArgs: []any{`%100\%\_free%`, `%100\%\_free%`},
		},
		{
			name: "combined search alongside other filters keeps AND semantics for the rest",
			filter: &ServerFilter{
				SubstringName:        ptr("weather"),
				SubstringDescription: ptr("weather"),
				IsLatest:             ptr(true),
			},
			wantConditions: []string{
				"(server_name ILIKE $1 ESCAPE '\\' OR value ->> 'description' ILIKE $2 ESCAPE '\\')",
				"is_latest = $3",
				"status != 'deleted'",
			},
			wantArgs: []any{"%weather%", "%weather%", true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotConditions, gotArgs, _ := buildFilterConditions(tt.filter, 1)
			assert.Equal(t, tt.wantConditions, gotConditions)
			assert.Equal(t, tt.wantArgs, gotArgs)
		})
	}
}
