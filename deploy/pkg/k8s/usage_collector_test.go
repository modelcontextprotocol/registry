package k8s

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
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

const (
	usageFlagKey    = "mcp-registry:usageCollector"
	usageAliasesKey = "mcp-registry:usageCollectorOrganizationAliases"
	// Stand-ins for the Google key and for the organization aliases.
	fakeGoogleKey = "RkFLRS1HT09HTEUtS0VZ"
	fakeAliases   = "EXAMPLECORP=Example Corp"
)

// usageMonitor also gives every random password a recognisable value.
type usageMonitor struct{ grafanaChartMonitor }

func (m *usageMonitor) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	id, state, err := m.grafanaChartMonitor.NewResource(a)
	if a.TypeToken == "random:index/randomPassword:RandomPassword" {
		state = state.Copy()
		state["result"] = resource.NewStringProperty("FAKEPASS-" + a.Name)
	}
	return id, state, err
}

// plain drops Pulumi's secret and output wrappers.
func plain(value resource.PropertyValue) any {
	switch {
	case value.IsSecret():
		return plain(value.SecretValue().Element)
	case value.IsOutput():
		return plain(value.OutputValue().Element)
	case value.IsObject():
		object := map[string]any{}
		for name, item := range value.ObjectValue() {
			object[string(name)] = plain(item)
		}
		return object
	case value.IsArray():
		var array []any
		for _, item := range value.ArrayValue() {
			array = append(array, plain(item))
		}
		return array
	}
	return value.Mappable()
}

// at follows keys into what plain returned.
func at(value any, keys ...string) any {
	for _, key := range keys {
		object, _ := value.(map[string]any)
		value = object[key]
	}
	return value
}

// decoded reads a value from a Secret's data field.
func decoded(t *testing.T, value any) string {
	t.Helper()
	text, _ := value.(string)
	raw, err := base64.StdEncoding.DecodeString(text)
	if err != nil || text == "" {
		t.Errorf("%q is not a value of a Secret's data field", text)
	}
	return string(raw)
}

// fakeEncoded is how a fake password starts once it is encoded for a Secret.
const fakeEncoded = "RkFLRVBBU1Mt"

func (m *usageMonitor) resource(t *testing.T, identity string) map[string]any {
	t.Helper()
	inputs, ok := m.resources[identity]
	if !ok {
		t.Fatalf("%s was not created", identity)
	}
	return plain(resource.NewObjectProperty(inputs)).(map[string]any)
}

// deployGrafanaWithUsage deploys Grafana, and with it the usage collector, in
// a stack shaped like hosted staging.
func deployGrafanaWithUsage(settings map[string]string, secrets ...string) (*usageMonitor, error) {
	m := &usageMonitor{grafanaChartMonitor{backupMonitor: backupMonitor{resources: map[string]resource.PropertyMap{}}}}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		provider, err := kubernetes.NewProvider(ctx, "provider", &kubernetes.ProviderArgs{})
		if err != nil {
			return err
		}
		ns, err := corev1.NewNamespace(ctx, "monitoring", &corev1.NamespaceArgs{Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("monitoring")}})
		if err != nil {
			return err
		}
		cluster := &providers.ProviderInfo{Provider: provider, UsageCollectorKey: pulumi.ToSecret(pulumi.String(fakeGoogleKey)).(pulumi.StringOutput)}
		return deployGrafana(ctx, cluster, ns, "staging", nil, nil)
	}, pulumi.WithMocks("mcp-registry-infra", "test", m), func(info *pulumi.RunInfo) {
		info.Config = map[string]string{
			"mcp-registry:googleOauthClientSecret":  "oauth",
			"mcp-registry:gatewayStagingStandalone": "true",
			"mcp-registry:isolatedDatabase":         "true",
			"gcp:project":                           "mcp-registry-staging",
		}
		for key, value := range settings {
			info.Config[key] = value
		}
		info.ConfigSecretKeys = secrets
	})
	return m, err
}

