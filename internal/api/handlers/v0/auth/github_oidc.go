package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/golang-jwt/jwt/v5"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// GitHubOIDCTokenExchangeInput represents the input for GitHub OIDC token exchange
type GitHubOIDCTokenExchangeInput struct {
	Body struct {
		OIDCToken string `json:"oidc_token" doc:"GitHub Actions OIDC token" required:"true"`
	}
}

// GitHubOIDCClaims represents the claims we need from a GitHub OIDC token
type GitHubOIDCClaims struct {
	jwt.RegisteredClaims
	RepositoryOwner string `json:"repository_owner"` // e.g., "octo-org"
}

// JWKS represents a JSON Web Key Set
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a JSON Web Key
type JWK struct {
	KTY string `json:"kty"`
	KID string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// OIDCValidator defines the interface for OIDC token validation
type OIDCValidator interface {
	ValidateToken(ctx context.Context, token string, audience string) (*GitHubOIDCClaims, error)
}

// GitHubOIDCValidator validates GitHub OIDC tokens. It caches GitHub's
// signing keys, so it is safe for concurrent use and should be shared.
type GitHubOIDCValidator struct {
	jwksURL string
	issuer  string
	client  *http.Client
	now     func() time.Time

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	expiresAt time.Time
	// lastFetch is when the last fetch started, successful or not.
	lastFetch time.Time
	inflight  *jwksFetch
}

// jwksFetch is a JWKS fetch in progress. Callers that need the result wait
// for done; err is set before done is closed.
type jwksFetch struct {
	done chan struct{}
	err  error
}

// githubJWKSCacheTTL is how long fetched keys are trusted. It matches the
// "Cache-Control: max-age=3600" GitHub sends with the JWKS document. After it
// passes the keys are not used until a fetch succeeds, so a long GitHub outage
// fails closed rather than serving old keys forever.
const githubJWKSCacheTTL = time.Hour

// githubJWKSMinRefreshInterval is the minimum gap between fetch attempts. The
// fetch happens before the token's signature is checked, so without this
// limit any token with an unknown kid would cost one request to GitHub. It
// also bounds the delay before a newly rotated key is picked up.
const githubJWKSMinRefreshInterval = 30 * time.Second

// githubJWKSFetchTimeout bounds a single JWKS fetch.
const githubJWKSFetchTimeout = 10 * time.Second

// NewGitHubOIDCValidator creates a new GitHub OIDC validator
func NewGitHubOIDCValidator() *GitHubOIDCValidator {
	return newGitHubOIDCValidator(
		"https://token.actions.githubusercontent.com/.well-known/jwks",
		"https://token.actions.githubusercontent.com",
	)
}

// NewMockOIDCValidator creates a mock validator for testing
func NewMockOIDCValidator(jwksURL, issuer string) *GitHubOIDCValidator {
	return newGitHubOIDCValidator(jwksURL, issuer)
}

func newGitHubOIDCValidator(jwksURL, issuer string) *GitHubOIDCValidator {
	return &GitHubOIDCValidator{
		jwksURL: jwksURL,
		issuer:  issuer,
		client:  &http.Client{Timeout: githubJWKSFetchTimeout},
		now:     time.Now,
	}
}

// ValidateToken validates a GitHub OIDC token
func (v *GitHubOIDCValidator) ValidateToken(ctx context.Context, tokenString string, audience string) (*GitHubOIDCClaims, error) {
	// Parse token to get header for key ID
	token, err := jwt.ParseWithClaims(
		tokenString,
		&GitHubOIDCClaims{},
		func(token *jwt.Token) (any, error) {
			// Get key ID from header
			kid, ok := token.Header["kid"].(string)
			if !ok {
				return nil, fmt.Errorf("missing kid in token header")
			}

			// Find matching public key
			publicKey, err := v.getPublicKey(ctx, kid)
			if err != nil {
				return nil, fmt.Errorf("failed to get public key: %w", err)
			}

			return publicKey, nil
		},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithExpirationRequired(),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Extract and validate claims
	claims, ok := token.Claims.(*GitHubOIDCClaims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Validate issuer
	if claims.Issuer != v.issuer {
		return nil, fmt.Errorf("invalid issuer: expected %s, got %s", v.issuer, claims.Issuer)
	}

	// Validate audience
	foundAudience := false
	for _, aud := range claims.Audience {
		if aud == audience {
			foundAudience = true
			break
		}
	}
	if !foundAudience {
		return nil, fmt.Errorf("invalid audience: expected %s, got %v", audience, claims.Audience)
	}

	// Validate repository format
	if claims.RepositoryOwner == "" {
		return nil, fmt.Errorf("repository owner claim is required")
	}

	return claims, nil
}

// fetchJWKS fetches the JSON Web Key Set from GitHub
func (v *GitHubOIDCValidator) fetchJWKS(ctx context.Context) (*JWKS, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %w", err)
	}

	return &jwks, nil
}

// getPublicKey returns the RSA public key for the given key ID. Keys come from
// the cache while it is fresh. An unknown kid or an expired cache starts a
// fetch, at most once per githubJWKSMinRefreshInterval; concurrent callers
// share the fetch in progress.
func (v *GitHubOIDCValidator) getPublicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	now := v.now()
	fresh := now.Before(v.expiresAt)
	if key, ok := v.keys[kid]; ok && fresh {
		v.mu.Unlock()
		return key, nil
	}

	call := v.inflight
	if call == nil {
		if !v.lastFetch.IsZero() && now.Sub(v.lastFetch) < githubJWKSMinRefreshInterval {
			v.mu.Unlock()
			if fresh {
				return nil, fmt.Errorf("key with ID %s not found", kid)
			}
			return nil, fmt.Errorf("JWKS unavailable: last fetch failed less than %s ago", githubJWKSMinRefreshInterval)
		}
		v.lastFetch = now
		call = &jwksFetch{done: make(chan struct{})}
		v.inflight = call
		// The fetch must not be cut short by this caller hanging up, or a
		// client could cancel every fetch and keep the cache empty while the
		// refresh limit blocks everyone else. It runs detached, bounded by
		// its own timeout, and this caller stops waiting if ctx ends.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), githubJWKSFetchTimeout)
		go func() {
			defer cancel()
			v.refreshKeys(fetchCtx, call)
		}()
	}
	v.mu.Unlock()

	select {
	case <-call.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if call.err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", call.err)
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	if key, ok := v.keys[kid]; ok && v.now().Before(v.expiresAt) {
		return key, nil
	}
	return nil, fmt.Errorf("key with ID %s not found", kid)
}

// refreshKeys fetches the JWKS and, on success, replaces the cached keys. On
// failure the cached keys are kept and stay usable until they expire.
func (v *GitHubOIDCValidator) refreshKeys(ctx context.Context, call *jwksFetch) {
	var keys map[string]*rsa.PublicKey
	jwks, err := v.fetchJWKS(ctx)
	if err == nil {
		keys = v.usableKeys(jwks)
		if len(keys) == 0 {
			// Don't let an empty or broken document evict good keys.
			err = fmt.Errorf("JWKS contains no usable RSA keys")
		}
	}

	v.mu.Lock()
	if err == nil {
		v.keys = keys
		v.expiresAt = v.now().Add(githubJWKSCacheTTL)
	}
	call.err = err
	v.inflight = nil
	v.mu.Unlock()
	close(call.done)
}

// usableKeys returns the RSA keys in jwks by kid. Only the first entry for a
// kid counts, as it did before keys were cached: if that entry is not a
// usable RSA key, the kid is left out even when a later entry would parse.
// Entries without a kid are skipped.
func (v *GitHubOIDCValidator) usableKeys(jwks *JWKS) map[string]*rsa.PublicKey {
	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	seen := make(map[string]bool, len(jwks.Keys))
	for _, jwk := range jwks.Keys {
		if jwk.KID == "" || seen[jwk.KID] {
			continue
		}
		seen[jwk.KID] = true
		key, err := v.parseRSAPublicKey(jwk)
		if err != nil {
			continue
		}
		keys[jwk.KID] = key
	}
	return keys
}

// parseRSAPublicKey converts JWK to RSA public key
func (v *GitHubOIDCValidator) parseRSAPublicKey(jwk JWK) (*rsa.PublicKey, error) {
	if jwk.KTY != "RSA" {
		return nil, fmt.Errorf("invalid key type: expected RSA, got %s", jwk.KTY)
	}

	// Decode modulus (n)
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode modulus: %w", err)
	}

	// Decode exponent (e)
	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode exponent: %w", err)
	}

	// Convert to big integers
	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	// Reject keys that rsa.VerifyPKCS1v15 would refuse, so a missing or
	// broken n or e cannot be cached in place of keys that still work.
	// The 31-bit limit also keeps the int conversion below from truncating.
	if n.BitLen() < 1024 || n.Bit(0) == 0 {
		return nil, fmt.Errorf("invalid modulus")
	}
	if e.BitLen() > 31 || e.Int64() < 3 || e.Bit(0) == 0 {
		return nil, fmt.Errorf("invalid exponent")
	}

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}

