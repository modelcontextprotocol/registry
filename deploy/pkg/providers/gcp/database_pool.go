package gcp

import (
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/container"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func createDatabaseNodePool(ctx *pulumi.Context, cluster *container.Cluster, project, zone, machineType string, provider *gcp.Provider) (*container.NodePool, error) {
	opts := []pulumi.ResourceOption{pulumi.Protect(true)}
	if provider != nil {
		opts = append(opts, pulumi.Provider(provider))
	}
	return container.NewNodePool(ctx, "registry-database-nodepool", &container.NodePoolArgs{
		Name: pulumi.String("mcp-registry-staging-database"), Cluster: cluster.Name, Location: pulumi.String(zone), Project: pulumi.String(project), NodeCount: pulumi.Int(1),
		NodeConfig: &container.NodePoolNodeConfigArgs{
			MachineType: pulumi.String(machineType), DiskSizeGb: pulumi.Int(50), DiskType: pulumi.String("pd-balanced"),
			Labels: pulumi.StringMap{"registry-role": pulumi.String("database")},
			Taints: container.NodePoolNodeConfigTaintArray{&container.NodePoolNodeConfigTaintArgs{Key: pulumi.String("registry-role"), Value: pulumi.String("database"), Effect: pulumi.String("NO_SCHEDULE")}},
		},
		Management:      &container.NodePoolManagementArgs{AutoRepair: pulumi.Bool(true), AutoUpgrade: pulumi.Bool(true)},
		UpgradeSettings: &container.NodePoolUpgradeSettingsArgs{Strategy: pulumi.String("SURGE"), MaxSurge: pulumi.Int(1), MaxUnavailable: pulumi.Int(0)},
	}, opts...)
}