const fakeWebhook = "https://discord.example/api/webhooks/1/FAKEHOOK-test"

// Production has no flag, and must not change until it gets one.
func TestUsageCollectorIsAbsentWithoutItsFlag(t *testing.T) {
	m, err := deployGrafanaWithUsage(map[string]string{alertWebhookKey: fakeWebhook}, alertWebhookKey)
	if err != nil {
		t.Fatal(err)
	}
	for identity := range m.resources {
		if strings.Contains(strings.ToLower(identity), "usage") || strings.HasPrefix(identity, "random:") {
			t.Errorf("%s created without the flag", identity)
		}
	}
	if strings.Contains(strings.ToLower(m.raw), "usage") {
		t.Errorf("the Grafana chart changes without the flag: %s", m.raw)
	}
	for _, configMap := range []string{"grafana-datasources", "grafana-registry-alerting"} {
		for name := range m.resource(t, "kubernetes:core/v1:ConfigMap::"+configMap)["data"].(map[string]any) {
			if strings.Contains(name, "usage") {
				t.Errorf("%s holds %s without the flag", configMap, name)
			}
		}
	}
}

func TestUsageCollectorRunsWithConfinedCredentials(t *testing.T) {
	m, err := deployGrafanaWithUsage(map[string]string{usageFlagKey: "true", usageAliasesKey: fakeAliases}, usageAliasesKey)
	if err != nil {
		t.Fatal(err)
	}

	// The registry role may read and nothing else; the other two are plain logins,
	// one of which owns the usage database.
	for name, want := range map[string]struct {
		namespace, cluster, role string
		memberOf                 any
	}{
		"registry-usage-reader":    {"default", "registry-pg", "registry_usage_reader", []any{"pg_read_all_data"}},
		"registry-usage-collector": {"monitoring", "grafana-pg", "registry_usage_collector", nil},
		"registry-usage-grafana":   {"monitoring", "grafana-pg", "registry_usage_grafana", nil},
	} {
		role := m.resource(t, "kubernetes:postgresql.cnpg.io/v1:DatabaseRole::"+name)
		spec := at(role, "spec").(map[string]any)
		if at(role, "metadata", "namespace") != want.namespace || at(spec, "cluster", "name") != want.cluster || spec["name"] != want.role {
			t.Errorf("%s is not %s in %s/%s: %v", name, want.role, want.namespace, want.cluster, role)
		}
		if fmt.Sprint(spec["inRoles"]) != fmt.Sprint(want.memberOf) {
			t.Errorf("%s is a member of %v, want %v", name, spec["inRoles"], want.memberOf)
		}
		for _, power := range []string{"superuser", "createdb", "createrole", "replication", "bypassrls"} {
			if spec[power] != nil {
				t.Errorf("%s is given %s", name, power)
			}
		}
		// Retiring the collector must leave the role, and what it owns, in place.
		if spec["databaseRoleReclaimPolicy"] != "retain" {
			t.Errorf("%s would be dropped with its resource", name)
		}
		password := m.resource(t, "kubernetes:core/v1:Secret::"+name+"-password")
		if at(password, "metadata", "namespace") != want.namespace || decoded(t, at(password, "data", "password")) != "FAKEPASS-"+name+"-password" ||
			decoded(t, at(password, "data", "username")) != want.role {
			t.Errorf("%s does not get its own password beside its cluster", name)
		}
	}
	database := at(m.resource(t, "kubernetes:postgresql.cnpg.io/v1:Database::registry-usage-database"), "spec").(map[string]any)
	if database["owner"] != "registry_usage_collector" || database["databaseReclaimPolicy"] != "retain" || at(database, "cluster", "name") != "grafana-pg" {
		t.Errorf("the usage database is not a retained database of the collector on grafana-pg: %v", database)
	}

	// The collector's Secret holds both URLs and the aliases, encrypted in Pulumi's state.
	stored := m.resources["kubernetes:core/v1:Secret::registry-usage-collector"]["data"]
	if !stored.IsSecret() {
		t.Error("the collector's settings are not a Pulumi secret")
	}
	settings := plain(stored).(map[string]any)
	for name, want := range map[string]string{
		"USAGE_COLLECTOR_DATABASE_URL":          "postgres://registry_usage_collector:FAKEPASS-registry-usage-collector-password@grafana-pg-rw:5432/registry_usage?sslmode=require",
		"USAGE_COLLECTOR_REGISTRY_DATABASE_URL": "postgres://registry_usage_reader:FAKEPASS-registry-usage-reader-password@registry-pg-rw.default.svc:5432/app?sslmode=require",
		"USAGE_COLLECTOR_ORGANIZATION_ALIASES":  fakeAliases,
	} {
		if decoded(t, settings[name]) != want {
			t.Errorf("%s is %v", name, settings[name])
		}
	}
	// A deployment applies a saved plan. The Kubernetes provider moves
	// stringData into data once it knows the values, so a Secret whose values
	// are unknown at preview differs from its plan and fails the apply. The
	// first staging deployment of the collector failed this way.
	for identity, inputs := range m.resources {
		if strings.HasPrefix(identity, "kubernetes:core/v1:Secret::registry-usage-") && inputs["stringData"].HasValue() {
			t.Errorf("%s uses stringData for values that are not known at preview", identity)
		}
	}
	if key := m.resources["kubernetes:core/v1:Secret::registry-usage-collector-gcp"]["data"]; !key.IsSecret() || at(plain(key), "key.json") != fakeGoogleKey {
		t.Error("the Google key is not stored as a Pulumi secret")
	}

	cronJob := m.resource(t, "kubernetes:batch/v1:CronJob::registry-usage-collector")
	job := at(cronJob, "spec", "jobTemplate", "spec").(map[string]any)
	pod := at(job, "template", "spec").(map[string]any)
	container := pod["containers"].([]any)[0].(map[string]any)
	if at(cronJob, "spec", "concurrencyPolicy") != "Forbid" {
		t.Error("two runs could read the logs at once")
	}
	// A failed run is not repeated; only an evicted pod is replaced.
	if job["backoffLimit"] != float64(0) || at(job["podFailurePolicy"], "rules").([]any)[0].(map[string]any)["action"] != "Ignore" {
		t.Errorf("unexpected retry behaviour: %v", job)
	}
	if pod["priorityClassName"] != usagePriorityClass || at(pod, "nodeSelector", registryRoleLabel) != "worker" || pod["automountServiceAccountToken"] != false {
		t.Errorf("the collector is not a low-priority worker pod without an API token: %v", pod)
	}
	if priority := m.resource(t, "kubernetes:scheduling.k8s.io/v1:PriorityClass::"+usagePriorityClass); priority["value"].(float64) >= 0 || priority["preemptionPolicy"] != "Never" {
		t.Errorf("the collector could displace other pods: %v", priority)
	}
	if at(container, "resources", "limits", "memory") == nil || at(container, "resources", "limits", "cpu") == nil {
		t.Error("the collector has no memory or CPU limit")
	}
	if container["image"] != "ghcr.io/modelcontextprotocol/registry-usage-collector:main" {
		t.Errorf("staging runs %v", container["image"])
	}
	// Credentials reach the pod from Secrets only.
	plainSettings, _ := json.Marshal(container["env"])
	for _, private := range []string{"FAKEPASS", fakeEncoded, fakeGoogleKey, fakeAliases, "postgres://"} {
		if strings.Contains(string(plainSettings), private) {
			t.Errorf("%s is written into the pod specification", private)
		}
	}
	if at(container["envFrom"].([]any)[0], "secretRef", "name") != "registry-usage-collector" {
		t.Errorf("the settings do not come from the Secret: %v", container["envFrom"])
	}
	// A claim that waits for its first pod would hold the deployment up forever.
	if at(m.resource(t, "kubernetes:core/v1:PersistentVolumeClaim::registry-usage-collector-cache"), "metadata", "annotations", "pulumi.com/skipAwait") != "true" {
		t.Error("the deployment waits for the cache volume")
	}

	// The history is archived by the role that owns it, which has no access to
	// Grafana's own database in the same cluster, and goes to a bucket of its
	// own with the collector's Google key.
	backup, _ := json.Marshal(at(m.resource(t, "kubernetes:batch/v1:CronJob::registry-usage-history-backup"), "spec", "jobTemplate", "spec", "template", "spec"))
	for _, required := range []string{
		`"name":"registry-usage-collector-password"`, `{"name":"PGDATABASE","value":"registry_usage"}`,
		`{"name":"PGOPTIONS","value":"-c default_transaction_read_only=on"}`, `{"name":"PGSSLMODE","value":"require"}`,
		`{"name":"RESTIC_REPOSITORY","value":"gs:mcp-registry-staging-usage-backups:/usage-history-v1"}`,
		`"secretName":"registry-usage-collector-gcp"`, "--keep-daily", "restic check --read-data",
	} {
		if !strings.Contains(string(backup), required) {
			t.Errorf("the history backup lacks %s", required)
		}
	}
	// Not Grafana's database credentials, and nothing of the registry's backup.
	for _, forbidden := range []string{"grafana-pg-app", "grafana-pg-superuser", "k8up-repo-password", "k8up-backup-credentials", "postgres-custom-v1", "registry-pg", "FAKEPASS", fakeEncoded, fakeGoogleKey} {
		if strings.Contains(string(backup), forbidden) {
			t.Errorf("the history backup refers to %s", forbidden)
		}
	}

	// Grafana gets the read-only role's password from its Secret. The chart
	// registers what it renders with plain inputs, so no credential may be a value.
	for _, private := range []string{"FAKEPASS", fakeEncoded, fakeGoogleKey, fakeAliases} {
		if strings.Contains(m.raw, private) {
			t.Errorf("%s reached the Grafana chart values", private)
		}
	}
	reference, _ := m.values["envValueFrom"].(map[string]any)[usagePasswordEnv].(map[string]any)
	if fmt.Sprint(reference["secretKeyRef"]) != fmt.Sprint(map[string]any{"name": "registry-usage-grafana-password", "key": "password"}) {
		t.Errorf("%s does not come from the read-only role's Secret: %v", usagePasswordEnv, reference)
	}
	dataSources := m.resource(t, "kubernetes:core/v1:ConfigMap::grafana-datasources")["data"].(map[string]any)
	var dataSource struct {
		Datasources []struct {
			UID, User      string
			SecureJSONData map[string]string `yaml:"secureJsonData"`
		}
	}
	if err := yaml.Unmarshal([]byte(dataSources["usage.yaml"].(string)), &dataSource); err != nil || len(dataSource.Datasources) != 1 {
		t.Fatalf("usage.yaml does not provision one data source: %v", err)
	}
	if source := dataSource.Datasources[0]; source.UID != usageDataSourceUID || source.User != "registry_usage_grafana" || source.SecureJSONData["password"] != "$"+usagePasswordEnv {
		t.Errorf("the data source does not use the read-only role: %+v", source)
	}
	if dataSources["datasources.yaml"] == nil {
		t.Error("the existing data sources are gone")
	}
	dashboards := m.resource(t, "kubernetes:core/v1:ConfigMap::grafana-registry-usage-dashboards")["data"].(map[string]any)
	if len(dashboards) != 2 {
		t.Errorf("%d usage dashboards are provisioned", len(dashboards))
	}
	if annotations, _ := m.values["podAnnotations"].(map[string]any); annotations["checksum/usage"] == nil {
		t.Error("Grafana is not restarted to read the data source")
	}
	// This stack has no webhook, so it has no contact point for the rules to notify.
	if _, ok := m.resources["kubernetes:core/v1:ConfigMap::grafana-registry-alerting"]; ok {
		t.Error("alert rules are provisioned without alerting")
	}
}

