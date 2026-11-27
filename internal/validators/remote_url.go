package validators

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const (
	// remoteDNSLookupTimeout bounds a single per-hostname DNS lookup during
	// publish and update validation.
	remoteDNSLookupTimeout = 3 * time.Second

	// remoteDNSOverallTimeout bounds total wall-clock time spent resolving all
	// remote hostnames within one publish or update validation call.
	remoteDNSOverallTimeout = 8 * time.Second

	// maxRemoteHostLookupsPerRequest caps unique concrete remote DNS hostnames
	// resolved in a single publish/update request to prevent DNS amplification
	// and handler tarpits on large payloads.
	maxRemoteHostLookupsPerRequest = 32

	remoteDNSLookupConcurrency = 4
)

// HostResolver resolves a DNS hostname to IP addresses during publish/update validation.
type HostResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

var defaultHostResolver HostResolver = net.DefaultResolver

var templateVarTokenRe = regexp.MustCompile(`\{([^}]+)\}`)

var (
	// blockedRemoteIPv4Prefixes covers special-purpose and cluster-internal IPv4
	// blocks not classified by netip.Addr's built-in Is* helpers:
	//   0.0.0.0/8        RFC 1122 "this host on this network"
	//   100.64.0.0/10    RFC 6598 Carrier-Grade NAT
	//   192.0.0.0/24     RFC 6890 IETF protocol assignments
	//   198.18.0.0/15    RFC 2544 benchmarking (commonly used by service meshes/VPCs)
	//   240.0.0.0/4      RFC 1112 Class E reserved + 255.255.255.255 limited broadcast
	//   34.118.224.0/20  GKE default privately-used public IP Service CIDR
	blockedRemoteIPv4Prefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("34.118.224.0/20"),
	}

	// blockedRemoteIPv6Prefixes covers IPv6 transition mechanisms that embed or
	// tunnel arbitrary IPv4 addresses, discard-only space, and deprecated
	// site-local ranges not covered by netip.Addr's built-in Is* classifiers:
	//   2002::/16         RFC 3056 6to4 (bits 16-47 embed IPv4)
	//   2001::/32         RFC 4380 Teredo (low 32 bits embed obfuscated IPv4)
	//   64:ff9b::/96      RFC 6052 NAT64 well-known prefix (low 32 bits embed IPv4)
	//   64:ff9b:1::/48    RFC 8215 NAT64 local-use prefix
	//   ::/96             RFC 4291 deprecated IPv4-compatible prefix (low 32 bits embed IPv4)
	//   ::ffff:0:0:0/96   RFC 2765 SIIT IPv4-translated prefix (low 32 bits embed IPv4)
	//   100::/64          RFC 6666 discard-only address block
	//   fec0::/10         RFC 3879 deprecated site-local prefix
	blockedRemoteIPv6Prefixes = []netip.Prefix{
		netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("2001::/32"),
		netip.MustParsePrefix("64:ff9b::/96"),
		netip.MustParsePrefix("64:ff9b:1::/48"),
		netip.MustParsePrefix("::/96"),
		netip.MustParsePrefix("::ffff:0:0:0/96"),
		netip.MustParsePrefix("100::/64"),
		netip.MustParsePrefix("fec0::/10"),
	}

	// blockedRemoteDomainSuffixes lists special-use / private TLDs (RFC 6761,
	// RFC 6762 mDNS / K8s cluster.local, RFC 8375 home.arpa, ICANN .internal)
	// and well-known public wildcard domains that unconditionally resolve to
	// loopback or internal endpoints.
	blockedRemoteDomainSuffixes = []string{
		"localhost",
		"internal",
		"local",
		"home.arpa",
		"localtest.me",
		"lvh.me",
		"vcap.me",
	}

	// wildcardIPDomains lists public wildcard DNS services that reflect an IP
	// address encoded in a subdomain label back as an A/AAAA answer. Public VPS
	// addresses on these domains remain valid; embedded non-global IPs are rejected.
	wildcardIPDomains = []string{
		"nip.io",
		"sslip.io",
		"xip.io",
		"traefik.me",
	}
)

