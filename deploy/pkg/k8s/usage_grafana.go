package k8s

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/usagecollector"
)

// The "Registry usage" and "Registry publishing" dashboards.
//
//go:embed usage/dashboards/*.json
var usageDashboards embed.FS

// The collector's alert rules, and what deletes them again.
//
//go:embed usage/alerting/rules.yaml
var usageAlertRules string

//go:embed usage/alerting/retired.yaml
var usageAlertRulesRetired string

const (
	// The dashboards and alert rules refer to the data source by this uid.
	usageDataSourceUID = "registry-usage-pg"
	// usageDataSourceFile refers to the password by this name.
	usagePasswordEnv    = "REGISTRY_USAGE_PASSWORD"
	usageDashboardsPath = "/var/lib/grafana/registry-usage-dashboards"
	usageAlertingFile   = "usage-rules.yaml"

	// Grafana reads the database with a role that cannot write to it.
	usageDataSourceFile = `apiVersion: 1
datasources:
  - name: Registry usage
    uid: ` + usageDataSourceUID + `
    type: grafana-postgresql-datasource
    access: proxy
    url: grafana-pg-rw:5432
    user: ` + usageGrafanaRole + `
    editable: false
    jsonData:
      database: ` + usageDatabase + `
      sslmode: require
      postgresVersion: 1700
      maxOpenConns: 5
      maxIdleConns: 2
      connMaxLifetime: 14400
    secureJsonData:
      password: $` + usagePasswordEnv + `
`
	usageDataSourceRetired = `apiVersion: 1
deleteDatasources:
  - name: Registry usage
    orgId: 1
`

	// A provider of their own: removing it is what deletes the two dashboards
	// when the collector is retired.
	usageDashboardProvider = `apiVersion: 1
providers:
  - name: registry-usage
    folder: ''
    type: file
    disableDeletion: true
    editable: false
    options:
      path: ` + usageDashboardsPath + `
`
)

func usageDashboardFiles() (map[string]string, error) {
	entries, err := fs.ReadDir(usageDashboards, "usage/dashboards")
	if err != nil {
		return nil, err
	}
	files := make(map[string]string, len(entries))
	for _, entry := range entries {
		data, err := usageDashboards.ReadFile(path.Join("usage/dashboards", entry.Name()))
		if err != nil {
			return nil, err
		}
		files[entry.Name()] = string(data)
	}
	return files, nil
}

// usageGrafana is what the Grafana chart needs for the usage collector. It is
// nil where the collector is not configured.
type usageGrafana struct {
	// dataSource provisions the data source, or deletes it.
	dataSource string
	// alertRules provisions the two rules, where alerting is configured.
	alertRules string
	// alertRemovals deletes them again, whether or not it still is.
	alertRemovals string
	mounts        pulumi.Array
	password      pulumi.Map
	checksum      string
}

// Grafana reads data source and provider files only when it starts, so the pod
// is replaced when one changes, and when the password does.
func usageChecksum(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte(part + "\x00"))
	}
	return hex.EncodeToString(hash.Sum(nil))[:12]
}

// enabledUsageGrafana creates the dashboard ConfigMaps. The password comes from
// the Secret that also sets it on the database role, never through chart
// values, which are stored and previewed in plain text.
func enabledUsageGrafana(ctx *pulumi.Context, provider *kubernetes.Provider, ns *corev1.Namespace, password *corev1.Secret) (*usageGrafana, error) {
	files, err := usageDashboardFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to read the usage dashboards: %w", err)
	}
	configMaps := map[string]pulumi.StringMap{
		"grafana-registry-usage-dashboards":         pulumi.ToStringMap(files),
		"grafana-registry-usage-dashboard-provider": {"registry-usage.yaml": pulumi.String(usageDashboardProvider)},
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
	return &usageGrafana{
		dataSource: usageDataSourceFile,
		alertRules: usageAlertRules,
		mounts: pulumi.Array{
			pulumi.Map{
				"name":      pulumi.String("grafana-registry-usage-dashboards"),
				"configMap": names["grafana-registry-usage-dashboards"],
				"mountPath": pulumi.String(usageDashboardsPath),
				"readOnly":  pulumi.Bool(true),
			},
			pulumi.Map{
				"name":      pulumi.String("grafana-registry-usage-dashboard-provider"),
				"configMap": names["grafana-registry-usage-dashboard-provider"],
				"mountPath": pulumi.String("/etc/grafana/provisioning/dashboards/registry-usage.yaml"),
				"subPath":   pulumi.String("registry-usage.yaml"),
				"readOnly":  pulumi.Bool(true),
			},
		},
		password: pulumi.Map{
			"secretKeyRef": pulumi.Map{
				"name": password.Metadata.Name(),
				"key":  pulumi.String("password"),
			},
		},
		checksum: usageChecksum("enabled", usageDataSourceFile, usageDashboardProvider, usagecollector.CredentialGeneration),
	}, nil
}

// retiredUsageGrafana has Grafana delete the data source and the two rules.
// The dashboards go with their provider, which is no longer mounted.
func retiredUsageGrafana() *usageGrafana {
	return &usageGrafana{
		dataSource:    usageDataSourceRetired,
		alertRemovals: usageAlertRulesRetired,
		checksum:      usageChecksum("retired", usageDataSourceRetired),
	}
}

// dataSourceFiles adds the usage data source file to Grafana's data sources.
func (u *usageGrafana) dataSourceFiles(files pulumi.StringMap) pulumi.StringMap {
	if u != nil {
		files["usage.yaml"] = pulumi.String(u.dataSource)
	}
	return files
}

// alertingFiles are provisioned beside the registry's own alert rules: the
// rules where alerting is configured, and their removal regardless of it.
func (u *usageGrafana) alertingFiles() (rules, removals map[string]string) {
	if u == nil {
		return nil, nil
	}
	if u.alertRules != "" {
		rules = map[string]string{usageAlertingFile: u.alertRules}
	}
	if u.alertRemovals != "" {
		removals = map[string]string{usageAlertingFile: u.alertRemovals}
	}
	return rules, removals
}

// addTo adds the dashboard mounts, the password variable and the restart
// trigger to the Grafana chart values. Without the collector the values are
// returned unchanged.
func (u *usageGrafana) addTo(values pulumi.Map) pulumi.Map {
	if u == nil {
		return values
	}
	values["extraConfigmapMounts"] = append(values["extraConfigmapMounts"].(pulumi.Array), u.mounts...)
	if u.password != nil {
		values["envValueFrom"].(pulumi.Map)[usagePasswordEnv] = u.password
	}
	addPodAnnotation(values, "checksum/usage", u.checksum)
	return values
}

// addPodAnnotation keeps the annotations other features set on the Grafana pod.
func addPodAnnotation(values pulumi.Map, name, value string) {
	annotations, ok := values["podAnnotations"].(pulumi.Map)
	if !ok {
		annotations = pulumi.Map{}
		values["podAnnotations"] = annotations
	}
	annotations[name] = pulumi.String(value)
}
