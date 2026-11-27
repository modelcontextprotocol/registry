package router_test

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
	"net/url"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/require"

	"github.com/modelcontextprotocol/registry/internal/api/router"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

type parityCase struct {
	name          string
	registryOnly  bool
	wantPath      string
	wantReference string
	wantMessage   string
	mutate        func(*apiv0.ServerJSON)
}

func parityCases() []parityCase {
	return []parityCase{
		{
			name:          "nested-string-limit",
			wantPath:      "packages[0].runtimeHint",
			wantReference: "submission-size",
			wantMessage:   "4097 bytes (limit 4096)",
			mutate: func(s *apiv0.ServerJSON) {
				s.Packages = []model.Package{{
					RegistryType: model.RegistryTypeNPM,
					Identifier:   "sample",
					Version:      "1.0.0",
					Transport:    model.Transport{Type: model.TransportTypeStdio},
					RunTimeHint:  strings.Repeat("x", 4097),
				}}
			},
		},
		{
			name:          "publisher-metadata-limit",
			wantPath:      `_meta["io.modelcontextprotocol.registry/publisher-provided"]`,
			wantReference: "submission-size",
			wantMessage:   "exceeds 4KB limit",
			mutate: func(s *apiv0.ServerJSON) {
				s.Meta = &apiv0.ServerMeta{
					PublisherProvided: map[string]any{"k": strings.Repeat("x", 4097)},
				}
			},
		},
		{
			name:          "serialized-record-limit",
			wantPath:      "$",
			wantReference: "submission-size",
			wantMessage:   "limit 131072",
			mutate: func(s *apiv0.ServerJSON) {
				var headers []model.KeyValueInput
				for i := range 6 {
					kv := model.KeyValueInput{Name: fmt.Sprintf("X-Large-%d", i)}
					kv.Description = strings.Repeat("&", 4096)
					headers = append(headers, kv)
				}
				s.Remotes = []model.Transport{{
					Type:    model.TransportTypeStreamableHTTP,
					URL:     "https://example.org/mcp",
					Headers: headers,
				}}
			},
		},
		{
			name:          "distinct-package-ownership-limit",
			registryOnly:  true,
			wantPath:      "packages[16]",
			wantReference: "package-ownership-limit",
			wantMessage:   "exceeds the limit of 16 distinct package ownership checks",
			mutate: func(s *apiv0.ServerJSON) {
				for i := range 17 {
					s.Packages = append(s.Packages, model.Package{
						RegistryType: model.RegistryTypeNPM,
						Identifier:   fmt.Sprintf("pkg-%d", i),
						Version:      "1.0.0",
						Transport:    model.Transport{Type: model.TransportTypeStdio},
					})
				}
			},
		},
		{
			name:          "remote-host-lookup-limit",
			registryOnly:  true,
			wantPath:      "remotes[32].url",
			wantReference: "remote-host-lookup-limit",
			wantMessage:   "more than 32 unique remote hostnames",
			mutate: func(s *apiv0.ServerJSON) {
				for i := range 33 {
					s.Remotes = append(s.Remotes, model.Transport{
						Type: model.TransportTypeStreamableHTTP,
						URL:  fmt.Sprintf("https://host-%d.example.org/mcp", i),
					})
				}
			},
		},
	}
}

func newParityHarness(t *testing.T) (*http.ServeMux, *config.Config, string) {
	t.Helper()

	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(seed),
		EnableRegistryValidation: false,
	}
	svc := service.NewRegistryService(database.NewTestDB(t), cfg)

	baseline := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/parity-server",
		Description: "Baseline version",
		Version:     "1.0.0",
	}
	_, err = svc.CreateServer(context.Background(), baseline)
	require.NoError(t, err)

	cfg.EnableRegistryValidation = true

	mux := http.NewServeMux()
	humaCfg := huma.DefaultConfig("Parity Test API", "1.0.0")
	humaCfg.CreateHooks = []func(huma.Config) huma.Config{}
	api := humago.New(mux, humaCfg)
	router.RegisterRoutes(api, cfg, svc, nil, nil)

	jwtMgr := auth.NewJWTManager(cfg)
	tok, err := jwtMgr.GenerateTokenResponse(context.Background(), auth.JWTClaims{
		AuthMethod: auth.MethodNone,
		Permissions: []auth.Permission{
			{Action: auth.PermissionActionPublish, ResourcePattern: "*"},
			{Action: auth.PermissionActionEdit, ResourcePattern: "*"},
		},
	})
	require.NoError(t, err)

	return mux, cfg, tok.RegistryToken
}

