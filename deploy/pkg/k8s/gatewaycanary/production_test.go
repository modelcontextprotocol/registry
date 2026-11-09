package gatewaycanary

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func productionConfig(phase string) map[string]string {
	return map[string]string{"mcp-registry:gatewayProductionPhase": phase, "mcp-registry:environment": "prod", "mcp-registry:provider": "gcp", "gcp:project": "mcp-registry-prod", "mcp-registry:gatewayProductionPublicIP": "34.61.200.254", "mcp-registry:gatewayProductionServiceUID": "expected-live-uid", "mcp-registry:gatewayProductionAddressName": "existing-address", "mcp-registry:nodeMachineType": "e2-standard-4", "mcp-registry:nodeMinCount": "3", "mcp-registry:nodeMaxCount": "8", "mcp-registry:isolatedDatabase": "true", "mcp-registry:nodeAutoscaling": "true"}
}
func TestProductionPhasesKeepPreparationOffPublicTraffic(t *testing.T) {
	for _, phase := range []string{"adopt", "managed", "standalone"} {
		t.Run(phase, func(t *testing.T) {
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				if err := Validate(ctx); err != nil {
					return err
				}
				active := phase == "standalone"
				if Enabled(ctx) != active || Standalone(ctx) != active || NativeCertificates(ctx) != active || ManagedMTLS(ctx) != active {
					t.Fatal("preparation changed runtime/renewal topology")
				}
				certs := publicCertificates(ctx)
				if len(certs) != 2 || len(certs[0].hosts) != 2 || certs[0].name != "mcp-registry-prod-tls" {
					t.Fatal("lost production certificate alias")
				}
				return nil
			}, pulumi.WithMocks("test", "test", &mocks{}), func(info *pulumi.RunInfo) { info.Config = productionConfig(phase) })
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestProductionRejectsStagingProfilesAndCapacityChanges(t *testing.T) {
	for key, value := range map[string]string{"mcp-registry:gatewayProductionPhase": "typo", "mcp-registry:environment": "staging", "gcp:project": "mcp-registry-staging", "mcp-registry:gatewayCanaryFixtures": "true", "mcp-registry:gatewayStagingCompact": "true", "mcp-registry:gatewayNativeCertificates": "true", "mcp-registry:nodeMaxCount": "9", "mcp-registry:nodeMachineType": "e2-standard-2", "mcp-registry:gatewayProductionPublicIP": "", "mcp-registry:gatewayProductionServiceUID": ""} {
		t.Run(key, func(t *testing.T) {
			c := productionConfig("standalone")
			c[key] = value
			err := pulumi.RunErr(func(ctx *pulumi.Context) error { return Validate(ctx) }, pulumi.WithMocks("test", "test", &mocks{}), func(info *pulumi.RunInfo) { info.Config = c })
			if err == nil {
				t.Fatal("unsafe production profile accepted")
			}
		})
	}
}
func TestProductionConnectivityAndPolicyContract(t *testing.T) {
	objects, err := handoffObjectsWithOptions(true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	policies := map[string]string{}
	for _, o := range objects {
		if strings.HasSuffix(o["kind"].(string), "Policy") || o["kind"] == "SnippetsFilter" {
			b, _ := json.Marshal(o)
			policies[o["kind"].(string)+"/"+o["metadata"].(map[string]any)["name"].(string)] = string(b)
		}
	}
	productionRoutes(objects)
	if !productionHostPolicyPresent(objects) {
		t.Fatal("production compression hosts missing")
	}
	listeners := map[string]string{}
	routes := map[string]bool{}
	grantedSecret := false
	for _, o := range objects {
		spec := o["spec"].(map[string]any)
		kind := o["kind"].(string)
		name := o["metadata"].(map[string]any)["name"].(string)
		b, _ := json.Marshal(o)
		if strings.Contains(string(b), "staging.registry.modelcontextprotocol.io") || strings.Contains(string(b), "mcp-registry-staging-tls") || strings.Contains(string(b), "legacy-acme") || strings.HasPrefix(name, "fixture-") {
			t.Fatalf("staging/legacy dependency retained in %s", name)
		}
		if want, ok := policies[kind+"/"+name]; ok && string(b) != want {
			t.Fatalf("policy semantics changed: %s", name)
		}
		if kind == "Gateway" {
			for _, raw := range spec["listeners"].([]any) {
				l := raw.(map[string]any)
				if l["protocol"] == "HTTPS" {
					ref := l["tls"].(map[string]any)["certificateRefs"].([]any)[0].(map[string]any)
					host, _ := l["hostname"].(string)
					listeners[host] = ref["name"].(string)
				}
			}
		}
		if kind == "HTTPRoute" {
			routes[name] = true
			if name == "bulk" || name == "registry" {
				if len(spec["hostnames"].([]any)) != 2 || len(spec["parentRefs"].([]any)) != 1 {
					t.Fatal("registry alias missing")
				}
				backend := spec["rules"].([]any)[0].(map[string]any)["backendRefs"].([]any)[0].(map[string]any)
				if backend["name"] != "mcp-registry" || backend["namespace"] != "default" || backend["port"] != float64(80) {
					t.Fatal("registry backend changed")
				}
			}
		}
		if kind == "ReferenceGrant" {
			for _, raw := range spec["to"].([]any) {
				if raw.(map[string]any)["name"] == "mcp-registry-prod-tls" {
					grantedSecret = true
				}
			}
		}
	}
	if len(listeners) != 2 || listeners[""] != "mcp-registry-prod-tls" || listeners["grafana.prod.registry.modelcontextprotocol.io"] != "grafana-tls" || !grantedSecret {
		t.Fatal("TLS routing/grants incomplete")
	}
	if len(routes) != 4 || !routes["redirect"] || !routes["bulk"] || !routes["registry"] || !routes["grafana"] {
		t.Fatal("public route set incomplete")
	}
}
func TestProductionPlacementAndBoundedRollout(t *testing.T) {
	objects, _ := handoffObjectsWithOptions(true, true, false)
	for _, o := range objects {
		if o["kind"] != "NginxProxy" {
			continue
		}
		productionObject(o)
		d := o["spec"].(map[string]any)["kubernetes"].(map[string]any)["deployment"].(map[string]any)
		a := d["autoscaling"].(map[string]any)
		if a["minReplicas"] != 3 || a["maxReplicas"] != 16 || a["targetCPUUtilizationPercentage"] != 70 {
			t.Fatal("production HPA changed")
		}
		p := d["pod"].(map[string]any)
		if p["nodeSelector"].(map[string]any)["registry-role"] != "worker" || len(p["tolerations"].([]any)) != 0 {
			t.Fatal("production scheduled outside worker pool")
		}
		if d["container"].(map[string]any)["resources"].(map[string]any)["requests"].(map[string]any)["cpu"] != "800m" {
			t.Fatal("production proxy request changed")
		}
		for _, r := range d["patches"].([]any) {
			s := r.(map[string]any)["value"].(map[string]any)["spec"].(map[string]any)
			pod := s["template"].(map[string]any)["spec"].(map[string]any)
			if pod["terminationGracePeriodSeconds"] != 180 {
				t.Fatal("gateway termination budget must include LB propagation and request draining")
			}
			container := pod["containers"].([]any)[0].(map[string]any)
			command := container["lifecycle"].(map[string]any)["preStop"].(map[string]any)["exec"].(map[string]any)["command"].([]any)
			if container["name"] != "nginx" || command[0] != "/bin/sh" || command[1] != "-c" || command[2] != "sleep 90; nginx -s quit; while killall -0 nginx 2>/dev/null; do sleep 1; done" {
				t.Fatal("must serve during LB withdrawal, then gracefully drain before entrypoint TERM")
			}
			rolling := s["strategy"].(map[string]any)["rollingUpdate"].(map[string]any)
			if rolling["maxSurge"] != 0 || rolling["maxUnavailable"] != 1 {
				t.Fatal("rollout requires extra peak capacity")
			}
		}
	}
}

func TestProductionGrafanaMetricsClassification(t *testing.T) {
	prod, err := vectorConfiguration(true, true)
	if err != nil {
		t.Fatal(err)
	}
	staging, err := vectorConfiguration(true, false)
	if err != nil {
		t.Fatal(err)
	}
	var p, s map[string]any
	if err := json.Unmarshal(prod, &p); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(staging, &s); err != nil {
		t.Fatal(err)
	}
	parse := p["transforms"].(map[string]any)["parse"].(map[string]any)["source"].(string)
	if !strings.Contains(parse, `if p.host == "grafana.prod.registry.modelcontextprotocol.io" { route = "grafana" }`) || strings.Contains(parse, "grafana.staging.registry.modelcontextprotocol.io") {
		t.Fatal("production Grafana traffic would be mislabeled as registry")
	}
	// Only the host classification changes; pipeline stages/sinks remain identical.
	p["transforms"].(map[string]any)["parse"].(map[string]any)["source"] = s["transforms"].(map[string]any)["parse"].(map[string]any)["source"]
	a, _ := json.Marshal(p)
	b, _ := json.Marshal(s)
	if string(a) != string(b) {
		t.Fatal("production changed the monitoring contract")
	}
}
