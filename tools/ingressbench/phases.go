package main

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"runtime/pprof"
	"strings"
	"sync"
	"time"
)

// The fixture exercises scheduling, HTTPS and response consumption on the load
// host. It includes server overhead, but cannot validate the external network.
func newCalibrationServer() *httptest.Server {
	body := []byte(`{"data":"` + strings.Repeat("x", 65536-11) + `"}`)
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Registry-Cache", "HIT")
		_, _ = w.Write(body)
	}))
}

type phaseObservation struct {
	connectionWait, tls, firstByte, body        time.Duration
	gotConnection, didTLS, gotFirstByte, reused bool
}

type requestTrace struct {
	mu                                                              sync.Mutex
	getConnection, gotConnection, tlsStart, wroteRequest, firstByte time.Time
	tlsDuration                                                     time.Duration
	didTLS, reused                                                  bool
}

func newRequestTrace() *requestTrace { return &requestTrace{} }

// Hooks can execute on transport goroutines, including after request failure.
// No addresses, URLs, headers or credentials are retained.
func (t *requestTrace) context(ctx context.Context) context.Context {
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GetConn: func(_ string) { t.mu.Lock(); defer t.mu.Unlock(); t.getConnection = time.Now() },
		GotConn: func(info httptrace.GotConnInfo) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.gotConnection = time.Now()
			t.reused = info.Reused
		},
		TLSHandshakeStart: func() { t.mu.Lock(); defer t.mu.Unlock(); t.tlsStart = time.Now() },
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			t.mu.Lock()
			defer t.mu.Unlock()
			if !t.tlsStart.IsZero() {
				t.tlsDuration += time.Since(t.tlsStart)
				t.didTLS = true
			}
		},
		WroteRequest:         func(_ httptrace.WroteRequestInfo) { t.mu.Lock(); defer t.mu.Unlock(); t.wroteRequest = time.Now() },
		GotFirstResponseByte: func() { t.mu.Lock(); defer t.mu.Unlock(); t.firstByte = time.Now() },
	})
}

func (t *requestTrace) observation(finished time.Time) phaseObservation {
	t.mu.Lock()
	defer t.mu.Unlock()
	o := phaseObservation{tls: t.tlsDuration, didTLS: t.didTLS, reused: t.reused}
	if !t.getConnection.IsZero() && !t.gotConnection.IsZero() {
		o.gotConnection = true
		o.connectionWait = t.gotConnection.Sub(t.getConnection)
	}
	if !t.firstByte.IsZero() {
		o.gotFirstByte = true
		o.body = finished.Sub(t.firstByte)
		if !t.wroteRequest.IsZero() {
			o.firstByte = t.firstByte.Sub(t.wroteRequest)
		}
	}
	return o
}

type phaseSummary struct {
	Attempts, NewConnections, ReusedConnections                          int
	SchedulerLag, QueueDelay, ConnectionWait, TLS, FirstByte, Body, HTTP durationSummary
}

type phaseAccumulator struct {
	attempts, newConnections, reusedConnections                          int
	schedulerLag, queueDelay, connectionWait, tls, firstByte, body, http durationHistogram
}

func phaseKey(kind, cache string) string {
	switch cache {
	case "HIT", "MISS", "STALE", "UPDATING", "BYPASS", "EXPIRED", "REVALIDATED":
	default:
		cache = "UNKNOWN"
	}
	return kind + ":" + cache
}

func (a *phaseAccumulator) add(p point) {
	a.attempts++
	a.schedulerLag.add(p.queuedAt.Sub(p.scheduledAt))
	a.queueDelay.add(p.startedAt.Sub(p.queuedAt))
	a.http.add(p.finishedAt.Sub(p.startedAt))
	o := p.phases
	if o.gotConnection {
		a.connectionWait.add(o.connectionWait)
		if o.reused {
			a.reusedConnections++
		} else {
			a.newConnections++
		}
	}
	if o.didTLS {
		a.tls.add(o.tls)
	}
	if o.gotFirstByte {
		a.firstByte.add(o.firstByte)
		a.body.add(o.body)
	}
}

func (a *phaseAccumulator) summary() phaseSummary {
	return phaseSummary{a.attempts, a.newConnections, a.reusedConnections,
		a.schedulerLag.summary(), a.queueDelay.summary(), a.connectionWait.summary(), a.tls.summary(), a.firstByte.summary(), a.body.summary(), a.http.summary()}
}

func configureCalibration(tr *http.Transport, enabled bool, tokenFile string, catalog bool) (string, func()) {
	if !enabled {
		return target, func() {}
	}
	if tokenFile != "" || catalog {
		panic("calibration cannot publish or collect a staging catalog")
	}
	fixture := newCalibrationServer()
	tr.TLSClientConfig = fixture.Client().Transport.(*http.Transport).TLSClientConfig
	return fixture.URL, fixture.Close
}

func startCPUProfile(path string) func() {
	if path == "" {
		return func() {}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		panic(err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		panic(err)
	}
	return func() { pprof.StopCPUProfile(); _ = f.Close() }
}
