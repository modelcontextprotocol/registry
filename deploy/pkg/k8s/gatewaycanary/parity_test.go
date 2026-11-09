package gatewaycanary

import (
	"strings"
	"testing"
)

// Values observed in production's nginx -T and Lua balancer during the parity
// audit. Exercise the handoff transform too: backend policies must stay beside
// their target Services, not move into the public Gateway namespace.
func TestGatewayPreservesObservedProductionProxyContract(t *testing.T) {
	objects, err := handoffObjectsWithOptions(true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	backends, proxyPolicies := 0, 0
	for _, o := range objects {
		s := o["spec"].(map[string]any)
		meta := o["metadata"].(map[string]any)
		switch o["kind"] {
		case "UpstreamSettingsPolicy":
			backends++
			service := s["targetRefs"].([]any)[0].(map[string]any)["name"]
			if (service == "mcp-registry" && meta["namespace"] != "default") || (service == "grafana" && meta["namespace"] != "monitoring") {
				t.Fatal("backend policy moved away from target Service")
			}
			keep := s["keepAlive"].(map[string]any)
			if s["loadBalancingMethod"] != "round_robin" || keep["connections"] != float64(320) || keep["requests"] != float64(10000) || keep["time"] != "1h" || keep["timeout"] != "60s" {
				t.Fatal("production upstream connection contract changed")
			}
			if s["healthCheck"].(map[string]any)["passive"].(map[string]any)["maxFails"] != float64(0) {
				t.Fatal("introduced a passive backend quarantine absent from the Lua round-robin balancer")
			}
		case "ProxySettingsPolicy":
			proxyPolicies++
			to := s["timeout"].(map[string]any)
			if to["connect"] != "5s" || to["read"] != "60s" || to["send"] != "60s" {
				t.Fatal("production proxy timeout changed")
			}
			if s["buffering"].(map[string]any)["disable"] != (meta["name"] != "bulk") {
				t.Fatal("only listing routes should buffer responses")
			}
		case "SnippetsPolicy":
			for _, raw := range s["snippets"].([]any) {
				sn := raw.(map[string]any)
				if sn["context"] != "http.server.location" {
					continue
				}
				v := sn["value"].(string)
				for _, directive := range []string{"proxy_next_upstream error timeout;", "proxy_next_upstream_tries 3;", "proxy_next_upstream_timeout 0;", "proxy_set_header Proxy \"\";", "proxy_http_version 1.1;", "proxy_request_buffering on;", "proxy_redirect off;"} {
					if !strings.Contains(v, directive) {
						t.Fatalf("missing production behavior: %s", directive)
					}
				}
				if strings.Contains(v, "non_idempotent") {
					t.Fatal("must not enable replay of sent publishing requests")
				}
			}
		}
	}
	if backends != 2 || proxyPolicies != 3 {
		t.Fatal("missing registry/Grafana policies or test fixture leaked into normal deployment")
	}
}

func TestSharedSANFixtureMatchesProductionListenerShape(t *testing.T) {
	objects, err := handoffObjectsWithOptions(true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	listener, certificate := false, false
	for _, o := range objects {
		s := o["spec"].(map[string]any)
		if o["kind"] == "Gateway" {
			for _, raw := range s["listeners"].([]any) {
				l := raw.(map[string]any)
				if l["name"] == "fixture" {
					_, hostnamePresent := l["hostname"]
					listener = !hostnamePresent
				}
			}
		}
		if o["kind"] == "Certificate" && o["metadata"].(map[string]any)["name"] == "fixture-tls" {
			certificate = len(s["dnsNames"].([]any)) == 2
		}
	}
	if !listener || !certificate {
		t.Fatal("alias validation must use a shared-SAN listener, not disabled SNI validation")
	}
}
