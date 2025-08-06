package k8s

import (
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	v1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
)

// SetupCertManager sets up cert-manager for TLS certificates
func SetupCertManager(ctx *pulumi.Context, cluster *providers.ProviderInfo) error {
	// Create namespace for cert-manager
	_, err := v1.NewNamespace(ctx, "cert-manager", &v1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String("cert-manager"),
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	// Install cert-manager for TLS certificates
	_, err = helm.NewChart(ctx, "cert-manager", helm.ChartArgs{
		Chart:   pulumi.String("cert-manager"),
		Version: pulumi.String("v1.18.2"),
		FetchArgs: helm.FetchArgs{
			Repo: pulumi.String("https://charts.jetstack.io"),
		},
		Namespace: pulumi.String("cert-manager"),
		Values: pulumi.Map{
			"installCRDs": pulumi.Bool(true),
			"ingressShim": pulumi.Map{
				"defaultIssuerName": pulumi.String("letsencrypt-prod"),
				"defaultIssuerKind": pulumi.String("ClusterIssuer"),
			},
		},
	}, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}

	return nil
}