// isValidRemoteURL validates rawURL as a remote MCP server endpoint. When
// allowPrivate is true (for self-hosted intranet registries), private/loopback/
// internal host restrictions are relaxed while structural HTTPS and authority
// safety rules remain enforced.
func isValidRemoteURL(rawURL string, allowPrivate bool) bool {
	if !IsValidURL(rawURL) {
		return false
	}

	testURL := replaceTemplateVariables(rawURL)
	u, err := url.Parse(testURL)
	if err != nil || u.Scheme != schemeHTTPS {
		return false
	}

	hostname := u.Hostname()
	if u.User != nil || strings.Contains(u.Host, "\\") || hostname == "" {
		return false
	}

	if allowPrivate {
		if strings.Contains(hostname, ":") {
			_, err := netip.ParseAddr(hostname)
			return err == nil
		}
		return true
	}

	_, isPureHost := pureAuthorityHostVariable(rawURL)
	if isDisallowedRemoteHost(hostname, isPureHost) {
		return false
	}

	// If the authority mixes literal text with {variable} tokens (e.g.
	// "https://127.0.0.1{p}", "https://{p}127-0-0-1.sslip.io/mcp", or
	// "https://YOUR_DOMAIN{p}"), also check the static host left behind when
	// template tokens are empty or begin with a path delimiter "/".
	if stripped, ok := strippedStaticAuthorityHost(rawURL, isPureHost); ok {
		if isDisallowedRemoteHost(stripped, false) {
			return false
		}
	}

	return true
}

// isDisallowedRemoteIP reports whether addr is non-global, host-scoped, or in a
// private/loopback/link-local/CGNAT/special-purpose/cluster-internal/transition
// range unsuitable for a public MCP remote server. IPv4-mapped IPv6 addresses
// (::ffff:0:0/96) are unmapped so all IPv4 checks apply consistently.
func isDisallowedRemoteIP(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" {
		return true
	}
	if addr.Is6() && !addr.Is4In6() {
		for _, p := range blockedRemoteIPv6Prefixes {
			if p.Contains(addr) {
				return true
			}
		}
	}
	unmapped := addr.Unmap()
	if unmapped.IsLoopback() || unmapped.IsUnspecified() || unmapped.IsPrivate() ||
		unmapped.IsLinkLocalUnicast() || unmapped.IsLinkLocalMulticast() || unmapped.IsMulticast() {
		return true
	}
	if unmapped.Is4() {
		for _, p := range blockedRemoteIPv4Prefixes {
			if p.Contains(unmapped) {
				return true
			}
		}
	}
	if k8sHost := os.Getenv("KUBERNETES_SERVICE_HOST"); k8sHost != "" {
		if k8sAddr, err := netip.ParseAddr(k8sHost); err == nil && unmapped == k8sAddr.Unmap() {
			return true
		}
	}
	return false
}

// rawAuthorityHost extracts the raw host substring from rawURL prior to
// template replacement.
func rawAuthorityHost(rawURL string) (string, bool) {
	schemeSep := strings.Index(rawURL, "://")
	if schemeSep < 0 {
		return "", false
	}
	authority := rawURL[schemeSep+3:]
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		authority = authority[at+1:]
	}
	host := authority
	if strings.HasPrefix(host, "[") {
		if closeBracket := strings.IndexByte(host, ']'); closeBracket >= 0 {
			host = host[:closeBracket+1]
		}
	} else if colon := strings.IndexByte(host, ':'); colon >= 0 {
		host = host[:colon]
	}
	return host, true
}

// pureAuthorityHostVariable reports whether rawURL's host component is a single
// standalone {variable} token (e.g. "https://{custom_host}/mcp" or
// "https://{host}:{port}/mcp"), returning that variable's name.
func pureAuthorityHostVariable(rawURL string) (string, bool) {
	host, ok := rawAuthorityHost(rawURL)
	if !ok || !strings.HasPrefix(host, "{") || !strings.HasSuffix(host, "}") {
		return "", false
	}
	inner := host[1 : len(host)-1]
	if inner == "" || strings.ContainsAny(inner, "{}") {
		return "", false
	}
	return inner, true
}

// strippedStaticAuthorityHost removes {variable} tokens from rawURL's host and
// returns the remaining literal string when it forms a standalone candidate
// hostname (catching boundary cases like "https://attacker.example{p}" where p
// starts with "/" or "https://{p}127-0-0-1.sslip.io/mcp" where p is "").
func strippedStaticAuthorityHost(rawURL string, isPureHost bool) (string, bool) {
	if isPureHost {
		return "", false
	}
	rawHost, ok := rawAuthorityHost(rawURL)
	if !ok || !strings.Contains(rawHost, "{") {
		return "", false
	}
	stripped := templateVarTokenRe.ReplaceAllString(rawHost, "")
	if !isCompleteHostCandidate(stripped) {
		return "", false
	}
	return stripped, true
}

