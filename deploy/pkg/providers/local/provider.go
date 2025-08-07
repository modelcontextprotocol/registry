package local

import (
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// Provider implements the ClusterProvider interface for local Kubernetes clusters
type Provider struct{}

// CreateCluster configures access to a local Kubernetes cluster via kubeconfig
func (p *Provider) CreateCluster(ctx *pulumi.Context, environment string) (*providers.ProviderInfo, error) {
	// Create Kubernetes provider for local cluster
	k8sProvider, err := kubernetes.NewProvider(ctx, "k8s-provider", &kubernetes.ProviderArgs{})
	if err != nil {
		return nil, err
	}

	return &providers.ProviderInfo{
		Name:     pulumi.String("local").ToStringOutput(),
		Provider: k8sProvider,
	}, nil
}
