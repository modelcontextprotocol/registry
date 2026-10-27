package database_test

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/require"
)

func TestSearchNamespaceSuffixPreservesLiteralSubstringAndPagination(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	names := []string{"io.github.example/tool", "org.io.github.example/tool", "io.github.example/tool_extra", "io.github.other/tool", "com.example/100_percent", "com.example/100Xpercent"}
	type row struct {
		name, version string
		deleted       bool
	}
	var fixture []row
	for i, name := range names {
		for _, version := range []string{"1.0.0", "1.0.1"} {
			deleted := i == 1 && version == "1.0.0"
			status := model.StatusActive
			if deleted {
				status = model.StatusDeleted
			}
			now := time.Now()
			_, err := db.CreateServer(ctx, nil, &apiv0.ServerJSON{Name: name, Version: version, Description: "Search equivalence fixture"}, &apiv0.RegistryExtensions{Status: status, PublishedAt: now, UpdatedAt: now, StatusChangedAt: now, IsLatest: version == "1.0.1"})
			require.NoError(t, err)
			fixture = append(fixture, row{name, version, deleted})
		}
	}
	for _, term := range []string{"github.example/tool", "Io.GiThUb.Example/TOOL", "github.example/", "le/tool", "/tool", "tool", "example/100_", "example/100%", `example/100\`, "example/tool/extra", "例子/工具"} {
		for _, includeDeleted := range []bool{false, true} {
			for _, latestOnly := range []bool{false, true} {
				t.Run(term+"/"+strings.Join([]string{boolLabel(includeDeleted), boolLabel(latestOnly)}, "/"), func(t *testing.T) {
					filter := &database.ServerFilter{SubstringName: &term, IncludeDeleted: &includeDeleted}
					if latestOnly {
						filter.IsLatest = &latestOnly
					}
					var want, got []string
					for _, r := range fixture {
						if strings.Contains(strings.ToLower(r.name), strings.ToLower(term)) && (includeDeleted || !r.deleted) && (!latestOnly || r.version == "1.0.1") {
							want = append(want, r.name+":"+r.version)
						}
					}
					sort.Strings(want)
					cursor := ""
					for page := 0; page < 20; page++ {
						rows, next, err := db.ListServers(ctx, nil, filter, cursor, 2)
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
					require.Equal(t, want, got)
				})
			}
		}
	}
}

func boolLabel(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
