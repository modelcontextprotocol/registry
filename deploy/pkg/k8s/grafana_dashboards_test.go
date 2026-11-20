package k8s

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Grafana derives these IDs from the names of the data sources provisioned in monitoring.go.
var provisionedDataSources = map[string]bool{
	"P4169E866C3094E38": true, // VictoriaMetrics
	"PD775F2863313E6C7": true, // VictoriaLogs
	"-- Grafana --":     true, // built-in annotations
}

func dataSourceIDs(node any, found map[string]bool) {
	switch value := node.(type) {
	case map[string]any:
		if source, ok := value["datasource"].(map[string]any); ok {
			if uid, ok := source["uid"].(string); ok {
				found[uid] = true
			}
		}
		for _, child := range value {
			dataSourceIDs(child, found)
		}
	case []any:
		for _, child := range value {
			dataSourceIDs(child, found)
		}
	}
}

func TestRegistryDashboardsCanBeProvisioned(t *testing.T) {
	files, err := registryDashboardFiles()
	if err != nil {
		t.Fatal(err)
	}
	uids, titles := map[string]string{}, map[string]string{}
	for name, text := range files {
		var dashboard map[string]any
		if err := json.Unmarshal([]byte(text), &dashboard); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		uid, _ := dashboard["uid"].(string)
		title, _ := dashboard["title"].(string)
		if uid == "" || title == "" {
			t.Errorf("%s: needs a uid and a title", name)
		}
		// A numeric id belongs to one Grafana database and makes provisioning fail elsewhere.
		if _, ok := dashboard["id"]; ok {
			t.Errorf("%s: remove the database id", name)
		}
		if other, ok := uids[uid]; ok {
			t.Errorf("%s and %s share uid %s", name, other, uid)
		}
		if other, ok := titles[title]; ok {
			t.Errorf("%s and %s share a title, which one folder does not allow", name, other)
		}
		uids[uid], titles[title] = name, name
		found := map[string]bool{}
		dataSourceIDs(dashboard, found)
		for id := range found {
			if !provisionedDataSources[id] {
				t.Errorf("%s: uses data source %s, which is not provisioned", name, id)
			}
		}
	}
	// Production built these in the UI. Grafana only adopts them if the uid is unchanged;
	// a new uid would leave the originals in place beside a duplicate.
	for _, uid := range []string{"1d4f7dbe-3a6c-47c7-adf3-da688b509748", "83a0f65c-bac4-40ce-a197-e77d67431ef4"} {
		if _, ok := uids[uid]; !ok {
			t.Errorf("no dashboard keeps production uid %s", uid)
		}
	}
}

func TestRegistryDashboardProviderReadsTheMountedFiles(t *testing.T) {
	m := &backupMonitor{resources: map[string]resource.PropertyMap{}}
	var mounts pulumi.Array
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		provider, err := kubernetes.NewProvider(ctx, "provider", &kubernetes.ProviderArgs{})
		if err != nil {
			return err
		}
		ns, err := corev1.NewNamespace(ctx, "monitoring", &corev1.NamespaceArgs{Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("monitoring")}})
		if err != nil {
			return err
		}
		mounts, err = registryDashboardMounts(ctx, provider, ns)
		return err
	}, pulumi.WithMocks("mcp-registry-infra", "test", m))
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 || mounts[0].(pulumi.Map)["mountPath"] != pulumi.String(registryDashboardsPath) {
		t.Fatalf("dashboards are not mounted at %s", registryDashboardsPath)
	}
	files, err := registryDashboardFiles()
	if err != nil {
		t.Fatal(err)
	}
	dashboards := m.resources["kubernetes:core/v1:ConfigMap::grafana-registry-dashboards"]["data"].ObjectValue()
	if len(dashboards) != len(files) {
		t.Fatalf("ConfigMap holds %d dashboards, repository has %d", len(dashboards), len(files))
	}
	provider := m.resources["kubernetes:core/v1:ConfigMap::grafana-registry-dashboard-provider"]["data"].ObjectValue()["registry.yaml"].StringValue()
	if !strings.Contains(provider, "path: "+registryDashboardsPath+"\n") {
		t.Fatalf("provider does not read %s:\n%s", registryDashboardsPath, provider)
	}
}
