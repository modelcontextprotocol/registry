package k8s //nolint:testpackage // Test the staged version and database engine gates directly.

import (
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestSecurityPatchPreservesDatabaseEngine(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		value := "false"
		if enabled {
			value = "true"
		}
		err := pulumi.RunErr(func(ctx *pulumi.Context) error {
			for _, protect := range []func(*pulumi.Context, map[string]any) map[string]any{protectDatabase, protectRegistryDatabase} {
				spec := protect(ctx, map[string]any{"instances": 1})
				image, pinned := spec["imageName"]
				if pinned != enabled || (enabled && image != "ghcr.io/cloudnative-pg/postgresql:17.5") {
					t.Fatal("operator upgrade must preserve the existing database engine")
				}
			}
			want := pulumi.String("old")
			if enabled {
				want = pulumi.String("patched")
			}
			if securityPatchVersion(ctx, "old", "patched") != want {
				t.Fatal("versions must change only on opted-in stacks")
			}
			return nil
		}, pulumi.WithMocks("security-test", "staging", ownershipMonitor{t}), func(info *pulumi.RunInfo) {
			info.Config = map[string]string{"mcp-registry:securityPatch202611": value}
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
