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

// IsValidURL checks if a URL is in valid format
func IsValidURL(rawURL string) bool {
	// Parse the URL
	u, err := url.Parse(rawURL)
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
