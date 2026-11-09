package gatewaycanary

import (
	"fmt"
	"net"
	"strings"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// ProductionPhase separates certificate adoption from the destructive retirement.
// Empty keeps the deployed production topology unchanged.
func ProductionPhase(ctx *pulumi.Context) string {
	return config.New(ctx, "mcp-registry").Get("gatewayProductionPhase")
}
func Production(ctx *pulumi.Context) bool           { return ProductionPhase(ctx) != "" }
func ProductionStandalone(ctx *pulumi.Context) bool { return ProductionPhase(ctx) == "standalone" }

func validateProduction(ctx *pulumi.Context) error {
	if !Production(ctx) {
		return nil
	}
	c := config.New(ctx, "mcp-registry")
	if c.Get("environment") != "prod" || c.Get("provider") != "gcp" || config.New(ctx, "gcp").Get("project") != "mcp-registry-prod" {
		return fmt.Errorf("production Gateway requires the production GCP project")
	}
	switch ProductionPhase(ctx) {
	case "adopt", "managed", "standalone":
	default:
		return fmt.Errorf("gatewayProductionPhase must be adopt, managed, or standalone")
	}
	for _, k := range []string{"gatewayCanary", "gatewayCanaryFixtures", "gatewayCanaryComparison", "gatewayStagingStandalone", "gatewayStagingCompact", "stagingIdleCapacity", "gatewayNativeCertificates", "gatewayManagedMTLS"} {
		if c.GetBool(k) {
			return fmt.Errorf("production Gateway cannot use %s", k)
		}
	}
	for _, k := range []string{"gatewayStagingHandoff", "gatewayStagingPublicIP", "gatewayPublicCertificates"} {
		if c.Get(k) != "" {
			return fmt.Errorf("production Gateway cannot use staging transition setting %s", k)
		}
	}
	ip := net.ParseIP(c.Get("gatewayProductionPublicIP"))
	if ip == nil || ip.To4() == nil || c.Get("gatewayProductionServiceUID") == "" || c.Get("gatewayProductionAddressName") == "" {
		return fmt.Errorf("production Gateway requires the verified current public IPv4, address reservation name, and Service UID")
	}
	if c.Get("nodeMachineType") != "e2-standard-4" || c.Get("nodeMinCount") != "3" || c.Get("nodeMaxCount") != "8" || !c.GetBool("isolatedDatabase") || !c.GetBool("nodeAutoscaling") {
		return fmt.Errorf("production Gateway must preserve the existing 2-7 workers and separate database node")
	}
	return nil
}

func RegistryHosts(ctx *pulumi.Context) []string {
	if Production(ctx) {
		return []string{"prod.registry.modelcontextprotocol.io", "registry.modelcontextprotocol.io"}
	}
	return []string{"staging.registry.modelcontextprotocol.io"}
}

type publicCertificate struct {
	namespace, name string
	hosts           []string
}

func publicCertificates(ctx *pulumi.Context) []publicCertificate {
	if Production(ctx) {
		return []publicCertificate{{"default", "mcp-registry-prod-tls", RegistryHosts(ctx)}, {"monitoring", "grafana-tls", []string{"grafana.prod.registry.modelcontextprotocol.io"}}}
	}
	return []publicCertificate{{"default", "mcp-registry-staging-tls", RegistryHosts(ctx)}, {"monitoring", "grafana-tls", []string{"grafana.staging.registry.modelcontextprotocol.io"}}}
}

// Rewrite only public host/Secret references. Policies and metric contracts are
// shared with the qualified standalone staging implementation.
func productionRoutes(objects []map[string]any) {
	for _, o := range objects {
		spec := o["spec"].(map[string]any)
		switch o["kind"] {
		case "Gateway":
			listeners := []any{}
			for _, raw := range spec["listeners"].([]any) {
				l := raw.(map[string]any)
				if l["name"] == "registry" {
					// Both SANs share one listener, allowing HTTP/2 connection
					// coalescing without disabling SNI/Host validation globally.
					// The attached HTTPRoutes still allow only the two names.
					delete(l, "hostname")
					l["tls"].(map[string]any)["certificateRefs"].([]any)[0].(map[string]any)["name"] = "mcp-registry-prod-tls"
				}
				if l["name"] == "grafana" {
					l["hostname"] = "grafana.prod.registry.modelcontextprotocol.io"
				}
				listeners = append(listeners, l)
			}
			spec["listeners"] = listeners
		case "HTTPRoute":
			hosts := []any{}
			for _, h := range spec["hostnames"].([]any) {
				switch h {
				case "staging.registry.modelcontextprotocol.io":
					hosts = append(hosts, "prod.registry.modelcontextprotocol.io", "registry.modelcontextprotocol.io")
				case "grafana.staging.registry.modelcontextprotocol.io":
					hosts = append(hosts, "grafana.prod.registry.modelcontextprotocol.io")
				default:
					hosts = append(hosts, h)
				}
			}
			spec["hostnames"] = hosts
		case "ReferenceGrant":
			for _, r := range spec["to"].([]any) {
				ref := r.(map[string]any)
				if ref["name"] == "mcp-registry-staging-tls" {
					ref["name"] = "mcp-registry-prod-tls"
				}
			}
		}
	}
}

// Production shares its existing worker pool; never create a staging canary
// pool or copy the compact one-worker resource settings.
func productionObject(o map[string]any) {
	spec, ok := o["spec"].(map[string]any)
	if !ok {
		return
	}
	switch o["kind"] {
	case "NginxProxy":
		boundedGatewayRollout(o)
		d := spec["kubernetes"].(map[string]any)["deployment"].(map[string]any)
		a := d["autoscaling"].(map[string]any)
		a["minReplicas"], a["maxReplicas"], a["targetCPUUtilizationPercentage"] = 3, 16, 70
		d["replicas"] = 3
		compactPlacement(d["pod"].(map[string]any))
		req := d["container"].(map[string]any)["resources"].(map[string]any)["requests"].(map[string]any)
		req["cpu"], req["memory"] = "800m", "512Mi"
		for _, r := range d["patches"].([]any) {
			pod := r.(map[string]any)["value"].(map[string]any)["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
			for _, c := range pod["topologySpreadConstraints"].([]any) {
				spread := c.(map[string]any)
				spread["whenUnsatisfiable"] = "DoNotSchedule"
				spread["nodeTaintsPolicy"] = "Honor"
			}
		}
	case "Deployment", "DaemonSet":
		compactPlacement(spec["template"].(map[string]any)["spec"].(map[string]any))
	}
}

// Public JSON compression must cover both production registry names.
func productionHostPolicyPresent(objects []map[string]any) bool {
	for _, o := range objects {
		if o["kind"] == "SnippetsPolicy" {
			for _, s := range o["spec"].(map[string]any)["snippets"].([]any) {
				v := s.(map[string]any)["value"].(string)
				if strings.Contains(v, "registry_public_json_gzip") {
					return strings.Contains(v, `registry\.modelcontextprotocol\.io`) && strings.Contains(v, `prod\.registry\.modelcontextprotocol\.io`)
				}
			}
		}
	}
	return false
}
