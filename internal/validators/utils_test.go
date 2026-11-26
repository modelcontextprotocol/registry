package validators_test

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

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

		// Shared address space and non-unicast - not publicly routable
		{name: "CGNAT 100.64.0.0/10", url: "https://100.64.0.1/", valid: false},
		{name: "CGNAT upper range", url: "https://100.127.255.255/", valid: false},
		{name: "this-network 0.0.0.0/8 non-zero host", url: "https://0.0.0.1/", valid: false},
		{name: "IPv4 multicast", url: "https://224.0.0.1/", valid: false},
		{name: "IPv4 broadcast", url: "https://255.255.255.255/", valid: false},
		{name: "IPv6 link-local multicast", url: "https://[ff02::1]/", valid: false},

		// IPv6 transition, site-local, mapped, and zone-scoped prefixes
		{name: "IPv6 6to4 2002::/16 metadata", url: "https://[2002:a9fe:a9fe::]/mcp", valid: false},
		{name: "IPv6 Teredo 2001::/32", url: "https://[2001:0000:4136:e378:8000:63bf:5601:5601]/mcp", valid: false},
		{name: "IPv6 NAT64 well-known 64:ff9b::/96", url: "https://[64:ff9b::a9fe:a9fe]/mcp", valid: false},
		{name: "IPv6 NAT64 local-use 64:ff9b:1::/48", url: "https://[64:ff9b:1::1]/mcp", valid: false},
		{name: "IPv6 deprecated site-local fec0::/10", url: "https://[fec0::1]/mcp", valid: false},
		{name: "IPv6 IPv4-compatible ::/96 metadata", url: "https://[::a9fe:a9fe]/mcp", valid: false},
		{name: "IPv6 IPv4-compatible ::/96 RFC1918", url: "https://[::c0a8:0101]/mcp", valid: false},
		{name: "IPv6 IPv4-compatible ::/96 loopback", url: "https://[::7f00:0001]/mcp", valid: false},
		{name: "IPv4-mapped IPv6 CGNAT", url: "https://[::ffff:100.64.0.1]/mcp", valid: false},
		{name: "IPv4-mapped IPv6 metadata", url: "https://[::ffff:169.254.169.254]/mcp", valid: false},
		{name: "IPv4-mapped IPv6 broadcast", url: "https://[::ffff:255.255.255.255]/mcp", valid: false},
		{name: "IPv6 link-local with percent-encoded zone ID", url: "https://[fe80::1%25eth0]/mcp", valid: false},

		// Non-decimal and shorthand IPv4 host forms
		{name: "shorthand 2-part IPv4", url: "https://127.1/mcp", valid: false},
		{name: "shorthand 3-part IPv4", url: "https://127.0.1/mcp", valid: false},
		{name: "octal IPv4", url: "https://0177.0.0.1/mcp", valid: false},
		{name: "hex single-integer IPv4", url: "https://0x7f000001/mcp", valid: false},
		{name: "dotted hex IPv4", url: "https://0x7f.0.0.1/mcp", valid: false},
		{name: "all-hex 4-part IPv4", url: "https://0x7f.0x00.0x00.0x01/mcp", valid: false},
		{name: "decimal integer IPv4", url: "https://2130706433/mcp", valid: false},

		// Single-label non-templated placeholder hostnames vs templated hostnames
		{name: "single-label placeholder YOUR_DOMAIN", url: "https://YOUR_DOMAIN/mcp", valid: false},
		{name: "single-label placeholder HERMES_MCP_HOST", url: "https://HERMES_MCP_HOST/mcp", valid: false},
		{name: "single-label host with path template", url: "https://YOUR_DOMAIN/{tenant_id}", valid: false},
		{name: "single-label host with port template", url: "https://YOUR_DOMAIN:{port}/mcp", valid: false},
		{name: "templated {host} remote URL", url: "https://{host}/mcp", valid: true},
		{name: "templated {custom_host} remote URL", url: "https://{custom_host}/mcp", valid: true},
		{name: "templated {custom_host}:{port} remote URL", url: "https://{custom_host}:{port}/mcp", valid: true},
		{name: "templated subdomain remote URL", url: "https://{region}.example.com/mcp/{tenant_id}", valid: true},
		{name: "templated subdomain on blocked localhost", url: "https://{sub}.localhost/mcp", valid: false},

		// Metadata, .internal TLD, and loopback-alias domains
		{name: "GCP metadata domain", url: "https://metadata.google.internal/computeMetadata/v1/", valid: false},
		{name: "ICANN reserved .internal TLD", url: "https://mcp.acme-corp.internal/mcp", valid: false},
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
		{name: "public FQDN with trailing root dot", url: "https://example.com./mcp", valid: true},
		{name: "public IPv4 address", url: "https://8.8.8.8/mcp", valid: true},
		{name: "public IPv4-mapped IPv6 address", url: "https://[::ffff:8.8.8.8]/mcp", valid: true},
		{name: "public IPv6 address", url: "https://[2001:4860:4860::8888]/mcp", valid: true},
		{name: "IPv6 just outside Teredo 2001::/32", url: "https://[2001:1::1]/mcp", valid: true},
		{name: "IPv6 just outside 6to4 2002::/16", url: "https://[2003::1]/mcp", valid: true},
		{name: "IPv6 just outside NAT64 64:ff9b::/96", url: "https://[64:ff9c::1]/mcp", valid: true},
		{name: "IPv6 just outside ::/96", url: "https://[::1:0:0]/mcp", valid: true},
		{name: "just below CGNAT range", url: "https://100.63.255.255/mcp", valid: true},
		{name: "just above CGNAT range", url: "https://100.128.0.1/mcp", valid: true},
		{name: "public VPS on sslip.io dotted IPv4", url: "https://89.167.12.34.sslip.io/mcp", valid: true},
		{name: "public VPS on nip.io subdomain dotted IPv4", url: "https://mcp.203.0.114.55.nip.io/mcp", valid: true},
		{name: "public VPS on sslip.io dashed IPv4", url: "https://89-167-12-34.sslip.io/mcp", valid: true},
		{name: "public VPS on nip.io prefixed dashed IPv4", url: "https://app-8-8-8-8.nip.io/mcp", valid: true},
		{name: "public VPS on nip.io hex IPv4", url: "https://59a70c22.nip.io/mcp", valid: true},
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

