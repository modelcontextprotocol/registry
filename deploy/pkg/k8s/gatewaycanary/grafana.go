package gatewaycanary

import (
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Add a separate canary folder; do not overwrite existing dashboards or alerts.
func GrafanaMounts(ctx *pulumi.Context, provider *kubernetes.Provider, namespace pulumi.Resource) (pulumi.Array, error) {
	if !Enabled(ctx) {
		return nil, nil
	}
	dashboard, err := assets.ReadFile("assets/dashboard.json")
	if err != nil {
		return nil, err
	}
	providerYAML := "apiVersion: 1\nproviders:\n  - name: gateway-canary\n    folder: Gateway Canary\n    type: file\n    disableDeletion: true\n    editable: false\n    options:\n      path: /var/lib/grafana/gateway-canary\n"
	names := map[string]pulumi.StringOutput{}
	for name, data := range map[string]map[string]any{"gateway-canary-dashboard": {"dashboard.json": string(dashboard)}, "gateway-canary-dashboard-provider": {"gateway-canary.yaml": providerYAML}} {
		cm, err := register(ctx, provider, map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": name, "namespace": "monitoring"}, "data": data}, []pulumi.Resource{namespace})
		if err != nil {
			return nil, err
		}
		names[name] = cm.Metadata.Name().Elem()
	}
	return pulumi.Array{
		pulumi.Map{"name": pulumi.String("gateway-canary-dashboard"), "configMap": names["gateway-canary-dashboard"], "mountPath": pulumi.String("/var/lib/grafana/gateway-canary"), "readOnly": pulumi.Bool(true)},
		pulumi.Map{"name": pulumi.String("gateway-canary-dashboard-provider"), "configMap": names["gateway-canary-dashboard-provider"], "mountPath": pulumi.String("/etc/grafana/provisioning/dashboards/gateway-canary.yaml"), "subPath": pulumi.String("gateway-canary.yaml"), "readOnly": pulumi.Bool(true)},
	}, nil
}
