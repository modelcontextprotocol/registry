package database

import (
	"fmt"
	"sync"
	"testing"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/require"
)

func TestUpdatedPageHintsCoverageAndExpiry(t *testing.T) {
	var hints updatedPageHints
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	before := now.Add(-time.Hour)
	hints.remember(now, updatedPageObservation{large: true, before: before, nameBefore: "com.example/m"}, false)
	for _, tc := range []struct {
		name             string
		at, since        time.Time
		cursor           string
		deleted, covered bool
	}{
		{"first page", now, before.Add(-time.Second), "", false, true},
		{"earlier cursor", now, before.Add(-time.Second), "com.example/a", false, true},
		{"equal cursor", now, before.Add(-time.Second), "com.example/m", false, false},
		{"later cursor", now, before.Add(-time.Second), "com.example/z", false, false},
		{"equal watermark", now, before, "", false, false},
		{"later watermark", now, before.Add(time.Second), "", false, false},
		{"different deletion filter", now, before.Add(-time.Second), "", true, false},
		{"before expiry", now.Add(updatedPageHintLifetime - time.Nanosecond), before.Add(-time.Second), "", false, true},
		{"at expiry", now.Add(updatedPageHintLifetime), before.Add(-time.Second), "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := hints.choose(tc.at, tc.since, tc.cursor, tc.deleted)
			require.Equal(t, tc.covered, got.skipProbe)
		})
	}
	// Identical observations cannot indefinitely prolong an old strategy.
	hints.remember(now.Add(time.Second), updatedPageObservation{large: true, before: before, nameBefore: "com.example/m"}, false)
	require.Len(t, hints.entries, 1)
	require.Equal(t, now.Add(updatedPageHintLifetime), hints.entries[0].expires)
}

func TestUpdatedPageHintsBoundedSamplingAndConcurrentAccess(t *testing.T) {
	var hints updatedPageHints
	now := time.Now()
	for i := 0; i < 64; i++ {
		got := hints.choose(now, now.Add(-time.Hour), "", false)
		require.Equal(t, i%updatedPageObservationInterval == 0, got.observe)
		hints.remember(now.Add(time.Duration(i)*time.Millisecond), updatedPageObservation{
			large: true, before: now, nameBefore: fmt.Sprintf("com.example/%03d", i),
		}, false)
	}
	require.Len(t, hints.entries, updatedPageHintLimit)
	for _, hint := range hints.entries {
		require.GreaterOrEqual(t, hint.nameBefore, "com.example/032", "oldest entries are evicted")
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				hints.choose(now, now.Add(-time.Hour), "com.example/000", false)
				hints.remember(now, updatedPageObservation{large: true, before: now, nameBefore: fmt.Sprintf("com.example/concurrent-%d", i)}, false)
			}
		}()
	}
	wg.Wait()
	require.Len(t, hints.entries, updatedPageHintLimit)
	hints.remember(now.Add(time.Minute), updatedPageObservation{large: true, before: now, nameBefore: "com.example/new"}, true)
	require.Len(t, hints.entries, 1, "expired entries are removed on observation")
}

func TestUpdatedPageObservationMarker(t *testing.T) {
	stamp := time.Now()
	name := "com.example/first"
	ordinary := listingRow{name: name, metadata: apiv0.RegistryExtensions{UpdatedAt: stamp, IsLatest: true}}
	var observation updatedPageObservation
	require.False(t, observation.consume(ordinary))
	require.Equal(t, updatedPageObservation{}, observation)
	marker := listingRow{metadata: apiv0.RegistryExtensions{UpdatedAt: stamp, StatusMessage: &name, IsLatest: true}}
	require.True(t, observation.consume(marker))
	require.Equal(t, updatedPageObservation{large: true, before: stamp, nameBefore: name}, observation)
}

func TestUpdatedPageStrategyScopeAndBoundParameters(t *testing.T) {
	db := &PostgreSQL{}
	now := time.Now()
	cutoff := now.Add(-time.Hour)
	name, version, search, remote, yes := "com.example/a", "1", "example", "https://example.com", true
	for _, filter := range []*ServerFilter{
		nil, {}, {UpdatedSince: &cutoff, Name: &name}, {UpdatedSince: &cutoff, Version: &version},
		{UpdatedSince: &cutoff, SubstringName: &search}, {UpdatedSince: &cutoff, IsLatest: &yes},
		{UpdatedSince: &cutoff, RemoteURL: &remote},
	} {
		strategy, _ := db.chooseUpdatedPageStrategy(nil, filter, 100, nil, 2, 2, nil)
		require.Equal(t, updatedPageStrategy{}, strategy)
	}
	for _, limit := range []int{1, 10, 128, 1000} {
		strategy, _ := db.chooseUpdatedPageStrategy(nil, &ServerFilter{UpdatedSince: &cutoff}, limit, nil, 2, 2, nil)
		require.Equal(t, updatedPageStrategy{}, strategy)
	}
	require.Zero(t, db.updatedPageHints.requests)
	db.updatedPageHints.remember(now, updatedPageObservation{large: true, before: now, nameBefore: "com.example/z'--"}, false)
	cursor := "com.example/a' OR true --"
	strategy, args := db.chooseUpdatedPageStrategy(nil, &ServerFilter{UpdatedSince: &cutoff}, 100, []any{cursor}, 2, 3, []any{cutoff, cursor})
	require.True(t, strategy.skipProbe)
	require.Equal(t, "($2::text < $3::text)", strategy.cursorGuard)
	require.Equal(t, []any{cutoff, cursor, "com.example/z'--"}, args)
	query := updatedPageQuery("WHERE updated_at > $1 AND server_name > $2", "100", "s.value", false, strategy)
	require.NotContains(t, query, cursor)
	require.NotContains(t, query, strategy.nameBefore)
}
