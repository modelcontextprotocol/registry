package gatewaycanary

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func vector(ctx *pulumi.Context, provider *kubernetes.Provider, namespace pulumi.Resource) error {
	data, err := assets.ReadFile("assets/vector.json")
	if err != nil {
		return err
	}
	if HandoffEnabled(ctx) {
		var cfg map[string]any
		if err := json.Unmarshal(data, &cfg); err != nil {
			return err
		}
		source := cfg["sources"].(map[string]any)["access"].(map[string]any)
		source["include"] = append(source["include"].([]any), "/var/log/pods/ingress-nginx_registry-edge-*/nginx/*.log")
		data, err = json.Marshal(cfg)
		if err != nil {
			return err
		}
	}
	cm, err := register(ctx, provider, map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "vector", "namespace": Namespace}, "data": map[string]any{"vector.json": string(data)}}, []pulumi.Resource{namespace})
	if err != nil {
		return err
	}
	labels := map[string]any{"app": "gateway-canary-vector"}
	ds := map[string]any{"apiVersion": "apps/v1", "kind": "DaemonSet", "metadata": map[string]any{"name": "vector", "namespace": Namespace}, "spec": map[string]any{
		"selector": map[string]any{"matchLabels": labels}, "template": map[string]any{
			"metadata": map[string]any{"labels": labels, "annotations": map[string]any{"checksum/config": fmt.Sprintf("%x", sha256.Sum256(data))}},
			"spec": map[string]any{"automountServiceAccountToken": false, "nodeSelector": map[string]any{"registry-role": "gateway-canary"},
				"tolerations": []any{map[string]any{"key": "registry-role", "operator": "Equal", "value": "gateway-canary", "effect": "NoSchedule"}},
				"containers": []any{map[string]any{"name": "vector", "image": "timberio/vector:0.61.0-debian@sha256:f6c23b5a7e16fb2838d33619ef7acf4bad2289a7cf22bc90efc2371f520c4ea2", "args": []string{"--config", "/etc/vector/vector.json", "--threads", "1"},
					"resources":       map[string]any{"requests": map[string]any{"cpu": "500m", "memory": "128Mi"}, "limits": map[string]any{"memory": "512Mi"}},
					"securityContext": map[string]any{"runAsUser": 0, "readOnlyRootFilesystem": true, "allowPrivilegeEscalation": false, "capabilities": map[string]any{"drop": []string{"ALL"}}, "seccompProfile": map[string]any{"type": "RuntimeDefault"}},
					"ports":           []any{map[string]any{"name": "metrics", "containerPort": 9598}, map[string]any{"name": "sizes", "containerPort": 9599}},
					"readinessProbe":  map[string]any{"httpGet": map[string]any{"path": "/metrics", "port": "metrics"}, "periodSeconds": 5},
					"volumeMounts":    []any{map[string]any{"name": "config", "mountPath": "/etc/vector", "readOnly": true}, map[string]any{"name": "logs", "mountPath": "/var/log/pods", "readOnly": true}, map[string]any{"name": "state", "mountPath": "/data"}}}},
				"volumes": []any{map[string]any{"name": "config", "configMap": map[string]any{"name": "vector"}}, map[string]any{"name": "logs", "hostPath": map[string]any{"path": "/var/log/pods", "type": "Directory"}}, map[string]any{"name": "state", "hostPath": map[string]any{"path": "/var/lib/registry-gateway-canary-vector", "type": "DirectoryOrCreate"}}}}}}}
	_, err = register(ctx, provider, ds, []pulumi.Resource{cm})
	return err
}

// Scrape each collector; never sample access events before metric extraction.
func Scrapes(ctx *pulumi.Context) pulumi.Array {
	result := pulumi.Array{}
	for _, port := range []string{"metrics", "sizes"} {
		result = append(result, pulumi.Map{
			"job_name": pulumi.String("gateway-canary-vector-" + port), "scrape_interval": pulumi.String("15s"), "sample_limit": pulumi.Int(30000),
			"kubernetes_sd_configs": pulumi.Array{pulumi.Map{"role": pulumi.String("pod"), "namespaces": pulumi.Map{"names": pulumi.StringArray{pulumi.String(Namespace)}}}},
			"relabel_configs": pulumi.Array{
				pulumi.Map{"source_labels": pulumi.StringArray{pulumi.String("__meta_kubernetes_pod_label_app")}, "regex": pulumi.String("gateway-canary-vector"), "action": pulumi.String("keep")},
				pulumi.Map{"source_labels": pulumi.StringArray{pulumi.String("__meta_kubernetes_pod_container_port_name")}, "regex": pulumi.String(port), "action": pulumi.String("keep")},
				pulumi.Map{"source_labels": pulumi.StringArray{pulumi.String("__meta_kubernetes_pod_node_name")}, "target_label": pulumi.String("node")},
			}})
	}
	nativeNamespaces := pulumi.StringArray{pulumi.String(Namespace)}
	if HandoffEnabled(ctx) {
		nativeNamespaces = append(nativeNamespaces, pulumi.String(EdgeNamespace))
	}
	for _, container := range []string{"nginx", "nginx-gateway", "controller"} {
		result = append(result, pulumi.Map{
			"job_name": pulumi.String("gateway-canary-native-" + container), "scrape_interval": pulumi.String("30s"),
			"kubernetes_sd_configs": pulumi.Array{pulumi.Map{"role": pulumi.String("pod"), "namespaces": pulumi.Map{"names": nativeNamespaces}}},
			"relabel_configs": pulumi.Array{
				pulumi.Map{"source_labels": pulumi.StringArray{pulumi.String("__meta_kubernetes_pod_label_app_kubernetes_io_name")}, "regex": pulumi.String("ingress-nginx"), "action": pulumi.String("drop")},
				pulumi.Map{"source_labels": pulumi.StringArray{pulumi.String("__meta_kubernetes_pod_container_name")}, "regex": pulumi.String(container), "action": pulumi.String("keep")},
				pulumi.Map{"source_labels": pulumi.StringArray{pulumi.String("__meta_kubernetes_pod_container_port_name")}, "regex": pulumi.String("metrics"), "action": pulumi.String("keep")},
				pulumi.Map{"source_labels": pulumi.StringArray{pulumi.String("__meta_kubernetes_pod_name")}, "target_label": pulumi.String("pod")},
			}})
	}
	return result
}
