package main

import (
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/k8s"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers/aks"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers/local"
)

// createProvider creates the appropriate cluster provider based on configuration
func createProvider(ctx *pulumi.Context) (providers.ClusterProvider, error) {
	conf := config.New(ctx, "mcp-registry")
	providerName := conf.Get("provider")
	if providerName == "" {
		providerName = "local" // Default to local provider
	}

	switch providerName {
	case "aks":
		return &aks.Provider{}, nil
	case "local":
		return &local.Provider{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", providerName)
	}
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Get configuration
		conf := config.New(ctx, "mcp-registry")
		environment := conf.Require("environment")

		// Create provider
		provider, err := createProvider(ctx)
		if err != nil {
			return err
		}

		// Create cluster
		cluster, err := provider.CreateCluster(ctx, environment)
		if err != nil {
			return err
		}

		// Deploy to Kubernetes
		_, err = k8s.DeployAll(ctx, cluster, environment)
		if err != nil {
			return err
		}

		// Export outputs
		ctx.Export("clusterName", cluster.Name)

		return nil
	})
}
