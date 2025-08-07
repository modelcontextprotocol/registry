package providers

import (
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ProviderInfo represents the information returned by a cluster provider
type ProviderInfo struct {
	Name     pulumi.StringOutput
	Provider *kubernetes.Provider
}

// ClusterProvider defines the interface that all cluster providers must implement
type ClusterProvider interface {
	// CreateCluster creates a Kubernetes cluster and returns provider info
	CreateCluster(ctx *pulumi.Context, environment string) (*ProviderInfo, error)
}
