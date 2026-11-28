package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A 200 with an empty body is not a public key. Before this was rejected, an
// endpoint that answered 200 and then stalled could race the client timeout:
// if the empty 200 arrived first, FetchKey returned ("", nil), skipping the
// retry and surfacing later as "no valid MCP public key found" instead of the
// real transport problem. Seen in CI as key="" err=<nil> requests=1.
func TestHTTPKeyFetcherEmptyBodyIsNotTreatedAsSuccess(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fetcher := newLocalTLSFetcher(srv)
	fetcher.client.Timeout = 2 * time.Second

	key, err := fetcher.FetchKey(context.Background(), internalTestDomain)
	if err == nil {
		t.Fatalf("empty body accepted as key: key=%q requests=%d", key, count.Load())
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("error should name the empty response, got %v", err)
	}
	if key != "" {
		t.Fatalf("key = %q, want empty", key)
	}
	// Transient, so both attempts are made and the count is reported.
	if count.Load() != httpKeyFetchAttempts {
		t.Fatalf("requests = %d, want %d (empty body must be retried)", count.Load(), httpKeyFetchAttempts)
	}
}

// The common real-world shape: one bad edge response, then the key. This must
// recover rather than fail the login.
func TestHTTPKeyFetcherRecoversAfterEmptyBody(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if count.Add(1) == 1 {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	key, err := newLocalTLSFetcher(srv).FetchKey(context.Background(), internalTestDomain)
	if err != nil || key != "recovered" || count.Load() != 2 {
		t.Fatalf("key=%q err=%v requests=%d", key, err, count.Load())
	}
}

// A body that is only whitespace is empty for this protocol's purposes.
func TestHTTPKeyFetcherWhitespaceBodyIsNotTreatedAsSuccess(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		_, _ = w.Write([]byte("  \n\t "))
	}))
	defer srv.Close()

	fetcher := newLocalTLSFetcher(srv)
	fetcher.client.Timeout = 2 * time.Second
	key, err := fetcher.FetchKey(context.Background(), internalTestDomain)
	if err == nil || key != "" {
		t.Fatalf("whitespace body accepted: key=%q err=%v", key, err)
	}
	if count.Load() != httpKeyFetchAttempts {
		t.Fatalf("requests = %d, want %d", count.Load(), httpKeyFetchAttempts)
	}
}
