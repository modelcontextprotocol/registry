package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seqTokenServer serves a fixed sequence of (status, body) responses, one per
// request, reusing the last one once the sequence is exhausted. Unlike
// newMockTokenServer it can serve non-200 statuses and non-JSON bodies, which
// the transient-5xx path needs.
type seqTokenServer struct {
	srv   *httptest.Server
	mu    sync.Mutex
	seen  int
	items []seqResp
}

type seqResp struct {
	status int
	body   string
}

func newSeqTokenServer(t *testing.T, items ...seqResp) *seqTokenServer {
	t.Helper()
	s := &seqTokenServer{items: items}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		item := s.items[len(s.items)-1]
		if s.seen < len(s.items) {
			item = s.items[s.seen]
		}
		s.seen++
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(item.status)
		_, _ = w.Write([]byte(item.body))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *seqTokenServer) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen
}

func okResp(body string) seqResp { return seqResp{status: http.StatusOK, body: body} }

// serverErrorResp is a 502 carrying an HTML body rather than JSON, the shape a
// transient edge failure takes.
func serverErrorResp() seqResp {
	return seqResp{status: http.StatusBadGateway, body: `<html>Server Error</html>`}
}

const (
	testClientID = "test-client-id"

	pendingBody   = `{"error":"authorization_pending"}`
	incorrectBody = `{"error":"incorrect_device_code"}`
	// The full shape GitHub sends for this error, description and URI included.
	incorrectVerboseBody = `{"error":"incorrect_device_code","error_description":"The device_code provided is not valid.","error_uri":"https://docs.github.com/developers/apps/authorizing-oauth-apps#error-codes-for-the-device-flow"}`
	tokenBody            = `{"access_token":"gho_test_token","token_type":"bearer","scope":"read:org,read:user"}` // #nosec G101 -- test fixture, not a real secret
)

// TestPollForToken_IncorrectDeviceCodeIsTerminalAfterGrace confirms a code that
// stays invalid past the grace polls still fails, and fails with the error
// GitHub reported rather than a rewritten one.
func TestPollForToken_IncorrectDeviceCodeIsTerminalAfterGrace(t *testing.T) {
	gh := newSeqTokenServer(t, okResp(pendingBody), okResp(pendingBody), okResp(incorrectBody))
	p, _ := newPollTestProvider(gh.srv.URL)

	_, err := p.pollForToken(context.Background(), "issued-device-code")
	require.Error(t, err)
	assert.Equal(t, "token request failed: incorrect_device_code", err.Error())
	// 2 pending + 1 incorrect + 2 grace polls that also answer incorrect.
	assert.Equal(t, 5, gh.requests())
}

// TestPollForToken_TransientIncorrectDeviceCodeRecovered covers the case the
// grace retries exist for: one incorrect_device_code between a pending poll and
// a successful one must not discard a token the user already authorised.
func TestPollForToken_TransientIncorrectDeviceCodeRecovered(t *testing.T) {
	gh := newSeqTokenServer(t, okResp(pendingBody), okResp(incorrectBody), okResp(tokenBody))
	p, _ := newPollTestProvider(gh.srv.URL)

	token, err := p.pollForToken(context.Background(), "issued-device-code")
	require.NoError(t, err, "one transient incorrect_device_code must not be fatal")
	assert.Equal(t, "gho_test_token", token)
	assert.Equal(t, 3, gh.requests())
}

// TestPollForToken_ErrorDescriptionAndURISurfaced confirms the grace budget is
// spent in full against a persistently invalid code, and that GitHub's
// error_description and error_uri reach the user instead of being discarded.
func TestPollForToken_ErrorDescriptionAndURISurfaced(t *testing.T) {
	gh := newSeqTokenServer(t, okResp(incorrectVerboseBody))
	p, _ := newPollTestProvider(gh.srv.URL)

	_, err := p.pollForToken(context.Background(), "issued-device-code")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "incorrect_device_code")
	assert.Contains(t, err.Error(), "The device_code provided is not valid.")
	assert.Contains(t, err.Error(), "error-codes-for-the-device-flow")
	assert.Equal(t, 1+incorrectDeviceCodeGraceRetries, gh.requests())
}

// TestPollForToken_TransientServerErrorRetried confirms a 5xx (which carries an
// HTML body, not JSON) is retried rather than ending the login.
func TestPollForToken_TransientServerErrorRetried(t *testing.T) {
	gh := newSeqTokenServer(t,
		serverErrorResp(),
		okResp(tokenBody),
	)
	p, slept := newPollTestProvider(gh.srv.URL)

	token, err := p.pollForToken(context.Background(), "issued-device-code")
	require.NoError(t, err)
	assert.Equal(t, "gho_test_token", token)
	assert.Equal(t, 2, gh.requests())
	assert.Len(t, *slept, 1, "the 5xx must be followed by one back-off")
}

