package k8s

import (
	"fmt"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/yaml"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// DeployK8up retains legacy backup resources and installs the production archive backup job.
func DeployK8up(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string, storage *providers.BackupStorageInfo) error {
	if storage == nil {
		ctx.Log.Info("No backup storage configured, skipping k8up deployment", nil)
		return nil
	}

	// Install the k8up CRDs before the helm chart
	// Related: https://github.com/k8up-io/k8up/issues/1050
	k8upCRDs, err := yaml.NewConfigFile(ctx, "k8up-crds", &yaml.ConfigFileArgs{
		File: "https://github.com/k8up-io/k8up/releases/download/k8up-4.8.4/k8up-crd.yaml",
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return fmt.Errorf("failed to install k8up CRDs: %w", err)
	}

	// Install k8up operator
	k8upValues := pulumi.Map{
		"k8up": pulumi.Map{
			// Disable implicit PVC copies while retaining the legacy operator.
			"skipWithoutAnnotation": pulumi.Bool(true),
			"timezone":              pulumi.String("UTC"),
		},
	}

	k8up, err := helm.NewChart(ctx, "k8up", helm.ChartArgs{
		Chart:   pulumi.String("k8up"),
		Version: pulumi.String("4.8.4"),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://k8up-io.github.io/k8up"),
		},
		Values: k8upValues,
	}, pulumi.Provider(cluster.Provider), pulumi.DependsOn([]pulumi.Resource{k8upCRDs}))
	if err != nil {
		return fmt.Errorf("failed to install k8up: %w", err)
	}

	// Create restic repository password secret
	repoPassword, err := corev1.NewSecret(ctx, "k8up-repo-password", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("k8up-repo-password"),
			Namespace: pulumi.String("default"),
			Labels: pulumi.StringMap{
				"k8up.io/backup": pulumi.String("true"),
			},
		},
		Type: pulumi.String("Opaque"),
		StringData: pulumi.StringMap{
			// Preserve the existing key/password association. Rotating a repository
			// password requires restic key operations, not just a Secret update.
			"password": pulumi.String("password"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return fmt.Errorf("failed to create repository password secret: %w", err)
	}

	// Keep the legacy Schedule resource and repository for investigation, but
	// remove all scheduled actions. A dedicated job below writes and validates a
	// complete PostgreSQL archive before uploading; it does not stream through
	// the Kubernetes exec API.
	_, err = apiextensions.NewCustomResource(ctx, "k8up-schedule", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("k8up.io/v1"),
		Kind:       pulumi.String("Schedule"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("backup-schedule"),
			Namespace: pulumi.String("default"),
			Labels:    pulumi.StringMap{"environment": pulumi.String(environment)},
		},
		OtherFields: map[string]any{"spec": map[string]any{}},
	}, pulumi.Provider(cluster.Provider), pulumi.DependsOn([]pulumi.Resource{k8up}))
	if err != nil {
		return fmt.Errorf("failed to retire the legacy backup schedule: %w", err)
	}

	return deployPostgresBackupJob(ctx, cluster, environment, storage, repoPassword)
}
