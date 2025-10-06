package validators

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/registry/internal/config"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// Server name validation patterns
var (
	// Component patterns for namespace and name parts
	namespacePattern = `[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]`
	namePartPattern  = `[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]`

	// Compiled regexes
	namespaceRegex  = regexp.MustCompile(`^` + namespacePattern + `$`)
	namePartRegex   = regexp.MustCompile(`^` + namePartPattern + `$`)
	serverNameRegex = regexp.MustCompile(`^` + namespacePattern + `/` + namePartPattern + `$`)
)

// Regexes to detect semver range syntaxes
var (
	// Case 1: comparator ranges
	// - "^1.2.3",
	// - "~1.2.3",
	// - ">=1.0.0",
	// - "<=1.0.0",
	// - ">1.0.0",
	// - "<1.0.0",
	// - "=1.0.0",
	comparatorRangeRe = regexp.MustCompile(`^\s*(?:\^|~|>=|<=|>|<|=)\s*v?\d+(?:\.\d+){0,3}(?:-[0-9A-Za-z.-]+)?\s*$`)
	// Case 2: hyphen ranges
	// - "1.2.3 - 2.0.0",
	hyphenRangeRe = regexp.MustCompile(`^\s*v?\d+(?:\.\d+){0,3}(?:-[0-9A-Za-z.-]+)?\s-\s*v?\d+(?:\.\d+){0,3}(?:-[0-9A-Za-z.-]+)?\s*$`)
	// Case 3: OR ranges
	// - "1.2 || 1.3",
	orRangeRe = regexp.MustCompile(`^\s*(?:v?\d+(?:\.\d+){0,3}(?:-[0-9A-Za-z.-]+)?\s*)(?:\|\|\s*v?\d+(?:\.\d+){0,3}(?:-[0-9A-Za-z.-]+)?\s*)+$`)
	// Case 4: dotted version wildcards
	// - "1.2.*",
	// - "1.2.x",
	// - "1.2.X",
	// - "1.x",
	// etc.
	dottedVersionLikeRe = regexp.MustCompile(`^\s*(?:v?\d+|x|X|\*)(?:\.(?:\d+|x|X|\*)){1,2}(?:-[0-9A-Za-z.-]+)?\s*$`)
)

func ValidateServerJSON(serverJSON *apiv0.ServerJSON) error {
	// Validate schema version is provided and supported
	// Note: Schema field is also marked as required in the ServerJSON struct definition
	// for API-level validation and documentation
	if serverJSON.Schema == "" {
		return fmt.Errorf("$schema field is required")
	}
	if !strings.Contains(serverJSON.Schema, model.CurrentSchemaVersion) {
		return fmt.Errorf("schema version %s is not supported. Please use schema version %s", serverJSON.Schema, model.CurrentSchemaVersion)
	}

	// Validate server name exists and format
	if _, err := parseServerName(*serverJSON); err != nil {
		return err
	}

	// Validate top-level server version is a specific version (not a range) & not "latest"
	if err := validateVersion(serverJSON.Version); err != nil {
		return err
	}

	// Validate repository
	if err := validateRepository(&serverJSON.Repository); err != nil {
		return err
	}

	// Validate website URL if provided
	if err := validateWebsiteURL(serverJSON.WebsiteURL); err != nil {
		return err
	}

	// Validate title if provided
	if err := validateTitle(serverJSON.Title); err != nil {
		return err
	}

	// Validate icons if provided
	if err := validateIcons(serverJSON.Icons); err != nil {
		return err
	}

	// Validate all packages (basic field validation)
	// Detailed package validation (including registry checks) is done during publish
	for _, pkg := range serverJSON.Packages {
		if err := validatePackageField(&pkg); err != nil {
			return err
		}
	}

	// Validate all remotes
	for _, remote := range serverJSON.Remotes {
		if err := validateRemoteTransport(&remote); err != nil {
			return err
		}
	}

	// Validate reverse-DNS namespace matching for remote URLs
	if err := validateRemoteNamespaceMatch(*serverJSON); err != nil {
		return err
	}

	// Validate reverse-DNS namespace matching for website URL
	if err := validateWebsiteURLNamespaceMatch(*serverJSON); err != nil {
		return err
	}

	return nil
}

