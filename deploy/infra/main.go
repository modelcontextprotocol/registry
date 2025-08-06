package main

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Get configuration
		conf := config.New(ctx, "mcp-registry")
		environment := conf.Require("environment")
		
		// Create Kubernetes cluster
		cluster, err := createKubernetesCluster(ctx, environment)
		if err != nil {
			return err
		}

		// Deploy MongoDB
		err = deployMongoDB(ctx, cluster, environment)
		if err != nil {
			return err
		}

		// Deploy MCP Registry
		err = deployMCPRegistry(ctx, cluster, environment)
		if err != nil {
			return err
		}

		// Export outputs
		ctx.Export("clusterName", cluster.Name)
		ctx.Export("kubeconfig", cluster.Kubeconfig)
		ctx.Export("registryEndpoint", pulumi.Sprintf("http://mcp-registry.%s.svc.cluster.local:8080", environment))

		return nil
	})
}