func isCompleteHostCandidate(h string) bool {
	if h == "" || strings.HasPrefix(h, ".") || strings.HasSuffix(h, ".") || strings.Contains(h, "..") {
		return false
	}
	if strings.HasPrefix(h, "-") || strings.HasSuffix(h, "-") || strings.Contains(h, "-.") || strings.Contains(h, ".-") {
		return false
	}
	return true
}

func extractSchemeAndAuthorityVariables(rawURL string) []string {
	schemeSep := strings.Index(rawURL, "://")
	if schemeSep < 0 {
		return extractTemplateVariables(rawURL)
	}
	rest := rawURL[schemeSep+3:]
	prefix := rawURL
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		prefix = rawURL[:schemeSep+3+end]
	}
	return extractTemplateVariables(prefix)
}

// validateRemoteTransportURL validates both obj.URL and any concrete URL
// instantiations produced by obj.Variables (Value, Choices, and Default).
func validateRemoteTransportURL(obj *model.Transport, allowPrivate bool) bool {
	if !isValidRemoteURL(obj.URL, allowPrivate) {
		return false
	}
	if hasUnsafeAuthorityVariableValues(obj) {
		return false
	}
	strictURLs, structuralOnlyURLs := instantiateRemoteVariableURLs(obj)
	for _, u := range strictURLs {
		if !isValidRemoteURL(u, allowPrivate) {
			return false
		}
	}
	for _, u := range structuralOnlyURLs {
		if !isValidRemoteURL(u, true) {
			return false
		}
	}
	return true
}

// hasUnsafeAuthorityVariableValues rejects scheme/authority/userinfo/backslash
// delimiter smuggling in any variable positioned before the path delimiter.
func hasUnsafeAuthorityVariableValues(obj *model.Transport) bool {
	if len(obj.Variables) == 0 {
		return false
	}
	for _, v := range extractSchemeAndAuthorityVariables(obj.URL) {
		in, ok := obj.Variables[v]
		if !ok {
			continue
		}
		if containsAuthorityInjectionChars(in.Value) || containsAuthorityInjectionChars(in.Default) {
			return true
		}
		for _, choice := range in.Choices {
			if containsAuthorityInjectionChars(choice) {
				return true
			}
		}
	}
	return false
}

func containsAuthorityInjectionChars(s string) bool {
	return strings.Contains(s, "://") || strings.ContainsAny(s, "@\\")
}

// instantiateRemoteVariableURLs substitutes concrete variable definitions from
// obj.Variables into obj.URL.
//
// Policy on Default vs Value/Choices:
//   - Value (fixed/non-editable) and Choices (enumerated allowed options) are
//     always strictly validated against remote host rules.
//   - Default is strictly validated whenever the variable is NOT a standalone
//     pure {host} template (catching partial-host or path-boundary defaults such
//     as "169.254.{oct}.254.sslip.io" or "attacker.example{p}"). On a pure
//     standalone {host} template (e.g. "https://{host}/mcp"), Default is checked
//     for structural HTTPS/no-userinfo validity so user-overridable self-hosted
//     templates defaulting to "localhost" or "127.0.0.1:9090" remain valid.
func instantiateRemoteVariableURLs(obj *model.Transport) (strictURLs []string, structuralOnlyURLs []string) {
	vars := extractTemplateVariables(obj.URL)
	if len(vars) == 0 || len(obj.Variables) == 0 {
		return nil, nil
	}

	pureHostVar, isPureHost := pureAuthorityHostVariable(obj.URL)
	baseVals, hasStrictConcrete := buildBaseTemplateValues(obj, vars, pureHostVar, isPureHost)

	if hasStrictConcrete {
		strictURLs = append(strictURLs, substituteTemplateMap(obj.URL, baseVals))
	}

	for _, v := range vars {
		in, ok := obj.Variables[v]
		if !ok {
			continue
		}
		if in.Value != "" {
			strictURLs = append(strictURLs, substituteTemplateOverride(obj.URL, baseVals, v, in.Value))
		}
		for _, choice := range in.Choices {
			strictURLs = append(strictURLs, substituteTemplateOverride(obj.URL, baseVals, v, choice))
		}
		if in.Default != "" {
			instantiated := substituteTemplateOverride(obj.URL, baseVals, v, in.Default)
			if isPureHostVarName(isPureHost, pureHostVar, v) {
				structuralOnlyURLs = append(structuralOnlyURLs, instantiated)
			} else {
				strictURLs = append(strictURLs, instantiated)
			}
		}
	}

	return strictURLs, structuralOnlyURLs
}

