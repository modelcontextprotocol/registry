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

// DeployK8up installs the k8up backup operator and configures scheduled backups
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
			"backupCommandAnnotation": pulumi.String("k8up.io/backup-command"),
			"fileExtensionAnnotation": pulumi.String("k8up.io/file-extension"),
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
			"password": pulumi.String("password"), // In production we use GCS, which is already encrypted
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return fmt.Errorf("failed to create repository password secret: %w", err)
	}

	// Determine schedule based on environment
	backupSchedule := "46 4 * * *" // Daily at 4:46 AM
	pruneSchedule := "46 5 * * *"  // Daily at 5:46 AM
	keepDaily := 28                // Keep daily backups for 28 days

	if environment == "local" || environment == "dev" {
		backupSchedule = "* * * * *"  // Every minute for testing
		pruneSchedule = "*/5 * * * *" // Every 5 minutes
		keepDaily = 1
	}

	// Create Schedule for automated backups
	_, err = apiextensions.NewCustomResource(ctx, "k8up-schedule", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("k8up.io/v1"),
		Kind:       pulumi.String("Schedule"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("backup-schedule"),
			Namespace: pulumi.String("default"),
			Labels: pulumi.StringMap{
				"environment": pulumi.String(environment),
			},
		},
		OtherFields: map[string]any{
			"spec": map[string]any{
				"backend": map[string]any{
					"repoPasswordSecretRef": map[string]any{
						"name": repoPassword.Metadata.Name().Elem(),
						"key":  "password",
					},
					"s3": map[string]any{
						"endpoint": storage.Endpoint,
						"bucket":   storage.BucketName,
						"accessKeyIDSecretRef": map[string]any{
							"name": storage.Credentials.Metadata.Name().Elem(),
							"key":  "AWS_ACCESS_KEY_ID",
						},
						"secretAccessKeySecretRef": map[string]any{
							"name": storage.Credentials.Metadata.Name().Elem(),
							"key":  "AWS_SECRET_ACCESS_KEY",
						},
					},
				},
				"backup": map[string]any{
					"schedule": backupSchedule,
					"podSecurityContext": map[string]any{
						"runAsUser": 0, // Run as root to access all files
					},
					"successfulJobsHistoryLimit": 3,
					"failedJobsHistoryLimit":     3,
				},
				"prune": map[string]any{
					"schedule": pruneSchedule,
					"retention": map[string]any{
						"keepDaily": keepDaily,
					},
					"successfulJobsHistoryLimit": 1,
					"failedJobsHistoryLimit":     1,
				},
			},
		},
	}, pulumi.Provider(cluster.Provider), pulumi.DependsOn([]pulumi.Resource{k8up, storage.Credentials, repoPassword}))
	if err != nil {
		return fmt.Errorf("failed to create k8up schedule: %w", err)
	}

	return nil
}
