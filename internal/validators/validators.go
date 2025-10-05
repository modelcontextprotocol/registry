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
	result := ValidateServerJSONDetailed(serverJSON, false)
	if !result.Valid {
		// Return the first error issue
		for _, issue := range result.Issues {
			if issue.Severity == ValidationIssueSeverityError {
				return fmt.Errorf("%s", issue.Message)
			}
		}
	}
	return nil
}

// ValidateServerJSONDetailed performs exhaustive validation and returns all issues found
// If validateSchema is true, it will also validate against server.schema.json
func ValidateServerJSONDetailed(serverJSON *apiv0.ServerJSON, validateSchema bool) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}
	ctx := &ValidationContext{}

	// Schema validation first (if requested) - catches structural issues early
	if validateSchema {
		schemaResult := validateServerJSONSchema(serverJSON)
		result.Merge(schemaResult)
		// If schema validation fails, we might still want to run semantic validation
		// to provide additional context, but schema errors take precedence
	}

	// Validate server name exists and format
	if _, err := parseServerName(*serverJSON); err != nil {
		issue := NewValidationIssueFromError(
			ValidationIssueTypeSemantic,
			ctx.Field("name").String(),
			err,
			"invalid-server-name",
		)
		result.AddIssue(issue)
	}

	// Validate top-level server version is a specific version (not a range) & not "latest"
	versionResult := validateVersion(ctx.Field("version"), serverJSON.Version)
	result.Merge(versionResult)

	// Validate repository
	repoResult := validateRepository(ctx.Field("repository"), &serverJSON.Repository)
	result.Merge(repoResult)

	// Validate website URL if provided
	websiteResult := validateWebsiteURL(ctx.Field("websiteUrl"), serverJSON.WebsiteURL)
	result.Merge(websiteResult)

	// Validate all packages (basic field validation)
	// Detailed package validation (including registry checks) is done during publish
	for i, pkg := range serverJSON.Packages {
		pkgResult := validatePackageField(ctx.Field("packages").Index(i), &pkg)
		result.Merge(pkgResult)
	}

	// Validate all remotes
	for i, remote := range serverJSON.Remotes {
		remoteResult := validateRemoteTransport(ctx.Field("remotes").Index(i), &remote)
		result.Merge(remoteResult)
	}

	// Validate reverse-DNS namespace matching for remote URLs
	remoteNamespaceResult := validateRemoteNamespaceMatch(ctx.Field("remotes"), *serverJSON)
	result.Merge(remoteNamespaceResult)

	// Validate reverse-DNS namespace matching for website URL
	websiteNamespaceResult := validateWebsiteURLNamespaceMatch(ctx.Field("websiteUrl"), *serverJSON)
	result.Merge(websiteNamespaceResult)

	return result
}

func validateRepository(ctx *ValidationContext, obj *model.Repository) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}

	// Skip validation for empty repository (optional field)
	if obj.URL == "" && obj.Source == "" {
		return result
	}

	// validate the repository source
	repoSource := RepositorySource(obj.Source)
	if !IsValidRepositoryURL(repoSource, obj.URL) {
		issue := NewValidationIssueFromError(
			ValidationIssueTypeSemantic,
			ctx.Field("url").String(),
			fmt.Errorf("%w: %s", ErrInvalidRepositoryURL, obj.URL),
			"invalid-repository-url",
		)
		result.AddIssue(issue)
	}

	// validate subfolder if present
	if obj.Subfolder != "" && !IsValidSubfolderPath(obj.Subfolder) {
		issue := NewValidationIssueFromError(
			ValidationIssueTypeSemantic,
			ctx.Field("subfolder").String(),
			fmt.Errorf("%w: %s", ErrInvalidSubfolderPath, obj.Subfolder),
			"invalid-subfolder-path",
		)
		result.AddIssue(issue)
	}

	return result
}

func validateWebsiteURL(ctx *ValidationContext, websiteURL string) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}

	// Skip validation if website URL is not provided (optional field)
	if websiteURL == "" {
		return result
	}

	// Parse the URL to ensure it's valid
	parsedURL, err := url.Parse(websiteURL)
	if err != nil {
		issue := NewValidationIssueFromError(
			ValidationIssueTypeSemantic,
			ctx.String(),
			fmt.Errorf("invalid websiteUrl: %w", err),
			"invalid-website-url",
		)
		result.AddIssue(issue)
		return result
	}

	// Ensure it's an absolute URL with valid scheme
	if !parsedURL.IsAbs() {
		issue := NewValidationIssue(
			ValidationIssueTypeSemantic,
			ctx.String(),
			fmt.Sprintf("websiteUrl must be absolute (include scheme): %s", websiteURL),
			ValidationIssueSeverityError,
			"website-url-must-be-absolute",
		)
		result.AddIssue(issue)
	}

	// Only allow HTTP/HTTPS schemes for security
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		issue := NewValidationIssue(
			ValidationIssueTypeSemantic,
			ctx.String(),
			fmt.Sprintf("websiteUrl must use http or https scheme: %s", websiteURL),
			ValidationIssueSeverityError,
			"website-url-invalid-scheme",
		)
		result.AddIssue(issue)
	}

	return result
}

