package usage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCollector wires a scenario's fake sources to a store, as Run does with the real ones.
func newCollector(t *testing.T, f *fixture, store *Store, options Options) *Collector {
	t.Helper()
	options.Version = "test"
	options.Logf = func(format string, args ...any) {
		f.log.WriteString(fmt.Sprintf(format, args...) + "\n")
	}
	return &Collector{Sources: f.sources, Limits: f.limits, Options: options, Lookback: 4, Store: store}
}

func storedDays(t *testing.T, store *Store) []string {
	t.Helper()
	stored, err := store.StoredDays(t.Context(), time.Unix(0, 0), time.Now().AddDate(1, 0, 0))
	require.NoError(t, err)
	var days []string
	for day := range stored {
		days = append(days, day.Format(dayLayout))
	}
	return days
}

func at(text string) func() time.Time {
	return func() time.Time {
		when, err := time.Parse(time.RFC3339, text)
		if err != nil {
			panic(err)
		}
		return when
	}
}

// TestRunFillsMissingDays checks that a run collects every day in the
// look-back window that is not stored, that one day failing does not stop the
// others, and that the next run tries only what is still missing.
func TestRunFillsMissingDays(t *testing.T) {
	f := newFixture(t, "typical")
	store := newStore(t, newDatabase(t))
	seedCatalogue(t, store)
	reports := t.TempDir()
	collector := newCollector(t, f, store, Options{MarkdownDir: reports, Now: at("2026-10-09T03:00:00Z")})

	err := collector.Run(t.Context())
	require.Error(t, err, "three of the four days in the window have no access records")
	for _, day := range []string{"2026-10-05", "2026-10-07", "2026-10-08"} {
		assert.ErrorContains(t, err, day+": no access records were found for this period, although the gateway counted 5073 requests")
	}
	assert.NotContains(t, err.Error(), "2026-10-06:")
	assert.Equal(t, []string{"2026-10-06"}, storedDays(t, store))

	report, err := os.ReadFile(filepath.Join(reports, "2026-10-06.md"))
	require.NoError(t, err)
	assert.Equal(t, string(readFixture(t, "scenarios", "typical", "report.md")), string(report))
	empty, err := os.ReadFile(filepath.Join(reports, "2026-10-07.md"))
	require.NoError(t, err)
	assert.Contains(t, string(empty), "No access records were found for this period.")

	// What is stored is exactly what the day's sample works out to, lookups included.
	f.sources.Catalogue, err = store.Catalogue(t.Context())
	require.NoError(t, err)
	sample, err := f.collect(t)
	require.NoError(t, err)
	daily, shares := sample.Rows()
	sort.SliceStable(shares, func(i, j int) bool { return shares[i].Dimension < shares[j].Dimension })
	stored := storedShares(t, store, daily.Day)
	assert.Equal(t, shares, stored)
	servers := 0
	for _, row := range stored {
		if row.Dimension == "server" && row.Kind == rowNamed {
			servers++
		}
	}
	assert.Equal(t, topServers, servers, "lookups are broken down using the stored catalogue")
	var requests int64
	var ratio float64
	var source string
	require.NoError(t, store.pool.QueryRow(t.Context(), "SELECT requests, log_requests_ratio::float8, totals_source FROM usage_daily").Scan(&requests, &ratio, &source))
	assert.Equal(t, daily.Requests, requests)
	assert.Equal(t, *daily.LogRequestsRatio, ratio)
	assert.InDelta(t, 1, ratio, 0.1, "the log-based estimate is close to the counters")
	assert.Equal(t, totalsFromCounters, source)

	f.gateway.filters = nil
	require.Error(t, collector.Run(t.Context()))
	for _, filter := range f.gateway.filters {
		assert.NotContains(t, filter, `timestamp>="2026-10-06T`, "a stored day is not read again")
	}
	assert.Len(t, f.gateway.filters, 6, "each of the three days is sized and then read")
}

