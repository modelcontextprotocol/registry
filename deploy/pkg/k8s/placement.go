package k8s

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

const (
	registryRoleLabel    = "registry-role"
	registryDatabaseRole = "database"
)

func workerNodeSelector(ctx *pulumi.Context) pulumi.StringMap {
	if !config.New(ctx, "mcp-registry").GetBool("isolatedDatabase") {
		return nil
	}
	return pulumi.StringMap{registryRoleLabel: pulumi.String("worker")}
}

func registryDatabaseAffinity(ctx *pulumi.Context) map[string]any {
	if !config.New(ctx, "mcp-registry").GetBool("isolatedDatabase") {
		return nil
	}
	return map[string]any{
		"nodeSelector": map[string]any{registryRoleLabel: registryDatabaseRole},
		"tolerations":  []any{map[string]any{"key": registryRoleLabel, "operator": "Equal", "value": registryDatabaseRole, "effect": "NoSchedule"}}, //nolint:goconst // Literal keys are Kubernetes toleration schema fields.
	}
}

func protectRegistryDatabase(ctx *pulumi.Context, spec map[string]any) map[string]any {
	spec = protectDatabase(ctx, spec)
	if affinity := registryDatabaseAffinity(ctx); affinity != nil {
		spec["affinity"] = affinity
	}
	return spec
}
