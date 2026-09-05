package validators

import (
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
)

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

var (
	// Regular expressions for validating repository URLs
	// These regex patterns ensure the URL is in the format of a valid GitHub or GitLab repository
	// For example:	// - GitHub: https://github.com/user/repo
	githubURLRegex = regexp.MustCompile(`^https?://(www\.)?github\.com/[\w.-]+/[\w.-]+/?$`)
	gitlabURLRegex = regexp.MustCompile(`^https?://(www\.)?gitlab\.com/[\w.-]+/[\w.-]+/?$`)
)

// IsValidRepositoryURL checks if the given URL is valid for the specified repository source
func IsValidRepositoryURL(source RepositorySource, url string) bool {
	switch source {
	case SourceGitHub:
		return githubURLRegex.MatchString(url)
	case SourceGitLab:
		return gitlabURLRegex.MatchString(url)
	}
	return false
}

// HasNoSpaces checks if a string contains no spaces
func HasNoSpaces(s string) bool {
	return !strings.Contains(s, " ")
}

// extractTemplateVariables extracts template variables from a URL string
// e.g., "http://{host}:{port}/mcp" returns ["host", "port"]
func extractTemplateVariables(url string) []string {
	re := regexp.MustCompile(`\{([^}]+)\}`)
	matches := re.FindAllStringSubmatch(url, -1)

	var variables []string
	for _, match := range matches {
		if len(match) > 1 {
			variables = append(variables, match[1])
		}
	}
	return variables
}

// replaceTemplateVariables replaces template variables with placeholder values for URL validation
func replaceTemplateVariables(rawURL string) string {
	// Replace common template variables with valid placeholder values for parsing
	templateReplacements := map[string]string{
		"{host}":     "example.com",
		"{port}":     "8080",
		"{path}":     "api",
		"{protocol}": schemeHTTP,
		"{scheme}":   schemeHTTP,
	}

	result := rawURL
	for placeholder, replacement := range templateReplacements {
		result = strings.ReplaceAll(result, placeholder, replacement)
	}

	// Handle any remaining {variable} patterns with context-appropriate placeholders
	// If the variable is in a port position (after a colon in the host), use a numeric placeholder
	// Pattern: :/{variable} or :{variable}/ or :{variable} at end
	portRe := regexp.MustCompile(`:(\{[^}]+\})(/|$)`)
	result = portRe.ReplaceAllString(result, ":8080$2")

	// Replace any other remaining {variable} patterns with generic placeholder
	re := regexp.MustCompile(`\{[^}]+\}`)
	result = re.ReplaceAllString(result, "placeholder")

	return result
}

// IsValidURL checks if a URL is in valid format (basic structure validation)
func IsValidURL(rawURL string) bool {
	// Replace template variables with placeholders for parsing
	testURL := replaceTemplateVariables(rawURL)

	// Parse the URL
	u, err := url.Parse(testURL)
	if err != nil {
		return false
	}

	// Check if scheme is present (http or https)
	if u.Scheme != schemeHTTP && u.Scheme != schemeHTTPS {
		return false
	}

	if u.Host == "" {
		return false
	}
	return true
}

// IsValidSubfolderPath checks if a subfolder path is valid
func IsValidSubfolderPath(path string) bool {
	// Empty path is valid (subfolder is optional)
	if path == "" {
		return true
	}

	// Must not start with / (must be relative)
	if strings.HasPrefix(path, "/") {
		return false
	}

	// Must not end with / (clean path format)
	if strings.HasSuffix(path, "/") {
		return false
	}

	// Check for valid path characters (alphanumeric, dash, underscore, dot, forward slash)
	validPathRegex := regexp.MustCompile(`^[a-zA-Z0-9\-_./]+$`)
	if !validPathRegex.MatchString(path) {
		return false
	}

	// Check that path segments are valid
	segments := strings.Split(path, "/")
	for _, segment := range segments {
		// Disallow empty segments ("//"), current dir ("."), and parent dir ("..")
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}

	return true
}

// IsValidRemoteURL checks if a URL is valid for remotes (stricter than packages - no localhost allowed)
func IsValidRemoteURL(rawURL string) bool {
	// First check basic URL structure
	if !IsValidURL(rawURL) {
		return false
	}

	// Replace template variables with placeholders before parsing for localhost check
	testURL := replaceTemplateVariables(rawURL)

	// Parse the URL to check for localhost restriction
	u, err := url.Parse(testURL)
	if err != nil {
		return false
	}

	// Reject localhost/loopback/unspecified/private/link-local hosts for remotes
	// (security/production concerns - a remote must point at a real, publicly
	// reachable, non-internal endpoint).
	if isDisallowedRemoteHost(u.Hostname()) {
		return false
	}

	if u.Scheme != schemeHTTPS {
		return false
	}

	return true
}

// cgnat is RFC 6598 shared address space. It is not routable on the public
// internet, but net.IP.IsPrivate covers only RFC1918 and fc00::/7.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// isDisallowedRemoteHost reports whether hostname is not allowed as a remote
// URL host because it is not a publicly reachable unicast address.
//
// hostname may be an IP literal - including bracketed IPv6 forms already
// stripped of their brackets by url.URL.Hostname (e.g. "::1"), and
// IPv4-mapped IPv6 forms (e.g. "::ffff:127.0.0.1") - or a DNS name. IP
// literals are checked with net.ParseIP and net.IP's classification methods
// so every notation for loopback/private/link-local addresses is caught, not
// just the literal strings "127.0.0.1" and "::1" the previous check used.
// DNS names fall back to the pre-existing "localhost" / "*.localhost" string
// checks, since resolving them here would require a network round trip
// during validation.
func isDisallowedRemoteHost(hostname string) bool {
	// DNS is case-insensitive and a trailing root dot is equivalent to its
	// absence, so "LOCALHOST" and "localhost." both reach loopback.
	hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))

	if ip := net.ParseIP(hostname); ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsPrivate() ||
			ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
			return true
		}
		addr, ok := netip.AddrFromSlice(ip)
		return ok && cgnat.Contains(addr.Unmap())
	}

	return hostname == "localhost" || strings.HasSuffix(hostname, ".localhost")
}

// IsValidTemplatedURL validates a URL with template variables against available variables
// For packages: validates that template variables reference package arguments or environment variables
// For remotes: validates that template variables reference the transport's variables map
func IsValidTemplatedURL(rawURL string, availableVariables []string) bool {
	// First check basic URL structure
	if !IsValidURL(rawURL) {
		return false
	}

	// Extract template variables from URL
	templateVars := extractTemplateVariables(rawURL)

	// If no templates are found, it's a valid static URL
	if len(templateVars) == 0 {
		return true
	}

	// Validate that all template variables are available
	availableSet := make(map[string]bool)
	for _, v := range availableVariables {
		availableSet[v] = true
	}

	for _, templateVar := range templateVars {
		if !availableSet[templateVar] {
			return false
		}
	}

	return true
}
