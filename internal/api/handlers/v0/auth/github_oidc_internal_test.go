package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testOIDCIssuer   = "https://token.actions.example"
	testOIDCAudience = "https://registry.test"
)

// fakeClock is a manually advanced clock for cache expiry tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type testSigningKey struct {
	kid string
	key *rsa.PrivateKey
}

func newTestSigningKey(t *testing.T, kid string) testSigningKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return testSigningKey{kid: kid, key: key}
}

func (k testSigningKey) jwk() JWK {
	return JWK{
		KTY: "RSA",
		KID: k.kid,
		Use: "sig",
		N:   base64.RawURLEncoding.EncodeToString(k.key.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.key.E)).Bytes()),
	}
}

// token signs a valid GitHub-shaped OIDC token with this key. The jwt
// expiry check uses the real clock, so the expiry is relative to time.Now.
func (k testSigningKey) token(t *testing.T) string {
	t.Helper()
	claims := GitHubOIDCClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testOIDCIssuer,
			Subject:   "repo:octo-org/octo-repo:ref:refs/heads/main",
			Audience:  jwt.ClaimStrings{testOIDCAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
		},
		RepositoryOwner: "octo-org",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = k.kid
	signed, err := tok.SignedString(k.key)
	require.NoError(t, err)
	return signed
}

// jwksServer serves a JWKS document that tests can change, and counts fetches.
type jwksServer struct {
	*httptest.Server
	fetches atomic.Int64

	mu    sync.Mutex
	keys  []JWK
	fail  bool
	block chan struct{}
}

func newJWKSServer(t *testing.T, keys ...testSigningKey) *jwksServer {
	t.Helper()
	s := &jwksServer{}
	s.setKeys(keys...)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.fetches.Add(1)
		s.mu.Lock()
		keys, fail, block := s.keys, s.fail, s.block
		s.mu.Unlock()
		if block != nil {
			select {
			case <-block:
			case <-r.Context().Done():
				return
			}
		}
		if fail {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(JWKS{Keys: keys})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *jwksServer) setKeys(keys ...testSigningKey) {
	jwks := make([]JWK, 0, len(keys))
	for _, k := range keys {
		jwks = append(jwks, k.jwk())
	}
	s.setRawKeys(jwks)
}

// setRawKeys serves exactly these entries, which may be malformed. nil is
// served as {"keys":null}.
func (s *jwksServer) setRawKeys(keys []JWK) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = keys
}

func (s *jwksServer) setFail(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = fail
}

// hold makes requests wait until the returned release func is called. Release
// is idempotent and also runs at cleanup, before the server is closed, so a
// failed assertion cannot leave a handler blocked.
func (s *jwksServer) hold(t *testing.T) (release func()) {
	t.Helper()
	ch := make(chan struct{})
	var once sync.Once
	release = func() {
		once.Do(func() {
			s.mu.Lock()
			s.block = nil
			s.mu.Unlock()
			close(ch)
		})
	}
	s.mu.Lock()
	s.block = ch
	s.mu.Unlock()
	t.Cleanup(release)
	return release
}

func newTestValidator(s *jwksServer, clock *fakeClock) *GitHubOIDCValidator {
	v := NewMockOIDCValidator(s.URL, testOIDCIssuer)
	v.now = clock.Now
	return v
}

func validate(v *GitHubOIDCValidator, token string) error {
	_, err := v.ValidateToken(context.Background(), token, testOIDCAudience)
	return err
}

func TestGitHubOIDCValidator_CachesJWKSAcrossExchanges(t *testing.T) {
	key := newTestSigningKey(t, "key-1")
	srv := newJWKSServer(t, key)
	v := newTestValidator(srv, newFakeClock())

	for range 5 {
		require.NoError(t, validate(v, key.token(t)))
	}
	assert.Equal(t, int64(1), srv.fetches.Load())
}

