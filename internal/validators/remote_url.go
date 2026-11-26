package validators

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// remoteDNSLookupTimeout bounds per-hostname DNS resolution during publish and
// update validation so a slow authoritative nameserver cannot stall requests.
const remoteDNSLookupTimeout = 3 * time.Second

// HostResolver resolves a DNS hostname to IP addresses during publish/update validation.
type HostResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

var defaultHostResolver HostResolver = net.DefaultResolver

var (
	// cgnatPrefix is RFC 6598 shared address space (100.64.0.0/10), which is not
	// publicly routable and is not covered by netip.Addr.IsPrivate.
	cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

	// thisNetPrefix is RFC 1122 "this host on this network" (0.0.0.0/8). Many
	// stacks route 0.x.x.x to local loopback, while IsUnspecified only checks 0.0.0.0.
	thisNetPrefix = netip.MustParsePrefix("0.0.0.0/8")

	ipv4Broadcast = netip.AddrFrom4([4]byte{255, 255, 255, 255})

	// blockedRemoteIPv6Prefixes covers IPv6 transition mechanisms that embed or
	// tunnel arbitrary IPv4 addresses and deprecated site-local ranges not covered
	// by netip.Addr's built-in Is* classifiers:
	//   2002::/16       RFC 3056 6to4 (bits 16-47 embed IPv4)
	//   2001::/32       RFC 4380 Teredo (low 32 bits embed obfuscated IPv4)
	//   64:ff9b::/96    RFC 6052 NAT64 well-known prefix (low 32 bits embed IPv4)
	//   64:ff9b:1::/48  RFC 8215 NAT64 local-use prefix
	//   ::/96           RFC 4291 deprecated IPv4-compatible prefix (low 32 bits embed IPv4)
	//   fec0::/10       RFC 3879 deprecated site-local prefix
	blockedRemoteIPv6Prefixes = []netip.Prefix{
		netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("2001::/32"),
		netip.MustParsePrefix("64:ff9b::/96"),
		netip.MustParsePrefix("64:ff9b:1::/48"),
		netip.MustParsePrefix("::/96"),
		netip.MustParsePrefix("fec0::/10"),
	}

	// blockedRemoteDomainSuffixes lists special-use / private TLDs and well-known
	// public wildcard domains that unconditionally resolve to loopback or internal
	// metadata endpoints.
	blockedRemoteDomainSuffixes = []string{
		"localhost",
		"internal",
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

// isDisallowedRemoteIP reports whether addr is non-global, host-scoped, or in a
// private/loopback/link-local/CGNAT/transition range unsuitable for a public
// MCP remote server. IPv4-mapped IPv6 addresses (::ffff:0:0/96) are unmapped
// so all IPv4 checks apply to the underlying address.
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
		return cgnatPrefix.Contains(unmapped) || thisNetPrefix.Contains(unmapped) || unmapped == ipv4Broadcast
	}
	return false
}

// rawURLHasTemplatedHostname reports whether the host component of rawURL (as
// distinct from scheme, port, or path) contains a {variable} placeholder.
func rawURLHasTemplatedHostname(rawURL string) bool {
	schemeSep := strings.Index(rawURL, "://")
	if schemeSep < 0 {
		return false
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
	return len(extractTemplateVariables(host)) > 0
}

// isDisallowedRemoteHost reports whether hostname is disallowed for a remote MCP
// endpoint using offline structural, IP-range, and domain checks.
func isDisallowedRemoteHost(hostname string, templatedHost bool) bool {
	if hostname == "" {
		return true
	}
	// DNS is case-insensitive and a single trailing root dot is equivalent to its absence.
	hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))
	if hostname == "" || strings.HasSuffix(hostname, ".") {
		return true
	}

	// Colons are never valid in DNS names; any host containing ':' must parse as
	// a valid, zone-free global IPv6 address (catches percent-encoded zone IDs
	// such as fe80::1%25eth0 that net.ParseIP/netip.ParseAddr reject).
	if strings.Contains(hostname, ":") {
		addr, err := netip.ParseAddr(hostname)
		if err != nil {
			return true
		}
		return isDisallowedRemoteIP(addr)
	}

	// Canonical dotted-decimal IPv4 literals (netip.ParseAddr rejects octal, hex,
	// and shorthand forms, which fall through and are rejected below).
	if addr, err := netip.ParseAddr(hostname); err == nil {
		return isDisallowedRemoteIP(addr)
	}

	labels := strings.Split(hostname, ".")
	if hasEmptyLabel(labels) || matchesDomainSuffixList(hostname, blockedRemoteDomainSuffixes) {
		return true
	}

	// Single-label hostnames (e.g. "YOUR_DOMAIN", "HERMES_MCP_HOST", integer IPv4
	// "2130706433", hex IPv4 "0x7f000001") are never valid public FQDNs. Allow a
	// single label only when replaceTemplateVariables substituted a host template
	// such as "https://{custom_host}/mcp" -> "placeholder".
	if len(labels) == 1 {
		return !templatedHost
	}

	// Reject shorthand/octal/hex IPv4 forms (e.g. "127.1", "0177.0.0.1",
	// "0x7f.0.0.1") and any multi-label host whose TLD does not conform to
	// ICANN/RFC 3696 alphabetic-start TLD rules.
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

