package usage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedLogging answers each request to the log API with the next step.
type scriptedLogging struct {
	server *httptest.Server
	calls  atomic.Int32
	tokens []string
	quota  []string
	bodies []map[string]any
	// clock is the reader's idea of the time. It moves only when the reader waits.
	clock time.Time
}

func newScriptedLogging(t *testing.T, steps ...func(w http.ResponseWriter)) *scriptedLogging {
	t.Helper()
	scripted := &scriptedLogging{}
	scripted.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(scripted.calls.Add(1)) - 1
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		scripted.bodies = append(scripted.bodies, body)
		scripted.tokens = append(scripted.tokens, r.Header.Get("Authorization"))
		scripted.quota = append(scripted.quota, r.Header.Get("X-Goog-User-Project"))
		steps[min(call, len(steps)-1)](w)
	}))
	t.Cleanup(scripted.server.Close)
	return scripted
}

func status(code int, body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}
}

func dropConnection(w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	if conn, _, err := hijacker.Hijack(); err == nil {
		_ = conn.Close()
	}
}

func (s *scriptedLogging) reader(waits *[]time.Duration) *CloudLogging {
	issued := 0
	s.clock = time.Unix(1790000000, 0)
	return &CloudLogging{
		Project: "example-project", Cluster: "example-cluster", Namespace: "ingress-nginx", Container: "nginx", QuotaProject: "billing-project",
		Endpoint: s.server.URL, Client: s.server.Client(), ReadsPerMinute: 60,
		now: func() time.Time { return s.clock },
		Token: func(context.Context) (string, error) {
			issued++
			return "token-" + strconv.Itoa(issued), nil
		},
		sleep: func(_ context.Context, wait time.Duration) error {
			*waits = append(*waits, wait)
			s.clock = s.clock.Add(wait)
			return nil
		},
	}
}

func readAll(t *testing.T, reader *CloudLogging) ([]string, error) {
	t.Helper()
	var seen []string
	err := reader.Read(t.Context(), LogWindow{Start: time.Unix(0, 0), End: time.Unix(86400, 0), Fraction: 1},
		func(timestamp string, payload map[string]any) error {
			seen = append(seen, timestamp+" "+pyField(payload, "n"))
			return nil
		})
	return seen, err
}

func TestLogReaderRetriesAndPages(t *testing.T) {
	var waits []time.Duration
	scripted := newScriptedLogging(t,
		status(http.StatusInternalServerError, `{"error":{"status":"INTERNAL"}}`),
		status(http.StatusTooManyRequests, `{"error":{"status":"RESOURCE_EXHAUSTED"}}`),
		status(http.StatusUnauthorized, `{"error":{"status":"UNAUTHENTICATED"}}`),
		status(http.StatusOK, `{"entries":[{"timestamp":"t1","jsonPayload":{"n":1}},{"timestamp":"t2"},{"timestamp":"t3","jsonPayload":{}}],"nextPageToken":"next"}`),
		dropConnection,
		status(http.StatusOK, `not json`),
		status(http.StatusOK, `{"nextPageToken":"more"}`),
		status(http.StatusOK, `{"entries":[{"timestamp":"t4","jsonPayload":{"n":2.50}}]}`),
	)
	seen, err := readAll(t, scripted.reader(&waits))
	require.NoError(t, err)
	assert.Equal(t, []string{"t1 1", "t4 2.5"}, seen, "entries without a payload are skipped; nothing is delivered twice")
	// A second passes between two requests, at 60 a minute; after a longer wait the next one starts at once.
	assert.Equal(t, []time.Duration{5 * time.Second, 20 * time.Second, 5 * time.Second, time.Second, 5 * time.Second,
		5 * time.Second, time.Second}, waits)
	assert.Equal(t, "Bearer token-1", scripted.tokens[0])
	assert.Equal(t, "Bearer token-4", scripted.tokens[3], "a token is asked for again on every request")
	assert.Equal(t, "billing-project", scripted.quota[0])
	assert.Nil(t, scripted.bodies[0]["pageToken"])
	assert.Equal(t, "next", scripted.bodies[4]["pageToken"], "a failed page is asked for again")
	assert.Equal(t, "next", scripted.bodies[6]["pageToken"])
	assert.Equal(t, "more", scripted.bodies[7]["pageToken"])
	assert.Equal(t, []any{"projects/example-project"}, scripted.bodies[0]["resourceNames"])
}