func provisionedUsageRules(t *testing.T, m *usageMonitor) alertingProvisioning {
	t.Helper()
	files := m.resource(t, "kubernetes:core/v1:ConfigMap::grafana-registry-alerting")["data"].(map[string]any)
	var file alertingProvisioning
	text, _ := files[usageAlertingFile].(string)
	if err := yaml.Unmarshal([]byte(text), &file); err != nil || text == "" {
		t.Fatalf("%s is missing or unreadable: %v", usageAlertingFile, err)
	}
	if files["rules.yaml"] == nil || files["contact-points.yaml"] == nil {
		t.Error("the registry's own alerting files are gone")
	}
	return file
}

func TestUsageAlertRulesNeedTheFlagAndAlerting(t *testing.T) {
	m, err := deployGrafanaWithUsage(map[string]string{usageFlagKey: "true", alertWebhookKey: fakeWebhook}, alertWebhookKey)
	if err != nil {
		t.Fatal(err)
	}
	file := provisionedUsageRules(t, m)
	if len(file.Groups) != 1 || len(file.Groups[0].Rules) != 2 || len(file.DeleteRules) != 0 {
		t.Errorf("the two usage rules are not provisioned: %+v", file)
	}
	// Each feature restarts Grafana for its own files without undoing the other's trigger.
	annotations, _ := m.values["podAnnotations"].(map[string]any)
	if annotations["checksum/usage"] == nil || annotations["checksum/alerting"] == nil {
		t.Errorf("a restart trigger is missing: %v", annotations)
	}
	without, err := deployGrafanaWith(fakeWebhook, true)
	if err != nil {
		t.Fatal(err)
	}
	if before, _ := without.values["podAnnotations"].(map[string]any); before["checksum/alerting"] == annotations["checksum/alerting"] {
		t.Error("adding the usage rules does not restart Grafana")
	}
}