func TestGitHubOIDCValidator_UnknownKidRefetchIsRateLimited(t *testing.T) {
	key := newTestSigningKey(t, "key-1")
	unknown := newTestSigningKey(t, "not-published")
	srv := newJWKSServer(t, key)
	clock := newFakeClock()
	v := newTestValidator(srv, clock)

	require.NoError(t, validate(v, key.token(t)))
	require.Equal(t, int64(1), srv.fetches.Load())

	// Within the refresh interval an unknown kid must not cause a fetch.
	for range 10 {
		err := validate(v, unknown.token(t))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	}
	assert.Equal(t, int64(1), srv.fetches.Load())

	// Once the interval has passed, one unknown kid may trigger one refetch...
	clock.Advance(githubJWKSMinRefreshInterval)
	require.Error(t, validate(v, unknown.token(t)))
	assert.Equal(t, int64(2), srv.fetches.Load())

	// ...and the tokens after it are answered from the cache.
	for range 10 {
		require.Error(t, validate(v, unknown.token(t)))
	}
	assert.Equal(t, int64(2), srv.fetches.Load())

	// The known key still validates without a fetch.
	require.NoError(t, validate(v, key.token(t)))
	assert.Equal(t, int64(2), srv.fetches.Load())
}

func TestGitHubOIDCValidator_PicksUpRotatedKey(t *testing.T) {
	oldKey := newTestSigningKey(t, "key-old")
	newKey := newTestSigningKey(t, "key-new")
	srv := newJWKSServer(t, oldKey)
	clock := newFakeClock()
	v := newTestValidator(srv, clock)

	require.NoError(t, validate(v, oldKey.token(t)))

	srv.setKeys(newKey)
	clock.Advance(githubJWKSMinRefreshInterval)

	require.NoError(t, validate(v, newKey.token(t)))
	assert.Equal(t, int64(2), srv.fetches.Load())

	// The new key is now cached, and the retired one is gone.
	require.NoError(t, validate(v, newKey.token(t)))
	require.Error(t, validate(v, oldKey.token(t)))
	assert.Equal(t, int64(2), srv.fetches.Load())
}

func TestGitHubOIDCValidator_RefetchesAfterTTL(t *testing.T) {
	key := newTestSigningKey(t, "key-1")
	srv := newJWKSServer(t, key)
	clock := newFakeClock()
	v := newTestValidator(srv, clock)

	require.NoError(t, validate(v, key.token(t)))
	clock.Advance(githubJWKSCacheTTL - time.Second)
	require.NoError(t, validate(v, key.token(t)))
	assert.Equal(t, int64(1), srv.fetches.Load())

	clock.Advance(2 * time.Second)
	require.NoError(t, validate(v, key.token(t)))
	assert.Equal(t, int64(2), srv.fetches.Load())
}

func TestGitHubOIDCValidator_FetchFailureWithWarmCache(t *testing.T) {
	key := newTestSigningKey(t, "key-1")
	unknown := newTestSigningKey(t, "not-published")
	srv := newJWKSServer(t, key)
	clock := newFakeClock()
	v := newTestValidator(srv, clock)

	require.NoError(t, validate(v, key.token(t)))
	srv.setFail(true)
	clock.Advance(githubJWKSCacheTTL / 2)

	// Cached keys keep working while GitHub is unreachable.
	require.NoError(t, validate(v, key.token(t)))

	// A failed refetch for an unknown kid does not drop the cached keys.
	require.Error(t, validate(v, unknown.token(t)))
	assert.Equal(t, int64(2), srv.fetches.Load())
	require.NoError(t, validate(v, key.token(t)))
	assert.Equal(t, int64(2), srv.fetches.Load())
}

