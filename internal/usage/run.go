package usage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/auth/credentials"
)

const (
	dayLayout = "2006-01-02"
	// A day is collected once it has been over for this long, so that the last
	// access records and counter samples have arrived.
	settleTime = 15 * time.Minute
)

// Options are what one run was asked to do.
type Options struct {
	// Days to collect, even if already stored. Empty means every day in the
	// look-back window that has not been stored yet.
	Days []time.Time
	// MarkdownDir, if set, receives each day's report as <day>.md.
	MarkdownDir string
	// NoStore collects and reports without touching any database.
	NoStore bool
	// AllowDegraded stores a day even though a source it needs was unavailable.
	AllowDegraded bool
	// ResyncPublishing reads every published version again instead of only the changed ones.
	ResyncPublishing bool
	// Version identifies the collector in the rows it writes.
	Version string
	Stdout  io.Writer
	Logf    func(format string, args ...any)
	Now     func() time.Time
}

// Collector runs the collection against a set of sources and a store.
type Collector struct {
	Sources  Sources
	Limits   Limits
	Options  Options
	Lookback int
	// Store is nil when nothing is to be stored; Registry is nil when the
	// catalogue is not to be refreshed.
	Store    *Store
	Registry *Registry
	// RegistryUnavailable says the registry database was configured but could
	// not be reached, so the catalogue is as an earlier run left it.
	RegistryUnavailable bool

	// lastChance is the oldest day in a scheduled run's look-back window. The
	// next scheduled run no longer looks at it.
	lastChance time.Time
}

func (c *Collector) logf(format string, args ...any) {
	if c.Options.Logf != nil {
		c.Options.Logf(format, args...)
	}
}

func (c *Collector) now() time.Time {
	if c.Options.Now != nil {
		return c.Options.Now().UTC()
	}
	return time.Now().UTC()
}

// Run uses the real sources described by the settings.
func Run(ctx context.Context, settings Settings, options Options) error {
	if err := settings.Validate(!options.NoStore); err != nil {
		return err
	}
	if options.NoStore && len(options.Days) == 0 {
		return errors.New("--no-store needs at least one --day")
	}
	logf := options.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	creds, err := credentials.DetectDefault(&credentials.DetectOptions{Scopes: []string{loggingScope}})
	if err != nil {
		return fmt.Errorf("no Google credentials found: %w", err)
	}
	quotaProject := settings.GCPQuotaProject
	if quotaProject == "" {
		quotaProject, _ = creds.QuotaProjectID(ctx)
	}
	client := &http.Client{}
	collector := &Collector{
		Limits:   Limits{MaxRecords: settings.MaxRecords, DollarsPerGiB: settings.DollarsPerGiB},
		Options:  options,
		Lookback: settings.LookbackDays,
		Sources: Sources{
			Logs: &CloudLogging{
				Project: settings.GCPProject, Cluster: settings.ClusterName, Namespace: settings.LogNamespace,
				Container: settings.LogContainer, QuotaProject: quotaProject, Client: client, Logf: logf,
				ReadsPerMinute: settings.LogReadsPerMinute,
				Token: func(ctx context.Context) (string, error) {
					token, err := creds.Token(ctx)
					if err != nil {
						return "", err
					}
					return token.Value, nil
				},
			},
			Metrics: &Prometheus{BaseURL: settings.MetricsURL, Client: client},
			Logf:    logf,
		},
	}
	var startup []error
	if !options.NoStore {
		store, err := OpenStore(ctx, settings.DatabaseURL)
		if err != nil {
			return err
		}
		defer store.Close()
		collector.Store = store
		if settings.ReaderRole != "" {
			if err := store.GrantReader(ctx, settings.ReaderRole); err != nil {
				// The days are still collected; the dashboards cannot read them yet.
				err = fmt.Errorf("reader role: %w", err)
				logf("%v", err)
				startup = append(startup, err)
			}
		}
		if settings.RegistryDatabaseURL != "" {
			registry, err := OpenRegistry(ctx, settings.RegistryDatabaseURL)
			if err != nil {
				logf("%v", err)
				startup = append(startup, err)
				collector.RegistryUnavailable = true
			} else {
				defer registry.Close()
				collector.Registry = registry
			}
		}
	}
	aliases, err := ParseOrganizationAliases(settings.OrganizationAliases)
	if err != nil {
		return err
	}
	collector.Sources.Networks = LoadNetworkTable(ctx, NetworkFiles{
		CacheDir: settings.CacheDir, OwnersURL: settings.NetworkOwnerURL, TypesURL: settings.NetworkTypesURL, Aliases: aliases,
		UserAgent: "registry-usage-collector/" + options.Version + " (+https://github.com/modelcontextprotocol/registry)",
		Client:    client, Logf: logf,
	})
	return errors.Join(append(startup, collector.Run(ctx))...)
}