func isPureHostVarName(isPureHost bool, pureHostVar, varName string) bool {
	return isPureHost && varName == pureHostVar
}

func buildBaseTemplateValues(obj *model.Transport, vars []string, pureHostVar string, isPureHost bool) (map[string]string, bool) {
	baseVals := make(map[string]string, len(vars))
	hasStrictConcrete := false

	for _, v := range vars {
		in, ok := obj.Variables[v]
		pureVar := isPureHostVarName(isPureHost, pureHostVar, v)
		switch {
		case ok && in.Value != "":
			baseVals[v] = in.Value
			hasStrictConcrete = true
		case ok && len(in.Choices) > 0:
			baseVals[v] = in.Choices[0]
			hasStrictConcrete = true
		case ok && in.Default != "" && !pureVar:
			baseVals[v] = in.Default
			hasStrictConcrete = true
		default:
			baseVals[v] = fallbackPlaceholderForVar(obj.URL, v, pureVar)
		}
	}

	return baseVals, hasStrictConcrete
}

func fallbackPlaceholderForVar(rawURL, varName string, isPureHostVar bool) string {
	if isPureHostVar || varName == "host" {
		return "example.com"
	}
	if varName == "port" || strings.Contains(rawURL, ":{"+varName+"}") {
		return "8080"
	}
	if varName == "path" {
		return "api"
	}
	if varName == "protocol" || varName == "scheme" {
		return schemeHTTP
	}
	return genericTemplatePlaceholder
}

func substituteTemplateOverride(rawURL string, base map[string]string, key, val string) string {
	return templateVarTokenRe.ReplaceAllStringFunc(rawURL, func(token string) string {
		name := token[1 : len(token)-1]
		if name == key {
			return val
		}
		if b, ok := base[name]; ok {
			return b
		}
		return genericTemplatePlaceholder
	})
}

func substituteTemplateMap(rawURL string, vals map[string]string) string {
	return templateVarTokenRe.ReplaceAllStringFunc(rawURL, func(token string) string {
		name := token[1 : len(token)-1]
		if v, ok := vals[name]; ok {
			return v
		}
		return genericTemplatePlaceholder
	})
}

// isDisallowedRemoteHost reports whether hostname is disallowed for a remote MCP
// endpoint using offline structural, IP-range, and domain checks.
func isDisallowedRemoteHost(hostname string, templatedHost bool) bool {
	if hostname == "" {
		return true
	}
	hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))
	if hostname == "" || strings.HasSuffix(hostname, ".") {
		return true
	}

	if strings.Contains(hostname, ":") {
		addr, err := netip.ParseAddr(hostname)
		if err != nil {
			return true
		}
		return isDisallowedRemoteIP(addr)
	}

	if addr, err := netip.ParseAddr(hostname); err == nil {
		return isDisallowedRemoteIP(addr)
	}

	labels := strings.Split(hostname, ".")
	if hasEmptyLabel(labels) || matchesDomainSuffixList(hostname, blockedRemoteDomainSuffixes) {
		return true
	}

	if len(labels) == 1 {
		return !templatedHost
	}

	if isLegacyIPv4Labels(labels) || !isValidTLDRootLabel(labels[len(labels)-1]) {
		return true
	}

	if sub, ok := cutWildcardIPDomain(hostname); ok {
		return hasDisallowedWildcardIP(sub)
	}

	return false
}

func hasEmptyLabel(labels []string) bool {
	for _, l := range labels {
		if l == "" {
			return true
		}
	}
	return false
}

func matchesDomainSuffixList(hostname string, suffixes []string) bool {
	for _, s := range suffixes {
		if hostname == s || strings.HasSuffix(hostname, "."+s) {
			return true
		}
	}
	return false
}

func isLegacyIPv4Labels(labels []string) bool {
	if len(labels) == 0 || len(labels) > 4 {
		return false
	}
	for _, l := range labels {
		if !isNumericOrHexPart(l) {
			return false
		}
	}
	return true
}

