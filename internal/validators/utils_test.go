package validators_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestIsValidRepositoryURL(t *testing.T) {
	tests := []struct {
		name   string
		source validators.RepositorySource
		url    string
		want   bool
	}{
		// GitHub
		{"github valid owner/repo", validators.SourceGitHub, "https://github.com/owner/repo", true},
		{"github valid with trailing slash", validators.SourceGitHub, "https://github.com/owner/repo/", true},
		{"github valid with www", validators.SourceGitHub, "https://www.github.com/owner/repo", true},
		{"github missing repo", validators.SourceGitHub, "https://github.com/owner", false},
		{"github extra path segment stays invalid", validators.SourceGitHub, "https://github.com/owner/repo/extra", false},

		// GitLab: flat owner/repo (no regression)
		{"gitlab valid owner/repo", validators.SourceGitLab, "https://gitlab.com/owner/repo", true},
		{"gitlab valid with trailing slash", validators.SourceGitLab, "https://gitlab.com/owner/repo/", true},
		{"gitlab valid with www", validators.SourceGitLab, "https://www.gitlab.com/owner/repo", true},
		{"gitlab valid with dots and dashes", validators.SourceGitLab, "https://gitlab.com/my-org.io/my_repo-v2.0", true},

		// GitLab: nested groups/subgroups (issue #1359)
		{"gitlab valid single subgroup", validators.SourceGitLab, "https://gitlab.com/group/subgroup/repo", true},
		{"gitlab valid deeply nested subgroups", validators.SourceGitLab, "https://gitlab.com/myorg/team/subgroup/my-mcp-server", true},
		{"gitlab valid nested with trailing slash", validators.SourceGitLab, "https://gitlab.com/group/subgroup/repo/", true},

		// GitLab: malformed URLs that must stay rejected
		{"gitlab missing owner and repo", validators.SourceGitLab, "https://gitlab.com", false},
		{"gitlab missing repo", validators.SourceGitLab, "https://gitlab.com/owner", false},
		{"gitlab empty path segment", validators.SourceGitLab, "https://gitlab.com/group//repo", false},
		{"gitlab leading empty segment", validators.SourceGitLab, "https://gitlab.com//group/repo", false},
		{"gitlab segment with space", validators.SourceGitLab, "https://gitlab.com/group/sub group/repo", false},
		{"gitlab spoofed host suffix", validators.SourceGitLab, "https://evilgitlab.com/group/repo", false},
		{"gitlab spoofed host prefix", validators.SourceGitLab, "https://gitlab.com.evil.com/group/repo", false},
		{"gitlab query string", validators.SourceGitLab, "https://gitlab.com/group/subgroup/repo?ref=main", false},
		{"gitlab fragment", validators.SourceGitLab, "https://gitlab.com/group/subgroup/repo#readme", false},
		{"gitlab route separator into tree", validators.SourceGitLab, "https://gitlab.com/group/repo/-/tree/main", false},
		{"gitlab route separator into issues", validators.SourceGitLab, "https://gitlab.com/group/subgroup/repo/-/issues", false},
		{"gitlab trailing route separator", validators.SourceGitLab, "https://gitlab.com/group/repo/-", false},
		{"gitlab segment starting with dash stays valid", validators.SourceGitLab, "https://gitlab.com/group/-repo", true},

		// Unknown source
		{"unknown source", validators.RepositorySource("bitbucket"), "https://bitbucket.org/owner/repo", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validators.IsValidRepositoryURL(tc.source, tc.url)
			assert.Equal(t, tc.want, got, "url: %s", tc.url)
		})
	}
}

