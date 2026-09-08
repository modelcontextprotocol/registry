package validators

// Test-only re-exports of internal helpers used by repository_reachability_test.go
// in the external validators_test package.

import (
	"context"
	"net/http"
	"time"
)

// ProbeRepositoryReachableForTest exposes the internal probe with an injectable
// client so tests can drive timeouts and redirect caps deterministically without
// hitting the package-level default client.
func ProbeRepositoryReachableForTest(ctx context.Context, client *http.Client, repoURL string) error {
	return probeRepositoryReachable(ctx, client, repoURL)
}

// NewRepoReachabilityClientForTest exposes the test-tunable client constructor.
func NewRepoReachabilityClientForTest(timeout time.Duration, maxHops int) *http.Client {
	return newRepoReachabilityClient(timeout, maxHops)
}
