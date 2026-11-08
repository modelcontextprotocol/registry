package k8s

import (
	"fmt"
	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"reflect"
	"sync"
	"testing"
)

func TestProductionServicePreservesNetworkIdentity(t *testing.T) {
	typ, local, tcp, cluster, affinity := "LoadBalancer", "Local", "TCP", "10.0.0.7", "None"
	health, http, https := 30123, 31935, 30874
	s := corev1.ServiceSpec{Type: &typ, ExternalTrafficPolicy: &local, HealthCheckNodePort: &health, ClusterIP: &cluster, ClusterIPs: []string{cluster}, IpFamilies: []string{"IPv4"}, SessionAffinity: &affinity, Ports: []corev1.ServicePort{{Port: 80, NodePort: &http, Protocol: &tcp, TargetPort: "http"}, {Port: 443, NodePort: &https, Protocol: &tcp, TargetPort: "https"}}}
	got, err := productionServiceSpec(s, "34.61.200.254")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.IpFamilies, s.IpFamilies) || got.HealthCheckNodePort != s.HealthCheckNodePort || *got.Ports[0].NodePort != 31935 || *got.Ports[1].NodePort != 30874 || got.SessionAffinity != s.SessionAffinity {
		t.Fatal("network identity changed")
	}
	if got.ClusterIP != nil || got.ClusterIPs != nil {
		t.Fatal("API-assigned cluster IP must not become a new managed input")
	}
	if got.Ports[0].TargetPort != 80 || got.Ports[1].TargetPort != 443 || got.Selector["gateway.networking.k8s.io/gateway-name"] != "registry-edge" {
		t.Fatal("new proxy ports/selector missing")
	}
	s.Ports = append(s.Ports, corev1.ServicePort{Port: 8443})
	if _, err := productionServiceSpec(s, "34.61.200.254"); err == nil {
		t.Fatal("unaccounted public port allowed")
	}
}

type rollbackServiceMonitor struct {
	t       *testing.T
	managed bool
	seen    map[string]bool
	mu      sync.Mutex
}

func (m *rollbackServiceMonitor) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	if a.Token != "kubernetes:helm:template" {
		return nil, fmt.Errorf("unexpected invoke %s", a.Token)
	}
	// Mock only Helm rendering. SetupIngressController and the SDK's chart
	// child registration/transformation path run normally.
	objects := []any{}
	for _, name := range []string{"ingress-nginx-controller", "unrelated"} {
		objects = append(objects, map[string]any{"apiVersion": "v1", "kind": "Service", "metadata": map[string]any{"name": name, "namespace": "ingress-nginx"}, "spec": map[string]any{"type": "LoadBalancer", "ports": []any{map[string]any{"port": 80}}}})
	}
	return resource.NewPropertyMapFromMap(map[string]any{"result": objects}), nil
}

func (m *rollbackServiceMonitor) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	if a.TypeToken == "kubernetes:core/v1:Service" {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.seen[a.Name] = true
		rpc := a.RegisterRPC
		if rpc == nil {
			return "", nil, fmt.Errorf("missing Service registration")
		}
		if rpc.GetParent() != "urn:pulumi:gcpProd::mcp-registry-infra::kubernetes:helm.sh/v3:Chart::ingress-nginx" {
			m.t.Error("Service was not registered as the real Helm chart child")
		}
		aliases := append([]string{}, rpc.GetAliasURNs()...)
		for _, alias := range rpc.GetAliases() {
			aliases = append(aliases, alias.GetUrn())
		}
		want := m.managed && a.Name == "ingress-nginx/ingress-nginx-controller"
		if rpc.GetProtect() != want {
			m.t.Errorf("unexpected protection for %s: %v", a.Name, rpc.GetProtect())
		}
		if want {
			if !reflect.DeepEqual(aliases, []string{"urn:pulumi:gcpProd::mcp-registry-infra::kubernetes:core/v1:Service::registry-public-gateway"}) {
				m.t.Errorf("rollback alias did not reach the public Service: %v", aliases)
			}
		} else if len(aliases) != 0 {
			m.t.Errorf("unexpected alias for %s: %v", a.Name, aliases)
		}
		a.Inputs["status"] = resource.NewObjectProperty(resource.NewPropertyMapFromMap(map[string]any{"loadBalancer": map[string]any{"ingress": []any{map[string]any{"ip": "34.61.200.254"}}}}))
	}
	return a.Name + "-id", a.Inputs, nil
}

func TestProductionRollbackAliasReachesHelmChildService(t *testing.T) {
	for _, phase := range []string{"", "managed"} {
		t.Run("phase="+phase, func(t *testing.T) {
			m := &rollbackServiceMonitor{t: t, managed: phase == "managed", seen: map[string]bool{}}
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				provider, err := kubernetes.NewProvider(ctx, "provider", &kubernetes.ProviderArgs{})
				if err != nil {
					return err
				}
				_, err = SetupIngressController(ctx, &providers.ProviderInfo{Provider: provider, Name: pulumi.String("mcp-registry-prod").ToStringOutput(), StagingIngressAddress: pulumi.String("34.61.200.254").ToStringOutput()}, "prod")
				return err
			}, pulumi.WithMocks("mcp-registry-infra", "gcpProd", m), func(info *pulumi.RunInfo) {
				info.Config = map[string]string{"mcp-registry:provider": "gcp", "mcp-registry:environment": "prod", "mcp-registry:gatewayProductionPhase": phase}
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(m.seen) != 2 || !m.seen["ingress-nginx/ingress-nginx-controller"] || !m.seen["ingress-nginx/unrelated"] {
				t.Fatalf("missing Service registrations: %v", m.seen)
			}
		})
	}
}
