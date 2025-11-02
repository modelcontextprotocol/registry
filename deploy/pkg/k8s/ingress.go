package k8s

import (
	"strings"

	v1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// SetupIngressController sets up the NGINX Ingress Controller
func SetupIngressController(ctx *pulumi.Context, cluster *providers.ProviderInfo, environment string) (*helm.Chart, error) {
	conf := config.New(ctx, "mcp-registry")
	provider := conf.Get("provider")
	if provider == "" {
		provider = "local"
	}

	// Create namespace for ingress-nginx
	ingressNginxNamespace, err := v1.NewNamespace(ctx, "ingress-nginx", &v1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("ingress-nginx"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return nil, err
	}

	// Usually we should expose the ingress to a LoadBalancer
	// This works in GCP and most local setups e.g. minikube (with minikube tunnel)
	// Kind unfortunately does not support LoadBalancer type, and hangs indefinitely. This is a workaround for that.
	serviceType := cluster.Name.ApplyT(func(name string) string {
		if name == "kind-kind" {
			return "NodePort"
		}
		return "LoadBalancer"
	}).(pulumi.StringOutput)

	// Install NGINX Ingress Controller
	ingressNginx, err := helm.NewChart(ctx, "ingress-nginx", helm.ChartArgs{
		Chart:   pulumi.String("ingress-nginx"),
		Version: pulumi.String("4.13.0"),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://kubernetes.github.io/ingress-nginx"),
		},
		Namespace: ingressNginxNamespace.Metadata.Name().Elem(),
		Values: pulumi.Map{
			"controller": pulumi.Map{
				"service": pulumi.Map{
					"type":        serviceType,
					"annotations": pulumi.Map{},
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
		return nil, err
	}

	// Extract ingress IPs from the Helm chart's controller service
	ingressIps := ingressNginx.Resources.ApplyT(func(resources interface{}) interface{} {
		// Look for the ingress-nginx-controller service
		resourceMap := resources.(map[string]pulumi.Resource)
		for resourceName, resource := range resourceMap {
			if strings.Contains(resourceName, "ingress-nginx-controller") &&
				!strings.Contains(resourceName, "admission") &&
				strings.Contains(resourceName, "Service") {
				if svc, ok := resource.(*v1.Service); ok {
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
				}
			}
		}
		// Return empty array if no matching service found
		return []string{}
	})
	ctx.Export("ingressIps", ingressIps)

	return ingressNginx, nil
}
