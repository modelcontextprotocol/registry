package gatewaycanary

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
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

func TestNativeCertificatesRemoveLegacyChallengeDependency(t *testing.T) {
	objects, err := handoffObjectsWithCertificates(true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range objects {
		data, _ := json.Marshal(o)
		if strings.Contains(string(data), "legacy-acme") {
			t.Fatal("native renewal still depends on ingress")
		}
		if o["kind"] == "Gateway" {
			for _, raw := range o["spec"].(map[string]any)["listeners"].([]any) {
				listener := raw.(map[string]any)
				namespaces := listener["allowedRoutes"].(map[string]any)["namespaces"].(map[string]any)
				if listener["name"] == "http" {
					found = true
					if namespaces["from"] != "Selector" {
						t.Fatal("solver namespaces must be restricted")
					}
					encoded, _ := json.Marshal(namespaces)
					for _, ns := range []string{EdgeNamespace, "default", "monitoring"} {
						if !strings.Contains(string(encoded), ns) {
							t.Fatal("missing solver namespace", ns)
						}
					}
				} else if namespaces["from"] != "Same" {
					t.Fatal("HTTPS route attachment was broadened")
				}
			}
		}
	}
	if !found {
		t.Fatal("missing HTTP solver listener")
	}
}

func TestManagedInternalCertificates(t *testing.T) {
	objects := internalCertificates()
	if len(objects) != 5 {
		t.Fatal("missing CA or leaf lifecycle")
	}
	for _, o := range objects {
		meta := o["metadata"].(map[string]any)
		if meta["namespace"] != Namespace || meta["annotations"].(map[string]any)["pulumi.com/waitFor"] != "condition=Ready" {
			t.Fatal("certificate dependency is not awaited")
		}
		if o["kind"] == "Certificate" && meta["name"] != "gateway-internal-ca" {
			spec := o["spec"].(map[string]any)
			if spec["duration"] != "720h" || spec["renewBefore"] != "240h" || spec["privateKey"].(map[string]any)["rotationPolicy"] != "Always" {
				t.Fatal("leaf renewal policy")
			}
		}
	}
	control, _ := objectsForControllerTest()
	managedControllerTLS(control)
	data, _ := json.Marshal(control)
	if !strings.Contains(string(data), "--agent-tls-secret=gateway-agent-tls") || !strings.Contains(string(data), `"secretName":"gateway-server-tls"`) {
		t.Fatal("controller did not switch both credentials")
	}
}

func objectsForControllerTest() (map[string]any, error) {
	control, err := objects("controller.json")
	for _, o := range control {
		if o["kind"] == "Deployment" {
			return o, err
		}
	}
	return nil, err
}

func TestStandaloneGatewayUsesFinalTopologyForLoadTests(t *testing.T) {
	manifests, err := handoffObjectsWithOptions(true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	fixtureRoutes := 0
	for _, o := range manifests {
		spec := o["spec"].(map[string]any)
		name := o["metadata"].(map[string]any)["name"].(string)
		if name == "legacy-acme" || (o["kind"] == "HTTPRoute" && name == "acme") {
			t.Fatal("legacy solver retained")
		}
		if o["kind"] == "NginxProxy" && (spec["kubernetes"].(map[string]any)["deployment"].(map[string]any)["autoscaling"].(map[string]any)["maxReplicas"] != 4 || spec["kubernetes"].(map[string]any)["deployment"].(map[string]any)["autoscaling"].(map[string]any)["minReplicas"] != 4) {
			t.Fatal("standalone replica count is not fixed at four")
		}
		if o["kind"] == "NginxProxy" && spec["workerProcesses"] != 1 {
			t.Fatal("staging workers must match the shared node CPU budget")
		}
		if o["kind"] == "HTTPRoute" && strings.HasPrefix(name, "fixture-") {
			fixtureRoutes++
			if spec["parentRefs"].([]any)[0].(map[string]any)["name"] != EdgeGateway {
				t.Fatal("load test bypasses public Gateway")
			}
			for _, rule := range spec["rules"].([]any) {
				for _, b := range rule.(map[string]any)["backendRefs"].([]any) {
					if b.(map[string]any)["namespace"] != Namespace {
						t.Fatal("wrong isolated fixture backend")
					}
				}
			}
		}
	}
	if fixtureRoutes != 2 {
		t.Fatal("missing fixture routes")
	}
}

type standaloneMocks struct {
	mocks
	t        *testing.T
	gateways atomic.Int32
}

func (m *standaloneMocks) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	if a.TypeToken == "kubernetes:gateway.networking.k8s.io/v1:Gateway" {
		m.gateways.Add(1)
		meta := a.Inputs["metadata"].ObjectValue()
		if meta["name"].StringValue() != EdgeGateway || meta["namespace"].StringValue() != EdgeNamespace {
			m.t.Error("duplicate canary Gateway retained")
		}
	}
	if strings.Contains(a.Name, "cert-generator") || strings.Contains(a.Name, "legacy-acme") {
		m.t.Error("legacy resource retained", a.Name)
	}
	return m.mocks.NewResource(a)
}
func TestStandaloneRegistersWithoutLegacyIngress(t *testing.T) {
	mock := &standaloneMocks{t: t}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		provider, err := kubernetes.NewProvider(ctx, "staging-provider", &kubernetes.ProviderArgs{})
		if err != nil {
			return err
		}
		crds, err := DeployCRDs(ctx, provider)
		if err != nil {
			return err
		}
		return Deploy(ctx, provider, pulumi.String("").ToStringOutput(), crds...)
	}, pulumi.WithMocks("test", "staging", mock), func(info *pulumi.RunInfo) {
		info.Config = map[string]string{"mcp-registry:gatewayStagingStandalone": "true", "mcp-registry:environment": "staging", "mcp-registry:provider": "gcp", "gcp:project": "mcp-registry-staging", "mcp-registry:gatewayStagingPublicIP": "35.222.36.75"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if mock.gateways.Load() != 1 {
		t.Fatal("expected exactly one public Gateway")
	}
}
