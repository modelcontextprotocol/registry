package v0_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/require"
)

// Use raw JSON: marshaling a Go string with omitempty would hide the difference
// between a missing value and an explicitly empty CLI argument.
func TestPublishEndpoint_ArgumentPresence(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	require.NoError(t, err)
	cfg := &config.Config{JWTPrivateKey: hex.EncodeToString(seed)}
	token, err := generateTestJWTToken(cfg, auth.JWTClaims{
		AuthMethod: auth.MethodNone,
		Permissions: []auth.Permission{
			{Action: auth.PermissionActionPublish, ResourcePattern: "example/*"},
		},
	})
	require.NoError(t, err)

	for _, prefix := range []string{"/v0", "/v0.1"} {
		for _, field := range []string{"runtimeArguments", "packageArguments"} {
			t.Run(prefix+"/"+field, func(t *testing.T) {
				registry := service.NewRegistryService(database.NewTestDB(t), cfg)
				mux := http.NewServeMux()
				api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
				v0.RegisterPublishEndpoint(api, prefix, registry, cfg)

				for i, tc := range []struct {
					name   string
					arg    string
					status int
				}{
					{"empty value", `{"type":"positional","value":""}`, http.StatusOK},
					{"empty hint", `{"type":"positional","valueHint":""}`, http.StatusOK},
					{"both empty", `{"type":"positional","value":"","valueHint":""}`, http.StatusOK},
					{"value", `{"type":"positional","value":"file.txt"}`, http.StatusOK},
					{"hint", `{"type":"positional","valueHint":"file"}`, http.StatusOK},
					{"named", `{"type":"named","name":"--file","value":""}`, http.StatusOK},
					{"missing", `{"type":"positional"}`, http.StatusUnprocessableEntity},
					{"null", `{"type":"positional","value":null}`, http.StatusUnprocessableEntity},
					{"invalid type", `{"type":"literal","value":"file.txt"}`, http.StatusUnprocessableEntity},
				} {
					t.Run(tc.name, func(t *testing.T) {
						name := fmt.Sprintf("example/argument-%d", i)
						body := fmt.Sprintf(`{"$schema":%q,"name":%q,"description":"Argument presence regression test","version":"1.0.0","packages":[{"registryType":"npm","identifier":"test-package","version":"1.0.0","transport":{"type":"stdio"},%q:[%s]}]}`, model.CurrentSchemaURL, name, field, tc.arg)
						req := httptest.NewRequest(http.MethodPost, prefix+"/publish", bytes.NewBufferString(body))
						req.Header.Set("Content-Type", "application/json")
						req.Header.Set("Authorization", "Bearer "+token)
						rr := httptest.NewRecorder()
						mux.ServeHTTP(rr, req)
						require.Equal(t, tc.status, rr.Code, rr.Body.String())
						if tc.status != http.StatusOK {
							return
						}

						// Read through the service again to exercise JSONB storage decoding,
						// then check that the value survives response serialization too.
						stored, err := registry.GetServerByNameAndVersion(context.Background(), name, "1.0.0", false)
						require.NoError(t, err)
						args := stored.Server.Packages[0].RuntimeArguments
						if field == "packageArguments" {
							args = stored.Server.Packages[0].PackageArguments
						}
						require.Len(t, args, 1)
						encoded, err := json.Marshal(args[0])
						require.NoError(t, err)
						require.JSONEq(t, tc.arg, string(encoded))
						require.Contains(t, rr.Body.String(), string(encoded))
					})
				}
			})
		}
	}
}
