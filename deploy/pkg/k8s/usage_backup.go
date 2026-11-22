package k8s

import (
	batchv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/batch/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/usagecollector"
)

const (
	usageBackupWorkload = "registry-usage-history-backup"
	// The time of the last backup that was stored and checked in full. The
	// "Usage history backup missed" rule in usage/alerting/rules.yaml reads it.
	usageBackupMetric = "registry_usage_backup_last_success_timestamp_seconds"

	// The same two steps as the registry's backup (postgres_backup_job.json):
	// an archive that has been read back in full, then Restic. The table data
	// is left uncompressed so that Restic stores only what changed.
	usageBackupDump = `umask 077
for attempt in $(seq 1 30); do
  if pg_isready -q; then break; fi
  sleep 10
done
if ! pg_isready; then
  echo REGISTRY_USAGE_BACKUP_FAILED: database unavailable >&2
  exit 1
fi
pg_dump --format=custom --compress=0 --lock-wait-timeout=30s --file=/backup/registry-usage.dump
pg_restore --file=/dev/null /backup/registry-usage.dump
cd /backup
sha256sum registry-usage.dump > registry-usage.dump.sha256
echo REGISTRY_USAGE_ARCHIVE_VALIDATED`

	// Unlike the registry's job this one creates its repository when there is
	// none, so that switching the collector on needs no step by hand. Every
	// snapshot holds the whole history, so a repository started afresh loses
	// only older restore points, and says so. Restic refuses to initialise
	// where a repository already is.
	//
	// The last step tells VictoriaMetrics that a backup succeeded, which is
	// what the alert rule watches. A backup that cannot report is still a
	// backup, so that step does not fail the job; the alert then fires and the
	// log says why.
	usageBackupUpload = `cd /backup
sha256sum -c registry-usage.dump.sha256
if ! restic cat config > /dev/null 2>&1; then
  restic init
  echo REGISTRY_USAGE_BACKUP_REPOSITORY_INITIALIZED
fi
restic backup --host registry-usage --tag usage-history /backup
restic check --read-data
restic forget --prune --keep-daily "$KEEP_DAILY" --group-by host,paths,tags
echo REGISTRY_USAGE_BACKUP_SUCCESS
if ! printf '%s %s\n' "$SUCCESS_METRIC" "$(date +%s)" | curl --fail --silent --show-error --max-time 10 \
    --retry 3 --retry-delay 5 --retry-connrefused --data-binary @- "$METRICS_URL/api/v1/import/prometheus"; then
  echo REGISTRY_USAGE_BACKUP_SUCCESS_NOT_REPORTED >&2
fi`
)