func validateRepository(obj *model.Repository) error {
	// Skip validation for empty repository (optional field)
	if obj.URL == "" && obj.Source == "" {
		return nil
	}

	// validate the repository source
	repoSource := RepositorySource(obj.Source)
	if !IsValidRepositoryURL(repoSource, obj.URL) {
		return fmt.Errorf("%w: %s", ErrInvalidRepositoryURL, obj.URL)
	}

	// validate subfolder if present
	if obj.Subfolder != "" && !IsValidSubfolderPath(obj.Subfolder) {
		return fmt.Errorf("%w: %s", ErrInvalidSubfolderPath, obj.Subfolder)
	}

	return nil
}

func validateWebsiteURL(websiteURL string) error {
	// Skip validation if website URL is not provided (optional field)
	if websiteURL == "" {
		return nil
	}

	// Parse the URL to ensure it's valid
	parsedURL, err := url.Parse(websiteURL)
	if err != nil {
		return fmt.Errorf("invalid websiteUrl: %w", err)
	}

	// Ensure it's an absolute URL with valid scheme
	if !parsedURL.IsAbs() {
		return fmt.Errorf("websiteUrl must be absolute (include scheme): %s", websiteURL)
	}

	// Only allow HTTPS scheme for security
	if parsedURL.Scheme != SchemeHTTPS {
		return fmt.Errorf("websiteUrl must use https scheme: %s", websiteURL)
	}

	return nil
}

func validateTitle(title string) error {
	// Skip validation if title is not provided (optional field)
	if title == "" {
		return nil
	}

	// Check that title is not only whitespace
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("title cannot be only whitespace")
	}

	return nil
}

func validateIcons(icons []model.Icon) error {
	// Skip validation if no icons are provided (optional field)
	if len(icons) == 0 {
		return nil
	}

	// Validate each icon
	for i, icon := range icons {
		if err := validateIcon(&icon); err != nil {
			return fmt.Errorf("invalid icon at index %d: %w", i, err)
		}
	}

	return nil
}

func validateIcon(icon *model.Icon) error {
	// Parse the URL to ensure it's valid
	parsedURL, err := url.Parse(icon.Src)
	if err != nil {
		return fmt.Errorf("invalid icon src URL: %w", err)
	}

	// Ensure it's an absolute URL
	if !parsedURL.IsAbs() {
		return fmt.Errorf("icon src must be an absolute URL (include scheme): %s", icon.Src)
	}

	// Only allow HTTPS scheme for security (no HTTP or data: URIs)
	if parsedURL.Scheme != SchemeHTTPS {
		return fmt.Errorf("icon src must use https scheme (got %s): %s", parsedURL.Scheme, icon.Src)
	}

	return nil
}

func validatePackageField(obj *model.Package) error {
	if !HasNoSpaces(obj.Identifier) {
		return ErrPackageNameHasSpaces
	}

	// Validate version string
	if err := validateVersion(obj.Version); err != nil {
		return err
	}

	// Validate runtime arguments
	for _, arg := range obj.RuntimeArguments {
		if err := validateArgument(&arg); err != nil {
			return fmt.Errorf("invalid runtime argument: %w", err)
		}
	}

	// Validate package arguments
	for _, arg := range obj.PackageArguments {
		if err := validateArgument(&arg); err != nil {
			return fmt.Errorf("invalid package argument: %w", err)
		}
	}

	// Validate transport with template variable support
	availableVariables := collectAvailableVariables(obj)
	if err := validatePackageTransport(&obj.Transport, availableVariables); err != nil {
		return fmt.Errorf("invalid transport: %w", err)
	}

	return nil
}

// validateVersion validates the version string.
// NB: we decided that we would not enforce strict semver for version strings
func validateVersion(version string) error {
	if version == "latest" {
		return ErrReservedVersionString
	}

	// Reject semver range-like inputs
	if looksLikeVersionRange(version) {
		return fmt.Errorf("%w: %q", ErrVersionLooksLikeRange, version)
	}

	return nil
}