// GitHubOIDCHandler handles GitHub OIDC authentication
type GitHubOIDCHandler struct {
	config     *config.Config
	jwtManager *auth.JWTManager
	validator  OIDCValidator
}

// NewGitHubOIDCHandler creates a new GitHub OIDC handler
func NewGitHubOIDCHandler(cfg *config.Config) *GitHubOIDCHandler {
	return &GitHubOIDCHandler{
		config:     cfg,
		jwtManager: auth.NewJWTManager(cfg),
		validator:  NewGitHubOIDCValidator(),
	}
}

// SetValidator sets a custom OIDC validator (used for testing)
func (h *GitHubOIDCHandler) SetValidator(validator OIDCValidator) {
	h.validator = validator
}

// RegisterGitHubOIDCEndpoint registers the GitHub OIDC authentication endpoint
func RegisterGitHubOIDCEndpoint(api huma.API, pathPrefix string, cfg *config.Config) {
	handler := NewGitHubOIDCHandler(cfg)

	// GitHub OIDC token exchange endpoint
	huma.Register(api, huma.Operation{
		OperationID: "exchange-github-oidc-token" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/auth/github-oidc",
		Summary:     "Exchange GitHub OIDC token for Registry JWT",
		Description: "Exchange a GitHub Actions OIDC token for a short-lived Registry JWT token",
		Tags:        []string{tagAuth},
	}, func(ctx context.Context, input *GitHubOIDCTokenExchangeInput) (*v0.Response[auth.TokenResponse], error) {
		response, err := handler.ExchangeToken(ctx, input.Body.OIDCToken)
		if err != nil {
			return nil, huma.Error401Unauthorized("Token exchange failed", err)
		}

		return &v0.Response[auth.TokenResponse]{
			Body: *response,
		}, nil
	})
}

