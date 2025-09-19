package registries

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// minimal structures to serialize JSON like real registry responses
type testOCIManifest struct {
	Manifests []struct {
		Digest string `json:"digest"`
	} `json:"manifests,omitempty"`
	Config struct {
		Digest string `json:"digest"`
	} `json:"config,omitempty"`
}

type testOCIImageConfig struct {
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"config"`
}

// Happy path: single-arch manifest with correct label and bearer auth header sent
func TestValidateOCI_WithMockedRegistry_SingleArchWithAuth(t *testing.T) {
	// Arrange mocked registry
	cfgDigest := "sha256:deadbeef"
	manifest := testOCIManifest{}
	manifest.Config.Digest = cfgDigest
	img := testOCIImageConfig{}
	img.Config.Labels = map[string]string{
		"io.modelcontextprotocol.server.name": "io.github.owner/repo",
	}

	var authHeaderSeen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/owner/repo/manifests/latest":
			authHeaderSeen = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(manifest)
		case r.URL.Path == "/v2/owner/repo/blobs/"+cfgDigest:
			_ = json.NewEncoder(w).Encode(img)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MCP_REGISTRY_OCI_TEST_BASE_URL", srv.URL)
	t.Setenv("MCP_REGISTRY_OCI_TOKEN_GHCR_IO", "bearer-xyz") // ensure header presence test

	pkg := model.Package{
		RegistryType:    model.RegistryTypeOCI,
		RegistryBaseURL: model.RegistryURLGHCR,
		Identifier:      "owner/repo",
		Version:         "latest",
	}

	// Act
	err := ValidateOCI(context.Background(), pkg, "io.github.owner/repo")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if authHeaderSeen == "" {
		t.Fatalf("expected Authorization header to be set from env token")
	}
}

// Multi-arch: manifest list -> specific manifest fetch -> config fetch
func TestValidateOCI_WithMockedRegistry_MultiArch(t *testing.T) {
	listDigest := "sha256:list"
	cfgDigest := "sha256:cfg"
	// top-level manifest list
	list := testOCIManifest{Manifests: []struct {
		Digest string `json:"digest"`
	}{{Digest: listDigest}}}
	// specific manifest
	sub := testOCIManifest{}
	sub.Config.Digest = cfgDigest
	img := testOCIImageConfig{}
	img.Config.Labels = map[string]string{
		"io.modelcontextprotocol.server.name": "io.github.owner/repo",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/owner/repo/manifests/latest":
			_ = json.NewEncoder(w).Encode(list)
		case "/v2/owner/repo/manifests/" + listDigest:
			_ = json.NewEncoder(w).Encode(sub)
		case "/v2/owner/repo/blobs/" + cfgDigest:
			_ = json.NewEncoder(w).Encode(img)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MCP_REGISTRY_OCI_TEST_BASE_URL", srv.URL)
	// Do not set token; should still work for mocked server without auth

	pkg := model.Package{
		RegistryType:    model.RegistryTypeOCI,
		RegistryBaseURL: model.RegistryURLGHCR,
		Identifier:      "owner/repo",
		Version:         "latest",
	}

	if err := ValidateOCI(context.Background(), pkg, "io.github.owner/repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Missing label should fail ownership validation
func TestValidateOCI_WithMockedRegistry_MissingLabel(t *testing.T) {
	cfgDigest := "sha256:nope"
	manifest := testOCIManifest{}
	manifest.Config.Digest = cfgDigest
	img := testOCIImageConfig{}
	img.Config.Labels = map[string]string{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/owner/repo/manifests/v1":
			_ = json.NewEncoder(w).Encode(manifest)
		case "/v2/owner/repo/blobs/" + cfgDigest:
			_ = json.NewEncoder(w).Encode(img)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MCP_REGISTRY_OCI_TEST_BASE_URL", srv.URL)

	pkg := model.Package{
		RegistryType:    model.RegistryTypeOCI,
		RegistryBaseURL: model.RegistryURLGHCR,
		Identifier:      "owner/repo",
		Version:         "v1",
	}

	if err := ValidateOCI(context.Background(), pkg, "io.github.owner/repo"); err == nil {
		t.Fatalf("expected error due to missing label")
	}
}
