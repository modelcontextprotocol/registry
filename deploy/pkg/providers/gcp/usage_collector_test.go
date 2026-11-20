package gcp //nolint:testpackage // Exercise resource registration with Pulumi mocks.

import (
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestUsageCollectorReadsOnlyItsOwnProjectsLogs(t *testing.T) {
	for flag, enabled := range map[string]bool{"": false, "false": false, "true": true} {
		t.Run("flag "+flag, func(t *testing.T) {
			m := &backupStorageMocks{resources: map[string]resource.PropertyMap{}}
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				_, err := usageCollectorKey(ctx, "mcp-registry-staging", nil)
				return err
			}, pulumi.WithMocks("test", "staging", m), func(i *pulumi.RunInfo) {
				i.Config = map[string]string{"mcp-registry:usageCollector": flag}
			})
			if err != nil {
				t.Fatal(err)
			}
			if !enabled {
				if len(m.resources) != 0 {
					t.Fatalf("created without the flag: %v", m.resources)
				}
				return
			}
			const account = "registry-usage-reader@mcp-registry-staging.iam.gserviceaccount.com"
			grant := m.resources["gcp:projects/iAMMember:IAMMember::usage-collector-log-viewer"]
			if grant["role"].StringValue() != "roles/logging.viewer" || grant["member"].StringValue() != "serviceAccount:"+account || grant["project"].StringValue() != "mcp-registry-staging" {
				t.Errorf("unexpected grant: %v", grant)
			}
			key := m.resources["gcp:serviceaccount/key:Key::usage-collector-key"]
			if key["serviceAccountId"].StringValue() != "projects/mcp-registry-staging/serviceAccounts/"+account {
				t.Errorf("the key is for %v", key["serviceAccountId"])
			}
			// One grant and one key. Managing the account itself would let
			// retiring the collector delete it, and the deployment could not recreate it.
			for identity := range m.resources {
				if strings.Contains(identity, "serviceaccount/account") || len(m.resources) != 2 {
					t.Errorf("unexpected resource %s", identity)
				}
			}
		})
	}
}
