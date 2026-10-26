package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

func TestDatabaseMetricsPoolLifecycle(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	metrics, err := telemetry.NewDatabaseMetrics(provider.Meter("test"))
	require.NoError(t, err)
	// Same local PostgreSQL service used by the repository's database tests.
	config, err := pgxpool.ParseConfig("postgres://mcpregistry:mcpregistry@localhost:5432/postgres?sslmode=disable")
	require.NoError(t, err)
	config.MaxConns = 1
	config.MinConns = 0
	config.ConnConfig.Tracer = metrics
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	registration, err := metrics.ObservePool(pool)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, registration.Unregister()) })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	data := collectDBMetrics(t, reader)
	gauge := data["mcp_registry.db.pool.connections"].(metricdata.Gauge[int64])
	for _, point := range gauge.DataPoints {
		state, _ := point.Attributes.Value("state")
		if state.AsString() == "acquired" {
			require.EqualValues(t, 1, point.Value)
		}
	}
	require.EqualValues(t, 1, data["mcp_registry.db.pool.max_connections"].(metricdata.Gauge[int64]).DataPoints[0].Value)

	waitCtx, stopWaiting := context.WithCancel(telemetry.WithDatabaseOperation(ctx, "list_search"))
	defer stopWaiting()
	done := make(chan error, 1)
	go func() {
		acquired, acquireErr := pool.Acquire(waitCtx)
		if acquired != nil {
			acquired.Release()
		}
		done <- acquireErr
	}()
	require.Eventually(t, func() bool {
		sum := collectDBMetrics(t, reader)["mcp_registry.db.pool.acquire.pending"].(metricdata.Sum[int64])
		for _, point := range sum.DataPoints {
			op, _ := point.Attributes.Value("operation")
			if op.AsString() == "list_search" {
				return point.Value == 1
			}
		}
		return false
	}, time.Second, time.Millisecond)
	stopWaiting()
	require.ErrorIs(t, <-done, context.Canceled)
	data = collectDBMetrics(t, reader)
	for _, point := range data["mcp_registry.db.pool.acquire.pending"].(metricdata.Sum[int64]).DataPoints {
		require.Zero(t, point.Value)
	}
	foundCanceled := false
	for _, point := range data["mcp_registry.db.pool.acquire.duration"].(metricdata.Histogram[float64]).DataPoints {
		outcome, _ := point.Attributes.Value("outcome")
		if outcome.AsString() == "canceled" {
			foundCanceled = true
			require.EqualValues(t, 1, point.Count)
			require.Positive(t, point.Sum)
		}
	}
	require.True(t, foundCanceled)
	// No query started for the canceled acquisition.
	_, exists := data["mcp_registry.db.query.duration"]
	require.False(t, exists)
	conn.Release()

	rows, err := pool.Query(telemetry.WithDatabaseOperation(ctx, "list_page"), "SELECT generate_series(1, 2)")
	require.NoError(t, err)
	defer rows.Close()
	_, exists = collectDBMetrics(t, reader)["mcp_registry.db.query.duration"]
	require.False(t, exists, "query timing must include row consumption")
	require.True(t, rows.Next())
	var value int
	require.NoError(t, rows.Scan(&value))
	rows.Close()
	require.NoError(t, rows.Err())
	hist := collectDBMetrics(t, reader)["mcp_registry.db.query.duration"].(metricdata.Histogram[float64])
	require.Len(t, hist.DataPoints, 1)
	require.EqualValues(t, 1, hist.DataPoints[0].Count)
	operation, _ := hist.DataPoints[0].Attributes.Value("operation")
	require.Equal(t, "list_page", operation.AsString())
}

func TestDatabaseMetricsBoundedLabelsAndErrors(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	metrics, err := telemetry.NewDatabaseMetrics(provider.Meter("test"))
	require.NoError(t, err)
	ctx := telemetry.WithDatabaseOperation(context.Background(), "private-search-term")
	ctx = metrics.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT secret", Args: []any{"secret"}})
	metrics.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: context.DeadlineExceeded})
	hist := collectDBMetrics(t, reader)["mcp_registry.db.query.duration"].(metricdata.Histogram[float64])
	require.Len(t, hist.DataPoints, 1)
	attrs := hist.DataPoints[0].Attributes
	require.Equal(t, 2, attrs.Len())
	operation, _ := attrs.Value("operation")
	outcome, _ := attrs.Value("outcome")
	require.Equal(t, "other", operation.AsString())
	require.Equal(t, "timeout", outcome.AsString())
}

func collectDBMetrics(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Aggregation {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &data))
	result := make(map[string]metricdata.Aggregation)
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			result[m.Name] = m.Data
		}
	}
	return result
}
