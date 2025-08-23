package k8s

import (
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// DeployAll orchestrates the complete deployment of the MCP Registry to Kubernetes
func DeployAll(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string) (service *corev1.Service, err error) {
	// Setup cert-manager
	err = SetupCertManager(ctx, cluster)
	if err != nil {
		return nil, err
	}

	// Setup ingress controller
	ingressNginx, err := SetupIngressController(ctx, cluster, environment)
	if err != nil {
		return nil, err
	}

	// Deploy PostgreSQL databases
	pgCluster, err := DeployPostgresDatabases(ctx, cluster, environment)
	if err != nil {
		return nil, err
	}

	// Deploy MCP Registry
	service, err = DeployMCPRegistry(ctx, cluster, environment, ingressNginx, pgCluster)
	if err != nil {
		return nil, err
	}

	return service, nil
}
