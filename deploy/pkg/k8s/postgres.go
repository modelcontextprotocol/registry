package k8s

import (
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// DeployPostgresDatabases deploys the CloudNative PostgreSQL operator and PostgreSQL cluster
func DeployPostgresDatabases(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string) (*apiextensions.CustomResource, error) {
	// Create cnpg-system namespace
	cnpgNamespace, err := corev1.NewNamespace(ctx, "cnpg-system", &corev1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("cnpg-system"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return nil, err
	}

	// Install cloudnative-pg Helm chart
	cloudNativePG, err := helm.NewChart(ctx, "cloudnative-pg", helm.ChartArgs{
		Chart:   pulumi.String("cloudnative-pg"),
		Version: pulumi.String("v0.26.0"),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://cloudnative-pg.github.io/charts"),
		},
		Namespace: cnpgNamespace.Metadata.Name().Elem(),
		Values: pulumi.Map{
			"webhooks": pulumi.Map{
				"replicaCount": pulumi.Int(1),
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return nil, err
	}

	// Create PostgreSQL cluster
	parameters := map[string]any{
		"pg_stat_statements.track": "all",
		"max_connections":          "200",
	}
	// Leave the PostgreSQL default in place unless this stack opts in. Changing
	// shared_buffers requires a restart, including downtime for our singleton.
	if sharedBuffers := config.New(ctx, "mcp-registry").Get("postgresSharedBuffers"); sharedBuffers != "" {
		parameters["shared_buffers"] = sharedBuffers
	}
	// Keep existing stacks unchanged unless they opt into a tested CPU budget.
	// This changes the container quota, not the node count or machine type.
	cpuLimit := config.New(ctx, "mcp-registry").Get("postgresCPULimit")
	if cpuLimit == "" {
		cpuLimit = "1500m"
	}

	pgCluster, err := apiextensions.NewCustomResource(ctx, "registry-pg", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("postgresql.cnpg.io/v1"),
		Kind:       pulumi.String("Cluster"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String("registry-pg"),
			Namespace: pulumi.String("default"),
			Labels: pulumi.StringMap{
				"app":         pulumi.String("registry-pg"),
				"environment": pulumi.String(environment),
			},
		},
		OtherFields: map[string]any{
			"spec": map[string]any{
				"instances": 1,
				"enablePDB": false,
				"storage": map[string]any{
					"size": "50Gi",
				},
				// Bound memory use, but budget shared buffers, backend/query memory
				// and file cache together when tuning. The request is a scheduling
				// reservation, not a guarantee of headroom up to the 4Gi limit.
				"resources": map[string]any{
					"requests": map[string]any{
						"memory": "512Mi",
						"cpu":    "200m",
					},
					"limits": map[string]any{
						"memory": "4Gi",
						"cpu":    cpuLimit,
					},
				},
				// Enable pg_stat_statements so we can attribute slow time to specific
				// queries (the 2026-04-27 incident took an EXPLAIN-the-one-query-I-saw
				// approach because we had no aggregate visibility). CNPG triggers a PG
				// restart on shared_preload_libraries change — with instances: 1 this
				// is brief downtime. CREATE EXTENSION still needs to run once as a
				// superuser on existing clusters; new clusters get it via the
				// postInitApplicationSQL hook below.
				//
				// max_connections raised from default 100 to 200 because the registry
				// pgxpool was bumped from 30 to 60 per pod (120 total across 2 pods)
				// + ~10 reserved for PG internals and ad-hoc admin = 130, with 70
				// headroom for future scale or burst.
				"postgresql": map[string]any{
					"shared_preload_libraries": []any{"pg_stat_statements"},
					"parameters":               parameters,
				},
				"bootstrap": map[string]any{
					"initdb": map[string]any{
						"postInitApplicationSQL": []any{
							"CREATE EXTENSION IF NOT EXISTS pg_stat_statements",
						},
					},
				},
			},
		},
	}, pulumi.Provider(cluster.Provider), pulumi.DependsOnInputs(cloudNativePG.Ready), pulumi.RetainOnDelete(true))
	if err != nil {
		return nil, err
	}

	return pgCluster, nil
}