// looksLikeVersionRange detects common semver range syntaxes and wildcard patterns.
// that indicate the value is not a single, specific version.
// Examples that should return true:
// - "^1.2.3",
// - "~1.2.3",
// - ">=1.0.0",
// - "1.x",
// - "1.2.*",
// - "1 - 2",
// - "1.2 || 1.3"
func looksLikeVersionRange(version string) bool {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return false
	}

	if comparatorRangeRe.MatchString(trimmed) {
		return true
	}
	if hyphenRangeRe.MatchString(trimmed) {
		return true
	}
	if orRangeRe.MatchString(trimmed) {
		return true
	}
	if dottedVersionLikeRe.MatchString(trimmed) {
		// wildcard in a dotted version (x/X/*) implies range-like intent
		return strings.Contains(trimmed, "x") || strings.Contains(trimmed, "X") || strings.Contains(trimmed, "*")
	}
	return false
}

// validateArgument validates argument details
func validateArgument(obj *model.Argument) error {
	if obj.Type == model.ArgumentTypeNamed {
		// Validate named argument name format
		if err := validateNamedArgumentName(obj.Name); err != nil {
			return err
		}

		// Validate value and default don't start with the name
		if err := validateArgumentValueFields(obj.Name, obj.Value, obj.Default); err != nil {
			return err
		}
	}
	return nil
}

func validateNamedArgumentName(name string) error {
	// Check if name is required for named arguments
	if name == "" {
		return ErrNamedArgumentNameRequired
	}

	// Check for invalid characters that suggest embedded values or descriptions
	// Valid: "--directory", "--port", "-v", "config", "verbose"
	// Invalid: "--directory <absolute_path_to_adfin_mcp_folder>", "--port 8080"
	if strings.Contains(name, "<") || strings.Contains(name, ">") ||
		strings.Contains(name, " ") || strings.Contains(name, "$") {
		return fmt.Errorf("%w: %s", ErrInvalidNamedArgumentName, name)
	}

	return nil
}

func validateArgumentValueFields(name, value, defaultValue string) error {
	// Check if value starts with the argument name (using startsWith, not contains)
	if value != "" && strings.HasPrefix(value, name) {
		return fmt.Errorf("%w: value starts with argument name '%s': %s", ErrArgumentValueStartsWithName, name, value)
	}

	if defaultValue != "" && strings.HasPrefix(defaultValue, name) {
		return fmt.Errorf("%w: default starts with argument name '%s': %s", ErrArgumentDefaultStartsWithName, name, defaultValue)
	}

	return nil
}

// collectAvailableVariables collects all available template variables from a package
func collectAvailableVariables(pkg *model.Package) []string {
	var variables []string

	// Add environment variable names
	for _, env := range pkg.EnvironmentVariables {
		variables = append(variables, env.Name)
	}

	// Add runtime argument names and value hints
	for _, arg := range pkg.RuntimeArguments {
		if arg.Name != "" {
			variables = append(variables, arg.Name)
		}
		if arg.ValueHint != "" {
			variables = append(variables, arg.ValueHint)
		}
	}

	// Add package argument names and value hints
	for _, arg := range pkg.PackageArguments {
		if arg.Name != "" {
			variables = append(variables, arg.Name)
		}
		if arg.ValueHint != "" {
			variables = append(variables, arg.ValueHint)
		}
	}

	return variables
}