func validatePackageField(ctx *ValidationContext, obj *model.Package) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}

	// Validate identifier has no spaces
	if !HasNoSpaces(obj.Identifier) {
		issue := NewValidationIssueFromError(
			ValidationIssueTypeSemantic,
			ctx.Field("identifier").String(),
			ErrPackageNameHasSpaces,
			"package-name-has-spaces",
		)
		result.AddIssue(issue)
	}

	// Validate version string
	versionResult := validateVersion(ctx.Field("version"), obj.Version)
	result.Merge(versionResult)

	// Validate runtime arguments
	for i, arg := range obj.RuntimeArguments {
		argResult := validateArgument(ctx.Field("runtimeArguments").Index(i), &arg)
		result.Merge(argResult)
	}

	// Validate package arguments
	for i, arg := range obj.PackageArguments {
		argResult := validateArgument(ctx.Field("packageArguments").Index(i), &arg)
		result.Merge(argResult)
	}

	// Validate transport with template variable support
	availableVariables := collectAvailableVariables(obj)
	transportResult := validatePackageTransport(ctx.Field("transport"), &obj.Transport, availableVariables)
	result.Merge(transportResult)

	return result
}

// validateVersion validates the version string.
// NB: we decided that we would not enforce strict semver for version strings
func validateVersion(ctx *ValidationContext, version string) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}

	if version == "latest" {
		issue := NewValidationIssueFromError(
			ValidationIssueTypeSemantic,
			ctx.String(),
			ErrReservedVersionString,
			"reserved-version-string",
		)
		result.AddIssue(issue)
		return result
	}

	// Reject semver range-like inputs
	if looksLikeVersionRange(version) {
		issue := NewValidationIssueFromError(
			ValidationIssueTypeSemantic,
			ctx.String(),
			fmt.Errorf("%w: %q", ErrVersionLooksLikeRange, version),
			"version-looks-like-range",
		)
		result.AddIssue(issue)
	}

	return result
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
func validateArgument(ctx *ValidationContext, obj *model.Argument) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}

	if obj.Type == model.ArgumentTypeNamed {
		// Validate named argument name format
		nameResult := validateNamedArgumentName(ctx.Field("name"), obj.Name)
		result.Merge(nameResult)

		// Validate value and default don't start with the name
		valueResult := validateArgumentValueFields(ctx, obj.Name, obj.Value, obj.Default)
		result.Merge(valueResult)
	}
	return result
}

func validateNamedArgumentName(ctx *ValidationContext, name string) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}

	// Check if name is required for named arguments
	if name == "" {
		issue := NewValidationIssueFromError(
			ValidationIssueTypeSemantic,
			ctx.String(),
			ErrNamedArgumentNameRequired,
			"named-argument-name-required",
		)
		result.AddIssue(issue)
		return result
	}

	// Check for invalid characters that suggest embedded values or descriptions
	// Valid: "--directory", "--port", "-v", "config", "verbose"
	// Invalid: "--directory <absolute_path_to_adfin_mcp_folder>", "--port 8080"
	if strings.Contains(name, "<") || strings.Contains(name, ">") ||
		strings.Contains(name, " ") || strings.Contains(name, "$") {
		issue := NewValidationIssueFromError(
			ValidationIssueTypeSemantic,
			ctx.String(),
			fmt.Errorf("%w: %s", ErrInvalidNamedArgumentName, name),
			"invalid-named-argument-name",
		)
		result.AddIssue(issue)
	}

	return result
}

