package v0_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/require"
)

type searchRecordingService struct {
	service.RegistryService
	calls  int
	search *string
}

func (s *searchRecordingService) ListServers(_ context.Context, filter *database.ServerFilter, _ string, _ int) ([]*apiv0.ServerResponse, string, error) {
	s.calls++
	s.search = filter.SubstringName
	return []*apiv0.ServerResponse{}, "", nil
}

func TestSearchMinimumBeforeService(t *testing.T) {
	for _, prefix := range []string{"/v0", "/v0.1"} {
		for _, tc := range []struct {
			search string
			status int
		}{
			{"", http.StatusOK}, {"a", http.StatusUnprocessableEntity}, {"ai", http.StatusUnprocessableEntity},
			{"  a  ", http.StatusUnprocessableEntity}, {" \t ", http.StatusUnprocessableEntity},
			{"测试", http.StatusUnprocessableEntity}, {"测试名", http.StatusOK},
			{"mcp", http.StatusOK}, {" abc ", http.StatusOK}, {"a.b", http.StatusOK},
		} {
			t.Run(prefix+"/"+tc.search, func(t *testing.T) {
				s := &searchRecordingService{}
				mux := http.NewServeMux()
				api := humago.New(mux, huma.DefaultConfig("search minimum", "1.0.0"))
				v0.RegisterServersEndpoints(api, prefix, s)
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, prefix+"/servers?search="+url.QueryEscape(tc.search), nil))
				require.Equal(t, tc.status, w.Code, w.Body.String())
				if tc.status != http.StatusOK {
					require.Zero(t, s.calls, "invalid search must not reach the service/database")
					require.Contains(t, w.Body.String(), "at least 3 characters")
				} else {
					require.Equal(t, 1, s.calls)
					if tc.search == "" {
						require.Nil(t, s.search)
					} else {
						require.Equal(t, tc.search, *s.search)
					}
				}
			})
		}
	}
}
