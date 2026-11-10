package database_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/require"
)

func TestUpdatedPagePreservesWatermarkPaginationAndPayloads(t *testing.T) {
	db := database.NewTestDB(t)
	fast := db.(*database.PostgreSQL)
	ctx := context.Background()
	epoch := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var fixture []*apiv0.ServerResponse
	for i := 0; i < 8; i++ {
		for j := 0; j < 3; j++ {
			status := model.StatusActive
			if j == 1 {
				status = model.StatusDeleted
			}
			stamp := epoch.Add(time.Duration(i+j) * time.Hour)
			msg := fmt.Sprintf("status %d/%d", i, j)
			r, err := db.CreateServer(ctx, nil, &apiv0.ServerJSON{Name: fmt.Sprintf("com.example/sync-%02d", i), Version: fmt.Sprintf("1.0.%d", j), Description: fmt.Sprintf("payload %d/%d", i, j)}, &apiv0.RegistryExtensions{Status: status, StatusChangedAt: stamp, StatusMessage: &msg, PublishedAt: epoch, UpdatedAt: stamp, IsLatest: j == 2})
			require.NoError(t, err)
			fixture = append(fixture, r)
		}
	}
	version := "1.0.1"
	yes, no := true, false
	for _, age := range []int{-1, 4, 9, 20} {
		for _, mode := range []string{"deleted", "active", "default", "latest", "not-latest", "version"} {
			t.Run(fmt.Sprintf("watermark=%d/%s", age, mode), func(t *testing.T) {
				cutoff := epoch.Add(time.Duration(age) * time.Hour)
				f := &database.ServerFilter{UpdatedSince: &cutoff, IncludeDeleted: &yes}
				switch mode {
				case "active":
					f.IncludeDeleted = &no
				case "default":
					f.IncludeDeleted = nil
				case "latest":
					f.IsLatest = &yes
				case "not-latest":
					f.IsLatest = &no
				case "version":
					f.Version = &version
				}
				var want []*apiv0.ServerResponse
				for _, r := range fixture {
					m := r.Meta.Official
					if !m.UpdatedAt.After(cutoff) || ((f.IncludeDeleted == nil || !*f.IncludeDeleted) && m.Status == model.StatusDeleted) || (f.Version != nil && r.Server.Version != *f.Version) || (f.IsLatest != nil && m.IsLatest != *f.IsLatest) {
						continue
					}
					want = append(want, r)
				}
				for _, limit := range []int{1, 3, 100} {
					var got []*apiv0.ServerResponse
					cursor := ""
					for page := 0; page < 30; page++ {
						rows, next, err := db.ListServers(ctx, nil, f, cursor, limit)
						require.NoError(t, err)
						wire, err := fast.ListServersJSON(ctx, f, cursor, limit)
						require.NoError(t, err)
						encoded, err := json.Marshal(wire)
						require.NoError(t, err)
						var decoded apiv0.ServerListResponse
						require.NoError(t, json.Unmarshal(encoded, &decoded))
						require.Equal(t, next, decoded.Metadata.NextCursor)
						require.Len(t, decoded.Servers, len(rows))
						for i, r := range rows {
							r.Meta.Official.PublishedAt = r.Meta.Official.PublishedAt.UTC()
							r.Meta.Official.UpdatedAt = r.Meta.Official.UpdatedAt.UTC()
							r.Meta.Official.StatusChangedAt = r.Meta.Official.StatusChangedAt.UTC()
							decoded.Servers[i].Meta.Official.PublishedAt = decoded.Servers[i].Meta.Official.PublishedAt.UTC()
							decoded.Servers[i].Meta.Official.UpdatedAt = decoded.Servers[i].Meta.Official.UpdatedAt.UTC()
							decoded.Servers[i].Meta.Official.StatusChangedAt = decoded.Servers[i].Meta.Official.StatusChangedAt.UTC()
							require.Equal(t, *r, decoded.Servers[i])
						}
						got = append(got, rows...)
						if next == "" {
							break
						}
						require.NotEqual(t, cursor, next)
						cursor = next
						require.Less(t, page, 29, "pagination did not terminate")
					}
					require.Equal(t, want, got)
				}
				// Preserve the established name-only fallback for malformed cursors.
				rows, _, err := db.ListServers(ctx, nil, f, "com.example/sync-04", 100)
				require.NoError(t, err)
				var remaining []*apiv0.ServerResponse
				for _, r := range want {
					if r.Server.Name > "com.example/sync-04" {
						remaining = append(remaining, r)
					}
				}
				require.Equal(t, len(remaining), len(rows))
				for i := range rows {
					require.Equal(t, remaining[i].Server, rows[i].Server)
				}
			})
		}
	}
}
