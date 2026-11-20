package gcp

import (
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/projects"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/storage"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/usagecollector"
)

// usageCollectorAccess lets the usage collector read this project's logs and
// back up its history, and returns a key for it, as base64-encoded JSON. The
// account is only referred to, never managed, so that retiring the collector
// cannot delete it.
func usageCollectorAccess(ctx *pulumi.Context, projectID, environment string, opts []pulumi.ResourceOption) (pulumi.StringOutput, error) {
	mode, err := usagecollector.Mode(ctx)
	if err != nil || mode == usagecollector.Absent {
		return pulumi.StringOutput{}, err
	}
	// The bucket stays managed while the collector is retired, and is never
	// deleted: not when the setting is removed, and not by a destroy.
	bucketOpts := append(append([]pulumi.ResourceOption{}, opts...), pulumi.RetainOnDelete(true))
	bucket, err := storage.NewBucket(ctx, "usage-backup-bucket", &storage.BucketArgs{
		Name:                     pulumi.String(usagecollector.BackupBucket(environment)),
		Project:                  pulumi.String(projectID),
		Location:                 pulumi.String("US"),
		StorageClass:             pulumi.String("STANDARD"),
		UniformBucketLevelAccess: pulumi.Bool(true),
		PublicAccessPrevention:   pulumi.String("enforced"),
		Versioning:               &storage.BucketVersioningArgs{Enabled: pulumi.Bool(true)},
		// Only Restic may remove live repository objects, whatever their age.
		// This bounds the versions its pruning leaves behind.
		LifecycleRules: storage.BucketLifecycleRuleArray{
			&storage.BucketLifecycleRuleArgs{
				Action: &storage.BucketLifecycleRuleActionArgs{Type: pulumi.String("Delete")},
				Condition: &storage.BucketLifecycleRuleConditionArgs{
					WithState:               pulumi.String("ARCHIVED"),
					DaysSinceNoncurrentTime: pulumi.Int(30),
				},
			},
		},
		Labels: pulumi.StringMap{
			"environment": pulumi.String(environment),
			"purpose":     pulumi.String("usage-history-backups"),
		},
	}, bucketOpts...)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	if mode != usagecollector.Enabled {
		return pulumi.StringOutput{}, nil
	}

	email := usagecollector.ServiceAccount + "@" + projectID + ".iam.gserviceaccount.com"
	member := pulumi.String("serviceAccount:" + email)
	// Reading access records needs logging.logEntries.list on the project. This
	// role also reads the project's other logs, but not its data-access logs.
	_, err = projects.NewIAMMember(ctx, "usage-collector-log-viewer", &projects.IAMMemberArgs{
		Project: pulumi.String(projectID),
		Role:    pulumi.String("roles/logging.viewer"),
		Member:  member,
	}, opts...)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	// Restic writes, lists and prunes objects, and looks the bucket up first.
	// Both grants are on this one bucket.
	for name, role := range map[string]string{
		"usage-backup-objects": "roles/storage.objectAdmin",
		"usage-backup-bucket":  "roles/storage.legacyBucketReader",
	} {
		_, err = storage.NewBucketIAMMember(ctx, name, &storage.BucketIAMMemberArgs{
			Bucket: bucket.Name,
			Role:   pulumi.String(role),
			Member: member,
		}, opts...)
		if err != nil {
			return pulumi.StringOutput{}, err
		}
	}
	key, err := serviceaccount.NewKey(ctx, "usage-collector-key", &serviceaccount.KeyArgs{
		ServiceAccountId: pulumi.String("projects/" + projectID + "/serviceAccounts/" + email),
		Keepers:          pulumi.StringMap{"generation": pulumi.String(usagecollector.CredentialGeneration)},
	}, opts...)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	return key.PrivateKey, nil
}
