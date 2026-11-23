package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/organizations"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/projects"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/storage"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/usagecollector"
)

// Keep the bucket and legacy resource identities stable throughout the migration.
func usageCollectorAccess(ctx *pulumi.Context, projectID, environment string, provider *gcp.Provider, cluster ...pulumi.Resource) (*providers.UsageAccess, error) {
	var opts []pulumi.ResourceOption
	var invokeOpts []pulumi.InvokeOption
	if provider != nil {
		opts = append(opts, pulumi.Provider(provider))
		invokeOpts = append(invokeOpts, pulumi.Provider(provider))
	}
	mode, err := usagecollector.Mode(ctx)
	if err != nil || mode == usagecollector.Absent {
		return nil, err
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
		return nil, err
	}
	if mode != usagecollector.Enabled {
		return nil, nil
	}

	grantLogs := func(name, member string, options []pulumi.ResourceOption) (*projects.IAMMember, error) {
		return projects.NewIAMMember(ctx, name, &projects.IAMMemberArgs{
			Project: pulumi.String(projectID), Role: pulumi.String("roles/logging.viewer"), Member: pulumi.String(member),
		}, options...)
	}
	grantBucket := func(name, role, member string, options []pulumi.ResourceOption) (*storage.BucketIAMMember, error) {
		return storage.NewBucketIAMMember(ctx, name, &storage.BucketIAMMemberArgs{
			Bucket: bucket.Name, Role: pulumi.String(role), Member: pulumi.String(member),
		}, options...)
	}

	access := &providers.UsageAccess{}
	phase := usagecollector.Identity(ctx)
	if phase != usagecollector.Legacy {
		project, err := organizations.LookupProject(ctx, &organizations.LookupProjectArgs{ProjectId: &projectID}, invokeOpts...)
		if err != nil {
			return nil, err
		}
		prefix := fmt.Sprintf("principal://iam.googleapis.com/projects/%s/locations/global/workloadIdentityPools/%s.svc.id.goog/subject/ns/%s/sa/",
			project.Number, projectID, usagecollector.MonitoringNamespace)
		federatedOpts := append([]pulumi.ResourceOption{}, opts...)
		federatedOpts = append(federatedOpts, pulumi.DependsOn(cluster))
		logs, err := grantLogs("usage-collector-log-viewer-ksa", prefix+usagecollector.CollectorAccount, federatedOpts)
		if err != nil {
			return nil, err
		}
		objects, err := grantBucket("usage-backup-objects-ksa", "roles/storage.objectUser", prefix+usagecollector.BackupAccount, federatedOpts)
		if err != nil {
			return nil, err
		}
		access.Grants = []pulumi.Resource{logs, objects}
	}
	if phase == usagecollector.Keyless {
		return access, nil
	}
	email := usagecollector.LegacyServiceAccount + "@" + projectID + ".iam.gserviceaccount.com"
	member := "serviceAccount:" + email
	if _, err := grantLogs("usage-collector-log-viewer", member, opts); err != nil {
		return nil, err
	}
	if _, err := grantBucket("usage-backup-objects", "roles/storage.objectAdmin", member, opts); err != nil {
		return nil, err
	}
	if _, err := grantBucket("usage-backup-bucket", "roles/storage.legacyBucketReader", member, opts); err != nil {
		return nil, err
	}
	key, err := serviceaccount.NewKey(ctx, "usage-collector-key", &serviceaccount.KeyArgs{
		ServiceAccountId: pulumi.String("projects/" + projectID + "/serviceAccounts/" + email),
		Keepers:          pulumi.StringMap{"generation": pulumi.String(usagecollector.CredentialGeneration)},
	}, opts...)
	if err != nil {
		return nil, err
	}
	access.LegacyKey = key.PrivateKey
	return access, nil
}
