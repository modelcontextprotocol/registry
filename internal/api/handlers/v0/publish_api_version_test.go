package v0_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// /v0 and /v0.1 register the same handler against the same service instance, so
// the API version a publish arrived on is only knowable if the handler tags the
// context. This exercises that wiring end to end rather than the service in
// isolation: the publish log has to name the prefix the route was registered under.
func TestPublishLogsAPIVersionForEachRoutePrefix(t *testing.T) {
	for _, prefix := range []string{"/v0", "/v0.1"} {
		t.Run(prefix, func(t *testing.T) {
			var logBuf bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })

			seed := make([]byte, ed25519.SeedSize)
			_, err := rand.Read(seed)
			require.NoError(t, err)
			cfg := &config.Config{
				JWTPrivateKey:            hex.EncodeToString(seed),
				EnableRegistryValidation: false,
			}

			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
			v0.RegisterPublishEndpoint(api, prefix, service.NewRegistryService(database.NewTestDB(t), cfg), cfg)

			token, err := generateTestJWTToken(cfg, auth.JWTClaims{
				AuthMethod:  auth.MethodNone,
				Permissions: []auth.Permission{{Action: auth.PermissionActionPublish, ResourcePattern: "*"}},
			})
			require.NoError(t, err)

			body, err := json.Marshal(apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/api-version-attribution",
				Description: "server used to assert the API version reaches the publish log",
				Version:     "1.0.0",
				Packages: []model.Package{{
					RegistryType: model.RegistryTypeNPM,
					Identifier:   "example-package",
					Version:      "1.0.0",
					Transport:    model.Transport{Type: model.TransportTypeStdio},
				}},
			})
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, prefix+"/publish", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)

			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			require.Equal(t, http.StatusOK, rr.Code, "publish should succeed: %s", rr.Body.String())

			assert.Contains(t, logBuf.String(), "api_version="+prefix,
				"the publish log must attribute the request to the route prefix it arrived on")
		})
	}
}
