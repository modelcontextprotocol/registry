package k8s

import (
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	v1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/k8s/gatewaycanary"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// SetupCertManager sets up cert-manager for TLS certificates
func SetupCertManager(ctx *pulumi.Context, cluster *providers.ProviderInfo) error {
	// Create namespace for cert-manager
	certManagerNamespace, err := v1.NewNamespace(ctx, "cert-manager", &v1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("cert-manager"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	values := pulumi.Map{
		"installCRDs": pulumi.Bool(true),
		"ingressShim": pulumi.Map{"defaultIssuerName": pulumi.String("letsencrypt-prod"), "defaultIssuerKind": pulumi.String("ClusterIssuer")},
	}
	if gatewaycanary.NativeCertificates(ctx) {
		// cert-manager 1.18 uses enableGatewayAPI (renamed in newer releases).
		values["config"] = pulumi.Map{"apiVersion": pulumi.String("controller.config.cert-manager.io/v1alpha1"), "kind": pulumi.String("ControllerConfiguration"), "enableGatewayAPI": pulumi.Bool(true)}
	}
	// Install cert-manager for TLS certificates
	certManager, err := helm.NewChart(ctx, "cert-manager", helm.ChartArgs{
		Chart:   pulumi.String("cert-manager"),
		Version: pulumi.String("v1.18.2"),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://charts.jetstack.io"),
		},
		Namespace: certManagerNamespace.Metadata.Name().Elem(),
		Values:    values,
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	http01 := pulumi.Map{"ingress": pulumi.Map{"ingressClassName": pulumi.String("nginx")}}
	if gatewaycanary.NativeCertificates(ctx) {
		http01 = pulumi.Map{"gatewayHTTPRoute": pulumi.Map{"parentRefs": pulumi.Array{pulumi.Any(gatewaycanary.ACMEParent())}}}
	}
	_, err = apiextensions.NewCustomResource(ctx, "letsencrypt-prod", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("cert-manager.io/v1"),
		Kind:       pulumi.String("ClusterIssuer"),
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("letsencrypt-prod"),
		},
		OtherFields: kubernetes.UntypedArgs{
			"spec": pulumi.Map{
				"acme": pulumi.Map{
					"server": pulumi.String("https://acme-v02.api.letsencrypt.org/directory"),
					"email":  pulumi.String("admin@modelcontextprotocol.io"),
					"privateKeySecretRef": pulumi.Map{
						"name": pulumi.String("letsencrypt-prod-key"),
					},
					"solvers": pulumi.Array{
						pulumi.Map{
							"http01": http01,
						},
					},
				},
			},
		},
	}, pulumi.Provider(cluster.Provider), pulumi.DependsOnInputs(certManager.Ready))
	if err != nil {
		return err
	}

	return nil
}