// isLegacyIPv4Labels reports whether 1..4 dot-separated labels are all decimal,
// octal, or 0x-hex numbers (BSD inet_aton / WHATWG non-canonical IPv4 forms).
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

// isValidTLDRootLabel checks that the rightmost DNS label begins with an ASCII
// letter (covering both standard alphabetic TLDs and "xn--" punycode IDN TLDs)
// and does not end with a hyphen.
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

func hasDisallowedHexIPv4(label string) bool {
	cand := label
	if len(cand) != 8 {
		idx := strings.LastIndexByte(label, '-')
		if idx < 0 || len(label[idx+1:]) != 8 {
			return false
		}
		cand = label[idx+1:]
	}
	b, err := hex.DecodeString(cand)
	if err != nil || len(b) != 4 {
		return false
	}
	return isDisallowedRemoteIP(netip.AddrFrom4([4]byte{b[0], b[1], b[2], b[3]}))
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

// validateRemoteURLs performs publish/update-time validation on req.Remotes,
// including DNS resolution of non-templated remote hostnames to reject domains
// that resolve to loopback, private, link-local, CGNAT, or transition IPs.
//
// Hostnames that do not resolve (e.g. NXDOMAIN on unpropagated domains or
// RFC 2606 test fixtures) or fail due to transient DNS errors are not blocked.
func validateRemoteURLs(ctx context.Context, req apiv0.ServerJSON) error {
	if len(req.Remotes) == 0 {
		return nil
	}
	checkedHosts := make(map[string]struct{}, len(req.Remotes))
	for i, remote := range req.Remotes {
		if remote.URL == "" {
			continue
		}
		if !IsValidRemoteURL(remote.URL) {
			return fmt.Errorf("remote %d (%s): %w", i, remote.URL, ErrInvalidRemoteURL)
		}
		if rawURLHasTemplatedHostname(remote.URL) {
			continue
		}
		u, err := url.Parse(replaceTemplateVariables(remote.URL))
		if err != nil {
			return fmt.Errorf("remote %d (%s): %w", i, remote.URL, ErrInvalidRemoteURL)
		}
		host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
		if _, err := netip.ParseAddr(host); err == nil {
			continue
		}
		if _, seen := checkedHosts[host]; seen {
			continue
		}
		checkedHosts[host] = struct{}{}

		if err := checkRemoteHostResolution(ctx, remote.URL, host); err != nil {
			return err
		}
	}
	return nil
}

func checkRemoteHostResolution(ctx context.Context, rawURL, host string) error {
	lookupCtx, cancel := context.WithTimeout(ctx, remoteDNSLookupTimeout)
	defer cancel()

	ips, err := defaultHostResolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return nil
	}
	for _, ipAddr := range ips {
		addr, ok := netip.AddrFromSlice(ipAddr.IP)
		if !ok || isDisallowedRemoteIP(addr) {
			return fmt.Errorf("%w: remote URL %s (%s) resolves to disallowed IP address %s",
				ErrInvalidRemoteURL, rawURL, host, ipAddr.IP.String())
		}
	}
	return nil
}
