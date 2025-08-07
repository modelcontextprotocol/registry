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
					// Add Azure Load Balancer health probe annotation as otherwise it defaults to / which fails
					"annotations": pulumi.Map{
						"service.beta.kubernetes.io/azure-load-balancer-health-probe-request-path": pulumi.String("/healthz"),
					},
				},
				"config": pulumi.Map{
					// Disable strict path validation, to work around a bug in ingress-nginx
					// https://cert-manager.io/docs/releases/release-notes/release-notes-1.18/#acme-http01-challenge-paths-now-use-pathtype-exact-in-ingress-routes
					// https://github.com/kubernetes/ingress-nginx/issues/11176
					"strict-validate-path-type": pulumi.String("false"),

					// Use forwarded headers for proper client IP handling
					"use-forwarded-headers": pulumi.String("true"),
				},
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Use the helm chart to get service information after deployment
	ingressIps := nginxIngress.Resources.ApplyT(func(resources interface{}) interface{} {
		if ctx.DryRun() {
			return []string{} // Return empty array on error during preview
		}

		// Look up the service after the chart is ready
		svc, err := v1.GetService(
			ctx,
			"nginx-ingress-controller-lookup",
			pulumi.ID("ingress-nginx/nginx-ingress-ingress-nginx-controller"),
			&v1.ServiceState{},
			pulumi.Provider(cluster.Provider),
			pulumi.DependsOn([]pulumi.Resource{nginxIngress}),
		)
		if err != nil {
			return []string{} // Return empty array on error during preview
		}

		// Return the LoadBalancer ingress IPs
		return svc.Status.LoadBalancer().Ingress().ApplyT(func(ingresses []v1.LoadBalancerIngress) []string {
			var ips []string
			for _, ingress := range ingresses {
				if ip := ingress.Ip; ip != nil && *ip != "" {
					ips = append(ips, *ip)
				}
			}
			return ips
		})
	})
	ctx.Export("ingressIps", ingressIps)

	return nil
}
