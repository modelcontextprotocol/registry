package k8s

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"testing"
)

func TestProductionDatabaseCapacity(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
		valid            bool
	}{
		{name: "eight-vcpu", valid: true},
		{name: "node-first-transition", key: "budget", value: "3", valid: true},
		{name: "staging-rejected", key: "environment", value: "staging"},
		{name: "non-isolated-rejected", key: "isolatedDatabase", value: "false"},
		{name: "unknown-machine-rejected", key: "databaseNodeMachineType", value: "e2-standard-16"},
		{name: "worker-resize-rejected", key: "nodeMachineType", value: "e2-standard-8"},
		{name: "excess-quota-rejected", key: "postgresCPULimit", value: "8"},
		{name: "mismatched-quota-rejected", key: "postgresCPURequest", value: "3"},
		{name: "small-node-six-cpu-rejected", key: "databaseNodeMachineType", value: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := productionScalingConfig("enabled")
			cfg["mcp-registry:databaseNodeMachineType"] = "e2-standard-8"
			cfg["mcp-registry:postgresCPURequest"] = "6"
			cfg["mcp-registry:postgresCPULimit"] = "6"
			if tc.key == "budget" {
				cfg["mcp-registry:postgresCPURequest"] = tc.value
				cfg["mcp-registry:postgresCPULimit"] = tc.value
			} else if tc.key != "" {
				cfg["mcp-registry:"+tc.key] = tc.value
			}
			err := pulumi.RunErr(ValidateCapacityConfig, pulumi.WithMocks("test", "prod", &capacityMonitor{t: t}), func(i *pulumi.RunInfo) { i.Config = cfg })
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