// TestPollForToken_FirstPollIsImmediate pins the polling schedule: the first
// request goes out without a preceding sleep, so a user who authorises quickly
// is not made to wait an interval for nothing.
func TestPollForToken_FirstPollIsImmediate(t *testing.T) {
	gh := newSeqTokenServer(t, okResp(tokenBody))
	p, slept := newPollTestProvider(gh.srv.URL)

	_, err := p.pollForToken(context.Background(), "issued-device-code")
	require.NoError(t, err)
	assert.Equal(t, 1, gh.requests())
	assert.Empty(t, *slept, "an immediately successful poll must not sleep")
}

// TestRequestDeviceCode_HonoursServerIntervalAndExpiry confirms the interval and
// expires_in in the device-code response are adopted rather than dropped.
func TestRequestDeviceCode_HonoursServerIntervalAndExpiry(t *testing.T) {
	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"dc-1","user_code":"AAAA-BBBB","verification_uri":"https://github.com/login/device","expires_in":60,"interval":7}`))
	}))
	t.Cleanup(device.Close)

	p := &GitHubATProvider{
		clientID:      testClientID,
		deviceCodeURL: device.URL,
		pollInterval:  defaultPollInterval,
		expiresIn:     defaultExpiresIn,
	}

	deviceCode, userCode, uri, err := p.requestDeviceCode(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "dc-1", deviceCode)
	assert.Equal(t, "AAAA-BBBB", userCode)
	assert.Equal(t, "https://github.com/login/device", uri)
	assert.Equal(t, 7, p.pollInterval, "interval from the response must be adopted")
	assert.Equal(t, 60, p.expiresIn, "expires_in from the response must be adopted")
}

// TestRequestDeviceCode_KeepsDefaultsWhenOmitted confirms a response without
// interval or expires_in leaves the documented defaults in place.
func TestRequestDeviceCode_KeepsDefaultsWhenOmitted(t *testing.T) {
	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"dc-2","user_code":"EEEE-FFFF","verification_uri":"https://github.com/login/device"}`))
	}))
	t.Cleanup(device.Close)

	p := &GitHubATProvider{
		clientID:      testClientID,
		deviceCodeURL: device.URL,
		pollInterval:  defaultPollInterval,
		expiresIn:     defaultExpiresIn,
	}

	_, _, _, err := p.requestDeviceCode(context.Background())
	require.NoError(t, err)
	assert.Equal(t, defaultPollInterval, p.pollInterval)
	assert.Equal(t, defaultExpiresIn, p.expiresIn)
}

