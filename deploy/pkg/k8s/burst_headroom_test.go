package k8s //nolint:testpackage // Verify environment and resource boundaries.

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"testing"
)

func TestProductionBurstHeadroomBoundaries(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		invalid    bool
	}{
		{"environment", "prod", false}, {"environment", "staging", true},
		{"productionIngressScaling", "false", true}, {"nodeMaxCount", "6", true},
		{"nodeMinCount", "4", true}, {"registryCPURequest", "800m", true},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			cfg := map[string]string{}
			values := map[string]string{"productionBurstHeadroom": "true", "productionIngressScaling": "true", "productionInfrastructureOnly": "true", "productionAppScaling": "enabled", "coordinatedCapacity": "true", "isolatedDatabase": "true", "burstAutoscaling": "true", "provider": "gcp", "environment": "prod", "nodeMachineType": "e2-standard-4", "registryCPURequest": "1200m", "postgresCPURequest": "3", "postgresCPULimit": "3", "nodeMinCount": "3", "nodeMaxCount": "8", "imageTag": "1.8.7"}
			values[tc.key] = tc.value
			for k, v := range values {
				cfg["mcp-registry:"+k] = v
			}
			err := pulumi.RunErr(ValidateCapacityConfig, pulumi.WithMocks("test", "prod", &capacityMonitor{t: t}), func(i *pulumi.RunInfo) { i.Config = cfg })
			if (err != nil) != tc.invalid {
				t.Fatalf("unexpected validation: %v", err)
			}
		})
	}
}
