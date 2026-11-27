package validators_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/validators"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func recordNPMChecks(t *testing.T, serverName string, paths *[]string) {
	t.Helper()
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "registry.npmjs.org" {
			return original.RoundTrip(r)
		}
		*paths = append(*paths, r.URL.Path)
		body := fmt.Sprintf(`{"mcpName":%q}`, serverName)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
}

func TestDistinctPackageOwnershipChecks(t *testing.T) {
	req := serverWithNPMPackage()
	pkg := req.Packages[0]
	launch, otherID, otherVersion := pkg, pkg, pkg
	launch.RunTimeHint = "npx"
	launch.EnvironmentVariables = []model.KeyValueInput{{Name: "MODE"}}
	otherID.Identifier = "another"
	otherVersion.Version = "4.0.0"
	req.Packages = []model.Package{pkg, launch, otherID, pkg, otherVersion, pkg}

	var paths []string
	recordNPMChecks(t, req.Name, &paths)

	// Each final entry differs from a successful check in just one field.
	// Its failure must still be reported at its original position.
	otherType, otherBase, otherHash := pkg, pkg, pkg
	otherType.RegistryType = "unsupported"
	otherBase.RegistryBaseURL = "https://registry.example.com"
	otherHash.FileSHA256 = strings.Repeat("a", 64)
	cfg := &config.Config{EnableRegistryValidation: true}
	for _, tc := range []struct {
		last model.Package
		want string
	}{
		{otherType, "unsupported registry type"},
		{otherBase, "registry type and base URL do not match"},
		{otherHash, "must not have 'fileSha256'"},
	} {
		req.Packages[5] = tc.last
		paths = nil
		err := validators.ValidateWriteRequest(t.Context(), req, cfg, false)
		require.ErrorContains(t, err, "registry validation failed for package 5 (compromised)")
		require.ErrorContains(t, err, tc.want)
		require.Equal(t, []string{"/compromised/3.0.0", "/another/3.0.0", "/compromised/4.0.0"}, paths)
	}
}

func TestPackageOwnershipLimitOnWriteAndValidate(t *testing.T) {
	osvURL, queries := malwareOSV(t, "GHSA-aaaa-bbbb-cccc")
	resolver := &fakeHostResolver{}
	validators.SetHostResolverForTest(t, resolver)
	cfg := &config.Config{EnableRegistryValidation: true, MalwareCheckMode: config.MalwareCheckEnforce, OSVAPIURL: osvURL}
	req := serverWithNPMPackage()
	req.Remotes = []model.Transport{{Type: model.TransportTypeStreamableHTTP, URL: "https://remote.example.com/mcp"}}
	var paths []string
	recordNPMChecks(t, req.Name, &paths)
	for i := 0; i < 16; i++ {
		pkg := req.Packages[0]
		pkg.Identifier = fmt.Sprintf("pkg-%d", i)
		req.Packages = append(req.Packages, pkg)
	}

	const overLimit = "package 16 (pkg-15) exceeds the limit of 16 distinct package ownership checks"
	require.ErrorContains(t, validators.ValidateWriteRequest(t.Context(), req, cfg, false), overLimit)
	require.NoError(t, validators.ValidateWriteRequest(t.Context(), req, cfg, true))
	valRes := validators.ValidateServerJSON(&req, validators.ValidationAll.WithConfig(cfg))
	require.False(t, valRes.Valid)
	require.ErrorContains(t, valRes.FirstError(), overLimit)
	require.Zero(t, resolver.calls.Load())
	require.Zero(t, queries.Load())
	require.Empty(t, paths)

	// With sixteen distinct checks, an extra duplicate still reaches the normal validator.
	req.Packages[16] = req.Packages[0]
	require.NoError(t, validators.ValidateWriteRequest(t.Context(), req, cfg, false))
	require.Len(t, paths, 16)
	require.EqualValues(t, 1, resolver.calls.Load())
	require.EqualValues(t, 1, queries.Load())
}
