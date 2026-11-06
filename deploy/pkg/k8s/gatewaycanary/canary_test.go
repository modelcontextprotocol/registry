package gatewaycanary

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type mocks struct{}

func (mocks) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	return a.Name, a.Inputs, nil
}
func (mocks) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) { return a.Args, nil }
func TestCanaryCannotTargetProduction(t *testing.T) {
	for _, env := range []string{"prod", "local", "staging"} {
		t.Run(env, func(t *testing.T) {
			err := pulumi.RunErr(func(ctx *pulumi.Context) error { return Validate(ctx) }, pulumi.WithMocks("canary-test", env, mocks{}), func(info *pulumi.RunInfo) {
				info.Config = map[string]string{"mcp-registry:gatewayCanary": "true", "mcp-registry:environment": env, "mcp-registry:provider": "gcp"}
			})
			if (err == nil) != (env == "staging") {
				t.Fatalf("unexpected gate result: %v", err)
			}
		})
	}
}
func TestCanaryRoutesKeepExistingIngressAndTLS(t *testing.T) {
	routes, err := objects("routes.json")
	if err != nil {
		t.Fatal(err)
	}
	listeners := 0
	for _, o := range routes {
		kind := o["kind"].(string)
		if kind == "Ingress" {
			t.Fatalf("canary must not change ingress or certificate lifecycle: %s", kind)
		}
		if kind == "Gateway" {
			for _, raw := range o["spec"].(map[string]any)["listeners"].([]any) {
				l := raw.(map[string]any)
				if l["protocol"] != "HTTPS" {
					continue
				}
				listeners++
				for _, raw := range l["tls"].(map[string]any)["certificateRefs"].([]any) {
					ref := raw.(map[string]any)
					if ref["name"] != "mcp-registry-staging-tls" && ref["name"] != "grafana-tls" && ref["name"] != "fixture-tls" {
						t.Fatalf("unexpected TLS source: %v", ref)
					}
				}
			}
		}
		if kind == "RateLimitPolicy" {
			data, _ := json.Marshal(o)
			if !strings.Contains(string(data), "$binary_remote_addr") || strings.Contains(string(data), "benchmark") {
				t.Fatal("real routes must rate-limit real source IPs")
			}
		}
	}
	if listeners != 3 {
		t.Fatalf("expected registry, Grafana and isolated fixture TLS listeners, got %d", listeners)
	}
}
func TestImagesArePinnedAndPlusDisabled(t *testing.T) {
	control, err := objects("controller.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range control {
		if o["kind"] == "Deployment" || o["kind"] == "Job" {
			for _, c := range o["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any) {
				if !strings.Contains(c.(map[string]any)["image"].(string), "@sha256:") {
					t.Fatal("unpinned controller image")
				}
			}
		}
		if o["kind"] == "NginxProxy" {
			data, _ := json.Marshal(o)
			if strings.Contains(string(data), "nginx-plus") || !strings.Contains(string(data), "@sha256:") {
				t.Fatal("expected pinned OSS data plane")
			}
		}
	}
	data, err := assets.ReadFile("assets/vector.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\"type\": \"sample\"") {
		t.Fatal("metrics must use unsampled logs")
	}
}

func TestIdleCanaryDoesNotExposeUnthrottledFixture(t *testing.T) {
	routes, err := routeObjects(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range routes {
		if strings.HasPrefix(o["metadata"].(map[string]any)["name"].(string), "fixture-") {
			t.Fatal("fixture resource outside comparison")
		}
		if o["kind"] == "Gateway" {
			for _, raw := range o["spec"].(map[string]any)["listeners"].([]any) {
				if raw.(map[string]any)["hostname"] == "fixture.gateway-canary.invalid" {
					t.Fatal("fixture listener outside comparison")
				}
			}
		}
	}
}

func TestCanaryTestModesRequireEnabledCanary(t *testing.T) {
	for _, mode := range []string{"gatewayCanaryComparison", "gatewayCanaryFixtures"} {
		err := pulumi.RunErr(func(ctx *pulumi.Context) error { return Validate(ctx) }, pulumi.WithMocks("canary-test", "staging", mocks{}), func(info *pulumi.RunInfo) {
			info.Config = map[string]string{"mcp-registry:" + mode: "true", "mcp-registry:environment": "staging", "mcp-registry:provider": "gcp"}
		})
		if err == nil {
			t.Fatalf("%s without canary must be rejected", mode)
		}
	}
}

func TestFixtureSourcesMatchEmbeddedConfig(t *testing.T) {
	fixtures, err := objects("fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range fixtures {
		if o["kind"] != "ConfigMap" {
			continue
		}
		data := o["data"].(map[string]any)
		for _, name := range []string{"fixture.py", "payload.json"} {
			source, err := os.ReadFile("../../../gateway-canary/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if data[name] != string(source) {
				t.Fatalf("embedded %s is stale", name)
			}
		}
	}
}
