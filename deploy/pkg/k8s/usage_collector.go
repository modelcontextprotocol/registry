package k8s

import (
	"errors"
	"strings"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	batchv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/batch/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	schedulingv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/scheduling/v1"
	"github.com/pulumi/pulumi-random/sdk/v4/go/random"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/k8s/gatewaycanary"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/usagecollector"
)

const (
	// The collector's history: its own database on the grafana-pg cluster.
	usageDatabase = "registry_usage"
	// Owns that database and everything in it.
	usageCollectorRole = "registry_usage_collector"
	// Grafana's role. The collector grants it read access each time it starts.
	usageGrafanaRole = "registry_usage_grafana"
	// Reads published versions from the registry database.
	usageRegistryRole = "registry_usage_reader"

	usageImage         = "ghcr.io/modelcontextprotocol/registry-usage-collector"
	usageWorkload      = "registry-usage-collector"
	usagePriorityClass = "registry-usage-batch"
	usageCachePath     = "/var/cache/usage-collector"
	usageGoogleKeyPath = "/var/run/secrets/google"
)

// usageCollectorImage follows the registry's own image: the main build in
// staging, the configured release in production.
func usageCollectorImage(conf *config.Config, environment string) string {
	if environment == "prod" {
		return usageImage + ":" + conf.Require("imageTag")
	}
	return usageImage + ":main"
}

// usageRole is a login role in a CNPG cluster, and the Secret with its password.
type usageRole struct {
	password pulumi.StringOutput
	secret   *corev1.Secret
	resource pulumi.Resource
}

// usageDatabaseRole creates a login role with a random password in a CNPG
// cluster. The role outlives its resource, so that retiring the collector
// cannot take the role's objects with it.
func usageDatabaseRole(ctx *pulumi.Context, provider *kubernetes.Provider, namespace pulumi.StringPtrInput, cluster, role string,
	connections int, memberOf []any, dependencies []pulumi.Resource) (*usageRole, error) {
	name := strings.ReplaceAll(role, "_", "-")
	password, err := random.NewRandomPassword(ctx, name+"-password", &random.RandomPasswordArgs{
		Length: pulumi.Int(32),
		// Letters and digits only: the password is written into connection URLs.
		Special: pulumi.Bool(false),
		Keepers: pulumi.StringMap{"generation": pulumi.String(usagecollector.CredentialGeneration)},
	})
	if err != nil {
		return nil, err
	}
	// CNPG reads a role's password from a basic-auth Secret beside the cluster.
	secret, err := corev1.NewSecret(ctx, name+"-password", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(name + "-password"),
			Namespace: namespace,
			Labels:    pulumi.StringMap{"cnpg.io/reload": pulumi.String("true")},
		},
		Type:       pulumi.String("kubernetes.io/basic-auth"),
		StringData: pulumi.StringMap{"username": pulumi.String(role), "password": password.Result},
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, err
	}
	spec := map[string]any{
		"cluster":                   map[string]any{"name": cluster},
		"name":                      role,
		"login":                     true,
		"connectionLimit":           connections,
		"passwordSecret":            map[string]any{"name": name + "-password"},
		"databaseRoleReclaimPolicy": "retain",
	}
	if len(memberOf) > 0 {
		spec["inRoles"] = memberOf
	}
	resource, err := apiextensions.NewCustomResource(ctx, name, &apiextensions.CustomResourceArgs{
		ApiVersion:  pulumi.String("postgresql.cnpg.io/v1"),
		Kind:        pulumi.String("DatabaseRole"),
		Metadata:    &metav1.ObjectMetaArgs{Name: pulumi.String(name), Namespace: namespace},
		OtherFields: map[string]any{"spec": spec},
	}, pulumi.Provider(provider), pulumi.DependsOn(append([]pulumi.Resource{secret}, dependencies...)))
	if err != nil {
		return nil, err
	}
	return &usageRole{password: password.Result, secret: secret, resource: resource}, nil
}