// deployUsageBackup backs up the usage history every day to a bucket of its
// own. The archive is made by the role that owns the usage database, in
// read-only transactions; that role has no access to Grafana's own database
// in the same cluster. The bucket is reached with the collector's Google key.
//
// The backups are kept when this job is removed.
func deployUsageBackup(ctx *pulumi.Context, namespace pulumi.StringPtrInput, environment string, owner *usageRole, google *corev1.Secret,
	priority pulumi.StringPtrInput, options ...pulumi.ResourceOption) error {
	variable := func(name, value string) corev1.EnvVarInput {
		return &corev1.EnvVarArgs{Name: pulumi.String(name), Value: pulumi.String(value)}
	}
	fromOwner := func(name, key string) corev1.EnvVarInput {
		return &corev1.EnvVarArgs{Name: pulumi.String(name), ValueFrom: &corev1.EnvVarSourceArgs{
			SecretKeyRef: &corev1.SecretKeySelectorArgs{Name: owner.secret.Metadata.Name(), Key: pulumi.String(key)},
		}}
	}
	unprivileged := &corev1.SecurityContextArgs{
		AllowPrivilegeEscalation: pulumi.Bool(false),
		Capabilities:             &corev1.CapabilitiesArgs{Drop: pulumi.StringArray{pulumi.String("ALL")}},
	}
	resources := &corev1.ResourceRequirementsArgs{
		Requests: pulumi.StringMap{"cpu": pulumi.String("50m"), "memory": pulumi.String("128Mi")},
		Limits:   pulumi.StringMap{"cpu": pulumi.String("500m"), "memory": pulumi.String("512Mi")},
	}
	shell := pulumi.StringArray{pulumi.String("/bin/sh"), pulumi.String("-ec")}
	labels := pulumi.StringMap{"app": pulumi.String(usageBackupWorkload)}
	_, err := batchv1.NewCronJob(ctx, usageBackupWorkload, &batchv1.CronJobArgs{
		Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String(usageBackupWorkload), Namespace: namespace, Labels: labels},
		Spec: &batchv1.CronJobSpecArgs{
			// After the collector's run at 01:15 UTC, which takes minutes.
			Schedule:                   pulumi.String("30 3 * * *"),
			TimeZone:                   pulumi.String("UTC"),
			ConcurrencyPolicy:          pulumi.String("Forbid"),
			StartingDeadlineSeconds:    pulumi.Int(3600),
			SuccessfulJobsHistoryLimit: pulumi.Int(3),
			FailedJobsHistoryLimit:     pulumi.Int(3),
			JobTemplate: &batchv1.JobTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{Labels: labels},
				Spec: &batchv1.JobSpecArgs{
					BackoffLimit:          pulumi.Int(1),
					ActiveDeadlineSeconds: pulumi.Int(1800),
					Template: &corev1.PodTemplateSpecArgs{
						Metadata: &metav1.ObjectMetaArgs{Labels: labels},
						Spec: &corev1.PodSpecArgs{
							RestartPolicy:                pulumi.String("Never"),
							AutomountServiceAccountToken: pulumi.Bool(false),
							PriorityClassName:            priority,
							NodeSelector:                 workerNodeSelector(ctx),
							SecurityContext: &corev1.PodSecurityContextArgs{
								RunAsNonRoot: pulumi.Bool(true),
								RunAsUser:    pulumi.Int(65532),
								RunAsGroup:   pulumi.Int(65532),
								FsGroup:      pulumi.Int(65532),
							},
							InitContainers: corev1.ContainerArray{&corev1.ContainerArgs{
								Name:    pulumi.String("pg-dump"),
								Image:   pulumi.String("postgres:17"),
								Command: shell,
								Args:    pulumi.StringArray{pulumi.String(usageBackupDump)},
								Env: corev1.EnvVarArray{
									variable("PGHOST", "grafana-pg-rw"),
									variable("PGPORT", "5432"),
									variable("PGDATABASE", usageDatabase),
									variable("PGCONNECT_TIMEOUT", "30"),
									variable("PGSSLMODE", "require"),
									variable("PGOPTIONS", "-c default_transaction_read_only=on"),
									fromOwner("PGUSER", "username"),
									fromOwner("PGPASSWORD", "password"),
								},
								VolumeMounts:    corev1.VolumeMountArray{&corev1.VolumeMountArgs{Name: pulumi.String("archive"), MountPath: pulumi.String("/backup")}},
								Resources:       resources,
								SecurityContext: unprivileged,
							}},
							Containers: corev1.ContainerArray{&corev1.ContainerArgs{
								Name:    pulumi.String("restic"),
								Image:   pulumi.String("ghcr.io/k8up-io/k8up:v2.12.0"),
								Command: shell,
								Args:    pulumi.StringArray{pulumi.String(usageBackupUpload)},
								Env: corev1.EnvVarArray{
									variable("RESTIC_REPOSITORY", "gs:"+usagecollector.BackupBucket(environment)+":/"+usagecollector.BackupRepository),
									variable("RESTIC_PASSWORD", usagecollector.BackupPassword),
									variable("RESTIC_CACHE_DIR", "/tmp/restic-cache"),
									variable("GOOGLE_PROJECT_ID", config.New(ctx, "gcp").Get("project")),
									variable("GOOGLE_APPLICATION_CREDENTIALS", usageGoogleKeyPath+"/key.json"),
									variable("HOME", "/tmp"),
									// As for the registry. Each snapshot is the whole history;
									// older ones only serve to undo damage found late.
									variable("KEEP_DAILY", "28"),
									variable("METRICS_URL", usageMetricsURL),
									variable("SUCCESS_METRIC", usageBackupMetric),
								},
								VolumeMounts: corev1.VolumeMountArray{
									&corev1.VolumeMountArgs{Name: pulumi.String("archive"), MountPath: pulumi.String("/backup"), ReadOnly: pulumi.Bool(true)},
									&corev1.VolumeMountArgs{Name: pulumi.String("google"), MountPath: pulumi.String(usageGoogleKeyPath), ReadOnly: pulumi.Bool(true)},
								},
								Resources:       resources,
								SecurityContext: unprivileged,
							}},
							Volumes: corev1.VolumeArray{
								// Must hold the uncompressed archive.
								&corev1.VolumeArgs{Name: pulumi.String("archive"), EmptyDir: &corev1.EmptyDirVolumeSourceArgs{SizeLimit: pulumi.String("4Gi")}},
								&corev1.VolumeArgs{Name: pulumi.String("google"), Secret: &corev1.SecretVolumeSourceArgs{
									SecretName:  google.Metadata.Name(),
									DefaultMode: pulumi.Int(0o440),
								}},
							},
						},
					},
				},
			},
		},
	}, options...)
	return err
}
