package aks

import (
	"fmt"

	"github.com/pulumi/pulumi-azure-native-sdk/containerservice"
	"github.com/pulumi/pulumi-azure-native-sdk/resources"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// Provider implements the ClusterProvider interface for Azure Kubernetes Service
type Provider struct{}

// CreateCluster creates an Azure Kubernetes Service cluster
func (p *Provider) CreateCluster(ctx *pulumi.Context, environment string) (*providers.ProviderInfo, error) {
	conf := config.New(ctx, "mcp-registry")
	location := conf.Get("azureLocation")
	if location == "" {
		location = "East US"
	}

	// Create resource group
	resourceGroup, err := resources.NewResourceGroup(ctx, fmt.Sprintf("mcp-registry-%s-rg", environment), &resources.ResourceGroupArgs{
		Location: pulumi.String(location),
		Tags: pulumi.StringMap{
			"environment": pulumi.String(environment),
			"managed-by":  pulumi.String("pulumi"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Create AKS cluster
	clusterName := fmt.Sprintf("mcp-registry-%s", environment)

	// Determine node count based on environment
	nodeCount := 3
	if environment == "prod" {
		nodeCount = 5
	}

	cluster, err := containerservice.NewManagedCluster(ctx, clusterName, &containerservice.ManagedClusterArgs{
		ResourceGroupName: resourceGroup.Name,
		Location:          resourceGroup.Location,
		DnsPrefix:         pulumi.String(clusterName),
		KubernetesVersion: pulumi.String("1.30.2"),
		Identity: &containerservice.ManagedClusterIdentityArgs{
			Type: containerservice.ResourceIdentityTypeSystemAssigned,
		},
		AgentPoolProfiles: containerservice.ManagedClusterAgentPoolProfileArray{
			&containerservice.ManagedClusterAgentPoolProfileArgs{
				Name:   pulumi.String("nodepool1"),
				Count:  pulumi.Int(nodeCount),
				VmSize: pulumi.String("Standard_DS2_v2"),
				OsType: pulumi.String("Linux"),
				Mode:   pulumi.String("System"),
			},
		},
		NetworkProfile: &containerservice.ContainerServiceNetworkProfileArgs{
			NetworkPlugin: pulumi.String("azure"),
			ServiceCidr:   pulumi.String("10.0.0.0/16"),
			DnsServiceIP:  pulumi.String("10.0.0.10"),
		},
	})
	if err != nil {
		return nil, err
	}

	// Get AKS credentials
	creds := pulumi.All(cluster.Name, resourceGroup.Name).ApplyT(
		func(args []any) (string, error) {
			clusterName := args[0].(string)
			rgName := args[1].(string)
			credentials, err := containerservice.ListManagedClusterUserCredentials(ctx, &containerservice.ListManagedClusterUserCredentialsArgs{
				ResourceGroupName: rgName,
				ResourceName:      clusterName,
			})
			if err != nil {
				return "", err
			}
			return credentials.Kubeconfigs[0].Value, nil
		},
	).(pulumi.StringOutput)

	// Create Kubernetes provider for AKS
	k8sProvider, err := kubernetes.NewProvider(ctx, "k8s-provider", &kubernetes.ProviderArgs{
		Kubeconfig: creds,
	})
	if err != nil {
		return nil, err
	}

	return &providers.ProviderInfo{
		Name:       cluster.Name,
		Kubeconfig: creds,
		Provider:   k8sProvider,
	}, nil
}