// TestIsValidRemoteURL_LoopbackAndPrivateAddresses exercises IsValidRemoteURL
// directly against every bypass listed in the GitHub issue: the previous
// implementation only rejected the literal strings "localhost", "127.0.0.1",
// and "*.localhost", so IPv6 loopback, the rest of 127.0.0.0/8, unspecified
// addresses, IPv4-mapped loopback, and RFC1918/link-local addresses all
// passed validation despite the "no localhost allowed" contract.
func TestIsValidRemoteURL_LoopbackAndPrivateAddresses(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		valid bool
	}{
		// Previously-caught cases - must keep working
		{name: "literal localhost", url: "https://localhost/", valid: false},
		{name: "literal localhost with port", url: "https://localhost:8443/mcp", valid: false},
		{name: "localhost subdomain", url: "https://foo.localhost/", valid: false},
		{name: "literal 127.0.0.1", url: "https://127.0.0.1/", valid: false},

		// Previously-missed bypasses called out in the issue
		{name: "IPv6 loopback", url: "https://[::1]/", valid: false},
		{name: "rest of 127.0.0.0/8", url: "https://127.0.0.2/", valid: false},
		{name: "127.0.0.0/8 upper range", url: "https://127.255.255.254/", valid: false},
		{name: "IPv4 unspecified", url: "https://0.0.0.0/", valid: false},
		{name: "IPv6 unspecified", url: "https://[::]/", valid: false},
		{name: "IPv4-mapped IPv6 loopback", url: "https://[::ffff:127.0.0.1]/", valid: false},
		{name: "RFC1918 10.0.0.0/8", url: "https://10.0.0.1/", valid: false},
		{name: "RFC1918 172.16.0.0/12", url: "https://172.16.0.1/", valid: false},
		{name: "RFC1918 192.168.0.0/16", url: "https://192.168.1.1/", valid: false},
		{name: "IPv4 link-local (cloud metadata endpoint)", url: "https://169.254.169.254/", valid: false},
		{name: "IPv6 unique local address", url: "https://[fc00::1]/", valid: false},
		{name: "IPv6 link-local unicast", url: "https://[fe80::1]/", valid: false},

		// Case and trailing-dot variants - DNS is case-insensitive and the
		// root dot is equivalent to its absence.
		{name: "uppercase localhost", url: "https://LOCALHOST/", valid: false},
		{name: "mixed-case localhost subdomain", url: "https://Foo.LOCALHOST/", valid: false},
		{name: "localhost with trailing root dot", url: "https://localhost./", valid: false},

		// Shared address space, special-purpose, cluster-internal, and non-unicast
		{name: "CGNAT 100.64.0.0/10", url: "https://100.64.0.1/", valid: false},
		{name: "CGNAT upper range", url: "https://100.127.255.255/", valid: false},
		{name: "this-network 0.0.0.0/8 non-zero host", url: "https://0.0.0.1/", valid: false},
		{name: "IETF protocol assignments 192.0.0.0/24", url: "https://192.0.0.1/", valid: false},
		{name: "RFC2544 benchmarking 198.18.0.0/15", url: "https://198.18.0.1/", valid: false},
		{name: "Class E reserved 240.0.0.0/4", url: "https://240.0.0.1/", valid: false},
		{name: "GKE default Service CIDR 34.118.224.0/20", url: "https://34.118.224.1/mcp", valid: false},
		{name: "GKE default Service CIDR upper bound", url: "https://34.118.239.254/mcp", valid: false},
		{name: "IPv4-mapped GKE Service CIDR", url: "https://[::ffff:34.118.224.1]/mcp", valid: false},
		{name: "IPv4 multicast", url: "https://224.0.0.1/", valid: false},
		{name: "IPv4 broadcast", url: "https://255.255.255.255/", valid: false},
		{name: "IPv6 link-local multicast", url: "https://[ff02::1]/", valid: false},

		// IPv6 transition, site-local, discard, mapped, and zone-scoped prefixes
		{name: "IPv6 6to4 2002::/16 metadata", url: "https://[2002:a9fe:a9fe::]/mcp", valid: false},
		{name: "IPv6 Teredo 2001::/32", url: "https://[2001:0000:4136:e378:8000:63bf:5601:5601]/mcp", valid: false},
		{name: "IPv6 NAT64 well-known 64:ff9b::/96", url: "https://[64:ff9b::a9fe:a9fe]/mcp", valid: false},
		{name: "IPv6 NAT64 local-use 64:ff9b:1::/48", url: "https://[64:ff9b:1::1]/mcp", valid: false},
		{name: "IPv6 deprecated site-local fec0::/10", url: "https://[fec0::1]/mcp", valid: false},
		{name: "IPv6 IPv4-compatible ::/96 metadata", url: "https://[::a9fe:a9fe]/mcp", valid: false},
		{name: "IPv6 IPv4-compatible ::/96 RFC1918", url: "https://[::c0a8:0101]/mcp", valid: false},
		{name: "IPv6 IPv4-compatible ::/96 loopback", url: "https://[::7f00:0001]/mcp", valid: false},
		{name: "IPv6 SIIT IPv4-translated ::ffff:0:0:0/96", url: "https://[0:0:0:0:ffff:0:a9fe:a9fe]/mcp", valid: false},
		{name: "IPv6 discard-only 100::/64", url: "https://[100::1]/mcp", valid: false},
		{name: "IPv4-mapped IPv6 CGNAT", url: "https://[::ffff:100.64.0.1]/mcp", valid: false},
		{name: "IPv4-mapped IPv6 metadata", url: "https://[::ffff:169.254.169.254]/mcp", valid: false},
		{name: "IPv4-mapped IPv6 broadcast", url: "https://[::ffff:255.255.255.255]/mcp", valid: false},
		{name: "IPv6 link-local with percent-encoded zone ID", url: "https://[fe80::1%25eth0]/mcp", valid: false},

		// Userinfo and authority confusion
		{name: "userinfo authority confusion", url: "https://trusted.example.com@169.254.169.254/mcp", valid: false},
		{name: "backslash in host authority", url: `https://trusted.example.com\169.254.169.254/mcp`, valid: false},

		// Non-decimal and shorthand IPv4 host forms
		{name: "shorthand 2-part IPv4", url: "https://127.1/mcp", valid: false},
		{name: "shorthand 3-part IPv4", url: "https://127.0.1/mcp", valid: false},
		{name: "octal IPv4", url: "https://0177.0.0.1/mcp", valid: false},
		{name: "hex single-integer IPv4", url: "https://0x7f000001/mcp", valid: false},
		{name: "dotted hex IPv4", url: "https://0x7f.0.0.1/mcp", valid: false},
		{name: "all-hex 4-part IPv4", url: "https://0x7f.0x00.0x00.0x01/mcp", valid: false},
		{name: "decimal integer IPv4", url: "https://2130706433/mcp", valid: false},

		// Single-label non-templated placeholder hostnames, boundary templates, vs templated hostnames
		{name: "single-label placeholder YOUR_DOMAIN", url: "https://YOUR_DOMAIN/mcp", valid: false},
		{name: "single-label placeholder HERMES_MCP_HOST", url: "https://HERMES_MCP_HOST/mcp", valid: false},
		{name: "single-label host with path template", url: "https://YOUR_DOMAIN/{tenant_id}", valid: false},
		{name: "single-label host with unseparated path template", url: "https://YOUR_DOMAIN{p}", valid: false},
		{name: "loopback IP with unseparated path template", url: "https://127.0.0.1{p}", valid: false},
		{name: "wildcard IP with prefix template", url: "https://{p}127-0-0-1.sslip.io/mcp", valid: false},
		{name: "single-label host with port template", url: "https://YOUR_DOMAIN:{port}/mcp", valid: false},
		{name: "templated {host} remote URL", url: "https://{host}/mcp", valid: true},
		{name: "templated {custom_host} remote URL", url: "https://{custom_host}/mcp", valid: true},
		{name: "templated {custom_host}:{port} remote URL", url: "https://{custom_host}:{port}/mcp", valid: true},
		{name: "templated subdomain remote URL", url: "https://{region}.example.com/mcp/{tenant_id}", valid: true},
		{name: "templated subdomain on blocked localhost", url: "https://{sub}.localhost/mcp", valid: false},

		// Metadata, .internal, .local, .home.arpa, and loopback-alias domains
		{name: "GCP metadata domain", url: "https://metadata.google.internal/computeMetadata/v1/", valid: false},
		{name: "ICANN reserved .internal TLD", url: "https://mcp.acme-corp.internal/mcp", valid: false},
		{name: "K8s cluster.local service domain", url: "https://api.default.svc.cluster.local/mcp", valid: false},
		{name: "mDNS .local domain", url: "https://printer.local/mcp", valid: false},
		{name: "RFC 8375 home.arpa apex", url: "https://home.arpa/mcp", valid: false},
		{name: "RFC 8375 home.arpa subdomain", url: "https://nas.home.arpa/mcp", valid: false},
		{name: "localtest.me apex", url: "https://localtest.me/mcp", valid: false},
		{name: "localtest.me subdomain", url: "https://api.localtest.me/mcp", valid: false},
		{name: "lvh.me apex", url: "https://lvh.me/mcp", valid: false},
		{name: "lvh.me subdomain", url: "https://sub.lvh.me/mcp", valid: false},
		{name: "vcap.me apex", url: "https://vcap.me/mcp", valid: false},
		{name: "vcap.me subdomain", url: "https://app.vcap.me/mcp", valid: false},

		// Wildcard IP-reflecting domains embedding disallowed IPs
		{name: "nip.io dotted loopback", url: "https://127.0.0.1.nip.io/mcp", valid: false},
		{name: "sslip.io subdomain dotted RFC1918", url: "https://app.10.0.0.1.sslip.io/mcp", valid: false},
		{name: "sslip.io prefix dotted metadata", url: "https://169.254.169.254.app.sslip.io/mcp", valid: false},
		{name: "sslip.io dashed metadata", url: "https://169-254-169-254.sslip.io/mcp", valid: false},
		{name: "nip.io magic prefix dashed loopback", url: "https://app-127-0-0-1.nip.io/mcp", valid: false},
		{name: "xip.io dashed CGNAT", url: "https://100-64-0-1.xip.io/mcp", valid: false},
		{name: "traefik.me dotted RFC1918", url: "https://192.168.1.10.traefik.me/mcp", valid: false},
		{name: "nip.io hex IPv4 RFC1918", url: "https://0a000001.nip.io/mcp", valid: false},
		{name: "nip.io prefixed hex IPv4 loopback", url: "https://app-7f000001.nip.io/mcp", valid: false},
		{name: "nip.io suffixed hex IPv4 loopback", url: "https://7f000001-app.nip.io/mcp", valid: false},
		{name: "sslip.io middle hex IPv4 metadata", url: "https://pre-a9fea9fe-post.sslip.io/mcp", valid: false},
		{name: "sslip.io hex IPv4 metadata", url: "https://a9fea9fe.sslip.io/mcp", valid: false},
		{name: "sslip.io dashed IPv6 loopback", url: "https://--1.sslip.io/mcp", valid: false},
		{name: "sslip.io prefixed dashed IPv6 loopback", url: "https://app---1.sslip.io/mcp", valid: false},
		{name: "sslip.io dashed IPv6 IPv4-compatible metadata", url: "https://--a9fe-a9fe.sslip.io/mcp", valid: false},
		{name: "sslip.io dashed IPv6 IPv4-mapped loopback", url: "https://--ffff-7f00-1.sslip.io/mcp", valid: false},
		{name: "sslip.io dashed IPv6 ULA", url: "https://fc00--1.sslip.io/mcp", valid: false},
		{name: "sslip.io dashed IPv6 Teredo", url: "https://2001-0000-4136-e378-8000-63bf-5601-5601.sslip.io/mcp", valid: false},

		// Sanity checks - must not overblock legitimate remotes
		{name: "public hostname", url: "https://example.com/mcp", valid: true},
		{name: "public hostname with internal label in middle", url: "https://my-server.internal.example.com/mcp", valid: true},
		{name: "public hostname with local label in middle", url: "https://local.example.com/mcp", valid: true},
		{name: "public FQDN with trailing root dot", url: "https://example.com./mcp", valid: true},
		{name: "public IPv4 address", url: "https://8.8.8.8/mcp", valid: true},
		{name: "public IPv4-mapped IPv6 address", url: "https://[::ffff:8.8.8.8]/mcp", valid: true},
		{name: "public IPv6 address", url: "https://[2001:4860:4860::8888]/mcp", valid: true},
		{name: "IPv6 just outside Teredo 2001::/32", url: "https://[2001:1::1]/mcp", valid: true},
		{name: "IPv6 just outside 6to4 2002::/16", url: "https://[2003::1]/mcp", valid: true},
		{name: "IPv6 just outside NAT64 64:ff9b::/96", url: "https://[64:ff9c::1]/mcp", valid: true},
		{name: "IPv6 just outside ::/96", url: "https://[::1:0:0]/mcp", valid: true},
		{name: "IPv6 just outside 100::/64 discard", url: "https://[100:0:0:1::1]/mcp", valid: true},
		{name: "just below CGNAT range", url: "https://100.63.255.255/mcp", valid: true},
		{name: "just above CGNAT range", url: "https://100.128.0.1/mcp", valid: true},
		{name: "just below GKE Service CIDR", url: "https://34.118.223.255/mcp", valid: true},
		{name: "just above GKE Service CIDR", url: "https://34.118.240.1/mcp", valid: true},
		{name: "public VPS on sslip.io dotted IPv4", url: "https://89.167.12.34.sslip.io/mcp", valid: true},
		{name: "public VPS on nip.io subdomain dotted IPv4", url: "https://mcp.203.0.114.55.nip.io/mcp", valid: true},
		{name: "public VPS on sslip.io dashed IPv4", url: "https://89-167-12-34.sslip.io/mcp", valid: true},
		{name: "public VPS on nip.io prefixed dashed IPv4", url: "https://app-8-8-8-8.nip.io/mcp", valid: true},
		{name: "public VPS on nip.io hex IPv4", url: "https://59a70c22.nip.io/mcp", valid: true},
		{name: "public VPS on nip.io suffixed hex IPv4", url: "https://59a70c22-app.nip.io/mcp", valid: true},
		{name: "public VPS on sslip.io dashed IPv6", url: "https://2001-4860-4860--8888.sslip.io/mcp", valid: true},
		{name: "http scheme still rejected regardless of host", url: "http://example.com/mcp", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validators.IsValidRemoteURL(tt.url)
			assert.Equal(t, tt.valid, got, "IsValidRemoteURL(%q)", tt.url)
		})
	}
}

