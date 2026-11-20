package k8s

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/providers"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gopkg.in/yaml.v2"
)

const alertWebhookKey = "mcp-registry:" + alertWebhookConfig

type alertingProvisioning struct {
	Groups []struct {
		Name     string
		Folder   string
		Interval string
		Rules    []struct {
			UID   string `yaml:"uid"`
			Title string
			Data  []struct {
				DatasourceUID string `yaml:"datasourceUid"`
			}
			DashboardUID         string `yaml:"dashboardUid"`
			PanelID              int    `yaml:"panelId"`
			NotificationSettings struct {
				Receiver string
			} `yaml:"notification_settings"`
		}
	}
	ContactPoints []struct {
		Name      string
		Receivers []struct {
			UID      string `yaml:"uid"`
			Type     string
			Settings map[string]any
		}
	} `yaml:"contactPoints"`
	DeleteRules []struct {
		UID string `yaml:"uid"`
	} `yaml:"deleteRules"`
	DeleteContactPoints []struct {
		UID string `yaml:"uid"`
	} `yaml:"deleteContactPoints"`
}

func dashboardPanels(t *testing.T) map[string]map[int]bool {
	t.Helper()
	files, err := registryDashboardFiles()
	if err != nil {
		t.Fatal(err)
	}
	panels := map[string]map[int]bool{}
	for name, text := range files {
		var dashboard struct {
			UID    string `json:"uid"`
			Panels []struct {
				ID int `json:"id"`
			} `json:"panels"`
		}
		if err := json.Unmarshal([]byte(text), &dashboard); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		panels[dashboard.UID] = map[int]bool{}
		for _, panel := range dashboard.Panels {
			panels[dashboard.UID][panel.ID] = true
		}
	}
	return panels
}

func TestRegistryAlertingCanBeProvisioned(t *testing.T) {
	files, err := registryAlertingFiles()
	if err != nil {
		t.Fatal(err)
	}
	var all alertingProvisioning
	sectionsAllowed := map[string]bool{"apiVersion": true, "groups": true, "contactPoints": true, "deleteRules": true, "deleteContactPoints": true}
	for name, text := range files {
		// A webhook in a file would be published with this repository and copied into a ConfigMap.
		if strings.Contains(text, "/api/webhooks/") {
			t.Errorf("%s: contains a webhook; it belongs in the %s secret", name, alertWebhookKey)
		}
		var sections map[string]any
		if err := yaml.Unmarshal([]byte(text), &sections); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for section := range sections {
			// A provisioned policy replaces Grafana's whole notification policy tree,
			// which these rules do not use.
			if !sectionsAllowed[section] {
				t.Errorf("%s: section %q is not provisioned from this repository", name, section)
			}
		}
		var file alertingProvisioning
		if err := yaml.Unmarshal([]byte(text), &file); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		all.Groups = append(all.Groups, file.Groups...)
		all.ContactPoints = append(all.ContactPoints, file.ContactPoints...)
		all.DeleteRules = append(all.DeleteRules, file.DeleteRules...)
		all.DeleteContactPoints = append(all.DeleteContactPoints, file.DeleteContactPoints...)
	}
	deleted := map[string]bool{}
	for _, rule := range all.DeleteRules {
		deleted[rule.UID] = true
	}
	for _, contactPoint := range all.DeleteContactPoints {
		deleted[contactPoint.UID] = true
	}

	contactPoints := map[string]string{}
	for _, contactPoint := range all.ContactPoints {
		// Grafana adopts by the uid of the integration, so a second one would be created beside production's.
		if len(contactPoint.Receivers) != 1 {
			t.Fatalf("contact point %s: needs exactly one integration", contactPoint.Name)
		}
		receiver := contactPoint.Receivers[0]
		contactPoints[contactPoint.Name] = receiver.UID
		if deleted[receiver.UID] {
			t.Errorf("contact point %s: is both provisioned and deleted", contactPoint.Name)
		}
		for setting, value := range receiver.Settings {
			text, _ := value.(string)
			if setting == "url" && text != "$"+alertWebhookEnv {
				t.Errorf("contact point %s: url must be $%s", contactPoint.Name, alertWebhookEnv)
			}
			// Grafana expands $NAME in contact point settings, so a literal dollar has to be written as $$.
			if setting != "url" && strings.Contains(strings.ReplaceAll(text, "$$", ""), "$") {
				t.Errorf("contact point %s: write the dollar in %s as $$", contactPoint.Name, setting)
			}
		}
	}
	if contactPoints["Registry Dev"] != "eexrbxaqfwj5sf" && !deleted["eexrbxaqfwj5sf"] {
		t.Error(`contact point "Registry Dev" must keep production uid eexrbxaqfwj5sf`)
	}

	panels := dashboardPanels(t)
	rules := map[string]string{}
	for _, group := range all.Groups {
		for _, rule := range group.Rules {
			if rule.UID == "" || rules[rule.UID] != "" {
				t.Errorf("rule %q: needs a uid of its own", rule.Title)
			}
			rules[rule.UID] = group.Folder + "/" + group.Name
			// Grafana would delete and recreate it at every start, losing its state each time.
			if deleted[rule.UID] {
				t.Errorf("rule %q: is both provisioned and deleted", rule.Title)
			}
			// Grafana expands $NAME in titles too.
			if strings.Contains(rule.Title, "$") {
				t.Errorf("rule %q: a title cannot contain a dollar", rule.Title)
			}
			// Grafana refuses to start when a rule notifies a contact point that does not exist.
			if _, ok := contactPoints[rule.NotificationSettings.Receiver]; !ok {
				t.Errorf("rule %q: notifies %q, which is not in contact-points.yaml", rule.Title, rule.NotificationSettings.Receiver)
			}
			for _, query := range rule.Data {
				if query.DatasourceUID != "__expr__" && !provisionedDataSources[query.DatasourceUID] {
					t.Errorf("rule %q: uses data source %s, which is not provisioned", rule.Title, query.DatasourceUID)
				}
			}
			if rule.DashboardUID != "" && !panels[rule.DashboardUID][rule.PanelID] {
				t.Errorf("rule %q: links to panel %d of dashboard %s, which is not in dashboards/", rule.Title, rule.PanelID, rule.DashboardUID)
			}
		}
	}
	// Production built these in the UI. Grafana only adopts them if uid, folder and group are
	// unchanged; anything else leaves the originals in place, locked, beside a duplicate.
	// Retiring one takes a deleteRules entry.
	for _, uid := range []string{"bexrc60etvhmoa", "bexrekma88v7ke", "fexrh31qna3nkc", "bexrhynsud6gwa"} {
		if rules[uid] != "application alerts/application_alerts" && !deleted[uid] {
			t.Errorf("production rule %s must stay in folder %q, group %q", uid, "application alerts", "application_alerts")
		}
	}
}

