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
	identityConfig = "usageIdentityPhase"
	flagConfig     = "usageCollector"
	aliasesConfig  = "usageCollectorOrganizationAliases"

	// The KSA names are part of their GKE IAM principal identifiers.
	CollectorAccount    = "registry-usage-collector"
	BackupAccount       = "registry-usage-history-backup"
	MonitoringNamespace = "monitoring"

	// LegacyServiceAccount is the operator-owned account used before cutover.
	LegacyServiceAccount = "registry-usage-reader"

	// CredentialGeneration is recorded with the account key and the three
	// database passwords. Raising it replaces the credentials still managed.
	CredentialGeneration = "1"

	// BackupRepository is where, in the backup bucket, the history's Restic
	// repository is kept.
	BackupRepository = "usage-history-v1"
	// BackupPassword is what Restic requires to open that repository. It is
	// not a secret: a generated one would be lost with the Pulumi state that
	// held it, and the backups with it. Access to the bucket protects them.
	BackupPassword = "registry-usage-history"
)

// BackupBucket holds the backups of an environment's usage history, and
// nothing else, so that what can write them cannot reach any other backup.
func BackupBucket(environment string) string {
	return "mcp-registry-" + environment + "-usage-backups"
}

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

// IdentityPhase stages the migration of existing stacks. An unset phase preserves
// the deployed jobs until the worker pool change is approved.
type IdentityPhase string

const (
	Legacy  IdentityPhase = ""
	Prepare IdentityPhase = "prepare"
	Cutover IdentityPhase = "cutover"
	Keyless IdentityPhase = "keyless"
)

func Identity(ctx *pulumi.Context) IdentityPhase {
	return IdentityPhase(config.New(ctx, "mcp-registry").Get(identityConfig))
}

func (p IdentityPhase) UsesMetadata() bool { return p == Cutover || p == Keyless }

// Validate runs before any resource is created.
func Validate(ctx *pulumi.Context) error {
	phase := Identity(ctx)
	switch phase {
	case Legacy:
	case Prepare, Cutover, Keyless:
		if config.New(ctx, "mcp-registry").Get("provider") != "gcp" {
			return errors.New("usageIdentityPhase requires GKE")
		}
	default:
		return errors.New("usageIdentityPhase must be prepare, cutover, keyless or unset")
	}
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