func TestValidateServerJSON_TemplatedCustomHostRemoteURL(t *testing.T) {
	server := apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/custom-host-remote",
		Description: "Server using a templated custom host remote URL",
		Version:     "1.0.0",
		Remotes: []model.Transport{
			{
				Type: model.TransportTypeStreamableHTTP,
				URL:  "https://{custom_host}/mcp",
				Variables: map[string]model.Input{
					"custom_host": {
						Description: "Customer deployment hostname",
						IsRequired:  true,
					},
				},
			},
		},
	}
	res := validators.ValidateServerJSON(&server, validators.ValidationSchemaVersionAndSemantic)
	assert.NoError(t, res.FirstError())

	badPlaceholder := apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/bad-placeholder-remote",
		Description: "Server using an unbraced placeholder hostname",
		Version:     "1.0.0",
		Remotes: []model.Transport{
			{
				Type: model.TransportTypeStreamableHTTP,
				URL:  "https://YOUR_DOMAIN/mcp",
			},
		},
	}
	badRes := validators.ValidateServerJSON(&badPlaceholder, validators.ValidationSchemaVersionAndSemantic)
	require.Error(t, badRes.FirstError())
	assert.Contains(t, badRes.FirstError().Error(), validators.ErrInvalidRemoteURL.Error())
}