// deviceCodeServerReturning serves a single device-code response with the given
// raw JSON body, for exercising interval/expires_in handling.
func deviceCodeServerReturning(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestRequestDeviceCode_ClampsExcessiveInterval confirms an interval far above
// maxPollInterval is clamped, protecting the same invariant maxPollInterval
// enforces on the slow_down path.
func TestRequestDeviceCode_ClampsExcessiveInterval(t *testing.T) {
	url := deviceCodeServerReturning(t,
		`{"device_code":"dc","user_code":"AAAA-BBBB","verification_uri":"https://github.com/login/device","expires_in":900,"interval":86400}`)

	p := &GitHubATProvider{clientID: testClientID, deviceCodeURL: url, pollInterval: defaultPollInterval, expiresIn: defaultExpiresIn}
	_, _, _, err := p.requestDeviceCode(context.Background())
	require.NoError(t, err)
	assert.Equal(t, maxPollInterval, p.pollInterval, "interval above the cap must be clamped")
}

// TestRequestDeviceCode_ClampsOverflowValues covers the overflow pair: values
// large enough that time.Duration(n)*time.Second wraps negative would make
// sleep return instantly (spinning the loop) and put the deadline in the past
// (yielding zero polls). Clamping keeps both within safe bounds.
func TestRequestDeviceCode_ClampsOverflowValues(t *testing.T) {
	url := deviceCodeServerReturning(t,
		`{"device_code":"dc","user_code":"AAAA-BBBB","verification_uri":"https://github.com/login/device","expires_in":10000000000,"interval":10000000000}`)

	p := &GitHubATProvider{clientID: testClientID, deviceCodeURL: url, pollInterval: defaultPollInterval, expiresIn: defaultExpiresIn}
	_, _, _, err := p.requestDeviceCode(context.Background())
	require.NoError(t, err)

	assert.Equal(t, maxPollInterval, p.pollInterval)
	assert.Equal(t, maxExpiresIn, p.expiresIn)
}

// TestRequestDeviceCode_IgnoresNegativeValues confirms negative interval and
// expires_in are rejected in favour of the defaults, so neither can produce a
// negative sleep or a deadline already in the past.
func TestRequestDeviceCode_IgnoresNegativeValues(t *testing.T) {
	url := deviceCodeServerReturning(t,
		`{"device_code":"dc","user_code":"AAAA-BBBB","verification_uri":"https://github.com/login/device","expires_in":-1,"interval":-1}`)

	p := &GitHubATProvider{clientID: testClientID, deviceCodeURL: url, pollInterval: defaultPollInterval, expiresIn: defaultExpiresIn}
	_, _, _, err := p.requestDeviceCode(context.Background())
	require.NoError(t, err)

	assert.Equal(t, defaultPollInterval, p.pollInterval, "negative interval must fall back to the default")
	assert.Equal(t, defaultExpiresIn, p.expiresIn, "negative expires_in must fall back to the default")
}

// TestPollForToken_TimeoutSurfacesLastError covers F2: when the deadline lands
// mid-retry, the timeout must carry the last retryable diagnostic instead of a
// bare "timed out". The grace poll records the full-shape incorrect_device_code
// and its following sleep crosses the (1-second) deadline.
func TestPollForToken_TimeoutSurfacesLastError(t *testing.T) {
	gh := newSeqTokenServer(t, okResp(incorrectVerboseBody))
	p := &GitHubATProvider{
		clientID:       testClientID,
		accessTokenURL: gh.srv.URL,
		pollInterval:   1,
		expiresIn:      1, // deadline ~1s out
		sleep:          func(time.Duration) { time.Sleep(1100 * time.Millisecond) },
	}

	_, err := p.pollForToken(context.Background(), "issued-device-code")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.Contains(t, err.Error(), "The device_code provided is not valid.",
		"the last retryable diagnostic must survive the timeout")
}

// TestPollForToken_TimeoutAfterRecoveryIsBare confirms lastErr is cleared when
// polling recovers: one transient 502 followed by healthy pending polls and
// then a genuine user timeout must report plainly, not blame the stale 502.
func TestPollForToken_TimeoutAfterRecoveryIsBare(t *testing.T) {
	gh := newSeqTokenServer(t,
		serverErrorResp(),
		okResp(pendingBody),
	)
	p := &GitHubATProvider{
		clientID:       testClientID,
		accessTokenURL: gh.srv.URL,
		pollInterval:   1,
		expiresIn:      1, // deadline ~1s out
		sleep:          func(time.Duration) { time.Sleep(400 * time.Millisecond) },
	}

	_, err := p.pollForToken(context.Background(), "issued-device-code")
	require.Error(t, err)
	assert.Equal(t, "device code authorization timed out", err.Error(),
		"a recovered 502 must not be reported as the cause of a user timeout")
}

// TestPollForToken_NonOKStatusSurfacesStatusAndBody confirms a non-2xx below
// 500 (a 429 HTML page, say) reports its status and body rather than failing
// downstream on a JSON parse error.
func TestPollForToken_NonOKStatusSurfacesStatusAndBody(t *testing.T) {
	gh := newSeqTokenServer(t,
		seqResp{status: http.StatusTooManyRequests, body: "<html>rate limited</html>"},
	)
	p, _ := newPollTestProvider(gh.srv.URL)

	_, err := p.pollForToken(context.Background(), "issued-device-code")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
	assert.Contains(t, err.Error(), "rate limited")
	assert.NotContains(t, err.Error(), "invalid character",
		"must not surface as a JSON parse failure")
	assert.Equal(t, 1, gh.requests(), "a non-retryable status must stop polling")
}

// TestPollForToken_ConsecutiveServerErrorsBackOff confirms the 5xx wait grows by
// 5s per consecutive failure and is capped, so a sustained outage costs a
// bounded number of requests rather than hammering the endpoint.
func TestPollForToken_ConsecutiveServerErrorsBackOff(t *testing.T) {
	responses := make([]seqResp, 0, 4)
	for range [3]struct{}{} {
		responses = append(responses, serverErrorResp())
	}
	responses = append(responses, okResp(tokenBody))

	gh := newSeqTokenServer(t, responses...)
	p, slept := newPollTestProvider(gh.srv.URL)

	token, err := p.pollForToken(context.Background(), "issued-device-code")
	require.NoError(t, err)
	assert.Equal(t, "gho_test_token", token)

	// Base interval is 0 in the test provider, so the waits are 5, 10, 15.
	require.Len(t, *slept, 3)
	assert.Equal(t, 5*time.Second, (*slept)[0])
	assert.Equal(t, 10*time.Second, (*slept)[1])
	assert.Equal(t, 15*time.Second, (*slept)[2])
}

// TestPollForToken_ServerErrorBackOffResetsOnRecovery confirms the 5xx growth is
// reset by any non-5xx response, so an intermittent endpoint does not
// accumulate an ever-longer wait across separate blips.
func TestPollForToken_ServerErrorBackOffResetsOnRecovery(t *testing.T) {
	serverError := serverErrorResp()
	gh := newSeqTokenServer(t,
		serverError,
		okResp(pendingBody),
		serverError,
		okResp(tokenBody),
	)
	p, slept := newPollTestProvider(gh.srv.URL)

	token, err := p.pollForToken(context.Background(), "issued-device-code")
	require.NoError(t, err)
	assert.Equal(t, "gho_test_token", token)

	// 5s for the first 5xx, 0s for the pending poll (base interval), then 5s
	// again — not 10s — because the pending response reset the growth.
	require.Len(t, *slept, 3)
	assert.Equal(t, 5*time.Second, (*slept)[0])
	assert.Equal(t, 0*time.Second, (*slept)[1])
	assert.Equal(t, 5*time.Second, (*slept)[2])
}

// TestRequestDeviceCode_PacingDoesNotLeakAcrossCodes covers F4: interval and
// expires_in are bound to the code they were issued with, so a second request
// that omits them must fall back to defaults rather than inherit the first.
func TestRequestDeviceCode_PacingDoesNotLeakAcrossCodes(t *testing.T) {
	var call int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if call == 0 {
			_, _ = w.Write([]byte(`{"device_code":"dc1","user_code":"AAAA-BBBB","verification_uri":"https://github.com/login/device","expires_in":120,"interval":55}`))
		} else {
			_, _ = w.Write([]byte(`{"device_code":"dc2","user_code":"CCCC-DDDD","verification_uri":"https://github.com/login/device"}`))
		}
		call++
	}))
	t.Cleanup(srv.Close)

	p := &GitHubATProvider{clientID: testClientID, deviceCodeURL: srv.URL, pollInterval: defaultPollInterval, expiresIn: defaultExpiresIn}

	_, _, _, err := p.requestDeviceCode(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 55, p.pollInterval)
	assert.Equal(t, 120, p.expiresIn)

	_, _, _, err = p.requestDeviceCode(context.Background())
	require.NoError(t, err)
	assert.Equal(t, defaultPollInterval, p.pollInterval, "second code must not inherit the first's interval")
	assert.Equal(t, defaultExpiresIn, p.expiresIn, "second code must not inherit the first's expires_in")
}

