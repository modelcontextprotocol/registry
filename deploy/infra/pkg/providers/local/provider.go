package local

import (
	"fmt"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// Provider implements the ClusterProvider interface for local Kubernetes clusters
type Provider struct{}

// CreateCluster configures access to a local Kubernetes cluster via kubeconfig
func (p *Provider) CreateCluster(ctx *pulumi.Context, environment string) (*providers.ProviderInfo, error) {
	conf := config.New(ctx, "mcp-registry")

	// Get kubeconfig path or use default
	kubeconfigPath := conf.Get("kubeconfigPath")
	if kubeconfigPath == "" {
		kubeconfigPath = "~/.kube/config"
	}

	clusterName := fmt.Sprintf("mcp-registry-%s-local", environment)

	// Create Kubernetes provider for local cluster
	k8sProvider, err := kubernetes.NewProvider(ctx, "k8s-provider", &kubernetes.ProviderArgs{
		Kubeconfig: pulumi.String(kubeconfigPath),
	})
	if err != nil {
		return nil, err
	}

	return &providers.ProviderInfo{
		Name:       pulumi.String(clusterName).ToStringOutput(),
		Kubeconfig: pulumi.String(kubeconfigPath).ToStringOutput(),
		Provider:   k8sProvider,
	}, nil
}