// validatePackageTransport validates a package's transport with templating support
func validatePackageTransport(transport *model.Transport, availableVariables []string) error {
	// Validate transport type is supported
	switch transport.Type {
	case model.TransportTypeStdio:
		// Validate that URL is empty for stdio transport
		if transport.URL != "" {
			return fmt.Errorf("url must be empty for %s transport type, got: %s", transport.Type, transport.URL)
		}
		return nil
	case model.TransportTypeStreamableHTTP, model.TransportTypeSSE:
		// URL is required for streamable-http and sse
		if transport.URL == "" {
			return fmt.Errorf("url is required for %s transport type", transport.Type)
		}
		// Validate URL format with template variable support
		if !IsValidTemplatedURL(transport.URL, availableVariables, true) {
			// Check if it's a template variable issue or basic URL issue
			templateVars := extractTemplateVariables(transport.URL)
			if len(templateVars) > 0 {
				return fmt.Errorf("%w: template variables in URL %s reference undefined variables. Available variables: %v",
					ErrInvalidRemoteURL, transport.URL, availableVariables)
			}
			return fmt.Errorf("%w: %s", ErrInvalidRemoteURL, transport.URL)
		}
		return nil
	default:
		return fmt.Errorf("unsupported transport type: %s", transport.Type)
	}
}

// validateRemoteTransport validates a remote transport (no templating allowed)
func validateRemoteTransport(obj *model.Transport) error {
	// Validate transport type is supported - remotes only support streamable-http and sse
	switch obj.Type {
	case model.TransportTypeStreamableHTTP, model.TransportTypeSSE:
		// URL is required for streamable-http and sse
		if obj.URL == "" {
			return fmt.Errorf("url is required for %s transport type", obj.Type)
		}
		// Validate URL format (no templates allowed for remotes, no localhost)
		if !IsValidRemoteURL(obj.URL) {
			return fmt.Errorf("%w: %s", ErrInvalidRemoteURL, obj.URL)
		}
		return nil
	default:
		return fmt.Errorf("unsupported transport type for remotes: %s (only streamable-http and sse are supported)", obj.Type)
	}
}

// ValidatePublishRequest validates a complete publish request including extensions
func ValidatePublishRequest(ctx context.Context, req apiv0.ServerJSON, cfg *config.Config) error {
	// Validate publisher extensions in _meta
	if err := validatePublisherExtensions(req); err != nil {
		return err
	}

	// Validate the server detail (includes all nested validation)
	if err := ValidateServerJSON(&req); err != nil {
		return err
	}

	// Validate registry ownership for all packages if validation is enabled
	if cfg.EnableRegistryValidation {
		for i, pkg := range req.Packages {
			if err := ValidatePackage(ctx, pkg, req.Name); err != nil {
				return fmt.Errorf("registry validation failed for package %d (%s): %w", i, pkg.Identifier, err)
			}
		}
	}

	return nil
}

func validatePublisherExtensions(req apiv0.ServerJSON) error {
	const maxExtensionSize = 4 * 1024 // 4KB limit

	// Check size limit for _meta publisher-provided extension
	if req.Meta != nil && req.Meta.PublisherProvided != nil {
		extensionsJSON, err := json.Marshal(req.Meta.PublisherProvided)
		if err != nil {
			return fmt.Errorf("failed to marshal _meta.io.modelcontextprotocol.registry/publisher-provided extension: %w", err)
		}
		if len(extensionsJSON) > maxExtensionSize {
			return fmt.Errorf("_meta.io.modelcontextprotocol.registry/publisher-provided extension exceeds 4KB limit (%d bytes)", len(extensionsJSON))
		}
	}

	// Note: ServerJSON._meta only contains PublisherProvided data
	// Official registry metadata is handled separately in the response structure

	return nil
}

