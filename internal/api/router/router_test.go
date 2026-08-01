package router_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modelcontextprotocol/registry/internal/api/router"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

// TestMetricMiddleware_ClientCancelledNotCountedAsError verifies that a request
// the client gave up on (request-context Canceled) is recorded as 499 in
// http_requests_total and is NOT incremented in http_errors_total — even when
// the handler returned huma.Error500InternalServerError because its DB call
// surfaced context.Canceled.
//
// Regression test for the case where a ServiceNow-style scraper burst
// generated thousands of internal 500s for cancelled requests, tripping the
// "Availability dropped below 95%" alert despite zero 5xx reaching clients.
func TestMetricMiddleware_ClientCancelledNotCountedAsError(t *testing.T) {
	shutdown, metrics, err := telemetry.InitMetrics("test")
	require.NoError(t, err)
	defer func() { _ = shutdown(context.Background()) }()

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	api.UseMiddleware(router.MetricTelemetryMiddleware(metrics))

	// Handler that simulates: DB returns context.Canceled because the client
	// disconnected, handler converts to huma 500.
	huma.Register(api, huma.Operation{
		OperationID: "cancelled-list",
		Method:      http.MethodGet,
		Path:        "/v0/servers",
	}, func(_ context.Context, _ *struct{}) (*struct{}, error) {
		return nil, huma.Error500InternalServerError("Failed to get registry list",
			errors.New("error iterating rows: context canceled"))
	})

	mux.Handle("/metrics", metrics.PrometheusHandler())

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/v0/servers", nil).WithContext(cancelledCtx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	scrapeReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	scrapeRec := httptest.NewRecorder()
	mux.ServeHTTP(scrapeRec, scrapeReq)
	assert.Equal(t, http.StatusOK, scrapeRec.Code)

	body := scrapeRec.Body.String()

	// The request is recorded — but as 499, not 500.
	assert.True(t,
		containsMetric(body, `mcp_registry_http_requests_total`, `status_code="499"`, `path="/v0/servers"`),
		"expected requests_total to record status_code=499 for the cancelled request; got:\n%s",
		filterMetricLines(body, "mcp_registry_http_requests_total"),
	)
	assert.False(t,
		containsMetric(body, `mcp_registry_http_requests_total`, `status_code="500"`, `path="/v0/servers"`),
		"did not expect requests_total to record status_code=500 for a client-cancelled request; got:\n%s",
		filterMetricLines(body, "mcp_registry_http_requests_total"),
	)

	// The error counter is NOT bumped for 499s — that is the point of this fix.
	assert.False(t,
		containsMetric(body, `mcp_registry_http_errors_total`, `status_code="499"`),
		"errors_total must not be incremented for client-cancelled requests; got:\n%s",
		filterMetricLines(body, "mcp_registry_http_errors_total"),
	)
	assert.False(t,
		containsMetric(body, `mcp_registry_http_errors_total`, `status_code="500"`, `path="/v0/servers"`),
		"errors_total must not record a 500 for a request the client already gave up on; got:\n%s",
		filterMetricLines(body, "mcp_registry_http_errors_total"),
	)
}

// containsMetric returns true iff some line in body starts with `metric{...}`
// and that label set contains every requested label fragment.
func containsMetric(body, metric string, labels ...string) bool {
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, metric+"{") {
			continue
		}
		ok := true
		for _, l := range labels {
			if !strings.Contains(line, l) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func filterMetricLines(body, metric string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, metric) {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