// Grafana keeps a provisioned rule and data source after their files are gone,
// so retiring has to delete them, and has to remove everything else.
func TestRetiredUsageCollectorCleansUpGrafana(t *testing.T) {
	m, err := deployGrafanaWithUsage(map[string]string{usageFlagKey: "false", alertWebhookKey: fakeWebhook}, alertWebhookKey)
	if err != nil {
		t.Fatal(err)
	}
	for identity := range m.resources {
		if strings.Contains(strings.ToLower(identity), "usage") || strings.HasPrefix(identity, "random:") {
			t.Errorf("%s is kept after retiring", identity)
		}
	}
	file := provisionedUsageRules(t, m)
	var provisioned alertingProvisioning
	if err := yaml.Unmarshal([]byte(usageAlertRules), &provisioned); err != nil {
		t.Fatal(err)
	}
	deleted := map[string]bool{}
	for _, rule := range file.DeleteRules {
		deleted[rule.UID] = true
	}
	for _, rule := range provisioned.Groups[0].Rules {
		if !deleted[rule.UID] {
			t.Errorf("rule %s stays in Grafana, locked, after retiring", rule.UID)
		}
	}
	if len(file.Groups) != 0 || len(deleted) != len(provisioned.Groups[0].Rules) {
		t.Errorf("retiring does not delete exactly the usage rules: %+v", file)
	}
	dataSources := m.resource(t, "kubernetes:core/v1:ConfigMap::grafana-datasources")["data"].(map[string]any)
	var dataSource struct {
		Datasources       []any
		DeleteDatasources []struct{ Name string } `yaml:"deleteDatasources"`
	}
	if err := yaml.Unmarshal([]byte(dataSources["usage.yaml"].(string)), &dataSource); err != nil {
		t.Fatal(err)
	}
	if len(dataSource.Datasources) != 0 || len(dataSource.DeleteDatasources) != 1 || !strings.Contains(usageDataSourceFile, "name: "+dataSource.DeleteDatasources[0].Name+"\n") {
		t.Errorf("retiring does not delete the usage data source: %+v", dataSource)
	}
	// The password's Secret is gone, so Grafana must not refer to it. The
	// dashboards go because their provider is no longer mounted.
	if strings.Contains(m.raw, usagePasswordEnv) || strings.Contains(m.raw, usageDashboardsPath) {
		t.Errorf("Grafana still refers to the retired collector: %s", m.raw)
	}
	if annotations, _ := m.values["podAnnotations"].(map[string]any); annotations["checksum/usage"] == nil {
		t.Error("Grafana is not restarted to delete the data source")
	}
}

