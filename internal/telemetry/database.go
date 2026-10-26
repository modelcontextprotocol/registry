package telemetry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type databaseOperationKey struct{}
type acquireStartKey struct{}
type queryStartKey struct{}

// WithDatabaseOperation assigns a bounded operation label, never SQL or arguments.
func WithDatabaseOperation(ctx context.Context, operation string) context.Context {
	switch operation {
	case "list", "list_page", "list_search", "list_updated":
	default:
		operation = "other"
	}
	return context.WithValue(ctx, databaseOperationKey{}, operation)
}

func databaseOperation(ctx context.Context) string {
	if operation, ok := ctx.Value(databaseOperationKey{}).(string); ok {
		return operation
	}
	return "other"
}

// DatabaseMetrics implements pgx's query and pool-acquisition tracers. Query
// timing starts after acquisition and ends when rows are closed or exhausted.
// It includes network transfer and row consumption, not just PostgreSQL CPU.
type DatabaseMetrics struct {
	meter           metric.Meter
	acquireDuration metric.Float64Histogram
	queryDuration   metric.Float64Histogram
	pending         metric.Int64UpDownCounter
}

var _ pgx.QueryTracer = (*DatabaseMetrics)(nil)
var _ pgxpool.AcquireTracer = (*DatabaseMetrics)(nil)

// NewDatabaseMetrics creates instrumentation without recording SQL, parameters,
// connection strings, or unbounded labels.
func NewDatabaseMetrics(meter metric.Meter) (*DatabaseMetrics, error) {
	buckets := metric.WithExplicitBucketBoundaries(.001, .005, .01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60)
	acquire, err := meter.Float64Histogram(Namespace+".db.pool.acquire.duration",
		metric.WithDescription("Connection acquisition duration in seconds, including connection creation and failed attempts"), buckets)
	if err != nil {
		return nil, fmt.Errorf("create pool acquisition histogram: %w", err)
	}
	query, err := meter.Float64Histogram(Namespace+".db.query.duration",
		metric.WithDescription("Query duration in seconds after acquisition, through row consumption or close"), buckets)
	if err != nil {
		return nil, fmt.Errorf("create query histogram: %w", err)
	}
	pending, err := meter.Int64UpDownCounter(Namespace+".db.pool.acquire.pending",
		metric.WithDescription("Connection acquisition attempts currently in progress"))
	if err != nil {
		return nil, fmt.Errorf("create pending acquisition counter: %w", err)
	}
	return &DatabaseMetrics{meter: meter, acquireDuration: acquire, queryDuration: query, pending: pending}, nil
}

// ObservePool registers scrape-time pool statistics. Unregister before discarding
// the pool so a failed startup attempt or closed pool is not retained forever.
func (m *DatabaseMetrics) ObservePool(pool *pgxpool.Pool) (metric.Registration, error) {
	connections, err := m.meter.Int64ObservableGauge(Namespace+".db.pool.connections",
		metric.WithDescription("Pool connections by state: acquired, idle, or constructing"))
	if err != nil {
		return nil, fmt.Errorf("create pool connection gauge: %w", err)
	}
	maximum, err := m.meter.Int64ObservableGauge(Namespace+".db.pool.max_connections",
		metric.WithDescription("Configured maximum pool connections"))
	if err != nil {
		return nil, fmt.Errorf("create pool limit gauge: %w", err)
	}
	return m.meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		stat := pool.Stat()
		observer.ObserveInt64(connections, int64(stat.AcquiredConns()), metric.WithAttributes(attribute.String("state", "acquired")))
		observer.ObserveInt64(connections, int64(stat.IdleConns()), metric.WithAttributes(attribute.String("state", "idle")))
		observer.ObserveInt64(connections, int64(stat.ConstructingConns()), metric.WithAttributes(attribute.String("state", "constructing")))
		observer.ObserveInt64(maximum, int64(stat.MaxConns()))
		return nil
	}, connections, maximum)
}

func (m *DatabaseMetrics) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	m.pending.Add(ctx, 1, metric.WithAttributes(attribute.String("operation", databaseOperation(ctx))))
	return context.WithValue(ctx, acquireStartKey{}, time.Now())
}

func (m *DatabaseMetrics) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	start, ok := ctx.Value(acquireStartKey{}).(time.Time)
	if !ok {
		return
	}
	ctx = context.WithoutCancel(ctx)
	m.pending.Add(ctx, -1, metric.WithAttributes(attribute.String("operation", databaseOperation(ctx))))
	m.acquireDuration.Record(ctx, time.Since(start).Seconds(), databaseAttributes(ctx, data.Err))
}

func (m *DatabaseMetrics) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryStartKey{}, time.Now())
}

func (m *DatabaseMetrics) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	start, ok := ctx.Value(queryStartKey{}).(time.Time)
	if !ok {
		return
	}
	ctx = context.WithoutCancel(ctx)
	m.queryDuration.Record(ctx, time.Since(start).Seconds(), databaseAttributes(ctx, data.Err))
}

func databaseAttributes(ctx context.Context, err error) metric.MeasurementOption {
	outcome := "success"
	switch {
	case errors.Is(err, context.Canceled):
		outcome = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		outcome = "timeout"
	case err != nil:
		outcome = "error"
	}
	return metric.WithAttributes(attribute.String("operation", databaseOperation(ctx)), attribute.String("outcome", outcome))
}
