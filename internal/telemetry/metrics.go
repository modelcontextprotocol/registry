package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

const (
	Namespace = "mcp_registry"
)

type Metrics struct {
	// Requests tracks the number of HTTP requests
	Requests metric.Int64Counter

	// RequestDuration tracks the duration of HTTP Requests
	RequestDuration metric.Float64Histogram

	// ErrorCount tracks the number of errors
	ErrorCount metric.Int64Counter

	// Up tracks the health of the service
	Up metric.Int64Gauge
}

// ShutdownFunc is a delegate that shuts down the OpenTelemetry components.
type ShutdownFunc func(ctx context.Context) error

func NewMetrics(meter metric.Meter) (*Metrics, error) {
	req, err := meter.Int64Counter(
		Namespace+".http.requests",
		metric.WithDescription("Total number of HTTP requests"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request counter: %w", err)
	}

	reqDuration, err := meter.Float64Histogram(
		Namespace+".http.request.duration",
		metric.WithDescription("Duration of HTTP requests in seconds"),
		metric.WithExplicitBucketBoundaries(
			0.005, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 20.0, 50.0,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request duration histogram: %w", err)
	}

	errCount, err := meter.Int64Counter(
		Namespace+".http.errors",
		metric.WithDescription("Total number of HTTP errors"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create error counter: %w", err)
	}

	up, err := meter.Int64Gauge(
		Namespace+".service.up",
		metric.WithDescription("Service health status (1 for up, 0 for down)"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create service up gauge: %w", err)
	}

	return &Metrics{
		Requests:        req,
		RequestDuration: reqDuration,
		ErrorCount:      errCount,
		Up:              up,
	}, nil
}

func NewPrometheusMeterProvider(res *resource.Resource, exp *prometheus.Exporter) (*sdkmetric.MeterProvider, error) {
	if exp == nil {
		return nil, errors.New("exporter cannot be nil")
	}
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(exp),
	)

	return meterProvider, nil
}

func InitMetrics(version string) (ShutdownFunc, *Metrics, error) {
	// Initialized the returned shutdownFunc to no-op.
	shutdown := func(_ context.Context) error { return nil }

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(Namespace),
			semconv.ServiceVersion(version),
		),
		resource.WithProcessRuntimeDescription(),
	)
	if err != nil {
		return shutdown, nil, fmt.Errorf("failed to create resource: %w", err)
	}

	res, err = resource.Merge(resource.Default(), res)
	if err != nil {
		return shutdown, nil, fmt.Errorf("failed to merge resources: %w", err)
	}

	exporter, err := prometheus.New()
	if err != nil {
		return shutdown, nil, fmt.Errorf("failed to create Prometheus exporter: %w", err)
	}

	mp, err := NewPrometheusMeterProvider(res, exporter)
	if err != nil {
		return shutdown, nil, fmt.Errorf("failed to create Prometheus meter provider: %w", err)
	}
	otel.SetMeterProvider(mp)

	// Update the returned shutdownFunc that calls metric provider
	// shutdown methods and make sure that a non-nil error is returned
	// if any returned an error.
	shutdown = func(ctx context.Context) error {
		var retErr error
		if err := mp.Shutdown(ctx); err != nil {
			retErr = err
		}
		return retErr
	}

	meter := mp.Meter(Namespace, metric.WithSchemaURL(semconv.SchemaURL), metric.WithInstrumentationVersion(runtime.Version()))
	metrics, err := NewMetrics(meter)
	return shutdown, metrics, err
}

// PrometheusHandler returns the HTTP handler for Prometheus metrics
// This handler serves the metrics endpoint for Prometheus to scrape.
func (m *Metrics) PrometheusHandler() http.Handler {
	return promhttp.Handler()
}