// ExchangeToken exchanges a GitHub OIDC token for a Registry JWT token
func (h *GitHubOIDCHandler) ExchangeToken(ctx context.Context, oidcToken string) (*auth.TokenResponse, error) {
	if h.config.GitHubOIDCAudience == "" {
		return nil, fmt.Errorf("GitHub OIDC exchange disabled: this deployment has no audience configured")
	}
	claims, err := h.validator.ValidateToken(ctx, oidcToken, h.config.GitHubOIDCAudience)
	if err != nil {
		return nil, fmt.Errorf("failed to validate OIDC token: %w", err)
	}

	// Extract repository information and build permissions
	permissions := h.buildPermissions(claims)

	// Create JWT claims with GitHub OIDC info
	jwtClaims := auth.JWTClaims{
		AuthMethod:        auth.MethodGitHubOIDC,
		AuthMethodSubject: claims.Subject, // e.g. "repo:octo-org/octo-repo:environment:prod"
		Permissions:       permissions,
	}

	// Generate Registry JWT token
	tokenResponse, err := h.jwtManager.GenerateTokenResponse(ctx, jwtClaims)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JWT token: %w", err)
	}

	return tokenResponse, nil
}

func (h *GitHubOIDCHandler) buildPermissions(claims *GitHubOIDCClaims) []auth.Permission {
	permissions := []auth.Permission{}

	// Validate repository owner name
	if !isValidGitHubName(claims.RepositoryOwner) {
		return nil
	}

	// Grant publish permissions for the repository owner's namespace
	// We grant io.github.<owner>/* rather than io.github./repo/* because many people have monorepo setups where they want to deploy multiple servers from
	// This also reflects GitHub's permission model, in that GitHub Actions can push to any GitHub package in the repository owner's namespace (e.g. for GHCR)
	permissions = append(permissions, auth.Permission{
		Action:          auth.PermissionActionPublish,
		ResourcePattern: fmt.Sprintf("io.github.%s/*", claims.RepositoryOwner),
	})

	return permissions
}
