package gatewaycanary

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestHandoffKeepsTLSAndPolicyTargets(t *testing.T) {
	objects, err := handoffObjects()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objects {
		data, _ := json.Marshal(o)
		if strings.Contains(string(data), `"name":"registry-canary"`) || strings.Contains(string(data), "fixture-") {
			t.Fatalf("stale target or fixture: %s", data)
		}
		spec := o["spec"].(map[string]any)
		if o["kind"] == "NginxProxy" {
			kube := spec["kubernetes"].(map[string]any)
			service := kube["service"].(map[string]any)
			if service["type"] != "ClusterIP" || service["loadBalancerIP"] != "" {
				t.Fatal("handoff must not allocate another public IP")
			}
			if kube["deployment"].(map[string]any)["autoscaling"].(map[string]any)["maxReplicas"] != 2 {
				t.Fatal("unbounded overlap capacity")
			}
		}
		if o["kind"] == "Gateway" {
			for _, raw := range spec["listeners"].([]any) {
				l := raw.(map[string]any)
				if l["protocol"] != "HTTPS" {
					continue
				}
				ref := l["tls"].(map[string]any)["certificateRefs"].([]any)[0].(map[string]any)
				if ref["name"] != "mcp-registry-staging-tls" && ref["name"] != "grafana-tls" {
					t.Fatal("changed public TLS source")
				}
			}
		}
		if o["kind"] == "Service" && spec["selector"].(map[string]any)["app.kubernetes.io/name"] != "ingress-nginx" {
			t.Fatal("ACME service must never select Gateway pods")
		}
	}
}

func TestHandoffSelectorsPreserveServiceIdentity(t *testing.T) {
	for _, mode := range []string{"", "legacy", "overlap", "gateway"} {
		t.Run(mode, func(t *testing.T) {
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				spec := map[string]any{"selector": legacySelector(), "clusterIP": "10.0.0.1", "healthCheckNodePort": 30780, "externalTrafficPolicy": "Local", "ports": []any{map[string]any{"name": "http", "port": 80, "nodePort": 32008, "targetPort": "http"}, map[string]any{"name": "https", "port": 443, "nodePort": 31296, "targetPort": "https"}}}
				o := map[string]any{"kind": "Service", "metadata": map[string]any{"name": "ingress-nginx-controller", "uid": "original"}, "spec": spec}
				ServiceTransform(ctx)(o)
				if spec["clusterIP"] != "10.0.0.1" || spec["healthCheckNodePort"] != 30780 || spec["externalTrafficPolicy"] != "Local" || o["metadata"].(map[string]any)["uid"] != "original" {
					t.Fatal("service identity changed")
				}
				sel := spec["selector"].(map[string]any)
				switch mode {
				case "overlap":
					if len(sel) != 1 || sel[EdgeLabel] != "true" {
						t.Fatal(sel)
					}
				case "gateway":
					if len(sel) != 1 || sel["gateway.networking.k8s.io/gateway-name"] != EdgeGateway {
						t.Fatal(sel)
					}
				default:
					if sel["app.kubernetes.io/name"] != "ingress-nginx" {
						t.Fatal(sel)
					}
				}
				for _, raw := range spec["ports"].([]any) {
					p := raw.(map[string]any)
					if mode != "" && p["targetPort"] != p["port"] {
						t.Fatal("incompatible target ports")
					}
				}
				return nil
			}, pulumi.WithMocks("test", "staging", mocks{}), func(info *pulumi.RunInfo) {
				info.Config = map[string]string{"mcp-registry:gatewayStagingHandoff": mode}
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHandoffRejectsWrongProjectAndModes(t *testing.T) {
	for _, tc := range []struct {
		project, mode string
		valid         bool
	}{{"mcp-registry-staging", "legacy", true}, {"mcp-registry-staging", "overlap", true}, {"mcp-registry-staging", "gateway", true}, {"mcp-registry-prod", "gateway", false}, {"mcp-registry-staging", "typo", false}} {
		err := pulumi.RunErr(func(ctx *pulumi.Context) error { return Validate(ctx) }, pulumi.WithMocks("test", "staging", mocks{}), func(info *pulumi.RunInfo) {
			info.Config = map[string]string{"mcp-registry:gatewayCanary": "true", "mcp-registry:environment": "staging", "mcp-registry:provider": "gcp", "gcp:project": tc.project, "mcp-registry:gatewayStagingHandoff": tc.mode, "mcp-registry:gatewayStagingPublicIP": "35.222.36.75"}
		})
		if (err == nil) != tc.valid {
			t.Fatalf("%v: %v", tc, err)
		}
	}
}

func TestHandoffControllerCanWatchItsNewNamespace(t *testing.T) {
	rbac, err := handoffRBAC()
	if err != nil {
		t.Fatal(err)
	}
	if len(rbac) != 2 {
		t.Fatalf("expected namespace role and binding, got %d", len(rbac))
	}
	for _, o := range rbac {
		if o["metadata"].(map[string]any)["namespace"] != EdgeNamespace {
			t.Fatal("wrong RBAC namespace")
		}
		if o["kind"] == "RoleBinding" {
			subject := o["subjects"].([]any)[0].(map[string]any)
			if subject["namespace"] != Namespace || subject["name"] != "registry-gateway-canary" {
				t.Fatal("wrong controller identity")
			}
		}
	}
	control, _ := objects("controller.json")
	for _, o := range control {
		if o["kind"] == "Role" && o["metadata"].(map[string]any)["namespace"] == "default" {
			want, _ := json.Marshal(o["rules"])
			got, _ := json.Marshal(rbac[0]["rules"])
			if string(want) != string(got) {
				t.Fatal("handoff role must match the qualified controller permissions")
			}
		}
	}
}
