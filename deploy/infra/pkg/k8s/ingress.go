package k8s

import (
	v1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// SetupIngressController sets up the NGINX Ingress Controller
func SetupIngressController(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string) error {
	conf := config.New(ctx, "mcp-registry")
	provider := conf.Get("provider")
	if provider == "" {
		provider = "local"
	}

	// Create namespace for ingress-nginx
	_, err := v1.NewNamespace(ctx, "ingress-nginx", &v1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("ingress-nginx"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Install NGINX Ingress Controller
	ingressType := "LoadBalancer"
	if provider == "local" {
		ingressType = "NodePort"
	}

	nginxIngress, err := helm.NewChart(ctx, "nginx-ingress", helm.ChartArgs{
		Chart:   pulumi.String("ingress-nginx"),
		Version: pulumi.String("4.13.0"),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://kubernetes.github.io/ingress-nginx"),
		},
		Namespace: pulumi.String("ingress-nginx"),
		Values: pulumi.Map{
			"controller": pulumi.Map{
				"service": pulumi.Map{
					"type": pulumi.String(ingressType),
				},
				"config": pulumi.Map{
					// Disable strict path validation, to work around a bug in ingress-nginx
					// https://cert-manager.io/docs/releases/release-notes/release-notes-1.18/#acme-http01-challenge-paths-now-use-pathtype-exact-in-ingress-routes
					// https://github.com/kubernetes/ingress-nginx/issues/11176
					"strict-validate-path-type": pulumi.String("false"),
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// TODO: this doesn't work
	ctx.Export("ingressIp", nginxIngress.GetResource("v1/Service", "nginx-ingress-ingress-nginx-controller", "ingress-nginx"))

	return nil
}