func TestIsValidRemoteURL_KubernetesServiceHost(t *testing.T) {
	const customServiceIP = "34.118.240.99"
	require.True(t, validators.IsValidRemoteURL("https://"+customServiceIP+"/mcp"))

	t.Setenv("KUBERNETES_SERVICE_HOST", customServiceIP)
	assert.False(t, validators.IsValidRemoteURL("https://"+customServiceIP+"/mcp"))
	assert.False(t, validators.IsValidRemoteURL("https://[::ffff:"+customServiceIP+"]/mcp"))
}

func TestValidateServerJSON_TemplateVariableSubstitutions(t *testing.T) {
	tests := []struct {
		name      string
		transport model.Transport
		wantValid bool
	}{
		{
			name: "fixed Value on {host} pointing to cloud metadata is rejected",
			transport: model.Transport{
				Type:      model.TransportTypeStreamableHTTP,
				URL:       "https://{host}/computeMetadata/v1/",
				Variables: map[string]model.Input{"host": {Value: "169.254.169.254"}},
			},
			wantValid: false,
		},
		{
			name: "Choices containing loopback IP is rejected",
			transport: model.Transport{
				Type:      model.TransportTypeStreamableHTTP,
				URL:       "https://{host}/mcp",
				Variables: map[string]model.Input{"host": {Choices: []string{"api.example.com", "127.0.0.1"}}},
			},
			wantValid: false,
		},
		{
			name: "pure {host} template with user-overridable localhost Default stays allowed",
			transport: model.Transport{
				Type:      model.TransportTypeStreamableHTTP,
				URL:       "https://{host}/mcp",
				Variables: map[string]model.Input{"host": {Default: "localhost"}},
			},
			wantValid: true,
		},
		{
			name: "pure {host} template with user-overridable 127.0.0.1:9090 Default stays allowed",
			transport: model.Transport{
				Type:      model.TransportTypeStreamableHTTP,
				URL:       "https://{host}/mcp",
				Variables: map[string]model.Input{"host": {Default: "127.0.0.1:9090"}},
			},
			wantValid: true,
		},
		{
			name: "pure {host} Default injecting userinfo @ is rejected",
			transport: model.Transport{
				Type:      model.TransportTypeStreamableHTTP,
				URL:       "https://{host}/mcp",
				Variables: map[string]model.Input{"host": {Default: "trusted.example.com@169.254.169.254"}},
			},
			wantValid: false,
		},
		{
			name: "pure {host} Value injecting scheme delimiter is rejected",
			transport: model.Transport{
				Type:      model.TransportTypeStreamableHTTP,
				URL:       "https://{host}/mcp",
				Variables: map[string]model.Input{"host": {Value: "https://169.254.169.254"}},
			},
			wantValid: false,
		},
		{
			name: "partial-host {oct} Default completing metadata sslip.io IP is rejected",
			transport: model.Transport{
				Type:      model.TransportTypeStreamableHTTP,
				URL:       "https://169.254.{oct}.254.sslip.io/mcp",
				Variables: map[string]model.Input{"oct": {Default: "169"}},
			},
			wantValid: false,
		},
		{
			name: "partial-host {p} prefix with non-empty Default on wildcard loopback is rejected",
			transport: model.Transport{
				Type:      model.TransportTypeStreamableHTTP,
				URL:       "https://{p}127-0-0-1.sslip.io/mcp",
				Variables: map[string]model.Input{"p": {Default: "app-"}},
			},
			wantValid: false,
		},
		{
			name: "unseparated {p} variable injecting @authority is rejected",
			transport: model.Transport{
				Type:      model.TransportTypeStreamableHTTP,
				URL:       "https://public.example.com{p}",
				Variables: map[string]model.Input{"p": {Value: "@169.254.169.254/mcp"}},
			},
			wantValid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/template-sub-test",
				Description: "Template substitution test",
				Version:     "1.0.0",
				Remotes:     []model.Transport{tc.transport},
			}
			res := validators.ValidateServerJSON(&srv, validators.ValidationSchemaVersionAndSemantic)
			if tc.wantValid {
				assert.NoError(t, res.FirstError())
			} else {
				require.Error(t, res.FirstError())
				assert.Contains(t, res.FirstError().Error(), validators.ErrInvalidRemoteURL.Error())
			}
		})
	}
}

