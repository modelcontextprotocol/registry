package telemetry

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"golang.org/x/time/rate"
)

func diagnosticFixture(t *testing.T, threshold time.Duration) (*ListingDiagnostic, <-chan map[string]any) {
	t.Helper()
	events := make(chan map[string]any, 100)
	d := newListingDiagnostic("ingress-request-123", "/v0.1/servers", threshold, time.Hour, func(attrs []slog.Attr) {
		event := map[string]any{}
		for _, a := range attrs {
			event[a.Key] = a.Value.Any()
		}
		events <- event
	})
	t.Cleanup(func() { d.Finish(200, nil) })
	return d, events
}

func diagnosticEvent(t *testing.T, events <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case e := <-events:
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("missing diagnostic event")
		return nil
	}
}

func diagnosticPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	provider := sdkmetric.NewMeterProvider()
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	metrics, err := NewDatabaseMetrics(provider.Meter("test"))
	require.NoError(t, err)
	cfg, err := pgxpool.ParseConfig("postgres://mcpregistry:mcpregistry@localhost:5432/postgres?sslmode=disable")
	require.NoError(t, err)
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.ConnConfig.Tracer = metrics
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func TestListingDiagnosticBlockedPoolAndCancellation(t *testing.T) {
	pool := diagnosticPool(t)
	held, err := pool.Acquire(context.Background())
	require.NoError(t, err)
	defer held.Release()
	d, events := diagnosticFixture(t, 100*time.Millisecond)
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), listingKey{}, d))
	defer cancel()
	done := make(chan error, 1)
	go func() {
		rows, err := pool.Query(ctx, "SELECT 1")
		if rows != nil {
			rows.Close()
		}
		done <- err
	}()
	progress := diagnosticEvent(t, events)
	require.Equal(t, "listing_slow_in_progress", progress["event"])
	require.Equal(t, "pool_acquire", progress["phase"])
	require.EqualValues(t, 1, progress["pool_acquired_at_start"])
	require.EqualValues(t, 1, progress["pool_max"])
	require.EqualValues(t, 0, progress["backend_pid"])
	cancel()
	err = <-done
	require.ErrorIs(t, err, context.Canceled)
	d.DatabaseDone(err)
	d.Finish(499, ctx.Err())
	completion := diagnosticEvent(t, events)
	require.Equal(t, "canceled", completion["outcome"])
	require.Equal(t, "pool_acquire", completion["failed_phase"])
	require.Greater(t, completion["pool_acquire_ms"].(float64), float64(50))
	require.Equal(t, "ingress-request-123", completion["request_id"])
	require.Zero(t, completion["query_wait_ms"])
}

func TestListingDiagnosticSeparatesQueryFetchAndDecode(t *testing.T) {
	pool := diagnosticPool(t)
	require.NoError(t, pool.Ping(context.Background()))
	d, events := diagnosticFixture(t, 50*time.Millisecond)
	ctx := context.WithValue(context.Background(), listingKey{}, d)
	d.SetShape(ListingShape{Cursor: true, Search: true, Latest: true, Limit: 1})
	rows, err := pool.Query(ctx, "SELECT pg_sleep(0.15), 'private-payload'::text")
	require.NoError(t, err)
	d.Phase("row_fetch")
	require.True(t, rows.Next())
	d.RowReceived()
	// Deliberately slow application row processing, independent of server wait.
	time.Sleep(60 * time.Millisecond)
	d.AddBytes(15)
	d.Phase("row_fetch")
	rows.Close()
	d.DatabaseDone(rows.Err())
	d.Finish(200, nil)
	progress := diagnosticEvent(t, events)
	require.Equal(t, "listing_slow_in_progress", progress["event"])
	require.Contains(t, []string{"query_wait", "row_fetch"}, progress["phase"])
	completion := diagnosticEvent(t, events)
	require.Equal(t, "success", completion["outcome"])
	require.True(t, completion["first_row_received"].(bool))
	require.Greater(t, completion["first_row_ms"].(float64), float64(100))
	require.Greater(t, completion["row_decode_ms"].(float64), float64(50))
	require.Greater(t, completion["backend_pid"].(uint64), uint64(0))
	require.EqualValues(t, 1, completion["rows_received"])
	require.EqualValues(t, 15, completion["json_bytes"])
	require.Equal(t, true, completion["cursor"])
	require.Equal(t, true, completion["search"])
	encoded, err := json.Marshal(completion)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-payload")
	require.NotContains(t, string(encoded), "pg_sleep")
	sum := float64(0)
	for _, phase := range []string{"handler", "pool_acquire", "query_wait", "row_fetch", "row_decode", "response"} {
		sum += completion[phase+"_ms"].(float64)
	}
	require.InDelta(t, completion["total_ms"], sum, 0.01)
}

func TestListingDiagnosticPrivacyAndFastFailure(t *testing.T) {
	d, events := diagnosticFixture(t, time.Hour)
	d.Phase("row_decode")
	d.DatabaseDone(&pgconn.PgError{Code: "57014", Message: "private-query-arguments"})
	d.Finish(500, nil)
	e := diagnosticEvent(t, events)
	require.Equal(t, "57014", e["sqlstate"])
	require.Equal(t, "row_decode", e["failed_phase"])
	b, err := json.Marshal(e)
	require.NoError(t, err)
	require.NotContains(t, string(b), "private-query-arguments")
	for _, id := range []string{"\nforged", strings.Repeat("x", 129), "secret?query=value"} {
		actual := listingRequestID(id)
		require.NotEqual(t, id, actual)
		require.NotEmpty(t, actual)
	}
}

func TestListingDiagnosticFinishStopsProgress(t *testing.T) {
	d, events := diagnosticFixture(t, time.Hour)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { d.Phase("row_fetch"); d.AddBytes(1) })
	}
	wg.Wait()
	d.Finish(200, nil)
	d.progress()
	d.Finish(500, nil)
	require.Empty(t, events, "fast successful requests and callbacks after completion must be silent")
}

func TestListingDiagnosticRateLimit(t *testing.T) {
	// No parallel tests: temporarily replace the global logger and budget.
	oldLogger := slog.Default()
	var output strings.Builder
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	listingLogBudget.Lock()
	oldLimiter, oldSuppressed := listingLogBudget.limiter, listingLogBudget.suppressed
	listingLogBudget.limiter = rate.NewLimiter(0, 1)
	listingLogBudget.suppressed = 0
	listingLogBudget.Unlock()
	t.Cleanup(func() {
		listingLogBudget.Lock()
		defer listingLogBudget.Unlock()
		listingLogBudget.limiter = oldLimiter
		listingLogBudget.suppressed = oldSuppressed
	})
	for range 50 {
		emitListingDiagnostic([]slog.Attr{slog.String("event", "test")})
	}
	require.Equal(t, 1, strings.Count(output.String(), "\n"))
	listingLogBudget.Lock()
	require.EqualValues(t, 49, listingLogBudget.suppressed)
	listingLogBudget.limiter = rate.NewLimiter(0, 1)
	listingLogBudget.Unlock()
	emitListingDiagnostic(nil)
	require.Contains(t, output.String(), `"diagnostics_suppressed":49`)
}
