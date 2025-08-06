package main

import (
	"fmt"

	"github.com/pulumi/pulumi-azure-native/sdk/v2/go/azure/containerservice"
	"github.com/pulumi/pulumi-azure-native/sdk/v2/go/azure/resources"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// ClusterInfo represents the Kubernetes cluster information
type ClusterInfo struct {
	Name       pulumi.StringOutput
	Kubeconfig pulumi.StringOutput
	Provider   *kubernetes.Provider
}

// createKubernetesCluster creates a Kubernetes cluster based on the cloud provider
func createKubernetesCluster(ctx *pulumi.Context, environment string) (*ClusterInfo, error) {
	conf := config.New(ctx, "mcp-registry")
	provider := conf.Get("provider")
	if provider == "" {
		provider = "local" // Default to local provider
	}
	
	var clusterInfo *ClusterInfo
	var err error
	
	switch provider {
	case "aks":
		clusterInfo, err = createAKSCluster(ctx, environment)
	case "orbstack", "k3s", "local":
		clusterInfo, err = createLocalCluster(ctx, environment)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
	
	if err != nil {
		return nil, err
	}
	
	// Create namespace for the environment
	_, err = corev1.NewNamespace(ctx, environment, &corev1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(environment),
			Labels: pulumi.StringMap{
				"environment": pulumi.String(environment),
				"managed-by":  pulumi.String("pulumi"),
			},
		},
	}, pulumi.Provider(clusterInfo.Provider))
	if err != nil {
		return nil, err
	}

	// Install NGINX Ingress Controller
	ingressType := "LoadBalancer"
	if provider == "orbstack" || provider == "k3s" || provider == "local" {
		ingressType = "NodePort" // Use NodePort for local clusters
	}
	
	_, err = helm.NewChart(ctx, "nginx-ingress", helm.ChartArgs{
		Chart:   pulumi.String("ingress-nginx"),
		Version: pulumi.String("4.7.1"),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://kubernetes.github.io/ingress-nginx"),
		},
		Namespace: pulumi.String("ingress-nginx"),
		Values: pulumi.Map{
			"controller": pulumi.Map{
				"service": pulumi.Map{
					"type": pulumi.String(ingressType),
				},
			},
		},
	}, pulumi.Provider(clusterInfo.Provider))
	if err != nil {
		return nil, err
	}

	// Install cert-manager for TLS certificates
	_, err = helm.NewChart(ctx, "cert-manager", helm.ChartArgs{
		Chart:   pulumi.String("cert-manager"),
		Version: pulumi.String("v1.12.2"),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://charts.jetstack.io"),
		},
		Namespace: pulumi.String("cert-manager"),
		Values: pulumi.Map{
			"installCRDs": pulumi.Bool(true),
		},
	}, pulumi.Provider(clusterInfo.Provider))
	if err != nil {
		return nil, err
	}

	return clusterInfo, nil
}

// createAKSCluster creates an Azure Kubernetes Service cluster
func createAKSCluster(ctx *pulumi.Context, environment string) (*ClusterInfo, error) {
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
				Name:         pulumi.String("nodepool1"),
				Count:        pulumi.Int(nodeCount),
				VmSize:       pulumi.String("Standard_DS2_v2"),
				OsType:       pulumi.String("Linux"),
				Mode:         pulumi.String("System"),
			},
		},
		NetworkProfile: &containerservice.ContainerServiceNetworkProfileArgs{
			NetworkPlugin:    pulumi.String("azure"),
			ServiceCidr:      pulumi.String("10.0.0.0/16"),
			DnsServiceIP:     pulumi.String("10.0.0.10"),
		},
	})
	if err != nil {
		return nil, err
	}
	
	// Get AKS credentials
	creds := pulumi.All(cluster.Name, resourceGroup.Name).ApplyT(
		func(args []interface{}) (string, error) {
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
		KubeConfig: creds,
	})
	if err != nil {
		return nil, err
	}
	
	return &ClusterInfo{
		Name:       cluster.Name,
		Kubeconfig: creds,
		Provider:   k8sProvider,
	}, nil
}

// createLocalCluster configures access to a local Kubernetes cluster (OrbStack, k3s, etc.)
func createLocalCluster(ctx *pulumi.Context, environment string) (*ClusterInfo, error) {
	conf := config.New(ctx, "mcp-registry")
	
	// Get kubeconfig path or use default
	kubeconfigPath := conf.Get("kubeconfigPath")
	if kubeconfigPath == "" {
		kubeconfigPath = "~/.kube/config"
	}
	
	clusterName := fmt.Sprintf("mcp-registry-%s-local", environment)
	
	// Create Kubernetes provider for local cluster
	k8sProvider, err := kubernetes.NewProvider(ctx, "k8s-provider", &kubernetes.ProviderArgs{
		KubeConfig: pulumi.String(kubeconfigPath),
	})
	if err != nil {
		return nil, err
	}
	
	return &ClusterInfo{
		Name:       pulumi.String(clusterName).ToStringOutput(),
		Kubeconfig: pulumi.String(kubeconfigPath).ToStringOutput(),
		Provider:   k8sProvider,
	}, nil
}