type fakeHostResolver struct {
	records map[string][]net.IPAddr
	errs    map[string]error
	delay   time.Duration
	mu      sync.Mutex
	queried []string
	calls   atomic.Int32
}

func (f *fakeHostResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.queried = append(f.queried, host)
	f.mu.Unlock()

	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.delay):
		}
	}

	if err, ok := f.errs[host]; ok {
		return nil, err
	}
	if ips, ok := f.records[host]; ok {
		return ips, nil
	}
	return nil, errors.New("no such host")
}

func TestValidatePublishAndUpdate_RemoteDNSResolution(t *testing.T) {
	ipAddr := func(s string) net.IPAddr {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("invalid test IP %q", s)
		}
		return net.IPAddr{IP: ip}
	}

	// Keys all end with "." because checkRemoteHostResolution queries rooted FQDNs
	// to prevent Kubernetes ndots:5 search-domain expansion.
	resolver := &fakeHostResolver{
		records: map[string][]net.IPAddr{
			"loopback.attacker.example.":  {ipAddr("127.0.0.1")},
			"metadata.attacker.example.":  {ipAddr("169.254.169.254")},
			"private.attacker.example.":   {ipAddr("10.0.0.1")},
			"cgnat.attacker.example.":     {ipAddr("100.64.0.1")},
			"gke-svc.attacker.example.":   {ipAddr("34.118.224.10")},
			"teredo.attacker.example.":    {ipAddr("2001:0000:4136:e378:8000:63bf:5601:5601")},
			"v4compat.attacker.example.":  {ipAddr("::a9fe:a9fe")},
			"dualstack.attacker.example.": {ipAddr("93.184.215.14"), ipAddr("127.0.0.1")},
			"public.service.example.":     {ipAddr("93.184.215.14"), ipAddr("2606:2800:21f:cb07:6820:80da:af6b:8b2c")},
		},
		errs: map[string]error{
			"nxdomain.service.example.": &net.DNSError{Err: "no such host", Name: "nxdomain.service.example.", IsNotFound: true},
		},
	}
	validators.SetHostResolverForTest(t, resolver)

	enabledCfg := &config.Config{EnableRegistryValidation: true}
	disabledCfg := &config.Config{EnableRegistryValidation: false}

	makeServer := func(remotes ...model.Transport) apiv0.ServerJSON {
		return apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        "com.example/remote-dns-test",
			Description: "Remote DNS validation test",
			Version:     "1.0.0",
			Remotes:     remotes,
		}
	}

	disallowedHosts := []struct {
		host     string
		secretIP string
	}{
		{"loopback.attacker.example", "127.0.0.1"},
		{"metadata.attacker.example", "169.254.169.254"},
		{"private.attacker.example", "10.0.0.1"},
		{"cgnat.attacker.example", "100.64.0.1"},
		{"gke-svc.attacker.example", "34.118.224.10"},
		{"teredo.attacker.example", "2001:0:4136"},
		{"v4compat.attacker.example", "a9fe:a9fe"},
		{"dualstack.attacker.example", "127.0.0.1"},
	}
	for _, tc := range disallowedHosts {
		t.Run("rejects "+tc.host+" without leaking resolved IP", func(t *testing.T) {
			srv := makeServer(model.Transport{
				Type: model.TransportTypeStreamableHTTP,
				URL:  "https://" + tc.host + "/mcp",
			})

			err := validators.ValidateWriteRequest(context.Background(), srv, enabledCfg, false)
			require.ErrorIs(t, err, validators.ErrInvalidRemoteURL)
			assert.NotContains(t, err.Error(), tc.secretIP, "error must not leak resolved internal IP")

			// Deleted-version takedowns/edits skip registry validation, and offline
			// ValidateServerJSON never performs DNS lookups.
			require.NoError(t, validators.ValidateWriteRequest(context.Background(), srv, enabledCfg, true))
			require.NoError(t, validators.ValidateWriteRequest(context.Background(), srv, disabledCfg, false))
			offlineRes := validators.ValidateServerJSON(&srv, validators.ValidationSchemaVersionAndSemantic)
			assert.NoError(t, offlineRes.FirstError())
		})
	}

	t.Run("rejects concrete hostname with path variable (both / and unseparated {p})", func(t *testing.T) {
		for _, rawURL := range []string{
			"https://metadata.attacker.example/{p}",
			"https://metadata.attacker.example{p}",
		} {
			srv := makeServer(model.Transport{
				Type:      model.TransportTypeStreamableHTTP,
				URL:       rawURL,
				Variables: map[string]model.Input{"p": {Value: "/latest/meta-data/"}},
			})
			err := validators.ValidateWriteRequest(context.Background(), srv, enabledCfg, false)
			require.ErrorIs(t, err, validators.ErrInvalidRemoteURL, "url: %s", rawURL)
		}

		// Even without a fixed Value on {p}, static authority prefix is still resolved.
		srvNoVal := makeServer(model.Transport{
			Type:      model.TransportTypeStreamableHTTP,
			URL:       "https://metadata.attacker.example{p}",
			Variables: map[string]model.Input{"p": {IsRequired: true}},
		})
		require.ErrorIs(t, validators.ValidateWriteRequest(context.Background(), srvNoVal, enabledCfg, false), validators.ErrInvalidRemoteURL)
	})

	t.Run("rejects templated {host} when Value or Choices resolve to disallowed IP", func(t *testing.T) {
		srvVal := makeServer(model.Transport{
			Type:      model.TransportTypeStreamableHTTP,
			URL:       "https://{host}/mcp",
			Variables: map[string]model.Input{"host": {Value: "gke-svc.attacker.example"}},
		})
		require.ErrorIs(t, validators.ValidateWriteRequest(context.Background(), srvVal, enabledCfg, false), validators.ErrInvalidRemoteURL)

		srvChoice := makeServer(model.Transport{
			Type:      model.TransportTypeStreamableHTTP,
			URL:       "https://{host}/mcp",
			Variables: map[string]model.Input{"host": {Choices: []string{"public.service.example", "loopback.attacker.example"}}},
		})
		require.ErrorIs(t, validators.ValidateWriteRequest(context.Background(), srvChoice, enabledCfg, false), validators.ErrInvalidRemoteURL)
	})

	t.Run("allows public IPs, NXDOMAIN, templated hosts, deduplicates lookups, and queries trailing-dot FQDNs", func(t *testing.T) {
		before := resolver.calls.Load()
		srv := makeServer(
			model.Transport{Type: model.TransportTypeStreamableHTTP, URL: "https://public.service.example/mcp"},
			model.Transport{Type: model.TransportTypeSSE, URL: "https://public.service.example/sse"},
			model.Transport{Type: model.TransportTypeStreamableHTTP, URL: "https://nxdomain.service.example/mcp"},
			model.Transport{Type: model.TransportTypeStreamableHTTP, URL: "https://8.8.8.8/mcp"},
			model.Transport{
				Type:      model.TransportTypeStreamableHTTP,
				URL:       "https://{tenant}.loopback.attacker.example/mcp",
				Variables: map[string]model.Input{"tenant": {IsRequired: true}},
			},
		)
		require.NoError(t, validators.ValidateWriteRequest(context.Background(), srv, enabledCfg, false))
		// 2 unique non-templated DNS hosts ("public.service.example." and "nxdomain.service.example.").
		assert.Equal(t, int32(2), resolver.calls.Load()-before)

		resolver.mu.Lock()
		queried := append([]string(nil), resolver.queried...)
		resolver.mu.Unlock()
		assert.NotEmpty(t, queried)
		for _, q := range queried {
			assert.True(t, strings.HasSuffix(q, "."), "LookupIPAddr host %q must end with trailing dot", q)
		}
	})

	t.Run("caps unique remote host lookups per request before issuing DNS queries", func(t *testing.T) {
		before := resolver.calls.Load()
		remotes := make([]model.Transport, 33)
		for i := range remotes {
			remotes[i] = model.Transport{
				Type: model.TransportTypeStreamableHTTP,
				URL:  fmt.Sprintf("https://host-%d.public.service.example/mcp", i),
			}
		}
		err := validators.ValidateWriteRequest(context.Background(), makeServer(remotes...), enabledCfg, false)
		require.ErrorIs(t, err, validators.ErrInvalidRemoteURL)
		assert.Contains(t, err.Error(), "unique remote hostnames")
		assert.Equal(t, before, resolver.calls.Load(), "excessive unique remote hosts must fail before issuing DNS queries")
	})

	t.Run("cancels slow remote DNS lookup immediately when package validation fails", func(t *testing.T) {
		slowResolver := &fakeHostResolver{delay: 5 * time.Second}
		validators.SetHostResolverForTest(t, slowResolver)

		srv := apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        "com.example/fast-pkg-fail",
			Description: "Fast package failure cancels slow remote DNS",
			Version:     "1.0.0",
			Packages: []model.Package{{
				RegistryType:    model.RegistryTypeNPM,
				RegistryBaseURL: "https://evil-npm.example.com",
				Identifier:      "pkg",
				Version:         "1.0.0",
				Transport:       model.Transport{Type: model.TransportTypeStdio},
			}},
			Remotes: []model.Transport{{
				Type: model.TransportTypeStreamableHTTP,
				URL:  "https://slow-dns.example.com/mcp",
			}},
		}

		start := time.Now()
		err := validators.ValidateWriteRequest(context.Background(), srv, enabledCfg, false)
		elapsed := time.Since(start)
		require.Error(t, err)
		assert.Less(t, elapsed, 1*time.Second, "fast package error must cancel in-flight remote DNS lookup")
	})
}

