package v0_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

func TestPublishSharedRemoteURLPreservesValidation(t *testing.T) {
	for _, prefix := range []string{"/v0", "/v0.1"} {
		t.Run(prefix, func(t *testing.T) {
			seed := make([]byte, ed25519.SeedSize)
			_, err := rand.Read(seed)
			require.NoError(t, err)
			cfg := &config.Config{JWTPrivateKey: hex.EncodeToString(seed), EnableRegistryValidation: true}
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
			v0.RegisterPublishEndpoint(api, prefix, service.NewRegistryService(database.NewTestDB(t), cfg), cfg)

			publisherToken := func(namespace string) string {
				t.Helper()
				token, err := generateTestJWTToken(cfg, auth.JWTClaims{
					AuthMethod: auth.MethodGitHubAT,
					Permissions: []auth.Permission{{
						Action: auth.PermissionActionPublish, ResourcePattern: namespace + "/*",
					}},
				})
				require.NoError(t, err)
				return token
			}
			aToken := publisherToken("io.github.publisher-a")
			bToken := publisherToken("io.github.publisher-b")
			publish := func(server apiv0.ServerJSON, token string) *httptest.ResponseRecorder {
				t.Helper()
				body, err := json.Marshal(server)
				require.NoError(t, err)
				req := httptest.NewRequest(http.MethodPost, prefix+"/publish", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+token)
				rr := httptest.NewRecorder()
				mux.ServeHTTP(rr, req)
				return rr
			}
			a := apiv0.ServerJSON{
				Schema: model.CurrentSchemaURL, Name: "io.github.publisher-a/example", Version: "1.0.0",
				Description: "A remote server listing",
				Remotes:     []model.Transport{{Type: "streamable-http", URL: "https://api.example.com/mcp"}},
			}
			b := a
			b.Name = "io.github.publisher-b/example"
			for _, entry := range []struct {
				server apiv0.ServerJSON
				token  string
			}{{a, aToken}, {b, bToken}} {
				rr := publish(entry.server, entry.token)
				require.Equal(t, http.StatusOK, rr.Code, "%s", rr.Body.String())
				var got apiv0.ServerResponse
				require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
				assert.Equal(t, entry.server, got.Server)
			}

			// A shared endpoint grants no permission over the other publisher's name.
			b.Version = "2.0.0"
			rr := publish(b, aToken)
			require.Equal(t, http.StatusForbidden, rr.Code, "%s", rr.Body.String())
			rr = publish(b, bToken)
			require.Equal(t, http.StatusOK, rr.Code, "%s", rr.Body.String())
			rr = publish(b, bToken)
			require.Equal(t, http.StatusBadRequest, rr.Code, "%s", rr.Body.String())
			assert.Contains(t, rr.Body.String(), "already exists")

			// Relaxing cross-server uniqueness must not relax remote URL validation.
			b.Version = "3.0.0"
			b.Remotes = []model.Transport{{Type: "streamable-http", URL: "http://api.example.com/mcp"}}
			rr = publish(b, bToken)
			require.Equal(t, http.StatusUnprocessableEntity, rr.Code, "%s", rr.Body.String())
		})
	}
}