// grafanaChartMonitor records the resources and the Helm values of the Grafana chart.
type grafanaChartMonitor struct {
	backupMonitor
	values map[string]any
	raw    string
}

func (m *grafanaChartMonitor) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	if a.Token != "kubernetes:helm:template" {
		return m.backupMonitor.Call(a)
	}
	var options struct {
		Values map[string]any `json:"values"`
	}
	m.raw = a.Args["jsonOpts"].StringValue()
	if err := json.Unmarshal([]byte(m.raw), &options); err != nil {
		return nil, err
	}
	m.values = options.Values
	return resource.NewPropertyMapFromMap(map[string]any{"result": []any{}}), nil
}

func deployGrafanaWith(webhook string, secret bool) (*grafanaChartMonitor, error) {
	m := &grafanaChartMonitor{backupMonitor: backupMonitor{resources: map[string]resource.PropertyMap{}}}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		provider, err := kubernetes.NewProvider(ctx, "provider", &kubernetes.ProviderArgs{})
		if err != nil {
			return err
		}
		ns, err := corev1.NewNamespace(ctx, "monitoring", &corev1.NamespaceArgs{Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("monitoring")}})
		if err != nil {
			return err
		}
		return deployGrafana(ctx, &providers.ProviderInfo{Provider: provider}, ns, "staging", nil)
	}, pulumi.WithMocks("mcp-registry-infra", "test", m), func(info *pulumi.RunInfo) {
		// Standalone skips the ingress-nginx Ingress, which is not part of this test.
		info.Config = map[string]string{"mcp-registry:googleOauthClientSecret": "oauth", "mcp-registry:gatewayStagingStandalone": "true"}
		if webhook != "" {
			info.Config[alertWebhookKey] = webhook
		}
		if secret {
			info.ConfigSecretKeys = []string{alertWebhookKey}
		}
	})
	return m, err
}

func (m *grafanaChartMonitor) alertingMounts() int {
	count := 0
	mounts, _ := m.values["extraConfigmapMounts"].([]any)
	for _, mount := range mounts {
		if mount.(map[string]any)["mountPath"] == registryAlertingPath {
			count++
		}
	}
	return count
}

// No environment may get rules that notify a contact point it does not have, and
// production must not change before its webhook is set.
func TestGrafanaAlertingNeedsTheWebhookSecret(t *testing.T) {
	m, err := deployGrafanaWith("", false)
	if err != nil {
		t.Fatal(err)
	}
	if m.values == nil {
		t.Fatal("the Grafana chart was not rendered")
	}
	for identity := range m.resources {
		if strings.Contains(identity, "alerting") {
			t.Errorf("%s created without a webhook", identity)
		}
	}
	if _, ok := m.values["podAnnotations"]; ok || m.alertingMounts() != 0 || strings.Contains(m.raw, alertWebhookEnv) {
		t.Errorf("the Grafana chart changes without a webhook: %s", m.raw)
	}
}

