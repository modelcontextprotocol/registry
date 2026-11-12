package commands_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/modelcontextprotocol/registry/cmd/publisher/commands"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	stubServerName = "io.github.example/server"
	stubPackage    = "example-pkg"
	stubVersion    = "1.0.0"

	npmVersionURL = model.RegistryURLNPM + "/" + stubPackage + "/" + stubVersion
	npmPackageURL = model.RegistryURLNPM + "/" + stubPackage

	pypiVersionURL = model.RegistryURLPyPI + "/pypi/" + stubPackage + "/" + stubVersion + "/json"
	pypiPackageURL = model.RegistryURLPyPI + "/pypi/" + stubPackage + "/json"

	nugetContentURL = "https://nuget.stub/flat/"
	nugetReadmeURL  = nugetContentURL + stubPackage + "/" + stubVersion + "/readme"
	nugetIndexURL   = nugetContentURL + stubPackage + "/index.json"

	cargoMetaURL    = model.RegistryURLCrates + "/api/v1/crates/" + stubPackage + "/" + stubVersion + "/readme"
	cargoVersionURL = model.RegistryURLCrates + "/api/v1/crates/" + stubPackage + "/" + stubVersion
	cargoReadmeURL  = "https://static.crates.io/readmes/" + stubPackage + "/" + stubPackage + "-" + stubVersion + ".html"

	mcpbURL = "https://github.com/example/mcp-server/releases/download/v1.0.0/server.mcpb"

	ociPingURL     = "https://index.docker.io/v2/"
	ociManifestURL = "https://index.docker.io/v2/example/server/manifests/1.0.0"
)

type stubResponse struct {
	status int
	body   string
}

// stubUpstream answers the validators' outbound calls, keyed by "METHOD url", so
// the real validators run without touching the network.
type stubUpstream map[string]stubResponse

func (s stubUpstream) RoundTrip(req *http.Request) (*http.Response, error) {
	key := req.Method + " " + req.URL.String()
	stub, ok := s[key]
	if !ok {
		return nil, fmt.Errorf("unexpected upstream request: %s", key)
	}
	return &http.Response{
		StatusCode: stub.status,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(stub.body)),
		Request:    req,
	}, nil
}

func installStubUpstream(t *testing.T, upstream stubUpstream) {
	t.Helper()
	originalDefault, originalOCI := http.DefaultTransport, remote.DefaultTransport
	http.DefaultTransport, remote.DefaultTransport = upstream, upstream
	t.Cleanup(func() {
		http.DefaultTransport, remote.DefaultTransport = originalDefault, originalOCI
	})
}

// realPublishBody runs the real validators and wraps the failure the way the
// publish handler does (internal/api/handlers/v0/publish.go).
func realPublishBody(t *testing.T, pkg model.Package, upstream stubUpstream) []byte {
	t.Helper()
	installStubUpstream(t, upstream)

	err := validators.ValidatePublishRequest(context.Background(), apiv0.ServerJSON{
		Name:     stubServerName,
		Packages: []model.Package{pkg},
	}, &config.Config{EnableRegistryValidation: true})
	require.Error(t, err, "the stubbed upstream should make validation fail")

	body, err := json.Marshal(huma.Error400BadRequest("Failed to publish server", err))
	require.NoError(t, err)
	return body
}

func npmPkg() model.Package {
	return model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: model.RegistryURLNPM, Identifier: stubPackage, Version: stubVersion}
}

func pypiPkg() model.Package {
	return model.Package{RegistryType: model.RegistryTypePyPI, RegistryBaseURL: model.RegistryURLPyPI, Identifier: stubPackage, Version: stubVersion}
}

func nugetPkg() model.Package {
	return model.Package{RegistryType: model.RegistryTypeNuGet, RegistryBaseURL: model.RegistryURLNuGet, Identifier: stubPackage, Version: stubVersion}
}

func cargoPkg() model.Package {
	return model.Package{RegistryType: model.RegistryTypeCargo, RegistryBaseURL: model.RegistryURLCrates, Identifier: stubPackage, Version: stubVersion}
}

func mcpbPkg() model.Package {
	return model.Package{RegistryType: model.RegistryTypeMCPB, Identifier: mcpbURL, FileSHA256: strings.Repeat("a", 64)}
}

func ociPkg() model.Package {
	return model.Package{RegistryType: model.RegistryTypeOCI, Identifier: "docker.io/example/server:1.0.0"}
}