func isNumericOrHexPart(s string) bool {
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "0x") {
		if len(s) == 2 {
			return false
		}
		for i := 2; i < len(s); i++ {
			c := s[i]
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return false
			}
		}
		return true
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isValidTLDRootLabel(tld string) bool {
	if tld == "" || strings.HasSuffix(tld, "-") {
		return false
	}
	return tld[0] >= 'a' && tld[0] <= 'z'
}

func cutWildcardIPDomain(hostname string) (string, bool) {
	for _, domain := range wildcardIPDomains {
		suffix := "." + domain
		if strings.HasSuffix(hostname, suffix) {
			return strings.TrimSuffix(hostname, suffix), true
		}
	}
	return "", false
}

func hasDisallowedWildcardIP(sub string) bool {
	labels := strings.Split(sub, ".")
	if hasDisallowedDottedIPv4(labels) {
		return true
	}
	for _, label := range labels {
		if hasDisallowedDashedIPv4(label) || hasDisallowedHexIPv4(label) || hasDisallowedDashedIPv6(label) {
			return true
		}
	}
	return false
}

func hasDisallowedDottedIPv4(labels []string) bool {
	for i := 0; i+4 <= len(labels); i++ {
		if addr, ok := parseFourDecimalOctets(labels[i : i+4]); ok && isDisallowedRemoteIP(addr) {
			return true
		}
	}
	return false
}

func hasDisallowedDashedIPv4(label string) bool {
	parts := strings.Split(label, "-")
	for i := 0; i+4 <= len(parts); i++ {
		if addr, ok := parseFourDecimalOctets(parts[i : i+4]); ok && isDisallowedRemoteIP(addr) {
			return true
		}
	}
	return false
}

func parseFourDecimalOctets(parts []string) (netip.Addr, bool) {
	var octets [4]byte
	for i := range 4 {
		p := parts[i]
		if p == "" || len(p) > 3 {
			return netip.Addr{}, false
		}
		v, err := strconv.ParseUint(p, 10, 8)
		if err != nil {
			return netip.Addr{}, false
		}
		octets[i] = byte(v)
	}
	return netip.AddrFrom4(octets), true
}

// hasDisallowedHexIPv4 inspects both the full label and every '-'-delimited
// 8-hex-character token (catching e.g. "0a000001.nip.io", "app-7f000001.nip.io",
// and "7f000001-app.nip.io").
func hasDisallowedHexIPv4(label string) bool {
	for _, token := range strings.Split(label, "-") {
		if len(token) != 8 {
			continue
		}
		b, err := hex.DecodeString(token)
		if err != nil || len(b) != 4 {
			continue
		}
		if isDisallowedRemoteIP(netip.AddrFrom4([4]byte{b[0], b[1], b[2], b[3]})) {
			return true
		}
	}
	return false
}

func hasDisallowedDashedIPv6(label string) bool {
	if !strings.Contains(label, "-") {
		return false
	}
	if addr, ok := parseDashedIPv6(label); ok && isDisallowedRemoteIP(addr) {
		return true
	}
	if strings.HasPrefix(label, "xn--") {
		return false
	}
	for i := 0; i < len(label); i++ {
		if label[i] == '-' {
			if addr, ok := parseDashedIPv6(label[i+1:]); ok && isDisallowedRemoteIP(addr) {
				return true
			}
		}
	}
	return false
}

