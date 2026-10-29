package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestPhasesSeparateServerAndBodyDelayAndReuse(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(25 * time.Millisecond)
		w.Header().Set("X-Registry-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(25 * time.Millisecond)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	client := srv.Client()
	defer client.CloseIdleConnections()
	ch := make(chan point, 2)
	request := makeRequesterAt(client, srv.URL, "", ch)
	for range 2 {
		now := time.Now()
		request("/", "page", requestTiming{now.Add(-50 * time.Millisecond), now}, nil)
	}
	first, second := <-ch, <-ch
	if first.failure != "" || second.failure != "" {
		t.Fatal(first.failure, second.failure)
	}
	if !first.phases.didTLS || first.phases.reused || !second.phases.reused || second.phases.didTLS {
		t.Fatalf("unexpected connection reuse: first=%+v second=%+v", first.phases, second.phases)
	}
	for _, p := range []point{first, second} {
		if p.phases.firstByte < 20*time.Millisecond || p.phases.body < 20*time.Millisecond {
			t.Fatalf("server and body phases not separated: %+v", p.phases)
		}
		if p.elapsed < 90 {
			t.Fatalf("scheduled latency lost queue time: %f", p.elapsed)
		}
	}
}

func TestCalibrationFixtureSizeAndCacheHeader(t *testing.T) {
	srv := newCalibrationServer()
	defer srv.Close()
	ch := make(chan point, 1)
	now := time.Now()
	makeRequesterAt(srv.Client(), srv.URL, "", ch)("/v0.1/servers", "page", requestTiming{now, now}, nil)
	p := <-ch
	if p.status != 200 || p.bytes != 65536 || p.cache != "HIT" {
		t.Fatalf("invalid fixture result: %+v", p)
	}
}

func TestPhaseAggregationDoesNotInventMissingTransportEvents(t *testing.T) {
	a := &phaseAccumulator{}
	a.add(point{kind: "page", failure: "timeout"})
	s := a.summary()
	if s.Attempts != 1 || s.ConnectionWait.Count != 0 || s.FirstByte.Count != 0 || s.Body.Count != 0 || s.TLS.Count != 0 {
		t.Fatalf("missing events were treated as observations: %+v", s)
	}
	if phaseKey("page", "untrusted-header") != "page:UNKNOWN" {
		t.Fatal("unbounded cache label")
	}
}

func TestCalibrationRejectsPublishingAndCatalogCollection(t *testing.T) {
	for _, tc := range []struct {
		name, token string
		catalog     bool
	}{
		{"publish", "private-token-path", false}, {"catalog", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("unsafe calibration mode accepted")
				}
			}()
			_, closeFixture := configureCalibration(newTransport(false), true, tc.token, tc.catalog)
			defer closeFixture()
		})
	}
}