// TestLogin_ClientIDStableAcrossDeviceCodeAndPolls covers the whole device flow
// end to end and pins the one invariant that makes incorrect_device_code
// meaningful: the health endpoint is read once, and the client_id the device
// code was issued under is the one every poll carries. The mock token endpoint
// answers incorrect_device_code for any other pairing.
func TestLogin_ClientIDStableAcrossDeviceCodeAndPolls(t *testing.T) {
	var mu sync.Mutex
	healthCalls := 0
	deviceClientID := ""
	pollClientIDs := []string{}
	pollCount := 0
	const issued = "dc-login"

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/health" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		healthCalls++
		id := fmt.Sprintf("Iv23liCLIENT%04d", healthCalls)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"status":"ok","github_client_id":"%s"}`, id)
	}))
	t.Cleanup(registry.Close)

	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		// assert, not require: t.FailNow from a handler goroutine is unsupported.
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
		mu.Lock()
		deviceClientID = req["client_id"]
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"device_code":"%s","user_code":"CCCC-DDDD","verification_uri":"https://github.com/login/device","expires_in":900,"interval":5}`, issued)
	}))
	t.Cleanup(device.Close)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		// assert, not require: t.FailNow from a handler goroutine is unsupported.
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
		mu.Lock()
		pollClientIDs = append(pollClientIDs, req["client_id"])
		bound := req["client_id"] == deviceClientID && req["device_code"] == issued
		pollCount++
		first := pollCount == 1
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case !bound:
			_, _ = w.Write([]byte(incorrectBody))
		case first:
			_, _ = w.Write([]byte(pendingBody))
		default:
			_, _ = w.Write([]byte(tokenBody))
		}
	}))
	t.Cleanup(tokenSrv.Close)

	p := &GitHubATProvider{
		registryURL:    registry.URL,
		deviceCodeURL:  device.URL,
		accessTokenURL: tokenSrv.URL,
		pollInterval:   defaultPollInterval,
		expiresIn:      defaultExpiresIn,
		sleep:          func(time.Duration) {},
	}

	require.NoError(t, p.Login(context.Background()))
	assert.Equal(t, "gho_test_token", p.githubToken)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, healthCalls, "Login must read the health endpoint exactly once")
	assert.Equal(t, "Iv23liCLIENT0001", deviceClientID)
	for _, id := range pollClientIDs {
		assert.Equal(t, deviceClientID, id,
			"every poll must carry the client_id the device code was issued under")
	}
}
