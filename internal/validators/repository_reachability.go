package validators

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrRepositoryURLNotReachable is returned when a publisher-supplied repository
// URL fails a publish-time reachability probe (404, 5xx, network failure,
// timeout, or excessive redirect chain).
var ErrRepositoryURLNotReachable = errors.New("repository URL is not publicly reachable")

const (
	repoReachabilityTimeout   = 10 * time.Second
	repoReachabilityMaxHops   = 5
	repoReachabilityUserAgent = "MCP-Registry-Validator/1.0"
)

// defaultRepoReachabilityClient is reused across publishes so connections to
// github.com / gitlab.com can be pooled.
var defaultRepoReachabilityClient = newRepoReachabilityClient(repoReachabilityTimeout, repoReachabilityMaxHops)

func newRepoReachabilityClient(timeout time.Duration, maxHops int) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= maxHops {
				return fmt.Errorf("stopped after %d redirects", maxHops)
			}
			return nil
		},
	}
}

// ProbeRepositoryReachable issues an HTTP HEAD against repoURL to confirm the
// repository is publicly reachable. Returns nil on 2xx/3xx responses, and an
// error wrapping ErrRepositoryURLNotReachable for 4xx/5xx, network failures,
// timeouts, or redirect chains exceeding the cap. Callers should skip the
// probe when the repository field is unset or reachability checks are
// disabled in config.
func ProbeRepositoryReachable(ctx context.Context, repoURL string) error {
	return probeRepositoryReachable(ctx, defaultRepoReachabilityClient, repoURL)
}

func probeRepositoryReachable(ctx context.Context, client *http.Client, repoURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, repoURL, nil)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrRepositoryURLNotReachable, repoURL, err)
	}
	req.Header.Set("User-Agent", repoReachabilityUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrRepositoryURLNotReachable, repoURL, err)
	}
	defer resp.Body.Close()

	// Accept 2xx and 3xx (matches the spec's "200, 301, 302" with headroom for
	// 304/307/308 that GitHub/GitLab also legitimately return). 4xx and 5xx
	// are the failure modes the original PGA-golf bug reproduced as 404.
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}
	return fmt.Errorf("%w: %s (status %d)", ErrRepositoryURLNotReachable, repoURL, resp.StatusCode)
}
