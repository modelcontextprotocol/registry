package k8s

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/yaml"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

//go:embed postgres_backup_job.json
var postgresBackupJobTemplate string

func deployPostgresBackupJob(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string, storage *providers.BackupStorageInfo, password *corev1.Secret) error {
	// The backup checks PostgreSQL readiness itself. A slow CNPG management
	// probe must not remove an otherwise reachable primary from backup discovery.
	service, err := corev1.NewService(ctx, "registry-pg-backup", &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("registry-pg-backup"), Namespace: pulumi.String("default")},
		Spec: &corev1.ServiceSpecArgs{
			PublishNotReadyAddresses: pulumi.Bool(true),
			Selector:                 pulumi.StringMap{"cnpg.io/cluster": pulumi.String("registry-pg"), "cnpg.io/instanceRole": pulumi.String("primary")},
			Ports:                    corev1.ServicePortArray{&corev1.ServicePortArgs{Name: pulumi.String("postgres"), Port: pulumi.Int(5432), TargetPort: pulumi.Int(5432)}},
		},
	}, pulumi.Provider(cluster.Provider),
		// These addresses are allocated by Kubernetes. Preserve them when adopting
		// an existing Service instead of replacing it to remove imported defaults.
		pulumi.IgnoreChanges([]string{"spec.clusterIP", "spec.clusterIPs"}))
	if err != nil {
		return fmt.Errorf("failed to create backup database service: %w", err)
	}
	schedule, keepDaily := "46 4 * * *", "28"
	if environment == "local" || environment == "dev" {
		schedule, keepDaily = "* * * * *", "1"
	}
	values := map[string]string{
		"__ENVIRONMENT__":        environment,
		"__SCHEDULE__":           schedule,
		"__KEEP_DAILY__":         keepDaily,
		"__REPOSITORY__":         fmt.Sprintf("s3:%s/%s/postgres-custom-v1", storage.Endpoint, storage.BucketName),
		"__PASSWORD_SECRET__":    "k8up-repo-password",
		"__CREDENTIALS_SECRET__": "k8up-backup-credentials",
	}
	replacements := make([]string, 0, len(values)*2)
	for key, value := range values {
		encoded, _ := json.Marshal(value)
		replacements = append(replacements, `"`+key+`"`, string(encoded))
	}
	manifest := strings.NewReplacer(replacements...).Replace(postgresBackupJobTemplate)
	_, err = yaml.NewConfigGroup(ctx, "postgres-backup-job", &yaml.ConfigGroupArgs{
		YAML: []string{manifest},
	}, pulumi.Provider(cluster.Provider), pulumi.DependsOn([]pulumi.Resource{password, storage.Credentials, service}))
	return err
}
