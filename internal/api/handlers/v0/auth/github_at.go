package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// GitHubTokenExchangeInput represents the input for GitHub token exchange
type GitHubTokenExchangeInput struct {
	Body struct {
		GitHubToken string `json:"github_token" doc:"GitHub OAuth token" required:"true"`
	}
}

// GitHubHandler handles GitHub authentication
type GitHubHandler struct {
	config     *config.Config
	jwtManager *auth.JWTManager
	baseURL    string // Configurable for testing
}

// NewGitHubHandler creates a new GitHub handler
func NewGitHubHandler(cfg *config.Config) *GitHubHandler {
	return &GitHubHandler{
		config:     cfg,
		jwtManager: auth.NewJWTManager(cfg),
		baseURL:    "https://api.github.com",
	}
}

// SetBaseURL sets the base URL for GitHub API (used for testing)
func (h *GitHubHandler) SetBaseURL(url string) {
	h.baseURL = url
}

// RegisterGitHubATEndpoint registers the GitHub access token authentication endpoint with a custom path prefix
func RegisterGitHubATEndpoint(api huma.API, pathPrefix string, cfg *config.Config) {
	handler := NewGitHubHandler(cfg)

	// GitHub token exchange endpoint
	huma.Register(api, huma.Operation{
		OperationID: "exchange-github-token" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/auth/github-at",
		Summary:     "Exchange GitHub OAuth access token for Registry JWT",
		Description: "Exchange a GitHub OAuth access token for a short-lived Registry JWT token",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, input *GitHubTokenExchangeInput) (*v0.Response[auth.TokenResponse], error) {
		response, err := handler.ExchangeToken(ctx, input.Body.GitHubToken)
		if err != nil {
			return nil, tokenExchangeError(err)
		}

		return &v0.Response[auth.TokenResponse]{
			Body: *response,
		}, nil
	})
}

// tokenExchangeError maps an internal ExchangeToken failure to a client-facing
// 401 without leaking internal detail. huma serializes extra error args into the
// response body, and the wrapped err here can include the raw upstream GitHub
// response body captured by readErrorBody — so passing it to huma would echo that
// detail back to the caller (CWE-209). We log it server-side (like the sibling
// handlers in servers.go) and return only a generic message.
func tokenExchangeError(err error) error {
	log.Printf("github-at token exchange failed: %v", err)
	return huma.Error401Unauthorized("Token exchange failed")
}

// ExchangeToken exchanges a GitHub OAuth token for a Registry JWT token
func (h *GitHubHandler) ExchangeToken(ctx context.Context, githubToken string) (*auth.TokenResponse, error) {
	// Get GitHub user information
	user, err := h.getGitHubUser(ctx, githubToken)
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub user: %w", err)
	}

	// Get the organizations the user administers. Org namespaces are only granted
	// to org Owners (membership role "admin"), not to ordinary members.
	orgs, err := h.getGitHubAdminOrgs(ctx, githubToken)
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub organizations: %w", err)
	}

	// Build permissions based on user and organizations
	permissions := h.buildPermissions(user.Login, orgs)

	// Create JWT claims with GitHub user info
	claims := auth.JWTClaims{
		AuthMethod:        auth.MethodGitHubAT,
		AuthMethodSubject: user.Login,
		Permissions:       permissions,
	}

	// Generate Registry JWT token
	tokenResponse, err := h.jwtManager.GenerateTokenResponse(ctx, claims)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JWT token: %w", err)
	}

	return tokenResponse, nil
}

type GitHubUserOrOrg struct {
	Login string `json:"login"`
	ID    int    `json:"id"`
}

// getGitHubUser gets the authenticated user's information
func (h *GitHubHandler) getGitHubUser(ctx context.Context, token string) (*GitHubUserOrOrg, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/user", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	var user GitHubUserOrOrg
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}

	return &user, nil
}

// githubOrgRoleAdmin is the membership role GitHub returns for an organization
// Owner. It is the only role we treat as carrying publish authority for the org
// namespace. GET /user/memberships/orgs reports the caller's role as either
// "admin" (Owner) or "member" (GitHub has no org-level "maintainer" role); only
// "admin" carries publish authority, so a "member" is intentionally not granted.
const githubOrgRoleAdmin = "admin"

// githubMembershipStateActive is the membership state for an accepted (as opposed
// to merely invited/pending) org membership. We only honor active memberships so a
// not-yet-accepted owner invitation never grants the org namespace.
const githubMembershipStateActive = "active"

// orgMembershipsPageSize is the page size we request when listing the user's org
// memberships. 100 is GitHub's maximum.
const orgMembershipsPageSize = 100

// maxOrgMembershipPages bounds the pagination loop. At orgMembershipsPageSize=100
// this covers 10,000 org memberships — far beyond any real user — and exists only
// as a backstop so a misbehaving or redirected upstream cannot drive an unbounded
// number of requests.
const maxOrgMembershipPages = 100

// githubOrgMembership is one entry from GET /user/memberships/orgs: the
// authenticated user's membership in a single organization, including their role.
type githubOrgMembership struct {
	State        string          `json:"state"`
	Role         string          `json:"role"`
	Organization GitHubUserOrOrg `json:"organization"`
}