func TestRunWaitsForTheDayToSettle(t *testing.T) {
	f := newFixture(t, "typical")
	store := newStore(t, newDatabase(t))
	collector := newCollector(t, f, store, Options{Now: at("2026-10-07T00:05:00Z")})
	collector.Lookback = 1
	require.NoError(t, collector.Run(t.Context()))
	assert.Empty(t, storedDays(t, store), "five minutes after midnight the day's last records may not have arrived")

	collector.Options.Now = at("2026-10-07T00:20:00Z")
	require.NoError(t, collector.Run(t.Context()))
	assert.Equal(t, []string{"2026-10-06"}, storedDays(t, store))

	// A day named explicitly is held to the same rule.
	collector.Options.Days = []time.Time{time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)}
	require.ErrorContains(t, collector.Run(t.Context()), "2026-10-07 is not a full UTC day that ended at least 15m0s ago")
	collector.Options.Days = []time.Time{time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}
	collector.Options.Now = at("2026-10-07T00:05:00Z")
	require.ErrorContains(t, collector.Run(t.Context()), "2026-10-06 is not a full UTC day that ended at least 15m0s ago")
}

// TestRunDoesNotStoreDegradedDays checks that a day which lacks a source it
// needs stays missing, so that a later run collects it properly, and that its
// access records are not read for nothing.
func TestRunDoesNotStoreDegradedDays(t *testing.T) {
	day := []time.Time{time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}
	for name, reason := range map[string]string{
		"no-counters":        "the gateway counters",
		"no-bytes-counter":   "the gateway counters",
		"no-hourly-counters": "the hourly gateway counters",
		"no-networks":        "the network-owner table",
		"no-network-types":   "the network-type list",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, name)
			store := newStore(t, newDatabase(t))
			reports := t.TempDir()
			collector := newCollector(t, f, store, Options{Days: day, MarkdownDir: reports, Now: at("2026-10-09T03:00:00Z")})

			err := collector.Run(t.Context())
			require.ErrorContains(t, err, "not collected, because "+reason+" could not be used")
			assert.Empty(t, storedDays(t, store))
			assert.Zero(t, f.gateway.logCalls, "the access records are not read")
			assert.NoFileExists(t, filepath.Join(reports, "2026-10-06.md"))

			collector.Options.AllowDegraded = true
			require.NoError(t, collector.Run(t.Context()))
			report, err := os.ReadFile(filepath.Join(reports, "2026-10-06.md"))
			require.NoError(t, err)
			assert.Equal(t, string(readFixture(t, "scenarios", name, "report.md")), string(report))
			var caveats []string
			require.NoError(t, store.pool.QueryRow(t.Context(), "SELECT caveats FROM usage_daily").Scan(&caveats))
			assert.NotEmpty(t, caveats, "a degraded day that was stored on request says so")
		})
	}
}

type emptyMetrics struct{}

func (emptyMetrics) Query(context.Context, string, time.Time) ([]MetricSample, error) {
	return nil, nil
}

// TestRunNoticesCountersWithoutData covers a day older than the counters'
// retention: the queries succeed but return nothing, which the report does not
// mention. Such a day must not be stored as if it had been scaled.
func TestRunNoticesCountersWithoutData(t *testing.T) {
	f := newFixture(t, "typical")
	f.sources.Metrics = emptyMetrics{}
	store := newStore(t, newDatabase(t))
	collector := newCollector(t, f, store, Options{
		Days: []time.Time{time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}, Now: at("2026-10-30T03:00:00Z")})
	require.ErrorContains(t, collector.Run(t.Context()), "not collected, because the gateway counters (no data for this day)")
	assert.Empty(t, storedDays(t, store))
	assert.Zero(t, f.gateway.logCalls)

	collector.Options.AllowDegraded = true
	require.NoError(t, collector.Run(t.Context()))
	var source string
	var ratio *float64
	var caveats []string
	require.NoError(t, store.pool.QueryRow(t.Context(), "SELECT totals_source, log_requests_ratio::float8, caveats FROM usage_daily").Scan(&source, &ratio, &caveats))
	assert.Equal(t, totalsFromLogs, source)
	assert.Nil(t, ratio)
	assert.Contains(t, caveats, noteEmptyCounters)
}