func withSHA(pkg model.Package) model.Package {
	pkg.FileSHA256 = strings.Repeat("a", 64)
	return pkg
}

func withBaseURL(pkg model.Package, baseURL string) model.Package {
	pkg.RegistryBaseURL = baseURL
	return pkg
}

func nugetUpstream(extra stubUpstream) stubUpstream {
	upstream := stubUpstream{
		"GET " + model.RegistryURLNuGet: {http.StatusOK, `{"resources":[` +
			`{"@id":"` + nugetContentURL + `{lower_id}/{lower_version}/readme","@type":"ReadmeUriTemplate/6.13.0"},` +
			`{"@id":"` + nugetContentURL + `","@type":"PackageBaseAddress/3.0.0"}]}`},
	}
	for key, value := range extra {
		upstream[key] = value
	}
	return upstream
}

func cargoReadme403(probeStatus int) stubUpstream {
	return stubUpstream{
		"GET " + cargoMetaURL:    {http.StatusOK, `{"url":"` + cargoReadmeURL + `"}`},
		"GET " + cargoReadmeURL:  {http.StatusForbidden, ""},
		"GET " + cargoVersionURL: {probeStatus, ""},
	}
}

func cargoReadmeStatus(status int) stubUpstream {
	return stubUpstream{
		"GET " + cargoMetaURL:   {http.StatusOK, `{"url":"` + cargoReadmeURL + `"}`},
		"GET " + cargoReadmeURL: {status, ""},
	}
}

func ociManifest(status int) stubUpstream {
	return stubUpstream{
		"GET " + ociPingURL:     {http.StatusOK, ""},
		"GET " + ociManifestURL: {status, ""},
	}
}

