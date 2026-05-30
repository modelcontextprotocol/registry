package registries_test

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestValidateGoModuleOwnership(t *testing.T) {
	cases := []struct {
		name       string
		modulePath string
		serverName string
		wantErr    bool
	}{
		{"matching owner", "github.com/alice/mcp-foo", "io.github.alice/foo", false},
		{"matching owner with subpackage path", "github.com/alice/mcp-foo/cmd/foo", "io.github.alice/foo", false},
		{"case-insensitive owner match", "github.com/Alice/mcp-foo", "io.github.alice/foo", false},
		{"case-insensitive namespace match", "github.com/alice/mcp-foo", "io.github.Alice/foo", false},
		{"owner mismatch", "github.com/bob/mcp-foo", "io.github.alice/foo", true},
		{"module not on github", "gitlab.com/alice/mcp-foo", "io.github.alice/foo", true},
		{"non-github namespace", "github.com/alice/mcp-foo", "com.example/foo", true},
		{"missing owner segment", "github.com/", "io.github.alice/foo", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := registries.ValidateGoModuleOwnership(tc.modulePath, tc.serverName)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateGoModuleOwnership(%q, %q) error = %v, wantErr %v", tc.modulePath, tc.serverName, err, tc.wantErr)
			}
		})
	}
}

func TestEscapeGoModulePath(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"lowercase unchanged", "github.com/alice/mcp-foo", "github.com/alice/mcp-foo", false},
		{"uppercase escaped", "github.com/Alice/McpFoo", "github.com/!alice/!mcp!foo", false},
		{"version unchanged", "v1.2.3", "v1.2.3", false},
		{"parent dir rejected", "github.com/alice/../bob", "", true},
		{"current dir rejected", "github.com/alice/./foo", "", true},
		{"empty rejected", "", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := registries.EscapeGoModulePath(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("EscapeGoModulePath(%q) error = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("EscapeGoModulePath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidateGoGuardErrors(t *testing.T) {
	ctx := context.Background()
	base := model.Package{
		RegistryType: model.RegistryTypeGo,
		Identifier:   "github.com/alice/mcp-foo",
		Version:      "v1.0.0",
	}
	server := "io.github.alice/foo"

	t.Run("missing identifier", func(t *testing.T) {
		pkg := base
		pkg.Identifier = ""
		if err := registries.ValidateGo(ctx, pkg, server); err == nil {
			t.Error("expected error for missing identifier")
		}
	})

	t.Run("missing version", func(t *testing.T) {
		pkg := base
		pkg.Version = ""
		if err := registries.ValidateGo(ctx, pkg, server); err == nil {
			t.Error("expected error for missing version")
		}
	})

	t.Run("fileSha256 rejected", func(t *testing.T) {
		pkg := base
		pkg.FileSHA256 = "deadbeef"
		if err := registries.ValidateGo(ctx, pkg, server); err == nil {
			t.Error("expected error for fileSha256 on go package")
		}
	})

	t.Run("base URL mismatch", func(t *testing.T) {
		pkg := base
		pkg.RegistryBaseURL = "https://example.com"
		if err := registries.ValidateGo(ctx, pkg, server); err == nil {
			t.Error("expected error for non-proxy base URL")
		}
	})

	t.Run("ownership failure returns before network", func(t *testing.T) {
		pkg := base
		pkg.Identifier = "github.com/someone-else/mcp-foo"
		err := registries.ValidateGo(ctx, pkg, server)
		if err == nil || !strings.Contains(err.Error(), "ownership validation failed") {
			t.Errorf("expected ownership validation error, got %v", err)
		}
	})
}
