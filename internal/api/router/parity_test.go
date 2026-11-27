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

func newParityHarness(t *testing.T, registryValidation bool) (*http.ServeMux, string) {
	t.Helper()

	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(seed),
		EnableRegistryValidation: registryValidation,
	}
	svc := service.NewRegistryService(database.NewTestDB(t), cfg)

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

	return mux, tok.RegistryToken
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
	for _, enabled := range []bool{true, false} {
		mux, token := newParityHarness(t, enabled)
		for _, prefix := range router.APIVersionPrefixes {
			for _, tc := range parityCases() {
				t.Run(fmt.Sprintf("registry-validation=%t%s/%s", enabled, prefix, tc.name), func(t *testing.T) {
					testSubmissionLimit(t, mux, token, prefix, tc, !enabled && tc.registryOnly)
				})
			}
		}
	}
}

func testSubmissionLimit(t *testing.T, mux *http.ServeMux, token, prefix string, tc parityCase, wantAccepted bool) {
	t.Helper()
	baseline := apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/" + strings.TrimPrefix(prefix, "/") + "-" + tc.name,
		Description: "Baseline version",
		Version:     "1.0.0",
	}
	seed := sendJSON(t, mux, http.MethodPost, prefix+"/publish", token, baseline)
	require.Equal(t, http.StatusOK, seed.Code, "%s", seed.Body.String())
	versionsPath := prefix + "/servers/" + url.PathEscape(baseline.Name) + "/versions/"

	edit := baseline
	edit.Description = "Updated description"
	tc.mutate(&edit)
	valRR := sendJSON(t, mux, http.MethodPost, prefix+"/validate", "", edit)
	require.Equal(t, http.StatusOK, valRR.Code)
	var result validators.ValidationResult
	require.NoError(t, json.Unmarshal(valRR.Body.Bytes(), &result))
	require.Equal(t, wantAccepted, result.Valid, "%+v", result.Issues)
	if !wantAccepted {
		require.Len(t, result.Issues, 1)
		require.Equal(t, tc.wantPath, result.Issues[0].Path)
		require.Equal(t, tc.wantReference, result.Issues[0].Reference)
		require.Contains(t, result.Issues[0].Message, tc.wantMessage)
	}

	publish := edit
	publish.Version = "2.0.0"
	pubRR := sendJSON(t, mux, http.MethodPost, prefix+"/publish", token, publish)
	editRR := sendJSON(t, mux, http.MethodPut, versionsPath+edit.Version, token, edit)
	getNew := sendJSON(t, mux, http.MethodGet, versionsPath+publish.Version, "", nil)
	getExisting := sendJSON(t, mux, http.MethodGet, versionsPath+edit.Version, "", nil)
	require.Equal(t, http.StatusOK, getExisting.Code)

	if wantAccepted {
		require.Equal(t, http.StatusOK, pubRR.Code, "%s", pubRR.Body.String())
		require.Equal(t, http.StatusOK, editRR.Code, "%s", editRR.Body.String())
		require.Equal(t, http.StatusOK, getNew.Code)
		for _, rr := range []*httptest.ResponseRecorder{getNew, getExisting} {
			var stored apiv0.ServerResponse
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &stored))
			require.Equal(t, edit.Description, stored.Server.Description)
			require.Equal(t, edit.Packages, stored.Server.Packages)
			require.Equal(t, edit.Remotes, stored.Server.Remotes)
		}
		return
	}

	require.Equal(t, http.StatusBadRequest, pubRR.Code, "%s", pubRR.Body.String())
	require.Contains(t, pubRR.Body.String(), tc.wantMessage)
	require.Equal(t, http.StatusBadRequest, editRR.Code, "%s", editRR.Body.String())
	require.Contains(t, editRR.Body.String(), tc.wantMessage)
	require.Equal(t, http.StatusNotFound, getNew.Code)
	require.Contains(t, getExisting.Body.String(), baseline.Description)
}