// validatorCases drives every validator outcome the publisher has to classify.
// want pins the wording, retry the decision, so rewording a validator fails here.
var validatorCases = []struct {
	name     string
	pkg      model.Package
	upstream stubUpstream
	want     string
	retry    bool
}{
	// npm (internal/validators/registries/npm.go)
	{"npm version not visible", npmPkg(), stubUpstream{"GET " + npmVersionURL: {404, ""}, "HEAD " + npmPackageURL: {200, ""}}, "exists, but version '1.0.0' was not found (status: 404)", true},
	{"npm package not visible", npmPkg(), stubUpstream{"GET " + npmVersionURL: {404, ""}, "HEAD " + npmPackageURL: {404, ""}}, "NPM package 'example-pkg' not found (status: 404)", true},
	{"npm probe inconclusive", npmPkg(), stubUpstream{"GET " + npmVersionURL: {404, ""}, "HEAD " + npmPackageURL: {503, ""}}, "package check inconclusive). Likely transient, retry later", true},
	{"npm probe unclassified", npmPkg(), stubUpstream{"GET " + npmVersionURL: {404, ""}, "HEAD " + npmPackageURL: {403, ""}}, "version '1.0.0' not found (status: 404)", true},
	{"npm rate limited", npmPkg(), stubUpstream{"GET " + npmVersionURL: {429, ""}}, "(status: 429). Likely transient, retry later", true},
	{"npm upstream error", npmPkg(), stubUpstream{"GET " + npmVersionURL: {502, ""}}, "(status: 502). Likely transient, retry later", true},
	{"npm fetch refused", npmPkg(), stubUpstream{"GET " + npmVersionURL: {403, ""}}, "metadata fetch failed (status: 403)", false},
	{"npm missing mcpName", npmPkg(), stubUpstream{"GET " + npmVersionURL: {200, `{}`}}, "is missing required 'mcpName' field", false},
	{"npm mismatched mcpName", npmPkg(), stubUpstream{"GET " + npmVersionURL: {200, `{"mcpName":"io.github.other/server"}`}}, "NPM package ownership validation failed", false},
	{"npm fileSha256", withSHA(npmPkg()), stubUpstream{}, "must not have 'fileSha256' field", false},
	{"npm base URL mismatch", withBaseURL(npmPkg(), "https://npm.example.com"), stubUpstream{}, "registry type and base URL do not match", false},

	// PyPI (pypi.go)
	{"pypi version not visible", pypiPkg(), stubUpstream{"GET " + pypiVersionURL: {404, ""}, "HEAD " + pypiPackageURL: {200, ""}}, "exists, but version '1.0.0' was not found (status: 404)", true},
	{"pypi package not visible", pypiPkg(), stubUpstream{"GET " + pypiVersionURL: {404, ""}, "HEAD " + pypiPackageURL: {404, ""}}, "PyPI package 'example-pkg' not found (status: 404)", true},
	{"pypi probe inconclusive", pypiPkg(), stubUpstream{"GET " + pypiVersionURL: {404, ""}, "HEAD " + pypiPackageURL: {503, ""}}, "package check inconclusive). Likely transient, retry later", true},
	{"pypi probe unclassified", pypiPkg(), stubUpstream{"GET " + pypiVersionURL: {404, ""}, "HEAD " + pypiPackageURL: {403, ""}}, "version '1.0.0' not found (status: 404)", true},
	{"pypi rate limited", pypiPkg(), stubUpstream{"GET " + pypiVersionURL: {429, ""}}, "(status: 429). Likely transient, retry later", true},
	{"pypi upstream error", pypiPkg(), stubUpstream{"GET " + pypiVersionURL: {502, ""}}, "(status: 502). Likely transient, retry later", true},
	{"pypi fetch refused", pypiPkg(), stubUpstream{"GET " + pypiVersionURL: {403, ""}}, "metadata fetch failed (status: 403)", false},
	{"pypi missing token", pypiPkg(), stubUpstream{"GET " + pypiVersionURL: {200, `{"info":{"description":"no token"}}`}}, "ownership validation failed. The server name", false},
	{"pypi fileSha256", withSHA(pypiPkg()), stubUpstream{}, "must not have 'fileSha256' field", false},
	{"pypi base URL mismatch", withBaseURL(pypiPkg(), "https://pypi.example.com"), stubUpstream{}, "registry type and base URL do not match", false},

	// NuGet (nuget.go)
	{"nuget package not visible", nugetPkg(), nugetUpstream(stubUpstream{"GET " + nugetReadmeURL: {404, ""}, "GET " + nugetIndexURL: {404, ""}}), "does not exist in the registry. If you recently published the package", true},
	{"nuget version not visible", nugetPkg(), nugetUpstream(stubUpstream{"GET " + nugetReadmeURL: {404, ""}, "GET " + nugetIndexURL: {200, `{"versions":["0.9.0"]}`}}), "exists but version 1.0.0 does not exist in the registry", true},
	{"nuget no embedded README", nugetPkg(), nugetUpstream(stubUpstream{"GET " + nugetReadmeURL: {404, ""}, "GET " + nugetIndexURL: {200, `{"versions":["1.0.0"]}`}}), "does not have an embedded README", false},
	{"nuget missing token", nugetPkg(), nugetUpstream(stubUpstream{"GET " + nugetReadmeURL: {200, "no token"}}), "ownership validation for version 1.0.0 failed", false},
	{"nuget fileSha256", withSHA(nugetPkg()), stubUpstream{}, "must not have 'fileSha256' field", false},
	{"nuget base URL mismatch", withBaseURL(nugetPkg(), "https://nuget.example.com"), stubUpstream{}, "registry type and base URL do not match", false},

	// cargo (cargo.go)
	{"cargo README not rendered", cargoPkg(), cargoReadme403(200), "its rendered README could not be retrieved (status: 403)", true},
	{"cargo version not visible", cargoPkg(), cargoReadme403(404), "version '1.0.0' not found on crates.io", true},
	{"cargo probe inconclusive", cargoPkg(), cargoReadme403(503), "version check inconclusive) — likely transient, retry later", true},
	{"cargo probe unclassified", cargoPkg(), cargoReadme403(403), "not found on crates.io (status: 403)", true},
	{"cargo metadata rate limited", cargoPkg(), stubUpstream{"GET " + cargoMetaURL: {429, ""}}, "rate-limited the metadata request", true},
	{"cargo metadata upstream error", cargoPkg(), stubUpstream{"GET " + cargoMetaURL: {502, ""}}, "upstream error fetching metadata", true},
	{"cargo README rate limited", cargoPkg(), cargoReadmeStatus(429), "rate-limited the README fetch", true},
	{"cargo README upstream error", cargoPkg(), cargoReadmeStatus(502), "upstream error fetching README", true},
	{"cargo metadata 404", cargoPkg(), stubUpstream{"GET " + cargoMetaURL: {404, ""}}, "metadata fetch failed (status: 404)", false},
	{"cargo missing token", cargoPkg(), cargoReadmeStatus(200), "ownership validation failed. The server name", false},
	{"cargo fileSha256", withSHA(cargoPkg()), stubUpstream{}, "must not have 'fileSha256' field", false},
	{"cargo base URL mismatch", withBaseURL(cargoPkg(), "https://crates.example.com"), stubUpstream{}, "registry type and base URL do not match", false},

	// MCPB (mcpb.go)
	{"mcpb asset not visible", mcpbPkg(), stubUpstream{"HEAD " + mcpbURL: {404, ""}}, "is not publicly accessible (status: 404)", true},
	{"mcpb asset private", mcpbPkg(), stubUpstream{"HEAD " + mcpbURL: {403, ""}}, "is not publicly accessible (status: 403)", false},

	// OCI (oci.go)
	{"oci image not visible", ociPkg(), ociManifest(404), "does not exist in the registry", true},
	{"oci rate limited", ociPkg(), ociManifest(429), "please retry shortly", true},
	{"oci image private", ociPkg(), ociManifest(401), "is private or requires authentication", false},
	{"oci unsupported registry", model.Package{RegistryType: model.RegistryTypeOCI, Identifier: "oci.example.com/example/server:1.0.0"}, stubUpstream{}, "unsupported OCI registry", false},

	// package.go
	{"unsupported registry type", model.Package{RegistryType: "bogus", Identifier: stubPackage}, stubUpstream{}, "unsupported registry type: bogus", false},
}