func sendJSON(t *testing.T, mux *http.ServeMux, method, path, token string, payload any) *httptest.ResponseRecorder {
	t.Helper()

	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestSubmissionLimitsParityAcrossValidatePublishAndEdit(t *testing.T) {
	mux, cfg, token := newParityHarness(t)
	escapedName := url.PathEscape("com.example/parity-server")

	for _, prefix := range router.APIVersionPrefixes {
		for _, tc := range parityCases() {
			t.Run(prefix+"/"+tc.name, func(t *testing.T) {
				base := apiv0.ServerJSON{
					Schema:      model.CurrentSchemaURL,
					Name:        "com.example/parity-server",
					Description: "Updated description",
					Version:     "1.0.0",
				}
				tc.mutate(&base)

				// 1. /validate reports the structured issue offline.
				valRR := sendJSON(t, mux, http.MethodPost, prefix+"/validate", "", base)
				require.Equal(t, http.StatusOK, valRR.Code)
				var valRes validators.ValidationResult
				require.NoError(t, json.Unmarshal(valRR.Body.Bytes(), &valRes))
				require.False(t, valRes.Valid, "%+v", valRes.Issues)
				require.Len(t, valRes.Issues, 1, "%+v", valRes.Issues)
				require.Equal(t, tc.wantPath, valRes.Issues[0].Path)
				require.Equal(t, tc.wantReference, valRes.Issues[0].Reference)
				require.Contains(t, valRes.Issues[0].Message, tc.wantMessage)

				// 2. /publish rejects with 400 and persists nothing.
				pubReq := base
				pubReq.Version = "2.0.0"
				pubRR := sendJSON(t, mux, http.MethodPost, prefix+"/publish", token, pubReq)
				require.Equal(t, http.StatusBadRequest, pubRR.Code, "%s", pubRR.Body.String())
				require.Contains(t, pubRR.Body.String(), tc.wantMessage)
				getNew := sendJSON(t, mux, http.MethodGet, prefix+"/servers/"+escapedName+"/versions/2.0.0", "", nil)
				require.Equal(t, http.StatusNotFound, getNew.Code)

				// 3. Edit rejects with 400 and preserves the stored version.
				editRR := sendJSON(t, mux, http.MethodPut, prefix+"/servers/"+escapedName+"/versions/1.0.0", token, base)
				require.Equal(t, http.StatusBadRequest, editRR.Code, "%s", editRR.Body.String())
				require.Contains(t, editRR.Body.String(), tc.wantMessage)
				getExisting := sendJSON(t, mux, http.MethodGet, prefix+"/servers/"+escapedName+"/versions/1.0.0", "", nil)
				require.Equal(t, http.StatusOK, getExisting.Code)
				require.Contains(t, getExisting.Body.String(), "Baseline version")
			})
		}

		t.Run(prefix+"/registry-validation-disabled", func(t *testing.T) {
			cfg.EnableRegistryValidation = false
			t.Cleanup(func() { cfg.EnableRegistryValidation = true })

			for _, tc := range parityCases() {
				req := apiv0.ServerJSON{
					Schema:      model.CurrentSchemaURL,
					Name:        "com.example/parity-server",
					Description: "Self-hosted check",
					Version:     "1.0.0",
				}
				tc.mutate(&req)

				valRR := sendJSON(t, mux, http.MethodPost, prefix+"/validate", "", req)
				require.Equal(t, http.StatusOK, valRR.Code)
				var valRes validators.ValidationResult
				require.NoError(t, json.Unmarshal(valRR.Body.Bytes(), &valRes))
				require.Equal(t, tc.registryOnly, valRes.Valid, "%s: %+v", tc.name, valRes.Issues)
			}
		})
	}
}
