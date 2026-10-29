package k8s //nolint:testpackage // Exercise staging guards and scheduling rules directly.

import (
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestIsolatedDatabaseRequiresCompleteStagingProfile(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
		invalid          bool
	}{
		{"valid", "environment", "staging", false},
		{"production", "environment", "prod", true},
		{"uncoordinated", "coordinatedCapacity", "false", true},
		{"underreserved DB", "postgresCPURequest", "1500m", true},
		{"mutable app", "stagingImage", "ghcr.io/modelcontextprotocol/registry:main", true},
		{"invalid digest", "stagingImage", "example/image@sha256:0123", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"isolatedDatabase": "true", "coordinatedCapacity": "true", "burstAutoscaling": "true", "environment": "staging", "provider": "gcp", "nodeMachineType": "e2-standard-4", "nodeMinCount": "3", "nodeMaxCount": "4", "registryCPURequest": "800m", "postgresCPURequest": "3", "postgresCPULimit": "3", "stagingImage": "example/image@sha256:" + strings.Repeat("a", 64)}
			values[tc.key] = tc.value
			cfg := map[string]string{}
			for k, v := range values {
				cfg["mcp-registry:"+k] = v
			}
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				if err := ValidateCapacityConfig(ctx); err != nil {
					return err
				}
				db := protectRegistryDatabase(ctx, map[string]any{"instances": 1})
				affinity := db["affinity"].(map[string]any)
				if affinity["nodeSelector"].(map[string]any)["registry-role"] != "database" {
					t.Error("database placement is not required")
				}
				toleration := affinity["tolerations"].([]any)[0].(map[string]any)
				if toleration["effect"] != "NoSchedule" || toleration["value"] != "database" {
					t.Error("database cannot enter its reserved pool")
				}
				if workerNodeSelector(ctx)["registry-role"] != pulumi.String("worker") {
					t.Error("app/ingress must use worker nodes")
				}
				return nil
			}, pulumi.WithMocks("placement-test", "staging", ownershipMonitor{t}), func(info *pulumi.RunInfo) { info.Config = cfg })
			if (err != nil) != tc.invalid {
				t.Fatalf("unexpected validation: %v", err)
			}
		})
	}
}
