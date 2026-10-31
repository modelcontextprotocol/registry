package k8s //nolint:testpackage // Exercise environment boundaries and the idle transition.

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"strings"
	"testing"
)

func TestStagingIdleCapacityGuards(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		invalid    bool
	}{
		{"environment", "staging", false}, {"environment", "prod", true},
		{"isolatedDatabase", "false", true}, {"coordinatedCapacity", "false", true},
		{"nodeMinCount", "3", true}, {"nodeMaxCount", "5", true},
		{"postgresCPURequest", "1", true},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			values := map[string]string{"stagingIdleCapacity": "true", "isolatedDatabase": "true", "coordinatedCapacity": "true", "burstAutoscaling": "true", "environment": "staging", "provider": "gcp", "nodeMachineType": "e2-standard-4", "nodeMinCount": "2", "nodeMaxCount": "4", "registryCPURequest": "800m", "postgresCPURequest": "3", "postgresCPULimit": "3", "stagingImage": "example/image@sha256:" + strings.Repeat("a", 64)}
			values[tc.key] = tc.value
			cfg := map[string]string{}
			for k, v := range values {
				cfg["mcp-registry:"+k] = v
			}
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				if err := ValidateCapacityConfig(ctx); err != nil {
					return err
				}
				replicas, enabled := registryCapacitySettings(ctx)
				if replicas != 1 || !enabled || coordinatedMinimum(ctx) != 1 {
					t.Fatal("idle replicas not applied")
				}
				db := protectRegistryDatabase(ctx, map[string]any{"instances": 1})
				if db["enablePDB"] != true || db["affinity"] == nil {
					t.Fatal("database protection lost")
				}
				return nil
			}, pulumi.WithMocks("idle-test", "staging", ownershipMonitor{t}), func(info *pulumi.RunInfo) { info.Config = cfg })
			if (err != nil) != tc.invalid {
				t.Fatalf("unexpected validation result: %v", err)
			}
		})
	}
}
