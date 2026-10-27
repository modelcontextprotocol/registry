package k8s

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// ingressAutoscalingValues is consumed by ingress-nginx chart 4.13.0. With
// requests.cpu=800m, 88% means 704m average CPU per controller, including cache hits.
//
//nolint:goconst // Keep Helm schema keys literal alongside their values.
func ingressAutoscalingValues() pulumi.Map {
	return pulumi.Map{
		"enabled":                        pulumi.Bool(true),
		"minReplicas":                    pulumi.Int(2),
		"maxReplicas":                    pulumi.Int(6),
		"targetCPUUtilizationPercentage": pulumi.Int(88),
		// The chart defaults memory to 50%; cache memory persists after a burst
		// and would hold replicas up even when CPU demand has subsided.
		"targetMemoryUtilizationPercentage": pulumi.Int(0),
		"behavior": pulumi.Map{
			"scaleUp": pulumi.Map{
				"stabilizationWindowSeconds": pulumi.Int(0),
				"policies":                   pulumi.Array{pulumi.Map{"type": pulumi.String("Pods"), "value": pulumi.Int(2), "periodSeconds": pulumi.Int(60)}},
			},
			"scaleDown": pulumi.Map{
				"stabilizationWindowSeconds": pulumi.Int(600),
				"policies":                   pulumi.Array{pulumi.Map{"type": pulumi.String("Pods"), "value": pulumi.Int(1), "periodSeconds": pulumi.Int(120)}},
			},
		},
	}
}

// protectDatabase covers both singleton databases. A node containing either
// database must not be evicted to save capacity. This is not HA and does not
// prevent node failure or operator-managed restarts. CNPG propagates inherited
// metadata to its generated pods; setting this only on the Cluster is ineffective.
func protectDatabase(ctx *pulumi.Context, spec map[string]any) map[string]any {
	if config.New(ctx, "mcp-registry").GetBool("burstAutoscaling") {
		spec["enablePDB"] = true
		spec["inheritedMetadata"] = map[string]any{
			"annotations": map[string]any{"cluster-autoscaler.kubernetes.io/safe-to-evict": "false"},
		}
	}
	return spec
}

// Discover each controller pod, rather than scraping the load-balanced service,
// so arrivals, errors and latency include cache hits on every replica.
//
//nolint:goconst // Keep Prometheus configuration keys literal.
func ingressScrapeConfig() pulumi.Map {
	return pulumi.Map{
		"job_name":        pulumi.String("ingress-nginx"),
		"scrape_interval": pulumi.String("30s"),
		"kubernetes_sd_configs": pulumi.Array{pulumi.Map{
			"role":       pulumi.String("pod"),
			"namespaces": pulumi.Map{"names": pulumi.StringArray{pulumi.String("ingress-nginx")}},
		}},
		"relabel_configs": pulumi.Array{
			pulumi.Map{"source_labels": pulumi.StringArray{pulumi.String("__meta_kubernetes_pod_label_app_kubernetes_io_name")}, "regex": pulumi.String("ingress-nginx"), "action": pulumi.String("keep")},
			pulumi.Map{"source_labels": pulumi.StringArray{pulumi.String("__meta_kubernetes_pod_container_port_name")}, "regex": pulumi.String("metrics"), "action": pulumi.String("keep")},
			pulumi.Map{"source_labels": pulumi.StringArray{pulumi.String("__meta_kubernetes_pod_name")}, "target_label": pulumi.String("pod")},
		},
	}
}

// These options must reach chart children, not just the Chart component.
func burstResourceTransform(args *pulumi.ResourceTransformationArgs) *pulumi.ResourceTransformationResult {
	switch {
	case args.Type == "kubernetes:apps/v1:Deployment" && args.Name == "ingress-nginx/ingress-nginx-controller":
		return &pulumi.ResourceTransformationResult{Props: args.Props, Opts: append(args.Opts, pulumi.IgnoreChanges([]string{"spec.replicas"}))}
	case args.Type == "kubernetes:core/v1:ConfigMap" && (args.Name == "monitoring/victoria-metrics-agent-config" || args.Name == "ingress-nginx/ingress-nginx-controller"):
		return &pulumi.ResourceTransformationResult{Props: args.Props, Opts: append(args.Opts, pulumi.DeleteBeforeReplace(true))}
	default:
		return nil
	}
}

func withIngressScrape(ctx *pulumi.Context, configs pulumi.Array) pulumi.Array {
	if config.New(ctx, "mcp-registry").GetBool("burstAutoscaling") {
		return append(configs, ingressScrapeConfig())
	}
	return configs
}
