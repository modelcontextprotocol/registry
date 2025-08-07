package aks

import (
	"encoding/base64"
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
	// Get resource group name from config or use default
	conf := config.New(ctx, "mcp-registry")
	resourceGroupName := conf.Get("resourceGroupName")
	if resourceGroupName == "" {
		resourceGroupName = "official-mcp-registry-prod"
	}

	// Get existing resource group
	resourceGroup, err := resources.LookupResourceGroup(ctx, &resources.LookupResourceGroupArgs{
		ResourceGroupName: resourceGroupName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find resource group '%s': %w", resourceGroupName, err)
	}

	// Create AKS cluster
	clusterName := fmt.Sprintf("mcp-registry-%s", environment)

	cluster, err := containerservice.NewManagedCluster(ctx, clusterName, &containerservice.ManagedClusterArgs{
		ResourceGroupName: pulumi.String(resourceGroup.Name),
		Location:          pulumi.String(resourceGroup.Location),
		DnsPrefix:         pulumi.String(clusterName),
		KubernetesVersion: pulumi.String("1.33.2"),
		Identity: &containerservice.ManagedClusterIdentityArgs{
			Type: containerservice.ResourceIdentityTypeSystemAssigned,
		},
		AgentPoolProfiles: containerservice.ManagedClusterAgentPoolProfileArray{
			&containerservice.ManagedClusterAgentPoolProfileArgs{
				Name:   pulumi.String("nodepool1"),
				Count:  pulumi.Int(2),
				VmSize: pulumi.String("Standard_B2s"),
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
	creds := pulumi.All(cluster.Name, pulumi.String(resourceGroup.Name)).ApplyT(
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
			// Decode base64 kubeconfig
			kubeconfigData, err := base64.StdEncoding.DecodeString(credentials.Kubeconfigs[0].Value)
			if err != nil {
				return "", fmt.Errorf("failed to decode kubeconfig: %w", err)
			}
			return string(kubeconfigData), nil
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
		Name:     cluster.Name,
		Provider: k8sProvider,
	}, nil
}
