package registries_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pomTemplate produces a minimal valid POM with the requested fields. Empty
// values omit the corresponding element so we can test "missing field" branches.
//
//nolint:unparam // artifactID is intentionally configurable to keep this helper general for future tests.
func pomTemplate(groupID, artifactID, mcpName, description string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<project xmlns="http://maven.apache.org/POM/4.0.0">` + "\n")
	b.WriteString(`  <modelVersion>4.0.0</modelVersion>` + "\n")
	if groupID != "" {
		b.WriteString("  <groupId>" + groupID + "</groupId>\n")
	}
	if artifactID != "" {
		b.WriteString("  <artifactId>" + artifactID + "</artifactId>\n")
	}
	b.WriteString("  <version>1.0.0</version>\n")
	if description != "" {
		b.WriteString("  <description>" + description + "</description>\n")
	}
	if mcpName != "" {
		b.WriteString("  <properties>\n")
		b.WriteString("    <mcpName>" + mcpName + "</mcpName>\n")
		b.WriteString("  </properties>\n")
	}
	b.WriteString(`</project>` + "\n")
	return b.String()
}

// startMavenMock returns an httptest server that serves the test-provided POM
// body at the expected canonical Maven layout path; any other path returns 404.
//
//nolint:unparam // status is intentionally configurable for future non-200 test cases.
func startMavenMock(t *testing.T, expectedPath, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != expectedPath {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestValidateMaven_MissingOrInvalidFields(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		pkg     model.Package
		wantMsg string
	}{
		{
			name:    "missing identifier",
			pkg:     model.Package{RegistryType: model.RegistryTypeMaven, Version: "1.0.0"},
			wantMsg: "package identifier is required for Maven packages",
		},
		{
			name:    "missing version",
			pkg:     model.Package{RegistryType: model.RegistryTypeMaven, Identifier: "org.example:foo"},
			wantMsg: "package version is required for Maven packages",
		},
		{
			name: "fileSha256 not allowed",
			pkg: model.Package{
				RegistryType: model.RegistryTypeMaven,
				Identifier:   "org.example:foo",
				Version:      "1.0.0",
				FileSHA256:   "abc",
			},
			wantMsg: "must not have 'fileSha256' field",
		},
		{
			name: "wrong identifier shape",
			pkg: model.Package{
				RegistryType: model.RegistryTypeMaven,
				Identifier:   "org.example.foo",
				Version:      "1.0.0",
			},
			wantMsg: "groupId:artifactId",
		},
		{
			name: "empty group in identifier",
			pkg: model.Package{
				RegistryType: model.RegistryTypeMaven,
				Identifier:   ":foo",
				Version:      "1.0.0",
			},
			wantMsg: "groupId:artifactId",
		},
		{
			name: "non-Maven base URL rejected",
			pkg: model.Package{
				RegistryType:    model.RegistryTypeMaven,
				Identifier:      "org.example:foo",
				Version:         "1.0.0",
				RegistryBaseURL: "https://example.com/repo",
			},
			wantMsg: "registry type and base URL do not match",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := registries.ValidateMaven(ctx, tc.pkg, "com.example/server")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

// TestValidateMaven_OwnershipAgainstMockRepo exercises the parsing + ownership
// branches by pointing the validator at an httptest server via the test-only
// ValidateMavenAt helper (the production entry point is restricted to the
// official Maven Central host).
func TestValidateMaven_OwnershipAgainstMockRepo(t *testing.T) {
	const (
		groupID    = "io.github.example"
		artifactID = "demo-mcp-server"
		version    = "1.2.3"
		serverName = "io.github.example/demo-mcp-server"
	)
	expectedPath := "/io/github/example/" + artifactID + "/" + version + "/" + artifactID + "-" + version + ".pom"

	t.Run("matching mcpName property passes", func(t *testing.T) {
		body := pomTemplate(groupID, artifactID, serverName, "")
		srv := startMavenMock(t, expectedPath, body, http.StatusOK)
		defer srv.Close()

		err := registries.ValidateMavenAt(context.Background(), srv.URL, model.Package{
			RegistryType: model.RegistryTypeMaven,
			Identifier:   groupID + ":" + artifactID,
			Version:      version,
		}, serverName)
		assert.NoError(t, err)
	})

	t.Run("description fallback passes", func(t *testing.T) {
		body := pomTemplate(groupID, artifactID, "", "Sample server\nmcp-name: "+serverName+"\nmore notes")
		srv := startMavenMock(t, expectedPath, body, http.StatusOK)
		defer srv.Close()

		err := registries.ValidateMavenAt(context.Background(), srv.URL, model.Package{
			RegistryType: model.RegistryTypeMaven,
			Identifier:   groupID + ":" + artifactID,
			Version:      version,
		}, serverName)
		assert.NoError(t, err)
	})

	t.Run("mismatched mcpName fails", func(t *testing.T) {
		body := pomTemplate(groupID, artifactID, "io.github.other/wrong", "")
		srv := startMavenMock(t, expectedPath, body, http.StatusOK)
		defer srv.Close()

		err := registries.ValidateMavenAt(context.Background(), srv.URL, model.Package{
			RegistryType: model.RegistryTypeMaven,
			Identifier:   groupID + ":" + artifactID,
			Version:      version,
		}, serverName)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Expected mcpName")
	})

	t.Run("missing ownership signal fails with helpful message", func(t *testing.T) {
		body := pomTemplate(groupID, artifactID, "", "Just a description")
		srv := startMavenMock(t, expectedPath, body, http.StatusOK)
		defer srv.Close()

		err := registries.ValidateMavenAt(context.Background(), srv.URL, model.Package{
			RegistryType: model.RegistryTypeMaven,
			Identifier:   groupID + ":" + artifactID,
			Version:      version,
		}, serverName)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ownership validation failed")
		// Error should tell publishers exactly what to add.
		assert.Contains(t, err.Error(), "<mcpName>")
		assert.Contains(t, err.Error(), "mcp-name: "+serverName)
	})

	t.Run("groupId mismatch in POM fails", func(t *testing.T) {
		body := pomTemplate("io.github.different", artifactID, serverName, "")
		srv := startMavenMock(t, expectedPath, body, http.StatusOK)
		defer srv.Close()

		err := registries.ValidateMavenAt(context.Background(), srv.URL, model.Package{
			RegistryType: model.RegistryTypeMaven,
			Identifier:   groupID + ":" + artifactID,
			Version:      version,
		}, serverName)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "POM groupId")
	})

	t.Run("missing artifact returns 404 error", func(t *testing.T) {
		// Serve 404 for any path.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		err := registries.ValidateMavenAt(context.Background(), srv.URL, model.Package{
			RegistryType: model.RegistryTypeMaven,
			Identifier:   groupID + ":" + artifactID,
			Version:      version,
		}, serverName)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("malformed XML fails with parse error", func(t *testing.T) {
		srv := startMavenMock(t, expectedPath, "<not-xml", http.StatusOK)
		defer srv.Close()

		err := registries.ValidateMavenAt(context.Background(), srv.URL, model.Package{
			RegistryType: model.RegistryTypeMaven,
			Identifier:   groupID + ":" + artifactID,
			Version:      version,
		}, serverName)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse Maven POM")
	})
}
