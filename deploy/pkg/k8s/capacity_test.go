package k8s //nolint:testpackage // Verify guarded experimental profile and rendered HPA.

import (
	"strings"
	"testing"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type capacityMonitor struct {
	t       *testing.T
	hpaSeen bool
}

func (m *capacityMonitor) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return a.Args, nil
}
func (m *capacityMonitor) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	if a.TypeToken == "kubernetes:autoscaling/v2:HorizontalPodAutoscaler" {
		m.hpaSeen = true
		spec := a.Inputs["spec"].ObjectValue()
		if spec["minReplicas"].NumberValue() != 2 || spec["maxReplicas"].NumberValue()*30 > 120 {
			m.t.Error("HPA exceeds connection budget or changes idle count")
		}
		target := spec["scaleTargetRef"].ObjectValue()
		if target["name"].StringValue() != "mcp-registry" || target["kind"].StringValue() != "Deployment" {
			m.t.Error("HPA targets the wrong workload")
		}
		metrics := spec["metrics"].ArrayValue()
		if len(metrics) != 1 || metrics[0].ObjectValue()["resource"].ObjectValue()["name"].StringValue() != "cpu" {
			m.t.Error("memory/cache must not hold scaling up after recovery")
		}
		if spec["behavior"].ObjectValue()["scaleDown"].ObjectValue()["stabilizationWindowSeconds"].NumberValue() != 600 {
			m.t.Error("missing conservative recovery window")
		}
	}
	return a.Name + "-id", a.Inputs, nil
}

func TestCapacityRejectsIncompleteOrProductionProfile(t *testing.T) {
	for _, tt := range []struct {
		name, key, value string
		wantError        bool
	}{
		{"valid", "environment", "staging", false},
		{"production", "environment", "prod", true},
		{"no node scaling", "burstAutoscaling", "false", true},
		{"undersized nodes", "nodeMachineType", "e2-standard-2", true},
		{"unmatched database request", "postgresCPURequest", "2", true},
		{"unmatched app request", "registryCPURequest", "400m", true},
		{"disabled", "coordinatedCapacity", "false", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]string{"coordinatedCapacity": "true", "burstAutoscaling": "true", "provider": "gcp", "environment": "staging", "nodeMachineType": "e2-standard-4", "registryCPURequest": "800m", "postgresCPURequest": "1500m", "postgresCPULimit": "3"}
			values[tt.key] = tt.value
			cfg := map[string]string{}
			for k, v := range values {
				cfg["mcp-registry:"+k] = v
			}
			err := pulumi.RunErr(ValidateCapacityConfig, pulumi.WithMocks("capacity-test", "staging", &capacityMonitor{t: t}), func(info *pulumi.RunInfo) { info.Config = cfg })
			if (err != nil) != tt.wantError {
				t.Fatalf("unexpected validation result: %v", err)
			}
			if err != nil && !strings.Contains(err.Error(), "coordinatedCapacity") {
				t.Fatalf("unexpected failure: %v", err)
			}
		})
	}
}

func TestRegistryAutoscalerBoundsDatabaseConnections(t *testing.T) {
	m := &capacityMonitor{t: t}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		provider, err := kubernetes.NewProvider(ctx, "test-provider", &kubernetes.ProviderArgs{})
		if err != nil {
			return err
		}
		return deployRegistryAutoscaler(ctx, provider)
	}, pulumi.WithMocks("capacity-test", "staging", m))
	if err != nil {
		t.Fatal(err)
	}
	if !m.hpaSeen {
		t.Fatal("HPA was not registered")
	}
}
