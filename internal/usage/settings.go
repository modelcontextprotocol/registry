package usage

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	env "github.com/caarlos0/env/v11"
)

// Settings are read from USAGE_COLLECTOR_* environment variables. Nothing
// about a particular environment is built into the program.
type Settings struct {
	// Where the gateway's access records are: the Google Cloud project, and the
	// cluster, namespace and container that write them.
	GCPProject   string `env:"GCP_PROJECT"`
	ClusterName  string `env:"CLUSTER_NAME"`
	LogNamespace string `env:"LOG_NAMESPACE" envDefault:"ingress-nginx"`
	LogContainer string `env:"LOG_CONTAINER" envDefault:"nginx"`
	// GCPQuotaProject is the project charged for the log reads. Only needed
	// with a user login, which has no project of its own.
	GCPQuotaProject string `env:"GCP_QUOTA_PROJECT"`

	// MetricsURL is the base URL of the Prometheus-compatible store that holds
	// the gateway's request counters.
	MetricsURL string `env:"METRICS_URL"`

	// DatabaseURL is where the usage tables live. RegistryDatabaseURL is the
	// registry's own database, read only for what has been published; without
	// it the catalogue is left as it is.
	DatabaseURL         string `env:"DATABASE_URL"`
	RegistryDatabaseURL string `env:"REGISTRY_DATABASE_URL"`
	// ReaderRole is an existing database role that is given read-only access
	// to what the dashboards query, each time the collector starts.
	ReaderRole string `env:"READER_ROLE"`

	// LookbackDays is how far back a run looks for days it has not stored.
	// The gateway counters are kept for 14 days; older days cannot be scaled.
	LookbackDays int `env:"LOOKBACK_DAYS" envDefault:"13"`
	// MaxRecords is about how many access records are read for a day. A day
	// that holds more is read as a sample of about this many.
	MaxRecords int `env:"MAX_RECORDS" envDefault:"100000"`
	// LogReadsPerMinute paces the requests to Cloud Logging, which allows a
	// project 60 read requests a minute.
	LogReadsPerMinute int     `env:"LOG_READS_PER_MINUTE" envDefault:"50"`
	DollarsPerGiB     float64 `env:"DOLLARS_PER_GIB" envDefault:"0.10"`

	// OrganizationAliases name organizations' own networks, written as
	// "KEY=Name;KEY=Name". See ParseOrganizationAliases.
	OrganizationAliases string `env:"ORGANIZATION_ALIASES"`

	// CacheDir keeps the public network reference files between runs.
	CacheDir        string `env:"CACHE_DIR"`
	NetworkOwnerURL string `env:"NETWORK_OWNERS_URL" envDefault:"https://iptoasn.com/data/ip2asn-v4.tsv.gz"`
	NetworkTypesURL string `env:"NETWORK_TYPES_URL" envDefault:"https://www.peeringdb.com/api/net?fields=asn,name,info_type"`
}

// LoadSettings reads the settings from the environment.
func LoadSettings() (Settings, error) {
	var settings Settings
	if err := env.ParseWithOptions(&settings, env.Options{Prefix: "USAGE_COLLECTOR_"}); err != nil {
		return settings, err
	}
	if settings.CacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			base = os.TempDir()
		}
		settings.CacheDir = filepath.Join(base, "registry-usage-collector")
	}
	return settings, nil
}

// PostgreSQL truncates longer identifiers.
const maxRoleNameLength = 63

// These values are placed inside a quoted string in the log filter.
var filterValue = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

// Validate reports every problem with the settings at once. store says
// whether this run writes to the usage database.
func (s Settings) Validate(store bool) error {
	var problems []error
	for _, field := range []struct{ name, value string }{
		{"GCP_PROJECT", s.GCPProject}, {"CLUSTER_NAME", s.ClusterName}, {"LOG_NAMESPACE", s.LogNamespace}, {"LOG_CONTAINER", s.LogContainer},
	} {
		switch {
		case field.value == "":
			problems = append(problems, fmt.Errorf("USAGE_COLLECTOR_%s is required", field.name))
		case !filterValue.MatchString(field.value):
			problems = append(problems, fmt.Errorf("USAGE_COLLECTOR_%s may only contain letters, digits, dots, colons, underscores and hyphens", field.name))
		}
	}
	if s.GCPQuotaProject != "" && !filterValue.MatchString(s.GCPQuotaProject) {
		problems = append(problems, errors.New("USAGE_COLLECTOR_GCP_QUOTA_PROJECT is not a project ID"))
	}
	if parsed, err := url.Parse(s.MetricsURL); s.MetricsURL == "" || err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		problems = append(problems, errors.New("USAGE_COLLECTOR_METRICS_URL must be an http or https URL"))
	}
	if store && s.DatabaseURL == "" {
		problems = append(problems, errors.New("USAGE_COLLECTOR_DATABASE_URL is required unless --no-store is given"))
	}
	for _, limit := range []struct {
		ok      bool
		problem string
	}{
		{s.LookbackDays >= 1 && s.LookbackDays <= 30, "USAGE_COLLECTOR_LOOKBACK_DAYS must be between 1 and 30, the log retention"},
		{s.MaxRecords >= 1000, "USAGE_COLLECTOR_MAX_RECORDS must be at least 1000"},
		{s.LogReadsPerMinute >= 1 && s.LogReadsPerMinute <= 600, "USAGE_COLLECTOR_LOG_READS_PER_MINUTE must be between 1 and 600"},
		{s.DollarsPerGiB >= 0, "USAGE_COLLECTOR_DOLLARS_PER_GIB cannot be negative"},
	} {
		if !limit.ok {
			problems = append(problems, errors.New(limit.problem))
		}
	}
	if _, err := ParseOrganizationAliases(s.OrganizationAliases); err != nil {
		problems = append(problems, fmt.Errorf("USAGE_COLLECTOR_ORGANIZATION_ALIASES: %w", err))
	}
	if len(s.ReaderRole) > maxRoleNameLength || strings.ContainsRune(s.ReaderRole, 0) {
		problems = append(problems, errors.New("USAGE_COLLECTOR_READER_ROLE is not a role name"))
	}
	return errors.Join(problems...)
}
