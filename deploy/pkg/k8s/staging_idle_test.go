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

func TestSmallStagingProfile(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		invalid    bool
	}{
		{"stagingSmallCapacity", "prepare", false},
		{"environment", "prod", true}, {"stagingIdleCapacity", "false", true},
		{"nodeMachineType", "e2-standard-2", true}, {"registryCPURequest", "800m", true},
		{"postgresCPURequest", "3", true}, {"stagingSmallCapacity", "unknown", true},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			cfg := map[string]string{}
			values := map[string]string{"stagingSmallCapacity": "prepare", "stagingIdleCapacity": "true", "isolatedDatabase": "true", "coordinatedCapacity": "true", "burstAutoscaling": "true", "environment": "staging", "provider": "gcp", "nodeMachineType": "e2-standard-4", "nodeMinCount": "2", "nodeMaxCount": "4", "registryCPURequest": "100m", "postgresCPURequest": "250m", "postgresCPULimit": "3", "stagingImage": "example/image@sha256:" + strings.Repeat("a", 64)}
			values[tc.key] = tc.value
			for k, v := range values {
				cfg["mcp-registry:"+k] = v
			}
			err := pulumi.RunErr(ValidateCapacityConfig, pulumi.WithMocks("test", "staging", ownershipMonitor{t}), func(i *pulumi.RunInfo) { i.Config = cfg })
			if (err != nil) != tc.invalid {
				t.Fatalf("unexpected validation: %v", err)
			}
			if !tc.invalid {
				cfg["mcp-registry:stagingSmallCapacity"] = "enabled"
				cfg["mcp-registry:nodeMachineType"] = "e2-standard-2"
				if err := pulumi.RunErr(ValidateCapacityConfig, pulumi.WithMocks("test", "staging", ownershipMonitor{t}), func(i *pulumi.RunInfo) { i.Config = cfg }); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
