package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

type nodeScalingConfig struct {
	enabled bool
	minimum int
	maximum int
}

// Node scaling can be enabled without promoting the experimental ingress profile.
// Legacy stacks retain their two-node default and burstAutoscaling compatibility.
func readNodeScaling(ctx *pulumi.Context) (nodeScalingConfig, error) {
	c := config.New(ctx, "mcp-registry")
	result := nodeScalingConfig{enabled: c.GetBool("nodeAutoscaling") || c.GetBool("burstAutoscaling"), minimum: 2, maximum: 4}
	if c.Get("nodeMinCount") != "" {
		result.minimum = c.GetInt("nodeMinCount")
	}
	if c.Get("nodeMaxCount") != "" {
		result.maximum = c.GetInt("nodeMaxCount")
	}
	if result.minimum < 1 || result.maximum < result.minimum {
		return result, fmt.Errorf("nodeMinCount must be positive and nodeMaxCount must be at least nodeMinCount")
	}
	if !result.enabled && (c.Get("nodeMinCount") != "" || c.Get("nodeMaxCount") != "") {
		return result, fmt.Errorf("node count bounds require nodeAutoscaling or burstAutoscaling")
	}
	if c.GetBool("isolatedDatabase") {
		productionInfra := c.Get("environment") == "prod" && c.GetBool("productionInfrastructureOnly")
		maximum := 4
		phase := c.Get("productionAppScaling")
		if productionInfra && (phase == "prepare" || phase == "enabled") {
			maximum = 5
		}
		if !c.GetBool("coordinatedCapacity") || (c.Get("environment") != "staging" && !productionInfra) || !result.enabled || result.minimum != 3 || result.maximum != maximum {
			return result, fmt.Errorf("isolatedDatabase requires an explicitly enabled coordinatedCapacity profile and total node bounds 3–%d", maximum)
		}
		// Total bounds include the fixed database node; only workers autoscale.
		result.minimum--
		result.maximum--
	}
	return result, nil
}