// The webhook may be removed before the collector is retired, or with it. The
// rules must still be deleted then, with nothing else of alerting provisioned.
func TestRetiredUsageCollectorDeletesItsRulesWithoutAlerting(t *testing.T) {
	m, err := deployGrafanaWithUsage(map[string]string{usageFlagKey: "false"})
	if err != nil {
		t.Fatal(err)
	}
	files := m.resource(t, "kubernetes:core/v1:ConfigMap::grafana-registry-alerting")["data"].(map[string]any)
	var file alertingProvisioning
	text, _ := files[usageAlertingFile].(string)
	if err := yaml.Unmarshal([]byte(text), &file); err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || len(file.DeleteRules) != 2 || len(file.Groups) != 0 || len(file.ContactPoints) != 0 {
		t.Errorf("expected only the removal of the two usage rules: %v", files)
	}
	// No contact point is provisioned, so Grafana needs no webhook.
	if _, ok := m.resources["kubernetes:core/v1:Secret::grafana-alerting"]; ok || strings.Contains(m.raw, alertWebhookEnv) {
		t.Error("a webhook is expected where none is configured")
	}
	annotations, _ := m.values["podAnnotations"].(map[string]any)
	if m.alertingMounts() != 1 || annotations["checksum/alerting"] == nil {
		t.Error("Grafana is not restarted with the removal file mounted")
	}

	// Enabled without the webhook there is still nothing to notify, so no rules.
	enabled, err := deployGrafanaWithUsage(map[string]string{usageFlagKey: "true"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := enabled.resources["kubernetes:core/v1:ConfigMap::grafana-registry-alerting"]; ok || enabled.alertingMounts() != 0 {
		t.Error("usage rules are provisioned without a contact point to notify")
	}
}

func TestUsageCollectorFollowsTheRegistryRelease(t *testing.T) {
	m := &usageMonitor{grafanaChartMonitor{backupMonitor: backupMonitor{resources: map[string]resource.PropertyMap{}}}}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		provider, err := kubernetes.NewProvider(ctx, "provider", &kubernetes.ProviderArgs{})
		if err != nil {
			return err
		}
		ns, err := corev1.NewNamespace(ctx, "monitoring", &corev1.NamespaceArgs{Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("monitoring")}})
		if err != nil {
			return err
		}
		cluster := &providers.ProviderInfo{Provider: provider, UsageCollectorKey: pulumi.String(fakeGoogleKey).ToStringOutput()}
		_, err = deployUsageCollector(ctx, cluster, ns, "prod", nil, nil)
		return err
	}, pulumi.WithMocks("mcp-registry-infra", "test", m), func(info *pulumi.RunInfo) {
		info.Config = map[string]string{usageFlagKey: "true", "mcp-registry:imageTag": "1.2.3", "gcp:project": "mcp-registry-prod"}
	})
	if err != nil {
		t.Fatal(err)
	}
	container := at(m.resource(t, "kubernetes:batch/v1:CronJob::registry-usage-collector"), "spec", "jobTemplate", "spec", "template", "spec").(map[string]any)["containers"].([]any)[0].(map[string]any)
	if container["image"] != "ghcr.io/modelcontextprotocol/registry-usage-collector:1.2.3" {
		t.Errorf("production runs %v", container["image"])
	}
	settings, _ := json.Marshal(container["env"])
	for _, want := range []string{`"mcp-registry-prod"`, `"ingress-nginx"`, `"registry_usage_grafana"`} {
		if !strings.Contains(string(settings), want) {
			t.Errorf("the collector's settings lack %s: %s", want, settings)
		}
	}
}