type fakeHostResolver struct {
	records map[string][]net.IPAddr
	errs    map[string]error
	calls   atomic.Int32
}

func (f *fakeHostResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	f.calls.Add(1)
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

	resolver := &fakeHostResolver{
		records: map[string][]net.IPAddr{
			"loopback.attacker.example":  {ipAddr("127.0.0.1")},
			"metadata.attacker.example":  {ipAddr("169.254.169.254")},
			"private.attacker.example":   {ipAddr("10.0.0.1")},
			"cgnat.attacker.example":     {ipAddr("100.64.0.1")},
			"teredo.attacker.example":    {ipAddr("2001:0000:4136:e378:8000:63bf:5601:5601")},
			"v4compat.attacker.example":  {ipAddr("::a9fe:a9fe")},
			"dualstack.attacker.example": {ipAddr("93.184.215.14"), ipAddr("127.0.0.1")},
			"public.service.example":     {ipAddr("93.184.215.14"), ipAddr("2606:2800:21f:cb07:6820:80da:af6b:8b2c")},
		},
		errs: map[string]error{
			"nxdomain.service.example": &net.DNSError{Err: "no such host", Name: "nxdomain.service.example", IsNotFound: true},
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

	disallowedHosts := []string{
		"loopback.attacker.example",
		"metadata.attacker.example",
		"private.attacker.example",
		"cgnat.attacker.example",
		"teredo.attacker.example",
		"v4compat.attacker.example",
		"dualstack.attacker.example",
	}
	for _, host := range disallowedHosts {
		t.Run("rejects "+host, func(t *testing.T) {
			srv := makeServer(model.Transport{
				Type: model.TransportTypeStreamableHTTP,
				URL:  "https://" + host + "/mcp",
			})

			err := validators.ValidatePublishRequest(context.Background(), srv, enabledCfg)
			require.ErrorIs(t, err, validators.ErrInvalidRemoteURL)

			err = validators.ValidateUpdateRequest(context.Background(), srv, enabledCfg, false)
			require.ErrorIs(t, err, validators.ErrInvalidRemoteURL)

			// Deleted-version takedowns/edits skip registry validation, and offline
			// ValidateServerJSON never performs DNS lookups.
			require.NoError(t, validators.ValidateUpdateRequest(context.Background(), srv, enabledCfg, true))
			require.NoError(t, validators.ValidatePublishRequest(context.Background(), srv, disabledCfg))
			offlineRes := validators.ValidateServerJSON(&srv, validators.ValidationSchemaVersionAndSemantic)
			assert.NoError(t, offlineRes.FirstError())
		})
	}

	t.Run("rejects concrete hostname even when path contains template variable", func(t *testing.T) {
		srv := makeServer(model.Transport{
			Type: model.TransportTypeStreamableHTTP,
			URL:  "https://metadata.attacker.example/{tenant_id}/mcp",
			Variables: map[string]model.Input{
				"tenant_id": {IsRequired: true},
			},
		})
		err := validators.ValidatePublishRequest(context.Background(), srv, enabledCfg)
		require.ErrorIs(t, err, validators.ErrInvalidRemoteURL)
	})

	t.Run("allows public IPs, NXDOMAIN, templated hosts, and deduplicates lookups", func(t *testing.T) {
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
		require.NoError(t, validators.ValidatePublishRequest(context.Background(), srv, enabledCfg))
		require.NoError(t, validators.ValidateUpdateRequest(context.Background(), srv, enabledCfg, false))
		// 2 unique non-templated DNS hosts ("public.service.example" and "nxdomain.service.example") x 2 calls = 4 lookups.
		assert.Equal(t, int32(4), resolver.calls.Load()-before)
	})
}
