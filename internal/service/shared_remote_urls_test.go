//nolint:testpackage
package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func sharedRemoteServer(name, version, header string) *apiv0.ServerJSON {
	return &apiv0.ServerJSON{
		Schema: model.CurrentSchemaURL, Name: name, Version: version, Description: "Listing for " + name,
		Remotes: []model.Transport{{
			Type: "streamable-http", URL: "https://api.example.com/{organization}/mcp",
			Headers: []model.KeyValueInput{{Name: header}},
			Variables: map[string]model.Input{
				"organization": {Description: "Organization for " + name, IsRequired: true},
			},
		}},
	}
}

func TestSharedRemoteURLsPreserveServerIdentities(t *testing.T) {
	ctx := context.Background()
	svc := NewRegistryService(database.NewTestDB(t), &config.Config{EnableRegistryValidation: true})
	a1 := sharedRemoteServer("io.github.community/example", "1.0.0", "X-Community-Token")
	a2 := sharedRemoteServer(a1.Name, "2.0.0", "X-Community-Token")
	b1 := sharedRemoteServer("com.example/official", "1.0.0", "X-Official-Token")
	b2 := sharedRemoteServer(b1.Name, "2.0.0", "X-Official-Token")

	// Both publishers can add versions after another name uses their endpoint.
	for _, server := range []*apiv0.ServerJSON{a1, b1, a2, b2} {
		_, err := svc.CreateServer(ctx, server)
		require.NoError(t, err)
	}
	_, err := svc.CreateServer(ctx, b2)
	require.ErrorIs(t, err, database.ErrInvalidVersion)

	for _, server := range []*apiv0.ServerJSON{a1, b1, a2, b2} {
		got, err := svc.GetServerByNameAndVersion(ctx, server.Name, server.Version, false)
		require.NoError(t, err)
		assert.Equal(t, *server, got.Server, "metadata must not be merged by remote URL")
	}
	for _, server := range []*apiv0.ServerJSON{a2, b2} {
		latest, err := svc.GetServerByName(ctx, server.Name, false)
		require.NoError(t, err)
		assert.Equal(t, *server, latest.Server)
		assert.True(t, latest.Meta.Official.IsLatest)
		versions, err := svc.GetAllVersionsByServerName(ctx, server.Name, false)
		require.NoError(t, err)
		require.Len(t, versions, 2)
		for _, version := range versions {
			assert.Equal(t, server.Name, version.Server.Name)
		}
	}

	// Latest-only pagination must retain both names even though their URLs match.
	isLatest := true
	filter := &database.ServerFilter{IsLatest: &isLatest}
	first, cursor, err := svc.ListServers(ctx, filter, "", 1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.NotEmpty(t, cursor)
	second, _, err := svc.ListServers(ctx, filter, cursor, 1)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.ElementsMatch(t, []string{a2.Name, b2.Name}, []string{first[0].Server.Name, second[0].Server.Name})

	// The production listing representation must also preserve each configuration.
	listing, err := svc.(*registryServiceImpl).ListServersJSON(ctx, filter, "", 100)
	require.NoError(t, err)
	encoded, err := json.Marshal(listing)
	require.NoError(t, err)
	var decoded apiv0.ServerListResponse
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Len(t, decoded.Servers, 2)
	assert.ElementsMatch(t, []apiv0.ServerJSON{*a2, *b2}, []apiv0.ServerJSON{decoded.Servers[0].Server, decoded.Servers[1].Server})
}

func TestUpdateServerAllowsSharedRemoteURL(t *testing.T) {
	ctx := context.Background()
	svc := NewRegistryService(database.NewTestDB(t), &config.Config{EnableRegistryValidation: true})
	a := sharedRemoteServer("io.github.community/example", "1.0.0", "X-Community-Token")
	b := sharedRemoteServer("com.example/official", "1.0.0", "X-Official-Token")
	b.Remotes[0].URL = "https://api.example.com/old/mcp"
	for _, server := range []*apiv0.ServerJSON{a, b} {
		_, err := svc.CreateServer(ctx, server)
		require.NoError(t, err)
	}

	b.Remotes[0].URL = a.Remotes[0].URL
	b.Description = "Updated official listing"
	updated, err := svc.UpdateServer(ctx, b.Name, b.Version, b, nil)
	require.NoError(t, err)
	assert.Equal(t, *b, updated.Server)
	unchanged, err := svc.GetServerByName(ctx, a.Name, false)
	require.NoError(t, err)
	assert.Equal(t, *a, unchanged.Server)
}

func TestRestoreServerAllowsSharedRemoteURL(t *testing.T) {
	for _, allVersions := range []bool{false, true} {
		for _, status := range []model.Status{model.StatusActive, model.StatusDeprecated} {
			name := "single/" + string(status)
			if allVersions {
				name = "all/" + string(status)
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				svc := NewRegistryService(database.NewTestDB(t), &config.Config{EnableRegistryValidation: true})
				a1 := sharedRemoteServer("io.github.community/example", "1.0.0", "X-Community-Token")
				a2 := sharedRemoteServer(a1.Name, "2.0.0", "X-Community-Token")
				for _, server := range []*apiv0.ServerJSON{a1, a2} {
					_, err := svc.CreateServer(ctx, server)
					require.NoError(t, err)
				}
				_, err := svc.UpdateAllVersionsStatus(ctx, a1.Name, &StatusChangeRequest{NewStatus: model.StatusDeleted})
				require.NoError(t, err)
				b := sharedRemoteServer("com.example/official", "1.0.0", "X-Official-Token")
				_, err = svc.CreateServer(ctx, b)
				require.NoError(t, err)

				change := &StatusChangeRequest{NewStatus: status}
				if allVersions {
					_, err = svc.UpdateAllVersionsStatus(ctx, a1.Name, change)
				} else {
					_, err = svc.UpdateServerStatus(ctx, a2.Name, a2.Version, change)
				}
				require.NoError(t, err)
				latest, err := svc.GetServerByName(ctx, a2.Name, false)
				require.NoError(t, err)
				assert.Equal(t, *a2, latest.Server)
				assert.Equal(t, status, latest.Meta.Official.Status)
				assert.True(t, latest.Meta.Official.IsLatest)
				older, err := svc.GetServerByNameAndVersion(ctx, a1.Name, a1.Version, true)
				require.NoError(t, err)
				if allVersions {
					assert.Equal(t, status, older.Meta.Official.Status)
				} else {
					assert.Equal(t, model.StatusDeleted, older.Meta.Official.Status)
				}
				assert.False(t, older.Meta.Official.IsLatest)
				unchanged, err := svc.GetServerByName(ctx, b.Name, false)
				require.NoError(t, err)
				assert.Equal(t, *b, unchanged.Server)
				assert.Equal(t, model.StatusActive, unchanged.Meta.Official.Status)
				assert.True(t, unchanged.Meta.Official.IsLatest)
			})
		}
	}
}
