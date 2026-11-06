// Package gatewaycanary manages the staging-only, parallel OSS gateway.
package gatewaycanary

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

const Namespace = "registry-gateway-canary"

//go:embed assets/*.json
var assets embed.FS

func Enabled(ctx *pulumi.Context) bool {
	return config.New(ctx, "mcp-registry").GetBool("gatewayCanary")
}

func Validate(ctx *pulumi.Context) error {
	c := config.New(ctx, "mcp-registry")
	if (c.GetBool("gatewayCanaryComparison") || c.GetBool("gatewayCanaryFixtures")) && !Enabled(ctx) {
		return fmt.Errorf("gateway canary test modes require gatewayCanary")
	}
	if Enabled(ctx) && (c.Get("environment") != "staging" || c.Get("provider") != "gcp") {
		return fmt.Errorf("gatewayCanary is restricted to GCP staging")
	}
	return nil
}

func objects(file string) ([]map[string]any, error) {
	data, err := assets.ReadFile("assets/" + file)
	if err != nil {
		return nil, err
	}
	var result []map[string]any
	err = json.Unmarshal(data, &result)
	return result, err
}

// The upstream Helm render is registered as individual resources: all fixed
// identities and inputs are visible in saved plans, without Helm output-ID holes.
func register(ctx *pulumi.Context, provider *kubernetes.Provider, o map[string]any, dependencies []pulumi.Resource) (*apiextensions.CustomResource, error) {
	meta := o["metadata"].(map[string]any)
	name := meta["name"].(string)
	ns, _ := meta["namespace"].(string)
	api := o["apiVersion"].(string)
	if api == "v1" {
		api = "core/v1"
	}
	args := pulumi.Map{}
	for key, value := range o {
		args[key] = pulumi.Any(value)
	}
	resourceName := "gateway-canary-" + strings.ToLower(o["kind"].(string)) + "-" + ns + "-" + name
	var result apiextensions.CustomResource
	err := ctx.RegisterResource("kubernetes:"+api+":"+o["kind"].(string), resourceName, args, &result, pulumi.Provider(provider), pulumi.DependsOn(dependencies))
	return &result, err
}

func Deploy(ctx *pulumi.Context, provider *kubernetes.Provider, address pulumi.StringOutput) error {
	if !Enabled(ctx) {
		return nil
	}
	if err := Validate(ctx); err != nil {
		return err
	}
	crds, err := objects("crds.json")
	if err != nil {
		return err
	}
	deps := []pulumi.Resource{}
	for _, o := range crds {
		r, e := register(ctx, provider, o, nil)
		if e != nil {
			return e
		}
		deps = append(deps, r)
	}
	namespace := map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": Namespace}}
	ns, err := register(ctx, provider, namespace, nil)
	if err != nil {
		return err
	}
	deps = append(deps, ns)
	control, err := objects("controller.json")
	if err != nil {
		return err
	}
	// RBAC precedes the certificate generator, which precedes the controller.
	for _, kinds := range [][]string{{"ServiceAccount", "ClusterRole", "ClusterRoleBinding", "Role", "RoleBinding", "Service"}, {"Job"}, {"GatewayClass", "NginxGateway", "NginxProxy"}, {"Deployment"}} {
		prior := append([]pulumi.Resource{}, deps...)
		for _, o := range control {
			if !contains(kinds, o["kind"].(string)) {
				continue
			}
			if o["kind"] == "NginxProxy" {
				o["spec"].(map[string]any)["kubernetes"].(map[string]any)["service"].(map[string]any)["loadBalancerIP"] = address
				if config.New(ctx, "mcp-registry").GetBool("gatewayCanaryComparison") {
					o["spec"].(map[string]any)["kubernetes"].(map[string]any)["deployment"].(map[string]any)["autoscaling"].(map[string]any)["maxReplicas"] = 2
				}
			}
			r, e := register(ctx, provider, o, prior)
			if e != nil {
				return e
			}
			deps = append(deps, r)
		}
	}
	comparison := config.New(ctx, "mcp-registry").GetBool("gatewayCanaryComparison")
	fixturesEnabled := comparison || config.New(ctx, "mcp-registry").GetBool("gatewayCanaryFixtures")
	fixture, err := objects("fixture.json")
	if err != nil {
		return err
	}
	for _, o := range fixture {
		if !fixturesEnabled {
			continue
		}
		r, e := register(ctx, provider, o, deps)
		if e != nil {
			return e
		}
		deps = append(deps, r)
	}
	routes, err := routeObjects(fixturesEnabled)
	if err != nil {
		return err
	}
	// Install policy/filter/reference grants before exposing the routes.
	for _, group := range [][]string{{"Issuer"}, {"Certificate"}, {"ReferenceGrant", "SnippetsPolicy", "SnippetsFilter", "RateLimitPolicy", "ProxySettingsPolicy", "ClientSettingsPolicy"}, {"Gateway"}, {"HTTPRoute"}} {
		prior := append([]pulumi.Resource{}, deps...)
		for _, o := range routes {
			if contains(group, o["kind"].(string)) {
				r, e := register(ctx, provider, o, prior)
				if e != nil {
					return e
				}
				deps = append(deps, r)
			}
		}
	}
	if config.New(ctx, "mcp-registry").GetBool("gatewayCanaryComparison") {
		baseline, err := objects("baseline.json")
		if err != nil {
			return err
		}
		for _, group := range [][]string{{"ServiceAccount", "ConfigMap", "ClusterRole", "ClusterRoleBinding", "Role", "RoleBinding", "Service", "IngressClass"}, {"Deployment", "PodDisruptionBudget", "Ingress"}} {
			prior := append([]pulumi.Resource{}, deps...)
			for _, o := range baseline {
				if contains(group, o["kind"].(string)) {
					r, e := register(ctx, provider, o, prior)
					if e != nil {
						return e
					}
					deps = append(deps, r)
				}
			}
		}
	}
	if err := vector(ctx, provider, ns); err != nil {
		return err
	}
	ctx.Export("gatewayCanaryIP", address)
	ctx.Export("gatewayCanaryHost", pulumi.String("staging.registry.modelcontextprotocol.io"))
	return nil
}

// The public, unthrottled fixture listener exists only during an explicit test run.
func routeObjects(comparison bool) ([]map[string]any, error) {
	routes, err := objects("routes.json")
	if err != nil {
		return nil, err
	}
	if comparison {
		return routes, nil
	}
	result := []map[string]any{}
	for _, o := range routes {
		if strings.HasPrefix(o["metadata"].(map[string]any)["name"].(string), "fixture-") {
			continue
		}
		if o["kind"] == "Gateway" {
			spec := o["spec"].(map[string]any)
			listeners := []any{}
			for _, raw := range spec["listeners"].([]any) {
				if raw.(map[string]any)["hostname"] != "fixture.gateway-canary.invalid" {
					listeners = append(listeners, raw)
				}
			}
			spec["listeners"] = listeners
		}
		result = append(result, o)
	}
	return result, nil
}

func contains(values []string, s string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
