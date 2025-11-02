package validators

import (
	"net/url"
	"regexp"
	"strings"
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
		"{protocol}": "http",
		"{scheme}":   "http",
	}

	result := rawURL
	for placeholder, replacement := range templateReplacements {
		result = strings.ReplaceAll(result, placeholder, replacement)
	}

	// Handle any remaining {variable} patterns with generic placeholder
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
	if u.Scheme != "http" && u.Scheme != "https" {
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

	// Parse the URL to check for localhost restriction
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// Reject localhost URLs for remotes (security/production concerns)
	hostname := u.Hostname()
	if hostname == "localhost" || hostname == "127.0.0.1" || strings.HasSuffix(hostname, ".localhost") {
		return false
	}

	return true
}

// IsValidTemplatedURL validates a URL with template variables against available variables
// For packages: validates that template variables reference package arguments or environment variables
// For remotes: disallows template variables entirely
func IsValidTemplatedURL(rawURL string, availableVariables []string, allowTemplates bool) bool {
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

	// If templates are not allowed (e.g., for remotes), reject URLs with templates
	if !allowTemplates {
		return false
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
