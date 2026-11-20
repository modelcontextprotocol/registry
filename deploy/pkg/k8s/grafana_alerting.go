package k8s

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// Grafana provisions every file in this directory when it starts, and only then.
//
//go:embed alerting/*.yaml
var registryAlerting embed.FS

const (
	alertWebhookConfig = "alertDiscordWebhook"
	// contact-points.yaml refers to the webhook by this name.
	alertWebhookEnv      = "ALERT_DISCORD_WEBHOOK"
	registryAlertingPath = "/etc/grafana/provisioning/alerting"
)

func registryAlertingFiles() (map[string]string, error) {
	entries, err := fs.ReadDir(registryAlerting, "alerting")
	if err != nil {
		return nil, err
	}
	files := make(map[string]string, len(entries))
	for _, entry := range entries {
		data, err := registryAlerting.ReadFile(path.Join("alerting", entry.Name()))
		if err != nil {
			return nil, err
		}
		files[entry.Name()] = string(data)
	}
	return files, nil
}

// alertWebhook returns the configured Discord webhook, or "" where alerting is
// not configured. Its errors never contain the value.
func alertWebhook(ctx *pulumi.Context) (string, error) {
	key := "mcp-registry:" + alertWebhookConfig
	webhook := strings.TrimSpace(config.New(ctx, "mcp-registry").Get(alertWebhookConfig))
	if webhook == "" {
		return "", nil
	}
	if !ctx.IsConfigSecret(key) {
		return "", errors.New(key + " is stored unencrypted; set it with `pulumi config set --secret` and replace the webhook in Discord")
	}
	// Grafana starts with a webhook that is not a URL and then fails every notification.
	parsed, err := url.Parse(webhook)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || strings.ContainsAny(webhook, " \t\r\n") {
		return "", errors.New(key + " must be an https URL")
	}
	return webhook, nil
}

// grafanaAlerting is what the Grafana chart needs to provision alerting files.
// Rules and the contact point are provisioned only where the webhook is
// configured, so that no environment gets rules that notify a contact point it
// does not have. It is nil where there is nothing to provision.
type grafanaAlerting struct {
	mount pulumi.Map
	// webhook is nil where only removals are provisioned.
	webhook  pulumi.Map
	checksum string
}

// registryAlertingProvisioning creates the alerting ConfigMap and the webhook
// Secret. Grafana adopts an existing rule or contact point that has the same
// uid, then refuses edits made in the UI. Removing these files deletes nothing
// and leaves what they provisioned locked.
//
// rules and removals are files other features provision beside the registry's
// own, by file name. rules notify the contact point, so they need the webhook
// like the registry's own. removals only delete what was provisioned earlier
// and are provisioned without it: a rule must not outlive its feature, locked,
// because the webhook was removed first.
func registryAlertingProvisioning(ctx *pulumi.Context, provider *kubernetes.Provider, ns *corev1.Namespace, rules, removals map[string]string) (*grafanaAlerting, error) {
	webhook, err := alertWebhook(ctx)
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	if webhook != "" {
		if files, err = registryAlertingFiles(); err != nil {
			return nil, fmt.Errorf("failed to read Grafana alerting files: %w", err)
		}
		for name, text := range rules {
			if _, taken := files[name]; taken {
				return nil, fmt.Errorf("two Grafana alerting files are named %s", name)
			}
			files[name] = text
		}
	}
	for name, text := range removals {
		if _, taken := files[name]; taken {
			return nil, fmt.Errorf("two Grafana alerting files are named %s", name)
		}
		files[name] = text
	}
	if len(files) == 0 {
		return nil, nil
	}
	alerting := &grafanaAlerting{}
	if webhook != "" {
		secret, err := corev1.NewSecret(ctx, "grafana-alerting", &corev1.SecretArgs{
			Metadata:   &metav1.ObjectMetaArgs{Name: pulumi.String("grafana-alerting"), Namespace: ns.Metadata.Name()},
			StringData: pulumi.StringMap{alertWebhookEnv: pulumi.ToSecret(pulumi.String(webhook)).(pulumi.StringOutput)},
			Type:       pulumi.String("Opaque"),
		}, pulumi.Provider(provider))
		if err != nil {
			return nil, err
		}
		alerting.webhook = pulumi.Map{
			"secretKeyRef": pulumi.Map{
				"name": secret.Metadata.Name(),
				"key":  pulumi.String(alertWebhookEnv),
			},
		}
	}
	configMap, err := corev1.NewConfigMap(ctx, "grafana-registry-alerting", &corev1.ConfigMapArgs{
		Metadata: &metav1.ObjectMetaArgs{Name: pulumi.String("grafana-registry-alerting"), Namespace: ns.Metadata.Name()},
		Data:     pulumi.ToStringMap(files),
	}, pulumi.Provider(provider))
	if err != nil {
		return nil, err
	}

	// Kubernetes updates the mounted files in place and Grafana does not reread
	// them, so the pod has to be replaced when a file or the webhook changes.
	// The annotation is a plain chart value, and nothing else derived from the
	// webhook may enter the chart values: the chart resolves secret values and
	// renders them as plain inputs. These 48 bits of a hash identify a change
	// but cannot be turned back into the webhook's random token.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		hash.Write([]byte(name + "\x00" + files[name] + "\x00"))
	}
	hash.Write([]byte(webhook))

	alerting.mount = pulumi.Map{
		"name":      pulumi.String("grafana-registry-alerting"),
		"configMap": configMap.Metadata.Name().Elem(),
		"mountPath": pulumi.String(registryAlertingPath),
		"readOnly":  pulumi.Bool(true),
	}
	alerting.checksum = hex.EncodeToString(hash.Sum(nil))[:12]
	return alerting, nil
}

// addTo adds the alerting mount, webhook variable and restart trigger to the
// Grafana chart values. Without alerting the values are returned unchanged.
func (a *grafanaAlerting) addTo(values pulumi.Map) pulumi.Map {
	if a == nil {
		return values
	}
	values["extraConfigmapMounts"] = append(values["extraConfigmapMounts"].(pulumi.Array), a.mount)
	if a.webhook != nil {
		values["envValueFrom"].(pulumi.Map)[alertWebhookEnv] = a.webhook
	}
	addPodAnnotation(values, "checksum/alerting", a.checksum)
	return values
}