// TestRunWithoutStore is the daily report on its own: nothing but the report is produced.
func TestRunWithoutStore(t *testing.T) {
	f := newFixture(t, "typical")
	var out bytes.Buffer
	collector := newCollector(t, f, nil, Options{
		Days: []time.Time{time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}, NoStore: true, Stdout: &out, Now: at("2026-10-09T03:00:00Z")})
	require.NoError(t, collector.Run(t.Context()))
	assert.Equal(t, string(readFixture(t, "scenarios", "typical", "report.md")), out.String())
	assert.False(t, strings.Contains(f.log.String(), "stored"))
}

// TestRunNeedsAnUpToDateCatalogue covers a run whose catalogue refresh failed.
// Lookups of servers published since the last refresh would be counted as
// unknown names, and a stored day is never collected again, so the day waits.
func TestRunNeedsAnUpToDateCatalogue(t *testing.T) {
	day := []time.Time{time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}
	f := newFixture(t, "typical")
	store := newStore(t, newDatabase(t))
	seedCatalogue(t, store)
	collector := newCollector(t, f, store, Options{Days: day, Now: at("2026-10-09T03:00:00Z")})

	t.Run("the registry database could not be reached", func(t *testing.T) {
		collector.RegistryUnavailable = true
		require.ErrorContains(t, collector.Run(t.Context()), "not collected, because an up-to-date catalogue could not be used")
		assert.Empty(t, storedDays(t, store))
		assert.Zero(t, f.gateway.logCalls)
		collector.RegistryUnavailable = false
	})

	t.Run("the refresh failed part of the way", func(t *testing.T) {
		registry := newRegistryFixture(t)
		registry.reader.Close()
		collector.Registry = registry.reader
		err := collector.Run(t.Context())
		require.ErrorContains(t, err, "catalogue sync:")
		require.ErrorContains(t, err, "not collected, because an up-to-date catalogue could not be used")
		assert.Empty(t, storedDays(t, store))

		collector.Options.AllowDegraded = true
		require.ErrorContains(t, collector.Run(t.Context()), "catalogue sync:", "the run still reports the failed refresh")
		var caveats []string
		require.NoError(t, store.pool.QueryRow(t.Context(), "SELECT caveats FROM usage_daily").Scan(&caveats))
		assert.Contains(t, caveats, noteStaleCatalogue)
	})
}

// idleMetrics is a gateway whose counters were being recorded but show no
// registry request: the day's queries return nothing, and the question
// whether the counter had any series that day is answered with a count.
type idleMetrics struct{}

func (idleMetrics) Query(_ context.Context, query string, _ time.Time) ([]MetricSample, error) {
	if strings.Contains(query, "last_over_time") {
		return []MetricSample{{Value: 6}}, nil
	}
	return nil, nil
}

// TestRunSkipsADayWithoutTraffic covers a day on which the gateway served the
// registry nothing: its counters were recorded and show no request, and there
// are no access records. That is not a failure, and nothing is stored for it.
func TestRunSkipsADayWithoutTraffic(t *testing.T) {
	f := newFixture(t, "empty")
	f.sources.Metrics = idleMetrics{}
	store := newStore(t, newDatabase(t))
	reports := t.TempDir()
	collector := newCollector(t, f, store, Options{MarkdownDir: reports, Now: at("2026-10-07T03:00:00Z")})
	collector.Lookback = 1

	require.NoError(t, collector.Run(t.Context()))
	assert.Empty(t, storedDays(t, store))
	assert.Contains(t, f.log.String(), "2026-10-06 has nothing to collect: the gateway counted no registry requests and no access record shows one")
	assert.Equal(t, 1, f.gateway.logCalls, "one unsampled read")
	report, err := os.ReadFile(filepath.Join(reports, "2026-10-06.md"))
	require.NoError(t, err)
	assert.Equal(t, string(readFixture(t, "scenarios", "empty", "report.md")), string(report))
}

