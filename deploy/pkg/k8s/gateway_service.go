package k8s

import (
	"fmt"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/k8s/gatewaycanary"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// Adopt the existing network endpoint out of the retired Helm chart. Its
// historical Kubernetes name is retained to preserve the Service UID/GCP LB;
// there is no ingress-nginx controller, chart, solver or runtime dependency.
func setupPublicGatewayService(ctx *pulumi.Context, cluster *providers.ProviderInfo, namespace *corev1.Namespace) error {
	if gatewaycanary.ProductionStandalone(ctx) {
		return setupProductionGatewayService(ctx, cluster, namespace)
	}
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

// Adopt the actual production Service without changing its network identity.
func setupProductionGatewayService(ctx *pulumi.Context, cluster *providers.ProviderInfo, namespace *corev1.Namespace) error {
	c := config.New(ctx, "mcp-registry")
	current, err := corev1.GetService(ctx, "production-public-endpoint", pulumi.ID("ingress-nginx/ingress-nginx-controller"), nil, pulumi.Provider(cluster.Provider))
	if err != nil {
		return err
	}
	spec := pulumi.All(current.Metadata, current.Spec, current.Status, cluster.StagingIngressAddress).ApplyT(func(values []any) (corev1.ServiceSpec, error) {
		meta := values[0].(metav1.ObjectMeta)
		s := values[1].(corev1.ServiceSpec)
		status := values[2].(*corev1.ServiceStatus)
		if meta.Uid == nil || *meta.Uid != c.Require("gatewayProductionServiceUID") {
			return s, fmt.Errorf("production public Service UID changed")
		}
		if status == nil || status.LoadBalancer == nil || len(status.LoadBalancer.Ingress) != 1 || status.LoadBalancer.Ingress[0].Ip == nil || *status.LoadBalancer.Ingress[0].Ip != c.Require("gatewayProductionPublicIP") {
			return s, fmt.Errorf("production public IP changed")
		}
		return productionServiceSpec(s, values[3].(string))
	}).(corev1.ServiceSpecOutput)
	oldURN := pulumi.URN("urn:pulumi:" + ctx.Stack() + "::" + ctx.Project() + "::kubernetes:helm.sh/v3:Chart$kubernetes:core/v1:Service::ingress-nginx/ingress-nginx-controller")
	_, err = corev1.NewService(ctx, "registry-public-gateway", &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("ingress-nginx-controller"), Namespace: namespace.Metadata.Name(), Labels: pulumi.StringMap{"app.kubernetes.io/name": pulumi.String("nginx-gateway-fabric")}},
		Spec:     spec.ToServiceSpecPtrOutput(),
	}, pulumi.Provider(cluster.Provider), pulumi.Protect(true), pulumi.Aliases([]pulumi.Alias{{URN: oldURN}}))
	ctx.Export("ingressIps", pulumi.StringArray{cluster.StagingIngressAddress})
	return err
}

func productionServiceSpec(s corev1.ServiceSpec, ip string) (corev1.ServiceSpec, error) {
	if s.Type == nil || *s.Type != "LoadBalancer" || s.ExternalTrafficPolicy == nil || *s.ExternalTrafficPolicy != "Local" || s.HealthCheckNodePort == nil || *s.HealthCheckNodePort == 0 || len(s.Ports) != 2 {
		return s, fmt.Errorf("unexpected production load-balancer contract")
	}
	seen := map[int]bool{}
	for i, p := range s.Ports {
		if (p.Port != 80 && p.Port != 443) || seen[p.Port] || p.NodePort == nil || *p.NodePort == 0 || p.Protocol == nil || *p.Protocol != "TCP" {
			return s, fmt.Errorf("unexpected production public port")
		}
		seen[p.Port] = true
		s.Ports[i].TargetPort = p.Port
	}
	s.Selector = map[string]string{"gateway.networking.k8s.io/gateway-name": gatewaycanary.EdgeGateway}
	s.LoadBalancerIP = &ip
	// These API-assigned fields were never Helm inputs. Introducing clusterIP
	// as a managed input makes the provider propose an immutable-field
	// replacement, even when its value equals the live IP. Leave assignment
	// with Kubernetes; the aliased Service is updated in place.
	s.ClusterIP, s.ClusterIPs = nil, nil
	return s, nil
}

// Reverse adoption is available for a reviewed Git-based rollback. It retains
// the public Service rather than trying to create another object at its name.
func productionLegacyServiceTransform(ctx *pulumi.Context) pulumi.ResourceTransformation {
	return func(args *pulumi.ResourceTransformationArgs) *pulumi.ResourceTransformationResult {
		result := gatewaycanary.ProtectPublicService(args)
		if result == nil {
			return nil
		}
		urn := pulumi.URN("urn:pulumi:" + ctx.Stack() + "::" + ctx.Project() + "::kubernetes:core/v1:Service::registry-public-gateway")
		result.Opts = append(result.Opts, pulumi.Aliases([]pulumi.Alias{{URN: urn}}))
		return result
	}
}
