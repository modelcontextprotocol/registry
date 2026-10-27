package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Calibration must actually exercise the full real-cursor catalog, rather than
// accidentally revisiting a small subset because its length divides the rate.
func TestVariedPagesReachWholeCatalog(t *testing.T) {
	for _, hot := range []int{0, 50, 80, 99} {
		seen := map[int]bool{}
		for n := 0; n < 4000*100; n++ {
			i := pageIndex(n, hot, 4000)
			if i < 0 || i >= 4000 {
				t.Fatalf("invalid index %d", i)
			}
			seen[i] = true
		}
		if len(seen) != 4000 {
			t.Fatalf("hot=%d visited %d/4000 keys", hot, len(seen))
		}
	}
}

type unavailableTransport struct{}

func (unavailableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("private-body")), Header: make(http.Header), Request: req}, nil
}

func TestFailureSamplesIncludeTimingWithoutRequestData(t *testing.T) {
	points := make(chan point, 1)
	scheduled := time.Now().Add(-time.Second)
	client := &http.Client{Transport: unavailableTransport{}}
	makeRequester(client, "private-token", points)("/v0/servers?search=private-query", "page", scheduled, nil)
	close(points)
	r := result{Status: map[string]int{}, Cache: map[string]int{}, Errors: map[string]int{}, Protocols: map[string]int{}, ErrorDetails: map[string]string{}}
	done := make(chan struct{})
	collectResults(points, &r, map[string][]float64{}, done)
	if len(r.Failures) != 1 || r.Failures[0].Status != http.StatusServiceUnavailable ||
		!r.Failures[0].ScheduledAt.Equal(scheduled) || !r.Failures[0].FinishedAt.After(scheduled) {
		t.Fatalf("missing failure timing: %+v", r.Failures)
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-") {
		t.Fatal("request data leaked into results")
	}
}

func TestExplicitHTTPProtocol(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	for _, h2 := range []bool{false, true} {
		tr := newTransport(h2)
		tr.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig
		defer tr.CloseIdleConnections()
		client := &http.Client{Transport: tr}
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := 1
		if h2 {
			want = 2
		}
		if resp.ProtoMajor != want {
			t.Fatalf("allowHTTP2=%v negotiated %s", h2, resp.Proto)
		}
	}
}