func validateArgumentValueFields(ctx *ValidationContext, name, value, defaultValue string) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}

	// Check if value starts with the argument name (using startsWith, not contains)
	if value != "" && strings.HasPrefix(value, name) {
		issue := NewValidationIssueFromError(
			ValidationIssueTypeSemantic,
			ctx.Field("value").String(),
			fmt.Errorf("%w: value starts with argument name '%s': %s", ErrArgumentValueStartsWithName, name, value),
			"argument-value-starts-with-name",
		)
		result.AddIssue(issue)
	}

	if defaultValue != "" && strings.HasPrefix(defaultValue, name) {
		issue := NewValidationIssueFromError(
			ValidationIssueTypeSemantic,
			ctx.Field("default").String(),
			fmt.Errorf("%w: default starts with argument name '%s': %s", ErrArgumentDefaultStartsWithName, name, defaultValue),
			"argument-default-starts-with-name",
		)
		result.AddIssue(issue)
	}

	return result
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
func validatePackageTransport(ctx *ValidationContext, transport *model.Transport, availableVariables []string) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}

	// Validate transport type is supported
	switch transport.Type {
	case model.TransportTypeStdio:
		// Validate that URL is empty for stdio transport
		if transport.URL != "" {
			issue := NewValidationIssue(
				ValidationIssueTypeSemantic,
				ctx.Field("url").String(),
				fmt.Sprintf("url must be empty for %s transport type, got: %s", transport.Type, transport.URL),
				ValidationIssueSeverityError,
				"stdio-transport-url-not-empty",
			)
			result.AddIssue(issue)
		}
	case model.TransportTypeStreamableHTTP, model.TransportTypeSSE:
		// URL is required for streamable-http and sse
		if transport.URL == "" {
			issue := NewValidationIssue(
				ValidationIssueTypeSemantic,
				ctx.Field("url").String(),
				fmt.Sprintf("url is required for %s transport type", transport.Type),
				ValidationIssueSeverityError,
				"streamable-transport-url-required",
			)
			result.AddIssue(issue)
		} else {
			// Validate URL format with template variable support
			if !IsValidTemplatedURL(transport.URL, availableVariables, true) {
				// Check if it's a template variable issue or basic URL issue
				templateVars := extractTemplateVariables(transport.URL)
				var err error
				if len(templateVars) > 0 {
					err = fmt.Errorf("%w: template variables in URL %s reference undefined variables. Available variables: %v",
						ErrInvalidRemoteURL, transport.URL, availableVariables)
				} else {
					err = fmt.Errorf("%w: %s", ErrInvalidRemoteURL, transport.URL)
				}
				issue := NewValidationIssueFromError(
					ValidationIssueTypeSemantic,
					ctx.Field("url").String(),
					err,
					"invalid-templated-url",
				)
				result.AddIssue(issue)
			}
		}
	default:
		issue := NewValidationIssue(
			ValidationIssueTypeSemantic,
			ctx.Field("type").String(),
			fmt.Sprintf("unsupported transport type: %s", transport.Type),
			ValidationIssueSeverityError,
			"unsupported-transport-type",
		)
		result.AddIssue(issue)
	}

	return result
}

// validateRemoteTransport validates a remote transport (no templating allowed)
func validateRemoteTransport(ctx *ValidationContext, obj *model.Transport) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}

	// Validate transport type is supported - remotes only support streamable-http and sse
	switch obj.Type {
	case model.TransportTypeStreamableHTTP, model.TransportTypeSSE:
		// URL is required for streamable-http and sse
		if obj.URL == "" {
			issue := NewValidationIssue(
				ValidationIssueTypeSemantic,
				ctx.Field("url").String(),
				fmt.Sprintf("url is required for %s transport type", obj.Type),
				ValidationIssueSeverityError,
				"remote-transport-url-required",
			)
			result.AddIssue(issue)
		} else {
			// Validate URL format (no templates allowed for remotes, no localhost)
			if !IsValidRemoteURL(obj.URL) {
				issue := NewValidationIssueFromError(
					ValidationIssueTypeSemantic,
					ctx.Field("url").String(),
					fmt.Errorf("%w: %s", ErrInvalidRemoteURL, obj.URL),
					"invalid-remote-url",
				)
				result.AddIssue(issue)
			}
		}
	default:
		issue := NewValidationIssue(
			ValidationIssueTypeSemantic,
			ctx.Field("type").String(),
			fmt.Sprintf("unsupported transport type for remotes: %s (only streamable-http and sse are supported)", obj.Type),
			ValidationIssueSeverityError,
			"unsupported-remote-transport-type",
		)
		result.AddIssue(issue)
	}

	return result
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
func validateRemoteNamespaceMatch(ctx *ValidationContext, serverJSON apiv0.ServerJSON) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}
	namespace := serverJSON.Name

	for i, remote := range serverJSON.Remotes {
		if err := validateRemoteURLMatchesNamespace(remote.URL, namespace); err != nil {
			issue := NewValidationIssueFromError(
				ValidationIssueTypeSemantic,
				ctx.Index(i).Field("url").String(),
				fmt.Errorf("remote URL %s does not match namespace %s: %w", remote.URL, namespace, err),
				"remote-url-namespace-mismatch",
			)
			result.AddIssue(issue)
		}
	}

	return result
}

// validateWebsiteURLNamespaceMatch validates that website URL matches the reverse-DNS namespace
func validateWebsiteURLNamespaceMatch(ctx *ValidationContext, serverJSON apiv0.ServerJSON) *ValidationResult {
	result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}

	// Skip validation if website URL is not provided
	if serverJSON.WebsiteURL == "" {
		return result
	}

	namespace := serverJSON.Name
	if err := validateRemoteURLMatchesNamespace(serverJSON.WebsiteURL, namespace); err != nil {
		issue := NewValidationIssueFromError(
			ValidationIssueTypeSemantic,
			ctx.String(),
			fmt.Errorf("websiteUrl %s does not match namespace %s: %w", serverJSON.WebsiteURL, namespace, err),
			"website-url-namespace-mismatch",
		)
		result.AddIssue(issue)
	}

	return result
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