func TestUsageDashboardsCanBeProvisioned(t *testing.T) {
	files, err := usageDashboardFiles()
	if err != nil {
		t.Fatal(err)
	}
	existing, err := registryDashboardFiles()
	if err != nil {
		t.Fatal(err)
	}
	// Both sets are provisioned into the same folder, which allows a uid and a title once.
	uids, titles := map[string]string{}, map[string]string{}
	for _, set := range []map[string]string{existing, files} {
		for name, text := range set {
			var dashboard map[string]any
			if err := json.Unmarshal([]byte(text), &dashboard); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			uid, _ := dashboard["uid"].(string)
			title, _ := dashboard["title"].(string)
			if uid == "" || title == "" || uids[uid] != "" || titles[title] != "" {
				t.Errorf("%s: needs a uid and a title that no other dashboard has", name)
			}
			uids[uid], titles[title] = name, name
		}
	}
	for name, text := range files {
		var dashboard map[string]any
		_ = json.Unmarshal([]byte(text), &dashboard)
		if _, ok := dashboard["id"]; ok {
			t.Errorf("%s: remove the database id", name)
		}
		found := map[string]bool{}
		dataSourceIDs(dashboard, found)
		for id := range found {
			if id != usageDataSourceUID && id != "-- Grafana --" {
				t.Errorf("%s: uses data source %s, not the provisioned %s", name, id, usageDataSourceUID)
			}
		}
		if len(found) == 0 {
			t.Errorf("%s: names no data source, and the usage database is not Grafana's default", name)
		}
	}
	// The dashboards link to each other by uid.
	for _, uid := range []string{"registry-usage", "registry-publishing"} {
		if uids[uid] == "" {
			t.Errorf("no dashboard has uid %s", uid)
		}
	}
}

