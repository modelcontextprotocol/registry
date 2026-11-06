package k8s //nolint:testpackage // Check stack configuration and CI image selection together.

import (
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func TestStagingBuildImage(t *testing.T) {
	digest := "ghcr.io/modelcontextprotocol/registry@sha256:" + strings.Repeat("a", 64)
	for _, tc := range []struct{ name, configured, built, want string }{
		{"automatic build", "", digest, digest},
		{"explicit diagnostic pin", digest, "different", digest},
		{"local main fallback", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REGISTRY_STAGING_IMAGE", tc.built)
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				if got := stagingImage(config.New(ctx, "mcp-registry")); got != tc.want {
					t.Fatalf("unexpected image: %q", got)
				}
				return nil
			}, pulumi.WithMocks("image-test", "staging", ownershipMonitor{t}), func(info *pulumi.RunInfo) {
				info.Config = map[string]string{"mcp-registry:stagingImage": tc.configured}
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
