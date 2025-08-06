package k8s

import (
	v1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// SetupClusterInfrastructure sets up the basic cluster infrastructure (namespace, ingress, cert-manager)
func SetupClusterInfrastructure(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string) error {
	conf := config.New(ctx, "mcp-registry")
	provider := conf.Get("provider")
	if provider == "" {
		provider = "local"
	}

	// Create namespace for the environment
	_, err := v1.NewNamespace(ctx, environment, &v1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(environment),
			Labels: pulumi.StringMap{
				"environment": pulumi.String(environment),
				"managed-by":  pulumi.String("pulumi"),
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Create namespaces for helm charts
	_, err = v1.NewNamespace(ctx, "ingress-nginx", &v1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("ingress-nginx"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	_, err = v1.NewNamespace(ctx, "cert-manager", &v1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("cert-manager"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Install NGINX Ingress Controller
	ingressType := "LoadBalancer"
	if provider == "local" {
		ingressType = "NodePort" // Use NodePort for local clusters
	}

	_, err = helm.NewChart(ctx, "nginx-ingress", helm.ChartArgs{
		Chart:   pulumi.String("ingress-nginx"),
		Version: pulumi.String("4.7.1"),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://kubernetes.github.io/ingress-nginx"),
		},
		Namespace: pulumi.String("ingress-nginx"),
		Values: pulumi.Map{
			"controller": pulumi.Map{
				"service": pulumi.Map{
					"type": pulumi.String(ingressType),
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Install cert-manager for TLS certificates
	_, err = helm.NewChart(ctx, "cert-manager", helm.ChartArgs{
		Chart:   pulumi.String("cert-manager"),
		Version: pulumi.String("v1.12.2"),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://charts.jetstack.io"),
		},
		Namespace: pulumi.String("cert-manager"),
		Values: pulumi.Map{
			"installCRDs": pulumi.Bool(true),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	return nil
}