func TestGitHubOIDCValidator_FetchFailureFailsClosed(t *testing.T) {
	t.Run("cold cache", func(t *testing.T) {
		key := newTestSigningKey(t, "key-1")
		srv := newJWKSServer(t, key)
		srv.setFail(true)
		v := newTestValidator(srv, newFakeClock())

		err := validate(v, key.token(t))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "503")
	})

	t.Run("expired cache", func(t *testing.T) {
		key := newTestSigningKey(t, "key-1")
		srv := newJWKSServer(t, key)
		clock := newFakeClock()
		v := newTestValidator(srv, clock)

		require.NoError(t, validate(v, key.token(t)))
		srv.setFail(true)
		clock.Advance(githubJWKSCacheTTL + time.Second)

		require.Error(t, validate(v, key.token(t)))
		assert.Equal(t, int64(2), srv.fetches.Load())

		// Failed attempts are rate limited too, so an outage does not turn
		// every exchange into an outbound request.
		require.Error(t, validate(v, key.token(t)))
		assert.Equal(t, int64(2), srv.fetches.Load())

		// Once GitHub answers again the next attempt after the interval succeeds.
		srv.setFail(false)
		clock.Advance(githubJWKSMinRefreshInterval)
		require.NoError(t, validate(v, key.token(t)))
		assert.Equal(t, int64(3), srv.fetches.Load())
	})
}

func TestGitHubOIDCValidator_ConcurrentExchangesShareOneFetch(t *testing.T) {
	key := newTestSigningKey(t, "key-1")
	srv := newJWKSServer(t, key)
	release := srv.hold(t)
	v := newTestValidator(srv, newFakeClock())

	const n = 20
	tokens := make([]string, n)
	for i := range tokens {
		tokens[i] = key.token(t)
	}

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- validate(v, tokens[i])
		}()
	}
	// Give the goroutines time to pile up behind the first fetch.
	time.Sleep(50 * time.Millisecond)
	release()
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int64(1), srv.fetches.Load())
}

func TestGitHubOIDCValidator_CancelledCallerDoesNotAbortFetch(t *testing.T) {
	key := newTestSigningKey(t, "key-1")
	srv := newJWKSServer(t, key)
	release := srv.hold(t)
	v := newTestValidator(srv, newFakeClock())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := v.ValidateToken(ctx, key.token(t), testOIDCAudience)
		done <- err
	}()

	require.Eventually(t, func() bool { return srv.fetches.Load() == 1 }, time.Second, 5*time.Millisecond)
	cancel()
	err := <-done
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)

	// The fetch the cancelled caller started still fills the cache, so a
	// client that hangs up cannot leave the next caller throttled.
	release()
	require.NoError(t, validate(v, key.token(t)))
	assert.Equal(t, int64(1), srv.fetches.Load())
}

func TestGitHubOIDCValidator_SkipsUnusableKeys(t *testing.T) {
	good := newTestSigningKey(t, "good")
	ecKid := newTestSigningKey(t, "ec-key")
	badN := newTestSigningKey(t, "bad-n")
	noKid := newTestSigningKey(t, "")

	badNJWK := badN.jwk()
	badNJWK.N = "!!not-base64!!"
	srv := newJWKSServer(t)
	srv.setRawKeys([]JWK{
		good.jwk(),
		{KTY: "EC", KID: "ec-key", Use: "sig"},
		badNJWK,
		noKid.jwk(),
	})
	v := newTestValidator(srv, newFakeClock())

	require.NoError(t, validate(v, good.token(t)))
	for _, k := range []testSigningKey{ecKid, badN, noKid} {
		err := validate(v, k.token(t))
		require.Error(t, err, "kid %q", k.kid)
		assert.Contains(t, err.Error(), "not found", "kid %q", k.kid)
	}
	assert.Equal(t, int64(1), srv.fetches.Load())
}

