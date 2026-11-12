package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPKeyFetcherRetryCancellation(t *testing.T) {
	for _, duringBody := range []bool{false, true} {
		t.Run(fmt.Sprint(duringBody), func(t *testing.T) {
			var count atomic.Int32
			started := make(chan struct{})
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if count.Add(1) == 1 {
					close(started)
				}
				if duringBody {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				} else {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			}))
			defer srv.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := newLocalTLSFetcher(srv).FetchKey(ctx, internalTestDomain); done <- err }()
			<-started
			time.Sleep(50 * time.Millisecond)
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation did not promptly stop fetch")
			}
			if count.Load() != 1 {
				t.Fatalf("requests = %d", count.Load())
			}
		})
	}
}

func TestHTTPKeyFetcherRetryHonorsEarlierCallerDeadline(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := newLocalTLSFetcher(srv).FetchKey(ctx, internalTestDomain)
	if !errors.Is(err, context.DeadlineExceeded) || count.Load() != 1 || time.Since(started) > time.Second {
		t.Fatalf("err=%v requests=%d elapsed=%s", err, count.Load(), time.Since(started))
	}
}

func TestHTTPKeyFetcherRetryRecoversAfterAttemptTimeout(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Add(1) == 1 {
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()
	fetcher := newLocalTLSFetcher(srv)
	fetcher.client.Timeout = 200 * time.Millisecond
	key, err := fetcher.FetchKey(context.Background(), internalTestDomain)
	if err != nil || key != "recovered" || count.Load() != 2 {
		t.Fatalf("key=%q err=%v requests=%d", key, err, count.Load())
	}
}

type failingKeyTransport struct {
	err      error
	requests int
}

func (t *failingKeyTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	t.requests++
	return nil, t.err
}

func TestHTTPKeyFetcherRetryRejectsPermanentTransportFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"blocked-address", fmt.Errorf("dial example.com: %w", errHTTPKeyAddressBlocked)},
		{"nonexistent-domain", &net.DNSError{Err: "no such host", Name: internalTestDomain, IsNotFound: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &failingKeyTransport{err: tc.err}
			fetcher := &DefaultHTTPKeyFetcher{client: newHTTPKeyFetcherClient(transport)}
			_, err := fetcher.FetchKey(context.Background(), internalTestDomain)
			if !errors.Is(err, tc.err) || transport.requests != 1 {
				t.Fatalf("err=%v requests=%d", err, transport.requests)
			}
		})
	}
}

func TestHTTPKeyFetcherRetryBodyAndRedirectBoundaries(t *testing.T) {
	for _, scenario := range []string{"truncated", "oversized", "redirect-after-retry"} {
		t.Run(scenario, func(t *testing.T) {
			var count atomic.Int32
			var redirected atomic.Int32
			target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { redirected.Add(1); w.WriteHeader(http.StatusOK) }))
			defer target.Close()
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempt := count.Add(1)
				switch scenario {
				case "truncated":
					if attempt == 1 {
						w.Header().Set("Content-Length", "1000")
						_, _ = w.Write([]byte("partial"))
						return
					}
					_, _ = w.Write([]byte("complete"))
				case "oversized":
					_, _ = w.Write([]byte(strings.Repeat("x", MaxKeyResponseSize+1)))
				case "redirect-after-retry":
					if attempt == 1 {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					w.Header().Set("Location", target.URL)
					w.WriteHeader(http.StatusFound)
				}
			}))
			defer srv.Close()
			key, err := newLocalTLSFetcher(srv).FetchKey(context.Background(), internalTestDomain)
			want := int32(2)
			if scenario == "truncated" {
				if err != nil || key != "complete" {
					t.Fatalf("key=%q err=%v", key, err)
				}
			} else if err == nil {
				t.Fatal("expected error")
			}
			if scenario == "oversized" {
				want = 1
			}
			if count.Load() != want || redirected.Load() != 0 {
				t.Fatalf("requests=%d redirected=%d", count.Load(), redirected.Load())
			}
		})
	}
}

func TestHTTPKeyFetcherRetryTLSVerificationFailsClosed(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { count.Add(1) }))
	defer srv.Close()
	transport := newLocalTLSTransport(srv)
	transport.TLSClientConfig.InsecureSkipVerify = false
	var handshakes atomic.Int32
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{TLSHandshakeStart: func() { handshakes.Add(1) }})
	_, err := (&DefaultHTTPKeyFetcher{client: newHTTPKeyFetcherClient(transport)}).FetchKey(ctx, internalTestDomain)
	if err == nil || count.Load() != 0 || handshakes.Load() != 1 {
		t.Fatalf("err=%v HTTP requests=%d TLS handshakes=%d", err, count.Load(), handshakes.Load())
	}
	t.Logf("TLS rejection: %v", err)
}

func TestHTTPKeyFetcherRetryProductionTimeBudget(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	started := time.Now()
	_, err := newLocalTLSFetcher(srv).FetchKey(context.Background(), internalTestDomain)
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) || count.Load() != 2 || elapsed < 14*time.Second || elapsed > 18*time.Second {
		t.Fatalf("err=%v requests=%d elapsed=%s", err, count.Load(), elapsed)
	}
	t.Logf("requests=%d elapsed=%s error=%v", count.Load(), elapsed, err)
}
