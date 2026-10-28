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

func TestCheckoutTimingTracksOwnershipAndReuse(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	m, err := telemetry.NewDatabaseMetrics(provider.Meter("test"))
	require.NoError(t, err)
	conn := &pgx.Conn{}
	ctx := telemetry.WithDatabaseOperation(context.Background(), "list_page")
	ctx = m.TraceAcquireStart(ctx, nil, pgxpool.TraceAcquireStartData{})
	m.TraceAcquireEnd(ctx, nil, pgxpool.TraceAcquireEndData{Conn: conn})
	queryCtx := m.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{})
	m.TraceQueryEnd(queryCtx, conn, pgx.TraceQueryEndData{})
	time.Sleep(20 * time.Millisecond)
	_, exists := collectDBMetrics(t, reader)["mcp_registry.db.pool.checkout.duration"]
	require.False(t, exists, "a still-owned connection has not completed a checkout")
	m.TraceRelease(nil, pgxpool.TraceReleaseData{Conn: conn})
	m.TraceRelease(nil, pgxpool.TraceReleaseData{Conn: conn})
	data := collectDBMetrics(t, reader)
	for _, name := range []string{"mcp_registry.db.pool.checkout.duration", "mcp_registry.db.pool.after_query.duration"} {
		h := data[name].(metricdata.Histogram[float64])
		require.EqualValues(t, 1, h.DataPoints[0].Count)
		require.GreaterOrEqual(t, h.DataPoints[0].Sum, .020)
		require.Equal(t, 1, h.DataPoints[0].Attributes.Len())
	}
	// Reusing the same connection without a query must not inherit its prior
	// query-end timestamp. A failed acquisition must not produce a checkout.
	m.TraceAcquireEnd(ctx, nil, pgxpool.TraceAcquireEndData{Conn: conn})
	m.TraceRelease(nil, pgxpool.TraceReleaseData{Conn: conn})
	m.TraceAcquireEnd(ctx, nil, pgxpool.TraceAcquireEndData{Err: context.Canceled})
	data = collectDBMetrics(t, reader)
	require.EqualValues(t, 2, data["mcp_registry.db.pool.checkout.duration"].(metricdata.Histogram[float64]).DataPoints[0].Count)
	require.EqualValues(t, 1, data["mcp_registry.db.pool.after_query.duration"].(metricdata.Histogram[float64]).DataPoints[0].Count)
}
