package providers

import (
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ProviderInfo represents the information returned by a cluster provider
type ProviderInfo struct {
	Name     pulumi.StringOutput
	Provider *kubernetes.Provider
}

// BackupStorageInfo represents S3-compatible backup storage configuration
type BackupStorageInfo struct {
	Endpoint    string // S3 endpoint (e.g., https://storage.googleapis.com or http://minio:9000)
	BucketName  string
	Credentials *corev1.Secret // Should contain AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY
}

// ClusterProvider defines the interface that all cluster providers must implement
type ClusterProvider interface {
	// CreateCluster creates a Kubernetes cluster and returns provider info
	CreateCluster(ctx *pulumi.Context, environment string) (*ProviderInfo, error)

	// CreateBackupStorage creates backup storage infrastructure
	CreateBackupStorage(ctx *pulumi.Context, cluster *ProviderInfo, environment string) (*BackupStorageInfo, error)
}