// getGitHubAdminOrgs returns the organizations in which the authenticated user is
// an Owner (membership role "admin").
//
// This deliberately uses GET /user/memberships/orgs rather than
// GET /users/{username}/orgs. The latter only returns *public* memberships and
// carries no role, so it cannot distinguish an Owner from an ordinary member —
// historically every org a user belonged to was granted publish access to its
// namespace regardless of the user's role. The memberships endpoint returns the
// caller's role per org (and includes private memberships), letting us grant the
// org namespace only to people who actually administer the org.
//
// The endpoint requires the read:org scope. A minimal token used only for
// personal-namespace publishing won't have it and will get a 403 here; that is
// treated as "no admin orgs" (see fetchOrgMembershipsPage) so personal publishing
// keeps working without asking users to over-scope their token.
func (h *GitHubHandler) getGitHubAdminOrgs(ctx context.Context, token string) ([]GitHubUserOrOrg, error) {
	var adminOrgs []GitHubUserOrOrg

	for page := 1; page <= maxOrgMembershipPages; page++ {
		memberships, err := h.fetchOrgMembershipsPage(ctx, token, page)
		if err != nil {
			return nil, err
		}

		for _, m := range memberships {
			// state=active is already requested in the query string, but re-check it
			// here as defense in depth: if that param is ever dropped or changed, this
			// keeps a pending (not-yet-accepted) owner invitation from being granted
			// the org namespace.
			if m.State == githubMembershipStateActive && m.Role == githubOrgRoleAdmin {
				adminOrgs = append(adminOrgs, m.Organization)
			}
		}

		// A short page is the last page: return the complete result.
		if len(memberships) < orgMembershipsPageSize {
			return adminOrgs, nil
		}
	}

	// Every page up to the cap was full. A real user never has this many org
	// memberships, so rather than return a possibly-truncated set (which would
	// silently strip a legitimate Owner's grant), fail closed.
	return nil, fmt.Errorf("org memberships exceeded %d pages; refusing to issue a possibly-truncated grant", maxOrgMembershipPages)
}

// fetchOrgMembershipsPage fetches a single page of the authenticated user's
// active organization memberships.
func (h *GitHubHandler) fetchOrgMembershipsPage(ctx context.Context, token string, page int) ([]githubOrgMembership, error) {
	url := fmt.Sprintf("%s/user/memberships/orgs?state=active&per_page=%d&page=%d", h.baseURL, orgMembershipsPageSize, page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user organization memberships: %w", err)
	}
	defer resp.Body.Close()

	// GitHub returns 403 here for two very different reasons, which we must not
	// conflate:
	//  1. The token lacks the read:org scope. This is the common, benign case for a
	//     minimal personal-publishing token: GET /user still works, but org
	//     memberships are forbidden. We degrade gracefully to "no admin orgs" so
	//     personal-namespace publishing keeps working without over-scoping.
	//  2. Rate limiting. GitHub signals primary/secondary rate limits with 403 (or
	//     429), setting X-RateLimit-Remaining: 0 and/or Retry-After. Degrading here
	//     would silently strip a legitimate Owner's org grant, so we fail closed
	//     (return an error) rather than mistake a throttle for "not an admin".
	if resp.StatusCode == http.StatusForbidden {
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "" {
			return nil, fmt.Errorf("GitHub API rate limit exceeded while listing org memberships (status 403): %s", readErrorBody(resp.Body))
		}
		// Best-effort defense: GitHub uses the X-GitHub-SSO header to signal that a
		// resource is behind SAML/SSO the token hasn't been authorized for. If we see
		// it, treat that as an Owner being blocked (not a missing scope) and fail
		// closed rather than silently degrading them to personal-only. NOTE: we have
		// not confirmed that the memberships *list* endpoint emits this header on SSO
		// enforcement (it may instead just omit the SSO-protected org); this check is
		// safe either way — if the header is absent we fall through to the same
		// graceful degrade as before — but it is not a proven guarantee that every
		// SSO-blocked Owner is caught here.
		if resp.Header.Get("X-GitHub-SSO") != "" {
			return nil, fmt.Errorf("GitHub org memberships require SSO authorization for this token (status 403): %s", readErrorBody(resp.Body))
		}
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	var memberships []githubOrgMembership
	if err := json.NewDecoder(resp.Body).Decode(&memberships); err != nil {
		return nil, fmt.Errorf("failed to decode organization memberships response: %w", err)
	}

	return memberships, nil
}

// buildPermissions builds permissions based on GitHub user and their organizations
func (h *GitHubHandler) buildPermissions(username string, orgs []GitHubUserOrOrg) []auth.Permission {
	// Assert the username matches the expected regex, to harden against people doing
	// weird things in names. The username is the caller's own identity, so if it is
	// invalid we grant nothing.
	if !isValidGitHubName(username) {
		return nil
	}

	// Add permission for user's own namespace
	permissions := []auth.Permission{{
		Action:          auth.PermissionActionPublish,
		ResourcePattern: fmt.Sprintf("io.github.%s/*", username),
	}}

	// Add permissions for each organization the user administers (Owner role). Skip
	// any org whose name fails validation rather than rejecting the whole set, so one
	// unexpected org name can't strip the caller's personal namespace or their other
	// (valid) org grants.
	for _, org := range orgs {
		if !isValidGitHubName(org.Login) {
			continue
		}
		permissions = append(permissions, auth.Permission{
			Action:          auth.PermissionActionPublish,
			ResourcePattern: fmt.Sprintf("io.github.%s/*", org.Login),
		})
	}

	return permissions
}

func isValidGitHubName(name string) bool {
	return regexp.MustCompile(`^[a-zA-Z0-9-]+$`).MatchString(name)
}
