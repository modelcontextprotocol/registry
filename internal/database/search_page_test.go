package database_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/require"
)

// Exercise the filters inside the page boundary and verify that the subsequent
// payload fetch returns precisely those versions, including all official metadata.
func TestSearchPagePreservesFiltersAndPayloads(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	epoch := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var fixture []*apiv0.ServerResponse
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("com.example/broad-%02d", i)
		for j := 0; j < 3; j++ {
			status := model.StatusActive
			if i%3 == 0 && j == 1 {
				status = model.StatusDeleted
			}
			msg := fmt.Sprintf("status %d/%d", i, j)
			stamp := epoch.Add(time.Duration(i+j) * time.Hour)
			response, err := db.CreateServer(ctx, nil, &apiv0.ServerJSON{Name: name, Version: fmt.Sprintf("1.0.%d", j), Description: fmt.Sprintf("payload %d/%d", i, j)}, &apiv0.RegistryExtensions{Status: status, StatusChangedAt: stamp, StatusMessage: &msg, PublishedAt: epoch, UpdatedAt: stamp, IsLatest: j == 2})
			require.NoError(t, err)
			fixture = append(fixture, response)
		}
	}
	term := "BROAD"
	version := "1.0.1"
	cutoff := epoch.Add(4 * time.Hour)
	no := false
	for _, deleted := range []bool{false, true} {
		for _, mode := range []string{"all", "version", "not-latest", "updated"} {
			t.Run(fmt.Sprintf("deleted=%t/%s", deleted, mode), func(t *testing.T) {
				f := &database.ServerFilter{SubstringName: &term, IncludeDeleted: &deleted}
				switch mode {
				case "version":
					f.Version = &version
				case "not-latest":
					f.IsLatest = &no
				case "updated":
					f.UpdatedSince = &cutoff
				}
				var want, got []*apiv0.ServerResponse
				for _, r := range fixture {
					if !matchesSearchPageFixture(r, f) {
						continue
					}
					want = append(want, r)
				}
				cursor := ""
				for page := 0; page < 20; page++ {
					rows, next, err := db.ListServers(ctx, nil, f, cursor, 3)
					require.NoError(t, err)
					for _, r := range rows {
						m := r.Meta.Official
						m.PublishedAt = m.PublishedAt.UTC()
						m.UpdatedAt = m.UpdatedAt.UTC()
						m.StatusChangedAt = m.StatusChangedAt.UTC()
					}
					got = append(got, rows...)
					if next == "" {
						break
					}
					require.NotEqual(t, cursor, next)
					cursor = next
				}
				require.Equal(t, want, got)
			})
		}
	}
}

func matchesSearchPageFixture(r *apiv0.ServerResponse, f *database.ServerFilter) bool {
	m := r.Meta.Official
	return (*f.IncludeDeleted || m.Status != model.StatusDeleted) &&
		(f.Version == nil || r.Server.Version == *f.Version) &&
		(f.IsLatest == nil || m.IsLatest == *f.IsLatest) &&
		(f.UpdatedSince == nil || m.UpdatedAt.After(*f.UpdatedSince))
}
