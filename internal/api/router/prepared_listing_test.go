package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/modelcontextprotocol/registry/internal/api/router"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/require"
)

type listingSwitchService struct {
	service.RegistryService
	typed, prepared int
}

func (s *listingSwitchService) ListServers(context.Context, *database.ServerFilter, string, int) ([]*apiv0.ServerResponse, string, error) {
	s.typed++
	return []*apiv0.ServerResponse{}, "", nil
}
func (s *listingSwitchService) ListServersJSON(context.Context, *database.ServerFilter, string, int) (apiv0.ServerListResponse, error) {
	s.prepared++
	return apiv0.NewEncodedServerList([]apiv0.EncodedServerRecord{}, apiv0.Metadata{}), nil
}
func TestPreparedListingActivationAndRollback(t *testing.T) {
	for _, enabled := range []string{"false", "true", "false"} {
		t.Setenv("MCP_REGISTRY_PREPARED_LISTING_JSON", enabled)
		cfg := config.NewConfig()
		svc := &listingSwitchService{}
		mux := http.NewServeMux()
		hc := huma.DefaultConfig("test", "1")
		hc.CreateHooks = []func(huma.Config) huma.Config{}
		api := humago.New(mux, hc)
		router.RegisterV0Routes(api, cfg, svc, nil, nil)
		router.RegisterV0_1Routes(api, cfg, svc, nil, nil)
		for _, path := range []string{"/v0/servers", "/v0.1/servers"} {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusOK, w.Code)
			require.JSONEq(t, `{"servers":[],"metadata":{"count":0}}`, w.Body.String())
		}
		if enabled == "true" {
			require.Equal(t, 2, svc.prepared)
			require.Zero(t, svc.typed)
		} else {
			require.Equal(t, 2, svc.typed)
			require.Zero(t, svc.prepared)
		}
	}
}
