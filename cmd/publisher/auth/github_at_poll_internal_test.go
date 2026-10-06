package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMockTokenServer returns an httptest.Server that serves the given sequence
// of AccessTokenResponse values, one per request. Once the sequence is
// exhausted the final response is reused for any extra requests.
func newMockTokenServer(t *testing.T, responses []AccessTokenResponse) *httptest.Server {
	t.Helper()
	var i int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := responses[len(responses)-1]
		if i < len(responses) {
			resp = responses[i]
		}
		i++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp) // #nosec G117 -- test fixture token, not a real secret
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newPollTestProvider builds a provider wired to a mock token endpoint. The
// initial poll interval is 0 (so the back-off starts from a known base) and
// sleeps are captured rather than performed, making the tests deterministic and
// instant. The returned slice records every interval passed to sleep.
func newPollTestProvider(tokenURL string) (*GitHubATProvider, *[]time.Duration) {
	slept := &[]time.Duration{}
	p := &GitHubATProvider{
		clientID:       "test-client-id",
		accessTokenURL: tokenURL,
		pollInterval:   0,
		sleep:          func(d time.Duration) { *slept = append(*slept, d) },
	}
	return p, slept
}

// TestPollForToken_SlowDownThenSuccess confirms that a slow_down response is
// treated as retriable: polling continues and the success path is reached.
func TestPollForToken_SlowDownThenSuccess(t *testing.T) {
	srv := newMockTokenServer(t, []AccessTokenResponse{
		{Error: "slow_down"},
		{AccessToken: "gh-token"},
	})
	p, slept := newPollTestProvider(srv.URL)

	token, err := p.pollForToken(context.Background(), "device-code")
	require.NoError(t, err)
	assert.Equal(t, "gh-token", token)

	// One slow_down => one back-off, interval bumped 0 -> 5.
	require.Len(t, *slept, 1)
	assert.Equal(t, 5*time.Second, (*slept)[0])
}

// TestPollForToken_MixedRetryableErrorsCompoundInterval confirms the interval
// increment compounds correctly across mixed retryable errors. Per RFC 8628
// §3.5 the interval is increased by 5 seconds on each slow_down and is NOT
// reset between slow_down instances, while authorization_pending leaves it
// unchanged.
func TestPollForToken_MixedRetryableErrorsCompoundInterval(t *testing.T) {
	srv := newMockTokenServer(t, []AccessTokenResponse{
		{Error: "slow_down"},             // 0 -> 5
		{Error: "authorization_pending"}, // stays 5
		{Error: "slow_down"},             // 5 -> 10
		{AccessToken: "gh-token"},
	})
	p, slept := newPollTestProvider(srv.URL)

	token, err := p.pollForToken(context.Background(), "device-code")
	require.NoError(t, err)
	assert.Equal(t, "gh-token", token)

	require.Len(t, *slept, 3)
	assert.Equal(t, 5*time.Second, (*slept)[0])  // after first slow_down
	assert.Equal(t, 5*time.Second, (*slept)[1])  // authorization_pending: unchanged
	assert.Equal(t, 10*time.Second, (*slept)[2]) // second slow_down: compounded
}

// TestPollForToken_IntervalCappedAtMax confirms the polling interval is capped
// so a misbehaving auth server returning slow_down repeatedly cannot grow it
// without bound.
func TestPollForToken_IntervalCappedAtMax(t *testing.T) {
	// 13 slow_downs starting from interval 0 would otherwise reach 65s
	// (5, 10, ..., 60, 65); the cap holds it at 60.
	responses := make([]AccessTokenResponse, 0, 14)
	for range [13]struct{}{} {
		responses = append(responses, AccessTokenResponse{Error: "slow_down"})
	}
	responses = append(responses, AccessTokenResponse{AccessToken: "gh-token"})

	srv := newMockTokenServer(t, responses)
	p, slept := newPollTestProvider(srv.URL)

	token, err := p.pollForToken(context.Background(), "device-code")
	require.NoError(t, err)
	assert.Equal(t, "gh-token", token)

	require.Len(t, *slept, 13)
	for _, d := range *slept {
		assert.LessOrEqual(t, d, time.Duration(maxPollInterval)*time.Second)
	}
	// The last two back-offs are both pinned at the cap.
	assert.Equal(t, time.Duration(maxPollInterval)*time.Second, (*slept)[11])
	assert.Equal(t, time.Duration(maxPollInterval)*time.Second, (*slept)[12])
}

// TestPollForToken_FatalErrorStopsPolling confirms a non-retryable error ends
// polling immediately with an error.
func TestPollForToken_FatalErrorStopsPolling(t *testing.T) {
	srv := newMockTokenServer(t, []AccessTokenResponse{
		{Error: "access_denied"},
	})
	p, slept := newPollTestProvider(srv.URL)

	_, err := p.pollForToken(context.Background(), "device-code")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access_denied")
	assert.Empty(t, *slept, "fatal error should not trigger a back-off sleep")
}