// Run refreshes the catalogue, then collects each day that is due. A day that
// fails does not stop the others; the error returned names every failure.
func (c *Collector) Run(ctx context.Context) error {
	var failures []error
	if c.Store != nil {
		release, err := c.Store.Lock(ctx)
		if err != nil {
			return err
		}
		defer release()
		// A day collected against a catalogue that is behind would count lookups of
		// newly published servers as unknown names, and would never be corrected.
		c.Sources.CatalogueStale = c.RegistryUnavailable
		if c.Registry != nil {
			result, err := SyncPublishing(ctx, c.Registry, c.Store, PublishingOptions{Resync: c.Options.ResyncPublishing, Pause: publishingPagePause})
			if err != nil {
				c.logf("catalogue sync failed after %d versions: %v", result.Changed, err)
				failures = append(failures, fmt.Errorf("catalogue sync: %w", err))
				c.Sources.CatalogueStale = true
			} else {
				c.logf("catalogue: read %d changed versions in %d pages", result.Changed, result.Pages)
			}
		}
		catalogue, err := c.Store.Catalogue(ctx)
		if err != nil {
			return errors.Join(append(failures, fmt.Errorf("catalogue: %w", err))...)
		}
		c.Sources.Catalogue = catalogue
	}
	days, err := c.due(ctx)
	if err != nil {
		return errors.Join(append(failures, err)...)
	}
	if len(days) == 0 {
		c.logf("no days to collect")
	}
	for _, day := range days {
		if ctx.Err() != nil {
			failures = append(failures, ctx.Err())
			break
		}
		if err := c.collect(ctx, day); err != nil {
			c.logf("%s failed: %v", day.Format(dayLayout), err)
			failures = append(failures, fmt.Errorf("%s: %w", day.Format(dayLayout), err))
		}
	}
	return errors.Join(failures...)
}

// due lists the days this run should collect, oldest first.
func (c *Collector) due(ctx context.Context) ([]time.Time, error) {
	now := c.now()
	c.lastChance = time.Time{}
	if len(c.Options.Days) > 0 {
		for _, day := range c.Options.Days {
			if day.AddDate(0, 0, 1).Add(settleTime).After(now) {
				return nil, fmt.Errorf("%s is not a full UTC day that ended at least %s ago", day.Format(dayLayout), settleTime)
			}
		}
		return c.Options.Days, nil
	}
	if c.Store == nil {
		return nil, nil
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	end := today
	if now.Sub(today) < settleTime {
		end = today.AddDate(0, 0, -1)
	}
	c.lastChance = today.AddDate(0, 0, -c.Lookback)
	return c.Store.MissingDays(ctx, c.lastChance, end)
}

func (c *Collector) collect(ctx context.Context, day time.Time) error {
	c.logf("collecting %s", day.Format(dayLayout))
	limits := c.Limits
	limits.Require = c.tolerance(day)
	sample, err := CollectDay(ctx, c.Sources, limits, day)
	if err != nil && !errors.Is(err, ErrNoRecords) && !errors.Is(err, ErrNoTraffic) {
		return err
	}
	if reportErr := c.report(sample); reportErr != nil {
		return reportErr
	}
	if errors.Is(err, ErrNoTraffic) {
		// Nothing is stored, so the day is looked at again while it is in the
		// look-back window. That costs a few small requests.
		c.logf("%s has nothing to collect: %v", day.Format(dayLayout), err)
		return nil
	}
	if err != nil || c.Store == nil {
		return err
	}
	daily, shares := sample.Rows()
	if err := c.Store.ReplaceDay(ctx, daily, shares, c.Options.Version, c.now()); err != nil {
		return fmt.Errorf("could not store the day: %w", err)
	}
	c.logf("stored %s: %d requests, %.1f GiB, %d rows from %d records; log estimate vs counters: requests %s, data %s",
		day.Format(dayLayout), daily.Requests, daily.GiB, len(shares), daily.RecordsSampled,
		ratioOrUnknown(daily.LogRequestsRatio), ratioOrUnknown(daily.LogDataRatio))
	if missing := sample.Degraded(); len(missing) > 0 {
		c.logf("%s was stored without %s; see its caveats", day.Format(dayLayout), strings.Join(missing, " and "))
	}
	return nil
}

// tolerance decides what a day may lack and still be stored. A stored day is
// not collected again on its own, so a day is normally stored only when it is
// complete. The exception is a day's last chance: once the next scheduled run
// no longer looks at it, storing it with its caveats is better than losing it.
func (c *Collector) tolerance(day time.Time) Tolerance {
	switch {
	case c.Store == nil || c.Options.AllowDegraded:
		return AnyDay
	case !c.lastChance.IsZero() && !day.After(c.lastChance):
		return ScalableDay
	}
	return CompleteDay
}

func ratioOrUnknown(ratio *float64) string {
	if ratio == nil {
		return "unknown"
	}
	return fmt.Sprintf("%.3f", *ratio)
}

// report writes the day's Markdown report where it was asked for.
func (c *Collector) report(sample *Sample) error {
	if c.Options.MarkdownDir == "" {
		if c.Options.NoStore && c.Options.Stdout != nil {
			_, err := io.WriteString(c.Options.Stdout, sample.Markdown())
			return err
		}
		return nil
	}
	if err := os.MkdirAll(c.Options.MarkdownDir, 0o750); err != nil {
		return err
	}
	path := filepath.Join(c.Options.MarkdownDir, sample.Start.Format(dayLayout)+".md")
	if err := os.WriteFile(path, []byte(sample.Markdown()), 0o600); err != nil {
		return err
	}
	c.logf("wrote %s", path)
	return nil
}