// TestRunStoresADegradedDayOnItsLastChance checks what happens to a day that
// could not be collected complete while it was in the look-back window. On the
// last scheduled run that looks at it, it is stored with its caveats rather
// than lost, as long as the request counters can still size and scale it.
func TestRunStoresADegradedDayOnItsLastChance(t *testing.T) {
	// With a look-back of three days, 6 October is the oldest day in the window on 9 October.
	const earlier, lastRun = "2026-10-08T03:00:00Z", "2026-10-09T03:00:00Z"
	for name, missing := range map[string]string{
		"no-networks":        "the network-owner table",
		"no-network-types":   "the network-type list",
		"no-hourly-counters": "the hourly gateway counters",
		"no-bytes-counter":   "the gateway counters",
		"typical":            "an up-to-date catalogue",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, name)
			store := newStore(t, newDatabase(t))
			seedCatalogue(t, store)
			collector := newCollector(t, f, store, Options{Now: at(earlier)})
			collector.Lookback = 3
			collector.RegistryUnavailable = name == "typical"

			err := collector.Run(t.Context())
			require.ErrorContains(t, err, "2026-10-06: not collected, because "+missing+" could not be used; the day stays missing")
			assert.Empty(t, storedDays(t, store), "while a later run can still collect the day, it waits")
			for _, filter := range f.gateway.filters {
				assert.NotContains(t, filter, `timestamp>="2026-10-06T`, "its access records are not read")
			}

			collector.Options.Now = at(lastRun)
			err = collector.Run(t.Context())
			assert.Equal(t, []string{"2026-10-06"}, storedDays(t, store))
			assert.NotContains(t, err.Error(), "2026-10-06", "the day itself no longer fails")
			assert.ErrorContains(t, err, "2026-10-07: not collected", "newer days still wait for a complete collection")
			assert.Contains(t, f.log.String(), "2026-10-06 was stored without "+missing)

			var caveats []string
			var source string
			require.NoError(t, store.pool.QueryRow(t.Context(), "SELECT caveats, totals_source FROM usage_daily").Scan(&caveats, &source))
			assert.Contains(t, caveats, noteLastChance)
			assert.Greater(t, len(caveats), 1, "what was missing is recorded too")
			if name == "no-bytes-counter" {
				assert.Equal(t, totalsMixed, source, "requests from the counters, data from the access records")
			} else {
				assert.Equal(t, totalsFromCounters, source)
			}
		})
	}

	// Without the request counters the records cannot be scaled to the
	// gateway's totals, so there is nothing sound to store.
	t.Run("no-counters", func(t *testing.T) {
		f := newFixture(t, "no-counters")
		store := newStore(t, newDatabase(t))
		collector := newCollector(t, f, store, Options{Now: at(lastRun)})
		collector.Lookback = 3
		err := collector.Run(t.Context())
		require.ErrorContains(t, err, "2026-10-06: not collected, because the gateway counters could not be used; "+
			"this was the last scheduled run that looks at the day")
		assert.Empty(t, storedDays(t, store))
		assert.Zero(t, f.gateway.logCalls)
	})

	t.Run("a day named explicitly gets no such exception", func(t *testing.T) {
		f := newFixture(t, "no-network-types")
		store := newStore(t, newDatabase(t))
		collector := newCollector(t, f, store, Options{Now: at("2026-10-12T03:00:00Z")})
		collector.Lookback = 3
		require.Error(t, collector.Run(t.Context()), "a scheduled run first, whose window is past the day")
		collector.Options.Days = []time.Time{time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}
		require.ErrorContains(t, collector.Run(t.Context()), "2026-10-06: not collected, because the network-type list could not be used; the day stays missing")
		assert.Empty(t, storedDays(t, store))
	})
}
