// Package usagecollector holds what the cluster provider and the Kubernetes
// deployment both need to know about the daily usage collector. See
// "Usage collector" in deploy/README.md.
package usagecollector

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"

	"github.com/modelcontextprotocol/registry/deploy/infra/pkg/k8s/gatewaycanary"
)

const (
	flagConfig    = "usageCollector"
	aliasesConfig = "usageCollectorOrganizationAliases"

	// ServiceAccount reads the gateway's access records. An operator created it
	// in each project; the deployment account may not create accounts.
	ServiceAccount = "registry-usage-reader"

	// CredentialGeneration is recorded with the account key and the three
	// database passwords. Raising it replaces all four.
	CredentialGeneration = "1"
)

// State is what mcp-registry:usageCollector asks for.
type State int

const (
	// Absent leaves the environment exactly as it is.
	Absent State = iota
	// Enabled runs the collector and provisions its dashboards and alerts.
	Enabled
	// Retired removes the collector and has Grafana delete what was
	// provisioned for it. The stored history is kept.
	Retired
)

// Mode reads the flag.
func Mode(ctx *pulumi.Context) (State, error) {
	switch config.New(ctx, "mcp-registry").Get(flagConfig) {
	case "":
		return Absent, nil
	case "true":
		return Enabled, nil
	case "false":
		return Retired, nil
	}
	return Absent, errors.New(`mcp-registry:` + flagConfig + ` must be "true" or "false"`)
}

// Validate runs before any resource is created.
func Validate(ctx *pulumi.Context) error {
	mode, err := Mode(ctx)
	if err != nil || mode != Enabled {
		return err
	}
	conf := config.New(ctx, "mcp-registry")
	environment := conf.Get("environment")
	if conf.Get("provider") != "gcp" || config.New(ctx, "gcp").Get("project") == "" || (environment != "staging" && environment != "prod") {
		return errors.New("mcp-registry:" + flagConfig + " needs the hosted staging or production stack: the collector reads Cloud Logging")
	}
	// The request counters the collector scales by come from the Gateway's Vector.
	if !gatewaycanary.Standalone(ctx) {
		return errors.New("mcp-registry:" + flagConfig + " needs the standalone Gateway, which produces the request counters")
	}
	_, err = Aliases(ctx)
	return err
}

// Aliases returns the optional organization aliases, or "". The value names
// real organizations, so it is only accepted encrypted and its errors never
// contain it. The format is the collector's (ParseOrganizationAliases): a
// value it rejects would stop every run.
func Aliases(ctx *pulumi.Context) (string, error) {
	key := "mcp-registry:" + aliasesConfig
	aliases := strings.TrimSpace(config.New(ctx, "mcp-registry").Get(aliasesConfig))
	if aliases == "" {
		return "", nil
	}
	if !ctx.IsConfigSecret(key) {
		return "", errors.New(key + " is stored unencrypted; set it with `pulumi config set --secret`")
	}
	for _, pair := range strings.FieldsFunc(aliases, func(r rune) bool { return r == ';' || r == '\n' || r == '\r' }) {
		if strings.TrimSpace(pair) == "" {
			continue
		}
		name, label, found := strings.Cut(pair, "=")
		name, label = strings.TrimSpace(name), strings.TrimSpace(label)
		if !found || utf8.RuneCountInString(name) < 3 || label == "" || utf8.RuneCountInString(label) > 64 ||
			strings.ContainsFunc(label, func(r rune) bool { return r == '|' || r == '=' || unicode.IsControl(r) }) {
			return "", errors.New(key + " must be written KEY=Name;KEY=Name, with keys of at least 3 characters and names of at most 64")
		}
	}
	return aliases, nil
}
