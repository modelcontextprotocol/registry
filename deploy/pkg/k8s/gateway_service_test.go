package k8s

import (
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"reflect"
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
