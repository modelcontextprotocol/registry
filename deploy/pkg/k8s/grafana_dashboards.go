package k8s

import (
	"embed"
	"fmt"
	"io/fs"
	"path"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Every dashboard in this directory is provisioned into Grafana's General folder.
//
//go:embed dashboards/*.json
var registryDashboards embed.FS

const registryDashboardsPath = "/var/lib/grafana/registry-dashboards"

// Grafana adopts an existing dashboard that has the same uid, then refuses edits
// made in the UI. Removing this provider deletes the dashboards it manages.
const registryDashboardProvider = `apiVersion: 1
providers:
  - name: registry
    folder: ''
    type: file
    disableDeletion: true
    editable: false
    options:
      path: ` + registryDashboardsPath + `
`

func registryDashboardFiles() (map[string]string, error) {
	entries, err := fs.ReadDir(registryDashboards, "dashboards")
	if err != nil {
		return nil, err
	}
	files := make(map[string]string, len(entries))
	for _, entry := range entries {
		data, err := registryDashboards.ReadFile(path.Join("dashboards", entry.Name()))
		if err != nil {
			return nil, err
		}
		files[entry.Name()] = string(data)
	}
	return files, nil
}

// registryDashboardMounts creates the dashboard and provider ConfigMaps and
// returns the Grafana mounts for them.
func registryDashboardMounts(ctx *pulumi.Context, provider *kubernetes.Provider, ns *corev1.Namespace) (pulumi.Array, error) {
	files, err := registryDashboardFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to read Grafana dashboards: %w", err)
	}
	configMaps := map[string]pulumi.StringMap{
		"grafana-registry-dashboards":         pulumi.ToStringMap(files),
		"grafana-registry-dashboard-provider": {"registry.yaml": pulumi.String(registryDashboardProvider)},
	}
	names := make(map[string]pulumi.StringOutput, len(configMaps))
	for name, data := range configMaps {
		configMap, err := corev1.NewConfigMap(ctx, name, &corev1.ConfigMapArgs{
			Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String(name), Namespace: ns.Metadata.Name()},
			Data:     data,
		}, pulumi.Provider(provider))
		if err != nil {
			return nil, err
		}
		names[name] = configMap.Metadata.Name().Elem()
	}
	return pulumi.Array{
		pulumi.Map{
			"name":      pulumi.String("grafana-registry-dashboards"),
			"configMap": names["grafana-registry-dashboards"],
			"mountPath": pulumi.String(registryDashboardsPath),
			"readOnly":  pulumi.Bool(true),
		},
		pulumi.Map{
			"name":      pulumi.String("grafana-registry-dashboard-provider"),
			"configMap": names["grafana-registry-dashboard-provider"],
			"mountPath": pulumi.String("/etc/grafana/provisioning/dashboards/registry.yaml"),
			"subPath":   pulumi.String("registry.yaml"),
			"readOnly":  pulumi.Bool(true),
		},
	}, nil
}
