package validators_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/validators"
)

func newTestClient(timeout time.Duration, maxHops int) *http.Client {
	return validators.NewRepoReachabilityClientForTest(timeout, maxHops)
}

func probe(ctx context.Context, client *http.Client, url string) error {
	return validators.ProbeRepositoryReachableForTest(ctx, client, url)
}

func TestProbeRepositoryReachable_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := probe(context.Background(), newTestClient(2*time.Second, 5), srv.URL); err != nil {
		t.Fatalf("expected nil for 200, got %v", err)
	}
}

func TestProbeRepositoryReachable_FollowsRedirectThenAccepts(t *testing.T) {
	// First server returns 301 → second server returns 200. Default Go transport
	// follows the redirect; we should accept the resulting 200.
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer final.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusMovedPermanently)
	}))
	defer redirector.Close()

	if err := probe(context.Background(), newTestClient(2*time.Second, 5), redirector.URL); err != nil {
		t.Fatalf("expected nil for 301→200, got %v", err)
	}
}

func TestProbeRepositoryReachable_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	err := probe(context.Background(), newTestClient(2*time.Second, 5), srv.URL)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !errors.Is(err, validators.ErrRepositoryURLNotReachable) {
		t.Fatalf("expected wrapped ErrRepositoryURLNotReachable, got %v", err)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected error to include status 404, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("expected error to include URL %q, got %q", srv.URL, err.Error())
	}
}

func TestProbeRepositoryReachable_410And5xx(t *testing.T) {
	cases := []int{http.StatusGone, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable}
	for _, status := range cases {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			err := probe(context.Background(), newTestClient(2*time.Second, 5), srv.URL)
			if err == nil {
				t.Fatalf("expected error for status %d", status)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("%d", status)) {
				t.Fatalf("expected error to include status %d, got %q", status, err.Error())
			}
		})
	}
}

func TestProbeRepositoryReachable_Timeout(t *testing.T) {
	// Server holds the connection longer than the client timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := probe(context.Background(), newTestClient(50*time.Millisecond, 5), srv.URL)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, validators.ErrRepositoryURLNotReachable) {
		t.Fatalf("expected wrapped ErrRepositoryURLNotReachable, got %v", err)
	}
}

func TestProbeRepositoryReachable_RedirectChainLimit(t *testing.T) {
	// Endless redirect server — must trip the redirect cap and surface a
	// reachability error rather than spinning until timeout.
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "/next", http.StatusMovedPermanently)
	}))
	defer srv.Close()

	err := probe(context.Background(), newTestClient(2*time.Second, 3), srv.URL)
	if err == nil {
		t.Fatal("expected error for excessive redirect chain")
	}
	if !errors.Is(err, validators.ErrRepositoryURLNotReachable) {
		t.Fatalf("expected wrapped ErrRepositoryURLNotReachable, got %v", err)
	}
	// Net/http counts the initial request hop, then invokes CheckRedirect on
	// each redirect: with cap=3 we expect ~3 hits before bailing.
	if got := hits.Load(); got > 5 {
		t.Fatalf("expected redirect chain to be capped, got %d hits", got)
	}
}

func TestProbeRepositoryReachable_InvalidURL(t *testing.T) {
	err := probe(context.Background(), newTestClient(2*time.Second, 5), "://not-a-url")
	if err == nil {
		t.Fatal("expected error for malformed URL")
	}
	if !errors.Is(err, validators.ErrRepositoryURLNotReachable) {
		t.Fatalf("expected wrapped ErrRepositoryURLNotReachable, got %v", err)
	}
}

func TestProbeRepositoryReachable_NetworkError(t *testing.T) {
	// Bind a listener and immediately close it to get a guaranteed-unreachable URL.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	url := srv.URL
	srv.Close()

	err := probe(context.Background(), newTestClient(2*time.Second, 5), url)
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
	if !errors.Is(err, validators.ErrRepositoryURLNotReachable) {
		t.Fatalf("expected wrapped ErrRepositoryURLNotReachable, got %v", err)
	}
}