// deployUsageCollector runs the daily usage collector and returns what Grafana
// needs to show its results. It returns nil where the collector is not
// configured, and only Grafana's clean-up where it has been retired.
//
// Nothing here is awaited: a collector that cannot start or finish never holds
// up a deployment.
func deployUsageCollector(ctx *pulumi.Context, cluster *providers.ProviderInfo, ns *corev1.Namespace, environment string,
	grafanaPg, registryPg *apiextensions.CustomResource) (*usageGrafana, error) {
	mode, err := usagecollector.Mode(ctx)
	if err != nil || mode == usagecollector.Absent {
		return nil, err
	}
	if mode == usagecollector.Retired {
		return retiredUsageGrafana(), nil
	}
	if cluster.UsageCollectorKey.OutputState == nil {
		return nil, errors.New("the cluster provider supplied no Google credential for the usage collector")
	}
	aliases, err := usagecollector.Aliases(ctx)
	if err != nil {
		return nil, err
	}
	provider := pulumi.Provider(cluster.Provider)
	namespace := ns.Metadata.Name()
	var onGrafanaPg, onRegistryPg []pulumi.Resource
	if grafanaPg != nil {
		onGrafanaPg = append(onGrafanaPg, grafanaPg)
	}
	if registryPg != nil {
		onRegistryPg = append(onRegistryPg, registryPg)
	}

	// The collector's pool holds four connections, Grafana's data source five.
	collectorRole, err := usageDatabaseRole(ctx, cluster.Provider, namespace, "grafana-pg", usageCollectorRole, 6, nil, onGrafanaPg)
	if err != nil {
		return nil, err
	}
	grafanaRole, err := usageDatabaseRole(ctx, cluster.Provider, namespace, "grafana-pg", usageGrafanaRole, 10, nil, onGrafanaPg)
	if err != nil {
		return nil, err
	}
	// pg_read_all_data lets this role read every table of the registry database
	// and write none. Only the table's owner could grant SELECT on servers
	// alone, and the weekly staging restore would drop that grant.
	registryRole, err := usageDatabaseRole(ctx, cluster.Provider, pulumi.String("default"), "registry-pg", usageRegistryRole, 2,
		[]any{"pg_read_all_data"}, onRegistryPg)
	if err != nil {
		return nil, err
	}
	database, err := apiextensions.NewCustomResource(ctx, "registry-usage-database", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("postgresql.cnpg.io/v1"),
		Kind:       pulumi.String("Database"),
		Metadata:   &metav1.ObjectMetaArgs{Name: pulumi.String("registry-usage"), Namespace: namespace},
		OtherFields: map[string]any{"spec": map[string]any{
			"cluster": map[string]any{"name": "grafana-pg"},
			"name":    usageDatabase,
			// As its owner the collector may create its tables in the public schema.
			"owner": usageCollectorRole,
			// The history is kept when the collector is retired.
			"databaseReclaimPolicy": "retain",
		}},
	}, provider, pulumi.DependsOn([]pulumi.Resource{collectorRole.resource}))
	if err != nil {
		return nil, err
	}

	// The collector holds a session-level lock, so both URLs go straight to the
	// primary. Each key is the name of the setting it supplies.
	settings := pulumi.StringMap{
		"USAGE_COLLECTOR_DATABASE_URL": pulumi.Sprintf("postgres://%s:%s@grafana-pg-rw:5432/%s?sslmode=require",
			usageCollectorRole, collectorRole.password, usageDatabase),
		"USAGE_COLLECTOR_REGISTRY_DATABASE_URL": pulumi.Sprintf("postgres://%s:%s@registry-pg-rw.default.svc:5432/app?sslmode=require",
			usageRegistryRole, registryRole.password),
	}
	if aliases != "" {
		settings["USAGE_COLLECTOR_ORGANIZATION_ALIASES"] = pulumi.ToSecret(pulumi.String(aliases)).(pulumi.StringOutput)
	}
	settingsSecret, err := corev1.NewSecret(ctx, usageWorkload, &corev1.SecretArgs{
		Metadata:   &metav1.ObjectMetaArgs{Name: pulumi.String(usageWorkload), Namespace: namespace},
		Type:       pulumi.String("Opaque"),
		StringData: settings,
	}, provider)
	if err != nil {
		return nil, err
	}
	googleSecret, err := corev1.NewSecret(ctx, usageWorkload+"-gcp", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String(usageWorkload + "-gcp"), Namespace: namespace},
		Type:     pulumi.String("Opaque"),
		Data:     pulumi.StringMap{"key.json": cluster.UsageCollectorKey},
	}, provider)
	if err != nil {
		return nil, err
	}

	// Any ordinary pod that needs the room evicts the collector, and the
	// cluster autoscaler neither adds a node for it nor keeps one because of it.
	priority, err := schedulingv1.NewPriorityClass(ctx, usagePriorityClass, &schedulingv1.PriorityClassArgs{
		Metadata:         &metav1.ObjectMetaArgs{Name: pulumi.String(usagePriorityClass)},
		Value:            pulumi.Int(-100),
		PreemptionPolicy: pulumi.String("Never"),
		GlobalDefault:    pulumi.Bool(false),
		Description:      pulumi.String("Daily usage collection: runs in spare capacity and gives way to everything else."),
	}, provider)
	if err != nil {
		return nil, err
	}
	// Keeps the two public network reference files between runs; without a copy
	// of them a day is not collected. They need about 30 MB; 10Gi is the
	// smallest disk GKE's default storage class provisions. The claim binds
	// when the first run starts, so waiting for it would never end.
	cache, err := corev1.NewPersistentVolumeClaim(ctx, usageWorkload+"-cache", &corev1.PersistentVolumeClaimArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:        pulumi.String(usageWorkload + "-cache"),
			Namespace:   namespace,
			Annotations: pulumi.StringMap{"pulumi.com/skipAwait": pulumi.String("true")},
		},
		Spec: &corev1.PersistentVolumeClaimSpecArgs{
			AccessModes: pulumi.StringArray{pulumi.String("ReadWriteOnce")},
			Resources:   &corev1.VolumeResourceRequirementsArgs{Requests: pulumi.StringMap{"storage": pulumi.String("10Gi")}},
		},
	}, provider)
	if err != nil {
		return nil, err
	}

	setting := func(name, value string) corev1.EnvVarInput {
		return &corev1.EnvVarArgs{Name: pulumi.String(name), Value: pulumi.String(value)}
	}
	labels := pulumi.StringMap{"app": pulumi.String(usageWorkload)}
	_, err = batchv1.NewCronJob(ctx, usageWorkload, &batchv1.CronJobArgs{
		Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String(usageWorkload), Namespace: namespace, Labels: labels},
		Spec: &batchv1.CronJobSpecArgs{
			// A day can be collected 15 minutes after it ends. A run looks 13
			// days back, so a missed or failed one is made up by the next.
			Schedule:                   pulumi.String("15 1 * * *"),
			TimeZone:                   pulumi.String("UTC"),
			ConcurrencyPolicy:          pulumi.String("Forbid"),
			StartingDeadlineSeconds:    pulumi.Int(6 * 3600),
			SuccessfulJobsHistoryLimit: pulumi.Int(3),
			FailedJobsHistoryLimit:     pulumi.Int(3),
			JobTemplate: &batchv1.JobTemplateSpecArgs{
				Spec: &batchv1.JobSpecArgs{
					// The first run reads 13 days, about an hour. Days are stored one
					// by one, so a run stopped here keeps what it finished.
					ActiveDeadlineSeconds: pulumi.Int(3 * 3600),
					// A run that fails is not repeated: it would read the same logs
					// again, and tomorrow's run collects what is missing. A pod that
					// was evicted or preempted is replaced.
					BackoffLimit: pulumi.Int(0),
					PodFailurePolicy: &batchv1.PodFailurePolicyArgs{Rules: batchv1.PodFailurePolicyRuleArray{
						&batchv1.PodFailurePolicyRuleArgs{
							Action: pulumi.String("Ignore"),
							OnPodConditions: batchv1.PodFailurePolicyOnPodConditionsPatternArray{
								&batchv1.PodFailurePolicyOnPodConditionsPatternArgs{Type: pulumi.String("DisruptionTarget"), Status: pulumi.String("True")},
							},
						},
					}},
					Template: &corev1.PodTemplateSpecArgs{
						Metadata: &metav1.ObjectMetaArgs{Labels: labels},
						Spec: &corev1.PodSpecArgs{
							RestartPolicy:                pulumi.String("Never"),
							AutomountServiceAccountToken: pulumi.Bool(false),
							PriorityClassName:            priority.Metadata.Name(),
							// Never on the database's node.
							NodeSelector: workerNodeSelector(ctx),
							SecurityContext: &corev1.PodSecurityContextArgs{
								RunAsNonRoot:   pulumi.Bool(true),
								RunAsUser:      pulumi.Int(65532),
								RunAsGroup:     pulumi.Int(65532),
								FsGroup:        pulumi.Int(65532),
								SeccompProfile: &corev1.SeccompProfileArgs{Type: pulumi.String("RuntimeDefault")},
							},
							Containers: corev1.ContainerArray{&corev1.ContainerArgs{
								Name:  pulumi.String("usage-collector"),
								Image: pulumi.String(usageCollectorImage(config.New(ctx, "mcp-registry"), environment)),
								// The staging tag moves with every build.
								ImagePullPolicy: pulumi.String("Always"),
								Env: corev1.EnvVarArray{
									setting("USAGE_COLLECTOR_GCP_PROJECT", config.New(ctx, "gcp").Get("project")),
									setting("USAGE_COLLECTOR_CLUSTER_NAME", "mcp-registry-"+environment),
									// The Gateway's data plane writes the access records.
									setting("USAGE_COLLECTOR_LOG_NAMESPACE", gatewaycanary.EdgeNamespace),
									setting("USAGE_COLLECTOR_LOG_CONTAINER", "nginx"),
									setting("USAGE_COLLECTOR_METRICS_URL", "http://victoria-metrics-victoria-metrics-single-server:8428"),
									setting("USAGE_COLLECTOR_READER_ROLE", usageGrafanaRole),
									setting("USAGE_COLLECTOR_CACHE_DIR", usageCachePath),
									setting("GOOGLE_APPLICATION_CREDENTIALS", usageGoogleKeyPath+"/key.json"),
									// Go collects garbage harder before the limit is reached.
									setting("GOMEMLIMIT", "900MiB"),
								},
								EnvFrom: corev1.EnvFromSourceArray{&corev1.EnvFromSourceArgs{
									SecretRef: &corev1.SecretEnvSourceArgs{Name: settingsSecret.Metadata.Name()},
								}},
								// Measured: about 420 MB at peak and well under one core. The
								// request is small so that the run fits beside staging's workloads.
								Resources: &corev1.ResourceRequirementsArgs{
									Requests: pulumi.StringMap{"cpu": pulumi.String("50m"), "memory": pulumi.String("512Mi")},
									Limits:   pulumi.StringMap{"cpu": pulumi.String("1"), "memory": pulumi.String("1Gi")},
								},
								SecurityContext: &corev1.SecurityContextArgs{
									AllowPrivilegeEscalation: pulumi.Bool(false),
									ReadOnlyRootFilesystem:   pulumi.Bool(true),
									Capabilities:             &corev1.CapabilitiesArgs{Drop: pulumi.StringArray{pulumi.String("ALL")}},
								},
								VolumeMounts: corev1.VolumeMountArray{
									&corev1.VolumeMountArgs{Name: pulumi.String("cache"), MountPath: pulumi.String(usageCachePath)},
									&corev1.VolumeMountArgs{Name: pulumi.String("google"), MountPath: pulumi.String(usageGoogleKeyPath), ReadOnly: pulumi.Bool(true)},
								},
							}},
							Volumes: corev1.VolumeArray{
								&corev1.VolumeArgs{Name: pulumi.String("cache"), PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSourceArgs{
									ClaimName: cache.Metadata.Name().Elem(),
								}},
								&corev1.VolumeArgs{Name: pulumi.String("google"), Secret: &corev1.SecretVolumeSourceArgs{
									SecretName:  googleSecret.Metadata.Name(),
									DefaultMode: pulumi.Int(0o440),
								}},
							},
						},
					},
				},
			},
		},
	}, provider, pulumi.DependsOn([]pulumi.Resource{database, grafanaRole.resource, registryRole.resource}))
	if err != nil {
		return nil, err
	}

	return enabledUsageGrafana(ctx, cluster.Provider, ns, grafanaRole.secret)
}
