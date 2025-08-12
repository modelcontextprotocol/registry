package gcp

import (
	"encoding/base64"
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp"
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
	gcpConf := config.New(ctx, "gcp")

	// Get project ID from config or use default
	projectID := gcpConf.Get("project")
	if projectID == "" {
		return nil, fmt.Errorf("GCP project ID not configured. Set gcp:project")
	}

	// Get region from config or use default
	region := gcpConf.Get("region")
	if region == "" {
		region = "us-central1"
	}

	// Get credentials from config (base64 encoded service account JSON)
	credentials := gcpConf.Get("credentials")
	if credentials != "" {
		// Decode the base64 credentials
		decodedCreds, err := base64.StdEncoding.DecodeString(credentials)
		if err != nil {
			return nil, fmt.Errorf("failed to decode GCP credentials: %w", err)
		}
		credentials = string(decodedCreds)
	}

	// Create a GCP provider with explicit credentials if provided
	var gcpProvider *gcp.Provider
	var err error
	if credentials != "" {
		gcpProvider, err = gcp.NewProvider(ctx, "gcp-explicit", &gcp.ProviderArgs{
			Project:     pulumi.String(projectID),
			Region:      pulumi.String(region),
			Credentials: pulumi.String(credentials),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create GCP provider: %w", err)
		}
	}

	// Create GKE cluster
	clusterName := fmt.Sprintf("mcp-registry-%s", environment)

	// Use a specific zone instead of region for zonal cluster
	zone := fmt.Sprintf("%s-b", region)

	// Configure the GKE cluster
	clusterArgs := &container.ClusterArgs{
		Name:        pulumi.String(clusterName),
		Location:    pulumi.String(zone),
		Project:     pulumi.String(projectID),
		Description: pulumi.String(fmt.Sprintf("MCP Registry %s GKE Cluster", environment)),

		// Initial node count (will be managed by node pool)
		InitialNodeCount: pulumi.Int(1),

		// Remove default node pool after cluster creation
		RemoveDefaultNodePool: pulumi.Bool(true),

		AddonsConfig: &container.ClusterAddonsConfigArgs{
			// Disable as we use ingress-nginx
			HttpLoadBalancing: &container.ClusterAddonsConfigHttpLoadBalancingArgs{
				Disabled: pulumi.Bool(true),
			},
		},
	}

	// Add provider if we have explicit credentials
	clusterOpts := []pulumi.ResourceOption{}
	if gcpProvider != nil {
		clusterOpts = append(clusterOpts, pulumi.Provider(gcpProvider))
	}

	cluster, err := container.NewCluster(ctx, clusterName, clusterArgs, clusterOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GKE cluster: %w", err)
	}

	// Create a managed node pool for the cluster
	nodePoolName := fmt.Sprintf("%s-nodepool", clusterName)
	nodePoolArgs := &container.NodePoolArgs{
		Cluster:  cluster.Name,
		Location: pulumi.String(zone),
		Project:  pulumi.String(projectID),

		// Node pool configuration
		NodeCount: pulumi.Int(2),
		NodeConfig: &container.NodePoolNodeConfigArgs{
			MachineType: pulumi.String("e2-small"),
			DiskSizeGb:  pulumi.Int(20),
			DiskType:    pulumi.String("pd-standard"),
		},

		// Node management configuration
		Management: &container.NodePoolManagementArgs{
			AutoRepair:  pulumi.Bool(true),
			AutoUpgrade: pulumi.Bool(true),
		},
	}

	// Add provider if we have explicit credentials
	nodePoolOpts := []pulumi.ResourceOption{}
	if gcpProvider != nil {
		nodePoolOpts = append(nodePoolOpts, pulumi.Provider(gcpProvider))
	}

	nodePool, err := container.NewNodePool(ctx, nodePoolName, nodePoolArgs, nodePoolOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create node pool: %w", err)
	}

	// Create Kubernetes provider using the cluster directly
	k8sProvider, err := kubernetes.NewProvider(ctx, "k8s-provider", &kubernetes.ProviderArgs{
		Kubeconfig: pulumi.All(cluster.Endpoint, cluster.MasterAuth).ApplyT(func(args []any) (string, error) {
			endpoint := args[0].(string)
			masterAuth := args[1].(container.ClusterMasterAuth)
			context := fmt.Sprintf("%s_%s_%s", projectID, zone, clusterName)

			// Extract CA certificate
			caCert := ""
			if masterAuth.ClusterCaCertificate != nil {
				caCert = *masterAuth.ClusterCaCertificate
			}

			// Create kubeconfig using gke-gcloud-auth-plugin
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
      command: gke-gcloud-auth-plugin
      installHint: Install gke-gcloud-auth-plugin for use with kubectl by following
        https://cloud.google.com/blog/products/containers-kubernetes/kubectl-auth-changes-in-gke
      provideClusterInfo: true
`, caCert, endpoint, context, context, context, context, context, context)

			return kubeconfigYAML, nil
		}).(pulumi.StringOutput),
	}, pulumi.DependsOn([]pulumi.Resource{cluster, nodePool}))
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes provider: %w", err)
	}

	return &providers.ProviderInfo{
		Name:     nodePool.Cluster,
		Provider: k8sProvider,
	}, nil
}
