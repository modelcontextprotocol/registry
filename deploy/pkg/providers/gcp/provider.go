package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/container"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// Provider implements the ClusterProvider interface for Google Kubernetes Engine
type Provider struct{}

// CreateCluster creates a Google Kubernetes Engine cluster
func (p *Provider) CreateCluster(ctx *pulumi.Context, environment string) (*providers.ProviderInfo, error) {
	// Get configuration
	conf := config.New(ctx, "mcp-registry")

	// Get project ID from config or use default
	projectID := conf.Get("gcpProjectId")
	if projectID == "" {
		return nil, fmt.Errorf("GCP project ID not configured. Set mcp-registry:gcpProjectId")
	}

	// Get region from config or use default
	region := conf.Get("gcpRegion")
	if region == "" {
		region = "us-central1"
	}

	// Create GKE cluster
	clusterName := fmt.Sprintf("mcp-registry-%s", environment)

	// Configure the GKE cluster
	cluster, err := container.NewCluster(ctx, clusterName, &container.ClusterArgs{
		Name:        pulumi.String(clusterName),
		Location:    pulumi.String(region),
		Project:     pulumi.String(projectID),
		Description: pulumi.String(fmt.Sprintf("MCP Registry %s GKE Cluster", environment)),

		// Initial node count (will be managed by node pool)
		InitialNodeCount: pulumi.Int(1),

		// Remove default node pool after cluster creation
		RemoveDefaultNodePool: pulumi.Bool(true),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create GKE cluster: %w", err)
	}

	// Create a managed node pool for the cluster
	nodePoolName := fmt.Sprintf("%s-nodepool", clusterName)
	_, err = container.NewNodePool(ctx, nodePoolName, &container.NodePoolArgs{
		Cluster:  cluster.Name,
		Location: pulumi.String(region),
		Project:  pulumi.String(projectID),

		// Node pool configuration
		NodeCount: pulumi.Int(2),
		NodeConfig: &container.NodePoolNodeConfigArgs{
			MachineType: pulumi.String("e2-micro"),
			DiskSizeGb:  pulumi.Int(10),
			DiskType:    pulumi.String("pd-standard"),
		},

		// Node management configuration
		Management: &container.NodePoolManagementArgs{
			AutoRepair:  pulumi.Bool(true),
			AutoUpgrade: pulumi.Bool(true),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create node pool: %w", err)
	}

	// Get cluster endpoint and auth information
	clusterEndpoint := cluster.Endpoint
	clusterMasterAuth := cluster.MasterAuth

	// Create kubeconfig for the GKE cluster
	kubeconfig := pulumi.All(clusterEndpoint, clusterMasterAuth.ClusterCaCertificate, cluster.Name).ApplyT(
		func(args []any) (string, error) {
			endpoint := args[0].(string)
			caCert := args[1].(string)
			clusterName := args[2].(string)

			// Create kubeconfig YAML
			kubeconfigYAML := fmt.Sprintf(`apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: %s
    server: https://%s
  name: %s
contexts:
- context:
    cluster: %s
    user: %s
  name: %s
current-context: %s
kind: Config
preferences: {}
users:
- name: %s
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      args:
      - gke
      - get-credentials
      - %s
      - --region
      - %s
      - --project
      - %s
      command: gcloud
      env: null
      installHint: Install gcloud CLI and run 'gcloud auth login'
      interactiveMode: IfAvailable
      provideClusterInfo: true
`, caCert, endpoint, clusterName, clusterName, clusterName, clusterName, clusterName, clusterName, clusterName, region, projectID)

			return kubeconfigYAML, nil
		},
	).(pulumi.StringOutput)

	// Create Kubernetes provider for GKE
	k8sProvider, err := kubernetes.NewProvider(ctx, "k8s-provider", &kubernetes.ProviderArgs{
		Kubeconfig: kubeconfig,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes provider: %w", err)
	}

	return &providers.ProviderInfo{
		Name:     cluster.Name,
		Provider: k8sProvider,
	}, nil
}
