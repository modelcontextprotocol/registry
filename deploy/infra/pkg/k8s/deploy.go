package k8s

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// DeployAll orchestrates the complete deployment of the MCP Registry to Kubernetes
func DeployAll(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string) (pulumi.StringOutput, error) {
	// Setup cluster infrastructure (namespace, ingress, cert-manager)
	err := SetupClusterInfrastructure(ctx, cluster, environment)
	if err != nil {
		return pulumi.StringOutput{}, err
	}

	// Deploy MongoDB
	err = DeployMongoDB(ctx, cluster, environment)
	if err != nil {
		return pulumi.StringOutput{}, err
	}

	// Deploy MCP Registry
	err = DeployMCPRegistry(ctx, cluster, environment)
	if err != nil {
		return pulumi.StringOutput{}, err
	}

	return pulumi.Sprintf("http://mcp-registry.%s.svc.cluster.local:8080", environment), nil
}