func TestUsageAlertRulesCanBeProvisioned(t *testing.T) {
	var file alertingProvisioning
	if err := yaml.Unmarshal([]byte(usageAlertRules), &file); err != nil {
		t.Fatal(err)
	}
	own, err := registryAlertingFiles()
	if err != nil {
		t.Fatal(err)
	}
	contactPoints, taken := map[string]bool{}, map[string]bool{}
	for _, text := range own {
		var existing alertingProvisioning
		if err := yaml.Unmarshal([]byte(text), &existing); err != nil {
			t.Fatal(err)
		}
		for _, contactPoint := range existing.ContactPoints {
			contactPoints[contactPoint.Name] = true
		}
		for _, group := range existing.Groups {
			taken[group.Folder+"/"+group.Name] = true
			for _, rule := range group.Rules {
				taken[rule.UID] = true
			}
		}
	}
	// Grafana expands $NAME anywhere in an alerting file.
	if strings.Contains(usageAlertRules, "$") || strings.Contains(usageAlertRulesRetired, "$") {
		t.Error("an alerting file contains a dollar")
	}
	if len(file.Groups) != 1 || len(file.Groups[0].Rules) == 0 {
		t.Fatal("expected one group of usage rules")
	}
	group := file.Groups[0]
	// A group provisioned from two files is replaced by whichever is read last.
	if taken[group.Folder+"/"+group.Name] {
		t.Errorf("group %s is already provisioned by the registry's own rules", group.Name)
	}
	for _, rule := range group.Rules {
		if rule.UID == "" || taken[rule.UID] {
			t.Errorf("rule %q: needs a uid of its own", rule.Title)
		}
		taken[rule.UID] = true
		// Grafana refuses to start when a rule notifies a contact point that does not exist.
		if !contactPoints[rule.NotificationSettings.Receiver] {
			t.Errorf("rule %q: notifies %q, which is not in contact-points.yaml", rule.Title, rule.NotificationSettings.Receiver)
		}
		for _, query := range rule.Data {
			if query.DatasourceUID != "__expr__" && query.DatasourceUID != usageDataSourceUID {
				t.Errorf("rule %q: uses data source %s, not the provisioned %s", rule.Title, query.DatasourceUID, usageDataSourceUID)
			}
		}
	}
}