func TestGitHubOIDCValidator_DuplicateKidFirstEntryWins(t *testing.T) {
	a := newTestSigningKey(t, "dup")
	b := newTestSigningKey(t, "dup")
	broken := a.jwk()
	broken.N = "!!not-base64!!"

	tests := []struct {
		name     string
		keys     []JWK
		accepted []testSigningKey
		rejected []testSigningKey
	}{
		{name: "a then b", keys: []JWK{a.jwk(), b.jwk()}, accepted: []testSigningKey{a}, rejected: []testSigningKey{b}},
		{name: "b then a", keys: []JWK{b.jwk(), a.jwk()}, accepted: []testSigningKey{b}, rejected: []testSigningKey{a}},
		{name: "unusable first entry is not replaced", keys: []JWK{broken, b.jwk()}, rejected: []testSigningKey{a, b}},
		{name: "unusable later entry is ignored", keys: []JWK{a.jwk(), broken}, accepted: []testSigningKey{a}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The good key keeps the document from being empty.
			other := newTestSigningKey(t, "other")
			srv := newJWKSServer(t)
			srv.setRawKeys(append(tt.keys, other.jwk()))
			v := newTestValidator(srv, newFakeClock())

			for _, k := range tt.accepted {
				require.NoError(t, validate(v, k.token(t)))
			}
			for _, k := range tt.rejected {
				require.Error(t, validate(v, k.token(t)))
			}
			assert.Equal(t, int64(1), srv.fetches.Load())
		})
	}
}

func TestGitHubOIDCValidator_NoUsableKeysIsAFailedFetch(t *testing.T) {
	realN := newTestSigningKey(t, "n-source").jwk().N
	emptyDocs := map[string][]JWK{
		"null keys":  nil,
		"empty keys": {},
		"only unusable keys": {
			{KTY: "EC", KID: "ec-key", Use: "sig"},
			{KTY: "RSA", KID: "bad-n", N: "!!not-base64!!", E: "AQAB"},
		},
		"only key missing n":   {{KTY: "RSA", KID: "no-n", E: "AQAB"}},
		"only key missing e":   {{KTY: "RSA", KID: "no-e", N: realN}},
		"only key with tiny n": {{KTY: "RSA", KID: "tiny-n", N: "Aw", E: "AQAB"}},
		"only key with even e": {{KTY: "RSA", KID: "even-e", N: realN, E: "Ag"}},
		// 2^64 + 65537, which would truncate to 65537 in an int.
		"only key with oversized e": {{KTY: "RSA", KID: "big-e", N: realN, E: "AQAAAAAAAQAB"}},
	}

	for name, doc := range emptyDocs {
		t.Run(name+" with warm cache", func(t *testing.T) {
			key := newTestSigningKey(t, "key-1")
			unknown := newTestSigningKey(t, "not-published")
			srv := newJWKSServer(t, key)
			clock := newFakeClock()
			v := newTestValidator(srv, clock)

			require.NoError(t, validate(v, key.token(t)))
			srv.setRawKeys(doc)
			clock.Advance(githubJWKSMinRefreshInterval)

			err := validate(v, unknown.token(t))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no usable RSA keys")
			assert.Equal(t, int64(2), srv.fetches.Load())

			// The cached key survives until its TTL, and the failed fetch
			// still counts towards the refresh limit.
			require.NoError(t, validate(v, key.token(t)))
			require.Error(t, validate(v, unknown.token(t)))
			assert.Equal(t, int64(2), srv.fetches.Load())

			clock.Advance(githubJWKSCacheTTL - githubJWKSMinRefreshInterval - time.Second)
			require.NoError(t, validate(v, key.token(t)))
			assert.Equal(t, int64(2), srv.fetches.Load())

			clock.Advance(2 * time.Second)
			require.Error(t, validate(v, key.token(t)))
			assert.Equal(t, int64(3), srv.fetches.Load())
		})

		t.Run(name+" with cold cache", func(t *testing.T) {
			key := newTestSigningKey(t, "key-1")
			srv := newJWKSServer(t)
			srv.setRawKeys(doc)
			v := newTestValidator(srv, newFakeClock())

			err := validate(v, key.token(t))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no usable RSA keys")
		})
	}
}

func TestNewGitHubOIDCValidator_ClientHasTimeout(t *testing.T) {
	v := NewGitHubOIDCValidator()
	require.NotNil(t, v.client)
	assert.Equal(t, githubJWKSFetchTimeout, v.client.Timeout)
	assert.Positive(t, v.client.Timeout)
}
