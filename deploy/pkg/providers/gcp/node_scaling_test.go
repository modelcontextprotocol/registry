package gcp //nolint:testpackage // Test private configuration before any resources are registered.

import (
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type configMocks struct{}

func (configMocks) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) { return a.Args, nil }
func (configMocks) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	return a.Name, a.Inputs, nil
}

func TestNodeScalingConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name     string
		values   map[string]string
		enabled  bool
		min, max int
		invalid  bool
	}{
		{name: "legacy", min: 2, max: 4},
		{name: "legacy burst", values: map[string]string{"burstAutoscaling": "true"}, enabled: true, min: 2, max: 4},
		{name: "three ready nodes independently", values: map[string]string{"nodeAutoscaling": "true", "nodeMinCount": "3", "nodeMaxCount": "4"}, enabled: true, min: 3, max: 4},
		{name: "cheaper later", values: map[string]string{"nodeAutoscaling": "true", "nodeMinCount": "2", "nodeMaxCount": "3"}, enabled: true, min: 2, max: 3},
		{name: "isolated database", values: map[string]string{"nodeAutoscaling": "true", "isolatedDatabase": "true", "coordinatedCapacity": "true", "environment": "staging", "nodeMinCount": "3", "nodeMaxCount": "4"}, enabled: true, min: 2, max: 3},
		{name: "idle isolated staging", values: map[string]string{"stagingIdleCapacity": "true", "nodeAutoscaling": "true", "isolatedDatabase": "true", "coordinatedCapacity": "true", "environment": "staging", "nodeMinCount": "2", "nodeMaxCount": "4"}, enabled: true, min: 1, max: 3},
		{name: "idle forbidden in production", values: map[string]string{"stagingIdleCapacity": "true", "environment": "prod"}, invalid: true},
		{name: "explicit production infrastructure", values: map[string]string{"nodeAutoscaling": "true", "isolatedDatabase": "true", "coordinatedCapacity": "true", "productionInfrastructureOnly": "true", "environment": "prod", "nodeMinCount": "3", "nodeMaxCount": "4"}, enabled: true, min: 2, max: 3},
		{name: "production preparation workers", values: map[string]string{"nodeAutoscaling": "true", "isolatedDatabase": "true", "coordinatedCapacity": "true", "productionInfrastructureOnly": "true", "productionAppScaling": "prepare", "environment": "prod", "nodeMinCount": "3", "nodeMaxCount": "5"}, enabled: true, min: 2, max: 4},
		{name: "production enabled workers", values: map[string]string{"nodeAutoscaling": "true", "isolatedDatabase": "true", "coordinatedCapacity": "true", "productionInfrastructureOnly": "true", "productionAppScaling": "enabled", "environment": "prod", "nodeMinCount": "3", "nodeMaxCount": "5"}, enabled: true, min: 2, max: 4},
		{name: "larger workers require opt in", values: map[string]string{"nodeAutoscaling": "true", "isolatedDatabase": "true", "coordinatedCapacity": "true", "productionInfrastructureOnly": "true", "environment": "prod", "nodeMinCount": "3", "nodeMaxCount": "5"}, invalid: true},
		{name: "reject isolated production", values: map[string]string{"nodeAutoscaling": "true", "isolatedDatabase": "true", "coordinatedCapacity": "true", "environment": "prod", "nodeMinCount": "3", "nodeMaxCount": "4"}, invalid: true},
		{name: "reject uncoordinated isolation", values: map[string]string{"isolatedDatabase": "true"}, invalid: true},
		{name: "inverted", values: map[string]string{"nodeAutoscaling": "true", "nodeMinCount": "4", "nodeMaxCount": "3"}, invalid: true},
		{name: "zero", values: map[string]string{"nodeAutoscaling": "true", "nodeMinCount": "0"}, invalid: true},
		{name: "bounds without enabling", values: map[string]string{"nodeMinCount": "3"}, invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := map[string]string{}
			for k, v := range tt.values {
				cfg["mcp-registry:"+k] = v
			}
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				got, err := readNodeScaling(ctx)
				if (err != nil) != tt.invalid {
					t.Fatalf("unexpected validation: %v", err)
				}
				if err == nil && (got.enabled != tt.enabled || got.minimum != tt.min || got.maximum != tt.max) {
					t.Fatalf("unexpected bounds: %+v", got)
				}
				return nil
			}, pulumi.WithMocks("node-test", "staging", configMocks{}), func(info *pulumi.RunInfo) { info.Config = cfg })
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
