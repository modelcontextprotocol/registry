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
	mode := PublicCertificates(ctx)
	if mode != "" && (mode != "adopt" && mode != "managed" || !NativeCertificates(ctx)) {
		return fmt.Errorf("public Gateway certificates require native renewal and adopt/managed mode")
	}
	if ManagedMTLS(ctx) && !NativeCertificates(ctx) {
		return fmt.Errorf("managed Gateway mTLS requires native staging certificates")
	}
	if err := validateHandoff(ctx); err != nil {
		return err
	}
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
func register(ctx *pulumi.Context, provider *kubernetes.Provider, o map[string]any, dependencies []pulumi.Resource, extra ...pulumi.ResourceOption) (*apiextensions.CustomResource, error) {
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
	err := ctx.RegisterResource("kubernetes:"+api+":"+o["kind"].(string), resourceName, args, &result, append([]pulumi.ResourceOption{pulumi.Provider(provider), pulumi.DependsOn(dependencies)}, extra...)...)
	return &result, err
}

func Deploy(ctx *pulumi.Context, provider *kubernetes.Provider, address pulumi.StringOutput, ingress pulumi.Resource) error {
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
	if ManagedMTLS(ctx) {
		deps, err = deployInternalCertificates(ctx, provider, deps)
		if err != nil {
			return err
		}
	}
	control, err := objects("controller.json")
	if err != nil {
		return err
	}
	if HandoffEnabled(ctx) {
		rbac, err := handoffRBAC()
		if err != nil {
			return err
		}
		control = append(control, rbac...)
		public, err := handoffObjectsWithOptions(NativeCertificates(ctx), Standalone(ctx), false)
		if err != nil {
			return err
		}
		for _, o := range public {
			if o["kind"] == "NginxProxy" {
				control = append(control, o)
			}
		}
		if ingress != nil {
			deps = append(deps, ingress)
		}
	}
	// RBAC precedes the certificate generator, which precedes the controller.
	for _, kinds := range [][]string{{"ServiceAccount", "ClusterRole", "ClusterRoleBinding", "Role", "RoleBinding", "Service"}, {"Job"}, {"GatewayClass", "NginxGateway", "NginxProxy"}, {"Deployment"}} {
		prior := append([]pulumi.Resource{}, deps...)
		for _, o := range control {
			if ManagedMTLS(ctx) {
				meta := o["metadata"].(map[string]any)
				if strings.Contains(meta["name"].(string), "cert-generator") {
					continue
				}
				if o["kind"] == "Deployment" {
					managedControllerTLS(o)
				}
			}
			if !contains(kinds, o["kind"].(string)) {
				continue
			}
			if o["kind"] == "NginxProxy" && o["metadata"].(map[string]any)["namespace"] == Namespace {
				o["spec"].(map[string]any)["kubernetes"].(map[string]any)["service"].(map[string]any)["loadBalancerIP"] = address
				if Standalone(ctx) {
					o["spec"].(map[string]any)["kubernetes"].(map[string]any)["service"] = map[string]any{"type": "ClusterIP"}
				}
				if config.New(ctx, "mcp-registry").GetBool("gatewayCanaryComparison") || (HandoffEnabled(ctx) && !Standalone(ctx)) {
					o["spec"].(map[string]any)["kubernetes"].(map[string]any)["deployment"].(map[string]any)["autoscaling"].(map[string]any)["maxReplicas"] = 2
				}
			}
			if o["kind"] == "NginxProxy" && NativeCertificates(ctx) {
				boundedGatewayRollout(o)
			}
			if HandoffEnabled(ctx) && o["kind"] == "Deployment" {
				containers := o["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)
				args := containers[0].(map[string]any)["args"].([]any)
				for i, arg := range args {
					if strings.HasPrefix(arg.(string), "--watch-namespaces=") {
						args[i] = arg.(string) + "," + EdgeNamespace
					}
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
	if Standalone(ctx) {
		routes = nil
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
	if HandoffEnabled(ctx) {
		if err := deployHandoff(ctx, provider, deps); err != nil {
			return err
		}
	}
	if err := vector(ctx, provider, ns); err != nil {
		return err
	}
	if !Standalone(ctx) {
		ctx.Export("gatewayCanaryIP", address)
	}
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