func parseServerName(serverJSON apiv0.ServerJSON) (string, error) {
	name := serverJSON.Name
	if name == "" {
		return "", fmt.Errorf("server name is required and must be a string")
	}

	// Validate format: dns-namespace/name
	if !strings.Contains(name, "/") {
		return "", fmt.Errorf("server name must be in format 'dns-namespace/name' (e.g., 'com.example.api/server')")
	}

	// Check for multiple slashes - reject if found
	slashCount := strings.Count(name, "/")
	if slashCount > 1 {
		return "", ErrMultipleSlashesInServerName
	}

	// Split and check for empty parts
	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("server name must be in format 'dns-namespace/name' with non-empty namespace and name parts")
	}

	// Validate name format using regex
	if !serverNameRegex.MatchString(name) {
		namespace := parts[0]
		serverName := parts[1]

		// Check which part is invalid for a better error message
		if !namespaceRegex.MatchString(namespace) {
			return "", fmt.Errorf("%w: namespace '%s' is invalid. Namespace must start and end with alphanumeric characters, and may contain dots and hyphens in the middle", ErrInvalidServerNameFormat, namespace)
		}
		if !namePartRegex.MatchString(serverName) {
			return "", fmt.Errorf("%w: name '%s' is invalid. Name must start and end with alphanumeric characters, and may contain dots, underscores, and hyphens in the middle", ErrInvalidServerNameFormat, serverName)
		}
		// Fallback in case both somehow pass individually but not together
		return "", fmt.Errorf("%w: invalid format for '%s'", ErrInvalidServerNameFormat, name)
	}

	return name, nil
}

// validateRemoteNamespaceMatch validates that remote URLs match the reverse-DNS namespace
func validateRemoteNamespaceMatch(serverJSON apiv0.ServerJSON) error {
	namespace := serverJSON.Name

	for _, remote := range serverJSON.Remotes {
		if err := validateRemoteURLMatchesNamespace(remote.URL, namespace); err != nil {
			return fmt.Errorf("remote URL %s does not match namespace %s: %w", remote.URL, namespace, err)
		}
	}

	return nil
}

// validateWebsiteURLNamespaceMatch validates that website URL matches the reverse-DNS namespace
func validateWebsiteURLNamespaceMatch(serverJSON apiv0.ServerJSON) error {
	// Skip validation if website URL is not provided
	if serverJSON.WebsiteURL == "" {
		return nil
	}

	namespace := serverJSON.Name
	if err := validateRemoteURLMatchesNamespace(serverJSON.WebsiteURL, namespace); err != nil {
		return fmt.Errorf("websiteUrl %s does not match namespace %s: %w", serverJSON.WebsiteURL, namespace, err)
	}

	return nil
}

// validateRemoteURLMatchesNamespace checks if a remote URL's hostname matches the publisher domain from the namespace
func validateRemoteURLMatchesNamespace(remoteURL, namespace string) error {
	// Parse the URL to extract the hostname
	parsedURL, err := url.Parse(remoteURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	hostname := parsedURL.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL must have a valid hostname")
	}

	// Skip validation for localhost and local development URLs
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || hostname == "127.0.0.1" {
		return nil
	}

	// Extract publisher domain from reverse-DNS namespace
	publisherDomain := extractPublisherDomainFromNamespace(namespace)
	if publisherDomain == "" {
		return fmt.Errorf("invalid namespace format: cannot extract domain from %s", namespace)
	}

	// Check if the remote URL hostname matches the publisher domain or is a subdomain
	if !isValidHostForDomain(hostname, publisherDomain) {
		return fmt.Errorf("remote URL host %s does not match publisher domain %s", hostname, publisherDomain)
	}

	return nil
}

// extractPublisherDomainFromNamespace converts reverse-DNS namespace to normal domain format
// e.g., "com.example" -> "example.com"
func extractPublisherDomainFromNamespace(namespace string) string {
	// Extract the namespace part before the first slash
	namespacePart := namespace
	if slashIdx := strings.Index(namespace, "/"); slashIdx != -1 {
		namespacePart = namespace[:slashIdx]
	}

	// Split into parts and reverse them to get normal domain format
	parts := strings.Split(namespacePart, ".")
	if len(parts) < 2 {
		return ""
	}

	// Reverse the parts to convert from reverse-DNS to normal domain
	slices.Reverse(parts)

	return strings.Join(parts, ".")
}

// isValidHostForDomain checks if a hostname is the domain or a subdomain of the publisher domain
func isValidHostForDomain(hostname, publisherDomain string) bool {
	// Exact match
	if hostname == publisherDomain {
		return true
	}

	// Subdomain match - hostname should end with "." + publisherDomain
	if strings.HasSuffix(hostname, "."+publisherDomain) {
		return true
	}

	return false
}