// TestLogReaderPacesItsRequests checks that requests start no more often than
// the setting allows, however quickly the pages come back, and that the
// spacing carries over from one read to the next.
func TestLogReaderPacesItsRequests(t *testing.T) {
	var waits []time.Duration
	more := status(http.StatusOK, `{"entries":[{"timestamp":"t","jsonPayload":{"n":1}}],"nextPageToken":"next"}`)
	last := status(http.StatusOK, `{}`)
	scripted := newScriptedLogging(t, more, more, more, last, last)
	reader := scripted.reader(&waits)
	reader.ReadsPerMinute = 50
	// How long each request takes. The third is slower than the spacing.
	took := []time.Duration{200 * time.Millisecond, 200 * time.Millisecond, 3 * time.Second, 200 * time.Millisecond, 200 * time.Millisecond}
	token := reader.Token
	reader.Token = func(ctx context.Context) (string, error) {
		scripted.clock = scripted.clock.Add(took[len(scripted.tokens)])
		return token(ctx)
	}
	started := scripted.clock

	_, err := readAll(t, reader)
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{time.Second, time.Second}, waits,
		"requests start 1.2 seconds apart; nothing is added after one that took longer than that")
	_, err = readAll(t, reader)
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{time.Second, time.Second, time.Second}, waits, "the next read waits its turn too")
	assert.Equal(t, int32(5), scripted.calls.Load())
	assert.Equal(t, 6800*time.Millisecond, scripted.clock.Sub(started))
}

func TestLogReaderStopsOnPermanentErrors(t *testing.T) {
	t.Run("a refused request is not repeated", func(t *testing.T) {
		var waits []time.Duration
		scripted := newScriptedLogging(t, status(http.StatusForbidden,
			`{"error":{"status":"PERMISSION_DENIED","message":"caller user@example.org lacks logging.logEntries.list"}}`))
		_, err := readAll(t, scripted.reader(&waits))
		require.EqualError(t, err, "Cloud Logging read failed with HTTP 403 (PERMISSION_DENIED)")
		assert.Equal(t, int32(1), scripted.calls.Load())
		assert.Empty(t, waits)
	})
	t.Run("a failing server is given up on", func(t *testing.T) {
		var waits []time.Duration
		scripted := newScriptedLogging(t, status(http.StatusBadGateway, "<html>bad gateway</html>"))
		_, err := readAll(t, scripted.reader(&waits))
		require.EqualError(t, err, "Cloud Logging read failed with HTTP 502")
		assert.Equal(t, int32(logMaxRetries+1), scripted.calls.Load())
	})
	t.Run("no credentials", func(t *testing.T) {
		var waits []time.Duration
		scripted := newScriptedLogging(t, status(http.StatusOK, `{}`))
		reader := scripted.reader(&waits)
		reader.Token = func(context.Context) (string, error) { return "", errors.New("reauthentication needed") }
		_, err := readAll(t, reader)
		require.ErrorIs(t, err, errNoToken)
		assert.Equal(t, int32(0), scripted.calls.Load())
	})
}

func TestMetricsQuery(t *testing.T) {
	var targets []string
	failures := 2
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targets = append(targets, r.URL.RequestURI())
		switch {
		case failures > 0:
			failures--
			http.Error(w, "starting", http.StatusServiceUnavailable)
		case r.URL.Query().Get("query") == "broken":
			_, _ = io.WriteString(w, `{"status":"success","data":{"result":[{"metric":{},"value":[1,"not a number"]}]}}`)
		case r.URL.Query().Get("query") == "error":
			_, _ = io.WriteString(w, `{"status":"error","error":"bad query"}`)
		default:
			_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"route":"list_v01","status":"200"},"value":[1790000000,"1234.5"]},{"metric":{},"value":[1790000000,"7"]}]}}`)
		}
	}))
	t.Cleanup(server.Close)
	var waits []time.Duration
	metrics := &Prometheus{BaseURL: server.URL + "/proxy/", Client: server.Client(), sleep: func(_ context.Context, wait time.Duration) error {
		waits = append(waits, wait)
		return nil
	}}
	samples, err := metrics.Query(t.Context(), `sum by (route) (increase(x{traffic="registry"}[86400s]))`, time.Unix(1790000000, 0))
	require.NoError(t, err)
	assert.Equal(t, []MetricSample{{Labels: map[string]string{"route": "list_v01", "status": "200"}, Value: 1234.5}, {Labels: map[string]string{}, Value: 7}}, samples)
	assert.Len(t, waits, 2, "a query that fails is tried again")
	// Every reserved character is percent-encoded, as the Kubernetes API proxy requires.
	assert.Equal(t, "/proxy/api/v1/query?query=sum%20by%20%28route%29%20%28increase%28x%7Btraffic%3D%22registry%22%7D%5B86400s%5D%29%29&time=1790000000",
		targets[len(targets)-1])

	_, err = metrics.Query(t.Context(), "broken", time.Unix(0, 0))
	require.Error(t, err)
	_, err = metrics.Query(t.Context(), "error", time.Unix(0, 0))
	require.Error(t, err)
}