func TestGrafanaAlertingProvisionsWithTheWebhookSecret(t *testing.T) {
	const webhook = "https://discord.example/api/webhooks/1/FAKEHOOK-test"
	m, err := deployGrafanaWith(webhook+"\n", true)
	if err != nil {
		t.Fatal(err)
	}
	files, err := registryAlertingFiles()
	if err != nil {
		t.Fatal(err)
	}
	configMap := m.resources["kubernetes:core/v1:ConfigMap::grafana-registry-alerting"]["data"].ObjectValue()
	if len(configMap) != len(files) || len(files) == 0 {
		t.Fatalf("ConfigMap holds %d alerting files, repository has %d", len(configMap), len(files))
	}
	// Pulumi marks the whole map secret, which keeps the value out of previews and encrypts it in state.
	stored := m.resources["kubernetes:core/v1:Secret::grafana-alerting"]["stringData"]
	if !stored.IsSecret() || stored.SecretValue().Element.ObjectValue()[alertWebhookEnv].StringValue() != webhook {
		t.Error("the Secret does not hold the webhook as a Pulumi secret")
	}
	// The chart resolves its values, secret or not, and registers what it renders with plain
	// inputs. A webhook passed through them would be stored and previewed unencrypted.
	if strings.Contains(m.raw, "FAKEHOOK") {
		t.Error("the webhook reached the Grafana chart values")
	}
	reference, _ := m.values["envValueFrom"].(map[string]any)[alertWebhookEnv].(map[string]any)
	if fmt.Sprint(reference["secretKeyRef"]) != fmt.Sprint(map[string]any{"name": "grafana-alerting", "key": alertWebhookEnv}) {
		t.Errorf("%s does not come from the Secret: %v", alertWebhookEnv, reference)
	}
	if m.alertingMounts() != 1 {
		t.Errorf("alerting files are not mounted at %s", registryAlertingPath)
	}

	// Grafana reads alerting files and the webhook only when it starts.
	checksum := func(m *grafanaChartMonitor) any {
		annotations, _ := m.values["podAnnotations"].(map[string]any)
		return annotations["checksum/alerting"]
	}
	rotated, err := deployGrafanaWith(webhook+"-rotated", true)
	if err != nil {
		t.Fatal(err)
	}
	if checksum(m) == nil || checksum(m) == checksum(rotated) {
		t.Error("a new webhook does not restart Grafana")
	}
}

func TestGrafanaAlertingRejectsAnUnusableWebhook(t *testing.T) {
	for name, test := range map[string]struct {
		webhook string
		secret  bool
	}{
		"stored unencrypted": {"https://discord.example/api/webhooks/1/FAKEHOOK-test", false},
		"not a URL":          {"FAKEHOOK-test", true},
		"not https":          {"http://discord.example/api/webhooks/1/FAKEHOOK-test", true},
		"two lines":          {"https://discord.example/api/webhooks/1/FAKEHOOK\n-test", true},
	} {
		t.Run(name, func(t *testing.T) {
			m, err := deployGrafanaWith(test.webhook, test.secret)
			if err == nil {
				t.Fatal("accepted")
			}
			if strings.Contains(err.Error(), "FAKEHOOK") {
				t.Errorf("the error shows the webhook: %v", err)
			}
			if m.values != nil {
				t.Error("Grafana was deployed anyway")
			}
		})
	}
}

// This repository is public.
func TestStackFilesKeepTheWebhookEncrypted(t *testing.T) {
	stacks, err := filepath.Glob("../../Pulumi.*.yaml")
	if err != nil || len(stacks) == 0 {
		t.Fatalf("no stack files found: %v", err)
	}
	for _, stack := range stacks {
		data, err := os.ReadFile(stack)
		if err != nil {
			t.Fatal(err)
		}
		var file struct {
			Config map[string]any
		}
		if err := yaml.Unmarshal(data, &file); err != nil {
			t.Fatalf("%s: %v", stack, err)
		}
		value, ok := file.Config[alertWebhookKey]
		if !ok {
			continue
		}
		encrypted, _ := value.(map[any]any)
		if _, ok := encrypted["secure"]; !ok || len(encrypted) != 1 {
			t.Errorf("%s: %s is not encrypted; set it with `pulumi config set --secret` and replace the webhook in Discord", stack, alertWebhookKey)
		}
	}
}
