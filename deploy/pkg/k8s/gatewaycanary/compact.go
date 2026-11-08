package gatewaycanary

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// Compact shares the existing worker with the application and monitoring.
// It is a staging functional-test profile, not a production capacity model.
func Compact(ctx *pulumi.Context) bool {
	return config.New(ctx, "mcp-registry").GetBool("gatewayStagingCompact")
}

func compactObject(o map[string]any) {
	if o["kind"] != "NginxProxy" && o["kind"] != "Deployment" && o["kind"] != "DaemonSet" {
		return
	}
	spec := o["spec"].(map[string]any)
	switch o["kind"] {
	case "NginxProxy":
		d := spec["kubernetes"].(map[string]any)["deployment"].(map[string]any)
		a := d["autoscaling"].(map[string]any)
		a["minReplicas"], a["maxReplicas"] = 1, 1
		d["podDisruptionBudget"] = map[string]any{"minAvailable": 0}
		compactPlacement(d["pod"].(map[string]any))
		d["container"].(map[string]any)["resources"].(map[string]any)["requests"].(map[string]any)["cpu"] = "100m"
	case "Deployment", "DaemonSet":
		pod := spec["template"].(map[string]any)["spec"].(map[string]any)
		compactPlacement(pod)
		cpu := "50m"
		if o["kind"] == "DaemonSet" {
			cpu = "100m"
		}
		pod["containers"].([]any)[0].(map[string]any)["resources"].(map[string]any)["requests"].(map[string]any)["cpu"] = cpu
	}
}

func compactPlacement(pod map[string]any) {
	pod["nodeSelector"] = map[string]any{"registry-role": "worker"}
	pod["tolerations"] = []any{}
}
