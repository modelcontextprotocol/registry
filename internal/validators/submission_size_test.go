package validators_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestNestedStringByteLimit(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"package identifier", `{"packages":[{"identifier":"%s"}]}`},
		{"package transport URL", `{"packages":[{"transport":{"url":"%s"}}]}`},
		{"package transport variable", `{"packages":[{"transport":{"variables":{"host":{"default":"%s"}}}}]}`},
		{"package transport header", `{"packages":[{"transport":{"headers":[{"value":"%s"}]}}]}`},
		{"runtime argument hint", `{"packages":[{"runtimeArguments":[{"valueHint":"%s"}]}]}`},
		{"package argument name", `{"packages":[{"packageArguments":[{"name":"%s"}]}]}`},
		{"argument variable choice", `{"packages":[{"packageArguments":[{"variables":{"arg":{"choices":["ok","%s"]}}}]}]}`},
		{"environment description", `{"packages":[{"environmentVariables":[{"description":"%s"}]}]}`},
		{"environment variable name", `{"packages":[{"environmentVariables":[{"variables":{"%s":{}}}]}]}`},
		{"remote variable placeholder", `{"remotes":[{"variables":{"host":{"placeholder":"%s"}}}]}`},
		{"remote header name", `{"remotes":[{"headers":[{"name":"%s"}]}]}`},
		{"remote header variable value", `{"remotes":[{"headers":[{"variables":{"key":{"value":"%s"}}}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			field := strings.Repeat("é", 2048)
			for _, suffix := range []string{"", "x"} {
				var req apiv0.ServerJSON
				require.NoError(t, json.Unmarshal([]byte(fmt.Sprintf(tc.body, field+suffix)), &req))
				err := validators.ValidatePublishRequest(t.Context(), req, &config.Config{})
				if suffix == "" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "4097 bytes (limit 4096)")
					require.NotContains(t, err.Error(), field)
				}
			}
		})
	}
}

func TestSerializedServerByteLimit(t *testing.T) {
	// The encoder escapes each ampersand to six bytes. Each choice fits
	// individually, but the complete serialized document must also fit.
	choices := make([]string, 6)
	for i := range choices[:5] {
		choices[i] = strings.Repeat("&", 4096)
	}
	req := apiv0.ServerJSON{Remotes: []model.Transport{{Variables: map[string]model.Input{"opt": {Choices: choices}}}}}
	encoded, err := json.Marshal(req)
	require.NoError(t, err)
	remaining := 128*1024 - len(encoded)
	choices[5] = strings.Repeat("&", remaining/6) + strings.Repeat("x", remaining%6)
	require.NoError(t, validators.ValidatePublishRequest(t.Context(), req, &config.Config{}))
	choices[5] += "x"
	require.ErrorContains(t, validators.ValidatePublishRequest(t.Context(), req, &config.Config{}), "131073 bytes (limit 131072)")
}

func TestSubmissionLimitsBeforeRegistryValidation(t *testing.T) {
	resolver := &fakeHostResolver{}
	validators.SetHostResolverForTest(t, resolver)
	osvURL, queries := malwareOSV(t)
	req := serverWithNPMPackage()
	var paths []string
	recordNPMChecks(t, req.Name, &paths)
	req.Remotes = []model.Transport{{Type: model.TransportTypeStreamableHTTP, URL: "https://remote.example.org"}}

	for _, tc := range []struct {
		size int
		want string
	}{
		{4097, "runtimeHint is 4097 bytes"},
		{131073, "serialized server.json"},
	} {
		req.Packages[0].RunTimeHint = strings.Repeat("x", tc.size)
		for _, enabled := range []bool{false, true} {
			cfg := &config.Config{EnableRegistryValidation: enabled, MalwareCheckMode: config.MalwareCheckEnforce, OSVAPIURL: osvURL}
			require.ErrorContains(t, validators.ValidatePublishRequest(t.Context(), req, cfg), tc.want)
			require.ErrorContains(t, validators.ValidateUpdateRequest(t.Context(), req, cfg, false), tc.want)
			require.ErrorContains(t, validators.ValidateUpdateRequest(t.Context(), req, cfg, true), tc.want)
		}
	}
	require.Zero(t, resolver.calls.Load())
	require.Zero(t, queries.Load())
	require.Empty(t, paths)
}
