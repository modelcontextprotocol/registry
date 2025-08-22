package k8s

import (
	"fmt"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// DeployK8up installs the k8up backup operator and configures scheduled backups
func DeployK8up(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string, storage *providers.StorageInfo) error {
	if storage == nil {
		ctx.Log.Info("No backup storage configured, skipping k8up deployment", nil)
		return nil
	}

	// Install k8up CRDs first
	k8upCRDs, err := helm.NewChart(ctx, "k8up-crds", helm.ChartArgs{
		Chart:   pulumi.String("k8up-crd"),
		Version: pulumi.String("4.8.4"),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://k8up-io.github.io/k8up"),
		},
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
			"password": pulumi.String("changeme"), // In production, use a secure password
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return fmt.Errorf("failed to create repository password secret: %w", err)
	}

	// Determine schedule based on environment
	backupSchedule := "08 4 * * *"   // Daily at 4:08 AM
	pruneSchedule := "43 4 * * *"    // Daily at 4:43 AM
	checkSchedule := "13 5 * * 0"    // Weekly on Sunday at 5:13 AM
	keepDaily := 28                  // Keep daily backups for 28 days

	if environment == "local" || environment == "dev" {
		backupSchedule = "*/30 * * * *" // Every 30 minutes for testing
		pruneSchedule = "*/45 * * * *"  // Every 45 minutes
		checkSchedule = "0 */6 * * *"   // Every 6 hours
		keepDaily = 7
	}

	// Create Schedule for automated backups
	_, err = apiextensions.NewCustomResource(ctx, "k8up-schedule", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("k8up.io/v1"),
		Kind:       pulumi.String("Schedule"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("daily-backup"),
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
				"check": map[string]any{
					"schedule":                   checkSchedule,
					"successfulJobsHistoryLimit": 1,
					"failedJobsHistoryLimit":     1,
				},
			},
		},
	}, pulumi.Provider(cluster.Provider), pulumi.DependsOn([]pulumi.Resource{k8up, storage.Credentials, repoPassword}))
	if err != nil {
		return fmt.Errorf("failed to create k8up schedule: %w", err)
	}

	// Export backup information
	ctx.Export("backupOperator", pulumi.String("k8up v4.8.4"))
	ctx.Export("backupSchedule", pulumi.String(backupSchedule))
	ctx.Export("backupPruneSchedule", pulumi.String(pruneSchedule))
	ctx.Export("backupCheckSchedule", pulumi.String(checkSchedule))
	ctx.Export("backupRetentionDays", pulumi.Int(keepDaily))

	return nil
}