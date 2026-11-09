package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/container"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/logging"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func createGatewayCanary(ctx *pulumi.Context, cluster *container.Cluster, project, region, zone string, provider *gcp.Provider) (*container.NodePool, *compute.Address, error) {
	if project != "mcp-registry-staging" {
		return nil, nil, fmt.Errorf("gateway canary requires the staging GCP project")
	}
	opts := []pulumi.ResourceOption{}
	if provider != nil {
		opts = append(opts, pulumi.Provider(provider))
	}
	var pool *container.NodePool
	var err error
	if !config.New(ctx, "mcp-registry").GetBool("gatewayStagingCompact") {
		pool, err = container.NewNodePool(ctx, "registry-gateway-canary-nodepool", &container.NodePoolArgs{
			Name: pulumi.String("registry-gateway-canary"), Cluster: cluster.Name, Project: pulumi.String(project), Location: pulumi.String(zone), NodeCount: pulumi.Int(2),
			NodeConfig:  &container.NodePoolNodeConfigArgs{MachineType: pulumi.String("e2-standard-2"), DiskSizeGb: pulumi.Int(50), DiskType: pulumi.String("pd-balanced"), Labels: pulumi.StringMap{"registry-role": pulumi.String("gateway-canary")}, Taints: container.NodePoolNodeConfigTaintArray{&container.NodePoolNodeConfigTaintArgs{Key: pulumi.String("registry-role"), Value: pulumi.String("gateway-canary"), Effect: pulumi.String("NO_SCHEDULE")}}},
			Autoscaling: &container.NodePoolAutoscalingArgs{MinNodeCount: pulumi.Int(2), MaxNodeCount: pulumi.Int(2)}, Management: &container.NodePoolManagementArgs{AutoRepair: pulumi.Bool(true), AutoUpgrade: pulumi.Bool(true)}, UpgradeSettings: &container.NodePoolUpgradeSettingsArgs{Strategy: pulumi.String("SURGE"), MaxSurge: pulumi.Int(1), MaxUnavailable: pulumi.Int(0)},
		}, append(opts, pulumi.IgnoreChanges([]string{"nodeCount"}))...)
		if err != nil {
			return nil, nil, err
		}
	}
	var address *compute.Address
	if !config.New(ctx, "mcp-registry").GetBool("gatewayStagingStandalone") {
		address, err = compute.NewAddress(ctx, "registry-gateway-canary-ip", &compute.AddressArgs{Project: pulumi.String(project), Region: pulumi.String(region), AddressType: pulumi.String("EXTERNAL")}, opts...)
		if err != nil {
			return nil, nil, err
		}
	}
	_, err = logging.NewProjectExclusion(ctx, "registry-gateway-canary-fast-read-sampling", &logging.ProjectExclusionArgs{Project: pulumi.String(project), Name: pulumi.String("registry-gateway-canary-fast-read-sampling"), Description: pulumi.String("Keep 10% of fast successful canary listing logs; metrics use unsampled local logs."), Filter: pulumi.String(gatewayAccessLogExclusionFilter)}, opts...)
	return pool, address, err
}

// Promote the existing in-use address in place; never recreate its forwarding rule.
// Keep the reservation even when rolling traffic back to the legacy controller.
func reserveStagingIngressAddress(ctx *pulumi.Context, project, region string, provider *gcp.Provider) (pulumi.StringOutput, error) {
	value := config.New(ctx, "mcp-registry").Get("gatewayStagingPublicIP")
	if value == "" {
		return pulumi.String("").ToStringOutput(), nil
	}
	if project != "mcp-registry-staging" {
		return pulumi.StringOutput{}, fmt.Errorf("staging ingress reservation requires the staging GCP project")
	}
	opts := []pulumi.ResourceOption{pulumi.Protect(true)}
	if provider != nil {
		opts = append(opts, pulumi.Provider(provider))
	}
	address, err := compute.NewAddress(ctx, "registry-staging-ingress-ip", &compute.AddressArgs{Project: pulumi.String(project), Region: pulumi.String(region), Address: pulumi.String(value), AddressType: pulumi.String("EXTERNAL")}, opts...)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	return address.Address, nil
}

const gatewayAccessLogExclusionFilter = `resource.type="k8s_container"
resource.labels.namespace_name=("registry-gateway-canary" OR "ingress-nginx")
resource.labels.container_name="nginx"
log_id("stdout")
severity < WARNING
jsonPayload.kind="registry_access"
jsonPayload.method=("GET" OR "HEAD")
jsonPayload.status=("200" OR "304")
jsonPayload.uri =~ "^/v0(\\.1)?/servers(\\?|$)"
jsonPayload.duration =~ "^0\\."
insertId:*
sample(insertId, 0.9)`

// Adopt the existing production reservation without replacing its forwarding rule.
func reserveProductionIngressAddress(ctx *pulumi.Context, project, region string, provider *gcp.Provider) (pulumi.StringOutput, error) {
	c := config.New(ctx, "mcp-registry")
	value := c.Require("gatewayProductionPublicIP")
	name := c.Require("gatewayProductionAddressName")
	if project != "mcp-registry-prod" {
		return pulumi.StringOutput{}, fmt.Errorf("production ingress reservation requires production GCP")
	}
	opts := []pulumi.ResourceOption{pulumi.Protect(true)}
	if c.Get("gatewayProductionPhase") == "adopt" {
		opts = append(opts, pulumi.Import(pulumi.ID("projects/"+project+"/regions/"+region+"/addresses/"+name)))
	}
	if provider != nil {
		opts = append(opts, pulumi.Provider(provider))
	}

	readOpts := []pulumi.ResourceOption{}
	if provider != nil {
		readOpts = append(readOpts, pulumi.Provider(provider))
	}
	existing, err := compute.GetAddress(ctx, "production-public-address-identity", pulumi.ID("projects/"+project+"/regions/"+region+"/addresses/"+name), nil, readOpts...)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	verified := existing.Address.ApplyT(func(live string) (string, error) {
		if live != value {
			return "", fmt.Errorf("production address reservation does not match the public IP")
		}
		return live, nil
	}).(pulumi.StringOutput)
	address, err := compute.NewAddress(ctx, "registry-production-ingress-ip", &compute.AddressArgs{Name: pulumi.String(name), Project: pulumi.String(project), Region: pulumi.String(region), Address: verified, AddressType: pulumi.String("EXTERNAL"), Description: existing.Description, NetworkTier: existing.NetworkTier}, opts...)

	if err != nil {
		return pulumi.StringOutput{}, err
	}
	return address.Address, nil
}