func TestValidate_AllowPrivateRemoteURLs(t *testing.T) {
	resolver := &fakeHostResolver{
		records: map[string][]net.IPAddr{
			"intranet.acme.internal.": {{IP: net.ParseIP("10.10.0.5")}},
		},
	}
	validators.SetHostResolverForTest(t, resolver)

	privateServer := apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.acme/internal-mcp",
		Description: "Intranet MCP server",
		Version:     "1.0.0",
		Remotes: []model.Transport{
			{Type: model.TransportTypeStreamableHTTP, URL: "https://intranet.acme.internal/mcp"},
			{Type: model.TransportTypeStreamableHTTP, URL: "https://10.10.0.5:8443/mcp"},
			{Type: model.TransportTypeSSE, URL: "https://mcp-gateway:8443/sse"},
		},
	}

	// Default (AllowPrivateRemoteURLs = false): rejected offline and at publish time.
	defaultRes := validators.ValidateServerJSON(&privateServer, validators.ValidationAll)
	require.Error(t, defaultRes.FirstError())

	// Opt-in (AllowPrivateRemoteURLs = true): allowed offline and skips DNS IP blocklist.
	allowOpts := validators.ValidationAll.WithAllowPrivateRemoteURLs(true)
	allowRes := validators.ValidateServerJSON(&privateServer, allowOpts)
	require.NoError(t, allowRes.FirstError())

	allowCfg := &config.Config{EnableRegistryValidation: true, AllowPrivateRemoteURLs: true}
	require.NoError(t, validators.ValidateWriteRequest(context.Background(), privateServer, allowCfg, false))
	assert.Zero(t, resolver.calls.Load(), "AllowPrivateRemoteURLs=true must skip remote DNS blocklist lookups")

	// Structural rules (https:// scheme, no userinfo) remain enforced even when AllowPrivateRemoteURLs=true.
	httpServer := privateServer
	httpServer.Remotes = []model.Transport{{Type: model.TransportTypeStreamableHTTP, URL: "http://10.10.0.5:8443/mcp"}}
	assert.Error(t, validators.ValidateServerJSON(&httpServer, allowOpts).FirstError())

	userinfoServer := privateServer
	userinfoServer.Remotes = []model.Transport{{Type: model.TransportTypeStreamableHTTP, URL: "https://admin@10.10.0.5:8443/mcp"}}
	assert.Error(t, validators.ValidateServerJSON(&userinfoServer, allowOpts).FirstError())
}
