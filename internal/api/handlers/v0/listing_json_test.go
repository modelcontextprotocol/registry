package v0_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/require"
)

func TestEncodedListingMatchesTypedAPIThroughUpdates(t *testing.T) {
	db := database.NewTestDB(t)
	ctx := context.Background()
	registry := service.NewRegistryService(db, config.NewConfig())
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	server := apiv0.ServerJSON{Schema: model.CurrentSchemaURL, Name: "example/alpha", Version: "1", Description: "original <>&", Meta: &apiv0.ServerMeta{PublisherProvided: map[string]interface{}{"number": 123.5}}}
	meta := apiv0.RegistryExtensions{Status: model.StatusActive, PublishedAt: stamp, UpdatedAt: stamp, StatusChangedAt: stamp, IsLatest: true}
	_, err := db.CreateServer(ctx, nil, &server, &meta)
	require.NoError(t, err)
	second := server
	second.Name = "example/beta"
	_, err = db.CreateServer(ctx, nil, &second, &meta)
	require.NoError(t, err)
	muxes := []*http.ServeMux{http.NewServeMux(), http.NewServeMux()}
	var schemas [][]byte
	for i, mux := range muxes {
		cfg := huma.DefaultConfig("test", "1")
		cfg.CreateHooks = []func(huma.Config) huma.Config{}
		api := humago.New(mux, cfg)
		v0.RegisterServersEndpoints(api, "/v0", registry, i == 1)
		v0.RegisterServersEndpoints(api, "/v0.1", registry, i == 1)
		raw, err := json.Marshal(api.OpenAPI())
		require.NoError(t, err)
		schemas = append(schemas, raw)
	}
	require.JSONEq(t, string(schemas[0]), string(schemas[1]), "optimization must not change OpenAPI")
	check := func() {
		t.Helper()
		for _, path := range []string{"/v0/servers", "/v0.1/servers?limit=1", "/v0.1/servers?cursor=example/alpha:1&limit=1", "/v0.1/servers?search=alpha", "/v0.1/servers?search=absent", "/v0.1/servers?version=latest", "/v0.1/servers?include_deleted=true", "/v0.1/servers?updated_since=2025-01-01T00:00:00Z"} {
			var bodies []string
			for _, mux := range muxes {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				require.Equal(t, http.StatusOK, w.Code, path)
				bodies = append(bodies, w.Body.String())
			}
			require.JSONEq(t, bodies[0], bodies[1], path)
		}
	}
	check()
	check() // Repeated reads must preserve fresh metadata.
	server.Description = "updated payload"
	_, err = db.UpdateServer(ctx, nil, server.Name, server.Version, &server)
	require.NoError(t, err)
	check()
	_, err = db.SetServerStatus(ctx, nil, server.Name, server.Version, model.StatusDeleted, nil)
	require.NoError(t, err)
	check()
	third := second
	third.Name = "example/new"
	_, err = db.CreateServer(ctx, nil, &third, &meta)
	require.NoError(t, err)
	check()
}