func parseDashedIPv6(s string) (netip.Addr, bool) {
	if !strings.Contains(s, "-") {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(strings.ReplaceAll(s, "-", ":"))
	if err != nil || !addr.Is6() {
		return netip.Addr{}, false
	}
	return addr, true
}

type remoteHostLookupTarget struct {
	rawURL string
	host   string
}

// validateRemoteURLs performs publish/update-time validation on req.Remotes.
// When allowPrivate is false, concrete remote hostnames (including static
// prefixes before path variables and concrete variable substitutions) are
// resolved with a trailing root dot under bounded concurrency, a cap on unique
// hostnames, and an overall deadline.
func validateRemoteURLs(ctx context.Context, req apiv0.ServerJSON, allowPrivate bool) error {
	if len(req.Remotes) == 0 {
		return nil
	}

	seenHosts := make(map[string]struct{}, len(req.Remotes))
	var targets []remoteHostLookupTarget

	for i := range req.Remotes {
		remote := &req.Remotes[i]
		if remote.URL == "" {
			continue
		}
		if !validateRemoteTransportURL(remote, allowPrivate) {
			return fmt.Errorf("remote %d (%s): %w", i, remote.URL, ErrInvalidRemoteURL)
		}
		if allowPrivate {
			continue
		}
		for _, host := range collectConcreteRemoteDNSLookupHosts(remote) {
			if _, exists := seenHosts[host]; exists {
				continue
			}
			seenHosts[host] = struct{}{}
			targets = append(targets, remoteHostLookupTarget{rawURL: remote.URL, host: host})
		}
	}

	if len(targets) == 0 {
		return nil
	}
	if len(targets) > maxRemoteHostLookupsPerRequest {
		return fmt.Errorf("%w: server specifies more than %d unique remote hostnames",
			ErrInvalidRemoteURL, maxRemoteHostLookupsPerRequest)
	}

	return resolveRemoteHostTargets(ctx, targets)
}

func collectConcreteRemoteDNSLookupHosts(remote *model.Transport) []string {
	var hosts []string
	addHost := func(h string) {
		h = strings.ToLower(strings.TrimSuffix(h, "."))
		if h == "" {
			return
		}
		if _, err := netip.ParseAddr(h); err == nil {
			return
		}
		hosts = append(hosts, h)
	}

	rawHost, ok := rawAuthorityHost(remote.URL)
	pureHostVar, isPureHost := pureAuthorityHostVariable(remote.URL)
	if ok && !strings.Contains(rawHost, "{") {
		if u, err := url.Parse(replaceTemplateVariables(remote.URL)); err == nil {
			addHost(u.Hostname())
		}
	} else if stripped, ok := strippedStaticAuthorityHost(remote.URL, isPureHost); ok {
		addHost(stripped)
	}

	if !allAuthorityVarsHaveStrictValues(remote, pureHostVar, isPureHost) {
		return hosts
	}

	strictURLs, _ := instantiateRemoteVariableURLs(remote)
	for _, uStr := range strictURLs {
		if u, err := url.Parse(uStr); err == nil {
			addHost(u.Hostname())
		}
	}

	return hosts
}

func allAuthorityVarsHaveStrictValues(remote *model.Transport, pureHostVar string, isPureHost bool) bool {
	authVars := extractSchemeAndAuthorityVariables(remote.URL)
	if len(authVars) == 0 {
		return false
	}
	for _, v := range authVars {
		in, ok := remote.Variables[v]
		if !ok {
			return false
		}
		hasStrict := in.Value != "" || len(in.Choices) > 0 || (in.Default != "" && !isPureHostVarName(isPureHost, pureHostVar, v))
		if !hasStrict {
			return false
		}
	}
	return true
}

func resolveRemoteHostTargets(ctx context.Context, targets []remoteHostLookupTarget) error {
	overallCtx, cancel := context.WithTimeout(ctx, remoteDNSOverallTimeout)
	defer cancel()

	sem := make(chan struct{}, remoteDNSLookupConcurrency)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)

	for _, target := range targets {
		if overallCtx.Err() != nil {
			break
		}
		mu.Lock()
		failed := firstErr != nil
		mu.Unlock()
		if failed {
			break
		}

		select {
		case sem <- struct{}{}:
		case <-overallCtx.Done():
			break
		}
		if overallCtx.Err() != nil {
			break
		}

		wg.Add(1)
		go func(t remoteHostLookupTarget) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := checkRemoteHostResolution(overallCtx, t.rawURL, t.host); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
			}
		}(target)
	}

	wg.Wait()
	return firstErr
}

func checkRemoteHostResolution(ctx context.Context, rawURL, host string) error {
	lookupCtx, cancel := context.WithTimeout(ctx, remoteDNSLookupTimeout)
	defer cancel()

	// Append a trailing root dot so Go's resolver sends a single rooted query
	// rather than expanding Kubernetes ndots:5 search domains (.svc.cluster.local,
	// .google.internal).
	fqdn := strings.TrimSuffix(host, ".") + "."
	ips, err := defaultHostResolver.LookupIPAddr(lookupCtx, fqdn)
	if err != nil {
		return nil
	}
	for _, ipAddr := range ips {
		addr, ok := netip.AddrFromSlice(ipAddr.IP)
		if !ok || isDisallowedRemoteIP(addr) {
			// Do not echo the resolved IP address back to the caller, as that
			// would act as an internal DNS / Pod-IP disclosure oracle.
			return fmt.Errorf("%w: remote URL %s resolves to a disallowed non-public IP address",
				ErrInvalidRemoteURL, rawURL)
		}
	}
	return nil
}
