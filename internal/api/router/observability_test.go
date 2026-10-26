package router_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/modelcontextprotocol/registry/internal/api/router"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

func TestMetricMiddleware_ArrivalsBeforeCompletion(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	metrics, err := telemetry.NewMetrics(provider.Meter("test"))
	require.NoError(t, err)
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test", "1"))
	api.UseMiddleware(router.MetricTelemetryMiddleware(metrics))
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	huma.Register(api, huma.Operation{OperationID: "blocked", Method: http.MethodGet, Path: "/servers/{name}"},
		func(_ context.Context, _ *struct {
			Name string `path:"name"`
		}) (*struct{}, error) {
			close(entered)
			<-release
			return &struct{}{}, nil
		})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer close(done)
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/servers/private-name?search=private", nil).WithContext(ctx))
	}()
	// Release the handler even if an assertion below fails.
	defer func() { close(release); <-done }()
	<-entered
	values := httpMetricValues(t, reader)
	require.EqualValues(t, 1, values["mcp_registry.http.request.arrivals"])
	require.EqualValues(t, 1, values["mcp_registry.http.requests.in_flight"])
	require.Zero(t, values["mcp_registry.http.requests"])
	cancel()
}

func TestMetricMiddleware_InFlightBalanced(t *testing.T) {
	for _, mode := range []string{"success", "canceled", "panic", "skipped"} {
		t.Run(mode, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			metrics, err := telemetry.NewMetrics(provider.Meter("test"))
			require.NoError(t, err)
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test", "1"))
			options := []router.MiddlewareOption{}
			if mode == "skipped" {
				options = append(options, router.WithSkipPaths("/servers"))
			}
			api.UseMiddleware(router.MetricTelemetryMiddleware(metrics, options...))
			huma.Register(api, huma.Operation{OperationID: "balanced", Method: http.MethodGet, Path: "/servers/{name}"},
				func(_ context.Context, _ *struct {
					Name string `path:"name"`
				}) (*struct{}, error) {
					if mode == "panic" {
						panic("test panic")
					}
					return &struct{}{}, nil
				})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			path := "/servers/example"
			if mode == "skipped" {
				path = "/servers/servers"
			}
			request := func() {
				mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
			}
			if mode == "panic" {
				require.Panics(t, request)
			} else {
				request()
			}
			values := httpMetricValues(t, reader)
			require.Zero(t, values["mcp_registry.http.requests.in_flight"])
			if mode == "skipped" {
				require.Empty(t, values)
			} else {
				require.EqualValues(t, 1, values["mcp_registry.http.request.arrivals"])
			}
		})
	}
}

func httpMetricValues(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &data))
	values := make(map[string]int64)
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
				for _, point := range sum.DataPoints {
					values[m.Name] += point.Value
					path, ok := point.Attributes.Value("path")
					require.True(t, ok)
					require.Equal(t, "/servers/{name}", path.AsString())
				}
			}
		}
	}
	return values
}

// The real Huma adapter must propagate the diagnostic context and ingress ID to
// the handler, including error and panic completion paths.
func TestListingDiagnosticMiddleware(t *testing.T) {
	for _, mode := range []string{"error", "panic", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			var output strings.Builder
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			provider := sdkmetric.NewMeterProvider()
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			metrics, err := telemetry.NewMetrics(provider.Meter("test"))
			require.NoError(t, err)
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test", "1"))
			api.UseMiddleware(router.MetricTelemetryMiddleware(metrics))
			huma.Register(api, huma.Operation{OperationID: "listing", Method: http.MethodGet, Path: "/v0.1/servers"}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
				require.NotNil(t, telemetry.ListingFromContext(ctx))
				if mode == "panic" {
					panic("private panic")
				}
				return nil, huma.Error500InternalServerError("failure")
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			req := httptest.NewRequest(http.MethodGet, "/v0.1/servers?search=private-search", nil).WithContext(ctx)
			req.Header.Set("X-Request-ID", "ingress-test-id")
			serve := func() { mux.ServeHTTP(httptest.NewRecorder(), req) }
			if mode == "panic" {
				require.Panics(t, serve)
			} else {
				serve()
			}
			var event map[string]any
			require.NoError(t, json.Unmarshal([]byte(output.String()), &event))
			require.Equal(t, "ingress-test-id", event["request_id"])
			require.Equal(t, "listing_complete", event["event"])
			require.Equal(t, "/v0.1/servers", event["route"])
			require.NotContains(t, output.String(), "private-search")
			require.NotContains(t, output.String(), "private panic")
			if mode == "canceled" {
				require.Equal(t, "canceled", event["outcome"])
				require.EqualValues(t, 499, event["status"])
			} else {
				require.EqualValues(t, 500, event["status"])
			}
		})
	}
}
