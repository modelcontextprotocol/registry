package k8s

import (
	"fmt"

	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// DeployBackups preserves the existing repository credentials and production archive job.
// K8up is retired; the CronJob invokes pg_dump and restic directly.
func DeployBackups(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string, storage *providers.BackupStorageInfo) error {
	// Staging has no scheduled backups or retained legacy repository to unlock.
	if environment == "staging" {
		return deployStagingRestore(ctx, cluster)
	}
	if storage == nil {
		ctx.Log.Info("No backup storage configured, skipping backup deployment", nil)
		return nil
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

	return deployPostgresBackupJob(ctx, cluster, environment, storage, repoPassword)
}
