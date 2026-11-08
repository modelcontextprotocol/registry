package k8s

import (
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/k8s/gatewaycanary"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Adopt the existing network endpoint out of the retired Helm chart. Its
// historical Kubernetes name is retained to preserve the Service UID/GCP LB;
// there is no ingress-nginx controller, chart, solver or runtime dependency.
func setupPublicGatewayService(ctx *pulumi.Context, cluster *providers.ProviderInfo, namespace *corev1.Namespace) error {
	oldURN := pulumi.URN("urn:pulumi:" + ctx.Stack() + "::" + ctx.Project() + "::kubernetes:helm.sh/v3:Chart$kubernetes:core/v1:Service::ingress-nginx/ingress-nginx-controller")
	_, err := corev1.NewService(ctx, "registry-public-gateway", &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("ingress-nginx-controller"), Namespace: namespace.Metadata.Name(), Labels: pulumi.StringMap{"app.kubernetes.io/name": pulumi.String("nginx-gateway-fabric")}},
		Spec: &corev1.ServiceSpecArgs{Type: pulumi.String("LoadBalancer"), LoadBalancerIP: cluster.StagingIngressAddress, ExternalTrafficPolicy: pulumi.String("Local"), HealthCheckNodePort: pulumi.Int(30780), Selector: pulumi.StringMap{"gateway.networking.k8s.io/gateway-name": pulumi.String(gatewaycanary.EdgeGateway)}, Ports: corev1.ServicePortArray{
			&corev1.ServicePortArgs{Name: pulumi.String("http"), Port: pulumi.Int(80), TargetPort: pulumi.Int(80), NodePort: pulumi.Int(32008), Protocol: pulumi.String("TCP")},
			&corev1.ServicePortArgs{Name: pulumi.String("https"), Port: pulumi.Int(443), TargetPort: pulumi.Int(443), NodePort: pulumi.Int(31296), Protocol: pulumi.String("TCP")},
		}},
	}, pulumi.Provider(cluster.Provider), pulumi.Protect(true), pulumi.Aliases([]pulumi.Alias{{URN: oldURN}}))
	if err != nil {
		return err
	}
	ctx.Export("ingressIps", pulumi.StringArray{cluster.StagingIngressAddress})
	return nil
}
