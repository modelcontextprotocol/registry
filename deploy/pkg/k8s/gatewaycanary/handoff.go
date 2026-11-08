package gatewaycanary

import (
	"fmt"
	"net"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/yaml"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

const (
	EdgeNamespace = "ingress-nginx"
	EdgeGateway   = "registry-edge"
	EdgeLabel     = "registry.modelcontextprotocol.io/staging-edge"
)

func HandoffEnabled(ctx *pulumi.Context) bool {
	return config.New(ctx, "mcp-registry").Get("gatewayStagingHandoff") != ""
}

func validateHandoff(ctx *pulumi.Context) error {
	c := config.New(ctx, "mcp-registry")
	mode, ip := c.Get("gatewayStagingHandoff"), c.Get("gatewayStagingPublicIP")
	if mode == "" && ip == "" {
		return nil
	}
	if !Enabled(ctx) || c.Get("environment") != "staging" || c.Get("provider") != "gcp" || config.New(ctx, "gcp").Get("project") != "mcp-registry-staging" {
		return fmt.Errorf("gateway staging handoff requires the existing GCP staging canary")
	}
	if mode != "legacy" && mode != "overlap" && mode != "gateway" {
		return fmt.Errorf("gatewayStagingHandoff must be legacy, overlap, or gateway")
	}
	if address := net.ParseIP(ip); address == nil || address.To4() == nil {
		return fmt.Errorf("gatewayStagingPublicIP must be the current public IPv4 address")
	}
	if c.GetBool("gatewayCanaryComparison") || c.GetBool("gatewayCanaryFixtures") {
		return fmt.Errorf("disable gateway test fixtures before public handoff")
	}
	return nil
}

func legacySelector() map[string]any {
	return map[string]any{"app.kubernetes.io/component": "controller", "app.kubernetes.io/instance": "ingress-nginx", "app.kubernetes.io/name": "ingress-nginx"}
}

// Keep the existing GKE Service, node ports, health-check port and UID.
// Overlap warms destination nodes before removing the prior endpoints.
func ServiceTransform(ctx *pulumi.Context) yaml.Transformation {
	mode := config.New(ctx, "mcp-registry").Get("gatewayStagingHandoff")
	return func(o map[string]any, _ ...pulumi.ResourceOption) {
		meta, _ := o["metadata"].(map[string]any)
		if mode == "" || o["kind"] != "Service" || meta["name"] != "ingress-nginx-controller" {
			return
		}
		spec := o["spec"].(map[string]any)
		selector := legacySelector()
		if mode == "overlap" {
			selector = map[string]any{EdgeLabel: "true"}
		}
		if mode == "gateway" {
			selector = map[string]any{"gateway.networking.k8s.io/gateway-name": EdgeGateway}
		}
		spec["selector"] = selector
		// Both implementations listen on 80/443, but use different named ports.
		for _, raw := range spec["ports"].([]any) {
			port := raw.(map[string]any)
			if port["name"] == "http" {
				port["targetPort"] = 80
			}
			if port["name"] == "https" {
				port["targetPort"] = 443
			}
		}
	}
}

func ProtectPublicService(args *pulumi.ResourceTransformationArgs) *pulumi.ResourceTransformationResult {
	if args.Type != "kubernetes:core/v1:Service" || args.Name != "ingress-nginx/ingress-nginx-controller" {
		return nil
	}
	return &pulumi.ResourceTransformationResult{Props: args.Props, Opts: append(args.Opts, pulumi.Protect(true))}
}

// Derive the public Gateway from the policies qualified on the canary. A Service
// selects pods in its own namespace, so its new data plane must live beside it.
func handoffObjects() ([]map[string]any, error) {
	control, err := objects("controller.json")
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for _, o := range control {
		if o["kind"] != "NginxProxy" {
			continue
		}
		o["metadata"] = map[string]any{"name": EdgeGateway, "namespace": EdgeNamespace}
		kube := o["spec"].(map[string]any)["kubernetes"].(map[string]any)
		// Explicit empty IP overrides the class-level canary loadBalancerIP.
		kube["service"] = map[string]any{"type": "ClusterIP", "loadBalancerIP": ""}
		deployment := kube["deployment"].(map[string]any)
		deployment["autoscaling"].(map[string]any)["maxReplicas"] = 2
		for _, raw := range deployment["patches"].([]any) {
			template := raw.(map[string]any)["value"].(map[string]any)["spec"].(map[string]any)["template"].(map[string]any)
			template["metadata"] = map[string]any{"labels": map[string]any{EdgeLabel: "true"}}
			for _, constraint := range template["spec"].(map[string]any)["topologySpreadConstraints"].([]any) {
				constraint.(map[string]any)["labelSelector"].(map[string]any)["matchLabels"].(map[string]any)["gateway.networking.k8s.io/gateway-name"] = EdgeGateway
			}
		}
		result = append(result, o)
	}
	routes, err := routeObjects(false)
	if err != nil {
		return nil, err
	}
	for _, o := range routes {
		meta := o["metadata"].(map[string]any)
		spec := o["spec"].(map[string]any)
		if o["kind"] == "ReferenceGrant" {
			meta["name"] = EdgeGateway
			for _, raw := range spec["from"].([]any) {
				raw.(map[string]any)["namespace"] = EdgeNamespace
			}
		} else {
			meta["namespace"] = EdgeNamespace
		}
		if o["kind"] == "Gateway" {
			meta["name"] = EdgeGateway
			spec["infrastructure"] = map[string]any{"parametersRef": map[string]any{"group": "gateway.nginx.org", "kind": "NginxProxy", "name": EdgeGateway}}
		}
		if ref, ok := spec["targetRef"].(map[string]any); ok && ref["name"] == "registry-canary" {
			ref["name"] = EdgeGateway
		}
		for _, key := range []string{"parentRefs", "targetRefs"} {
			if refs, ok := spec[key].([]any); ok {
				for _, raw := range refs {
					ref := raw.(map[string]any)
					if ref["name"] == "registry-canary" {
						ref["name"] = EdgeGateway
					}
				}
			}
		}
		result = append(result, o)
	}
	// Never route challenges to the public Service: that would loop after handoff.
	result = append(result, map[string]any{"apiVersion": "v1", "kind": "Service", "metadata": map[string]any{"name": "legacy-acme", "namespace": EdgeNamespace}, "spec": map[string]any{"type": "ClusterIP", "selector": legacySelector(), "ports": []any{map[string]any{"name": "http", "port": 80, "targetPort": 80}}}})
	result = append(result, map[string]any{"apiVersion": "gateway.networking.k8s.io/v1", "kind": "HTTPRoute", "metadata": map[string]any{"name": "acme", "namespace": EdgeNamespace}, "spec": map[string]any{"parentRefs": []any{map[string]any{"name": EdgeGateway, "sectionName": "http"}}, "hostnames": []any{"staging.registry.modelcontextprotocol.io", "grafana.staging.registry.modelcontextprotocol.io"}, "rules": []any{map[string]any{"matches": []any{map[string]any{"path": map[string]any{"type": "PathPrefix", "value": "/.well-known/acme-challenge/"}}}, "backendRefs": []any{map[string]any{"name": "legacy-acme", "port": 80}}}}}})
	return result, nil
}

func deployHandoff(ctx *pulumi.Context, provider *kubernetes.Provider, dependencies []pulumi.Resource) error {
	objects, err := handoffObjects()
	if err != nil {
		return err
	}
	for _, kinds := range [][]string{{"NginxProxy", "Service", "ReferenceGrant", "SnippetsPolicy", "SnippetsFilter", "RateLimitPolicy", "ProxySettingsPolicy", "ClientSettingsPolicy"}, {"Gateway"}, {"HTTPRoute"}} {
		prior := append([]pulumi.Resource{}, dependencies...)
		for _, o := range objects {
			if !contains(kinds, o["kind"].(string)) {
				continue
			}
			r, err := register(ctx, provider, o, prior)
			if err != nil {
				return err
			}
			dependencies = append(dependencies, r)
		}
	}
	ctx.Export("gatewayStagingHandoff", pulumi.String(config.New(ctx, "mcp-registry").Get("gatewayStagingHandoff")))
	return nil
}