func TestWaitClassifierMatchesRealValidatorOutput(t *testing.T) {
	phraseHits := map[string]int{}

	for _, tt := range validatorCases {
		t.Run(tt.name, func(t *testing.T) {
			body := realPublishBody(t, tt.pkg, tt.upstream)
			require.Contains(t, string(body), jsonEscaped(t, tt.want), "validator wording changed")
			require.Contains(t, string(body), "registry validation failed for package 0", "validator wrapper changed")

			message, retry := commands.ClassifyResponse(http.StatusBadRequest, body)
			assert.Equal(t, tt.retry, retry, "classifier decision for: %s", body)
			if tt.retry {
				assert.Contains(t, message, tt.want)
				for _, phrase := range commands.NotVisibleYetPhrases() {
					if strings.Contains(strings.ToLower(message), phrase) {
						phraseHits[phrase]++
					}
				}
			}
		})
	}

	for _, phrase := range commands.NotVisibleYetPhrases() {
		assert.Positive(t, phraseHits[phrase], "no validator produces %q any more", phrase)
	}
}

// jsonEscaped returns s as it appears inside a JSON string.
func jsonEscaped(t *testing.T, s string) string {
	t.Helper()
	encoded, err := json.Marshal(s)
	require.NoError(t, err)
	return strings.Trim(string(encoded), `"`)
}

func TestRegistryValidationBodyIsReal(t *testing.T) {
	const identifier = "@example/server"
	body := realPublishBody(t,
		model.Package{RegistryType: model.RegistryTypeNPM, RegistryBaseURL: model.RegistryURLNPM, Identifier: identifier, Version: stubVersion},
		stubUpstream{
			"GET " + model.RegistryURLNPM + "/@example%2Fserver/" + stubVersion: {404, ""},
			"HEAD " + model.RegistryURLNPM + "/@example%2Fserver":               {200, ""},
		},
	)

	var got, want struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	require.NoError(t, json.Unmarshal([]byte(registryValidationBody), &want))
	assert.Equal(t, want, got)
}

func TestWaitClassifierRequiresRegistryValidation400(t *testing.T) {
	marked := []byte(registryValidationBody)
	withoutMarker := []byte(`{"errors":[{"message":"server not found (status: 404)"}]}`)

	tests := []struct {
		name   string
		status int
		body   []byte
	}{
		{"marker on a 401", http.StatusUnauthorized, marked},
		{"marker on a 429", http.StatusTooManyRequests, marked},
		{"marker on a 502", http.StatusBadGateway, marked},
		{"phrase without marker", http.StatusBadRequest, withoutMarker},
		{"duplicate version", http.StatusBadRequest, []byte(`{"errors":[{"message":"invalid version: cannot publish duplicate version"}]}`)},
		{"plain text body", http.StatusBadRequest, []byte("not found (status: 404)")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, retry := commands.ClassifyResponse(tt.status, tt.body)
			assert.False(t, retry)
		})
	}
}
