package validators

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/registry/internal/config"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func ValidateServerJSON(serverJSON *apiv0.ServerJSON) error {
	// Validate server name exists and format
	if _, err := parseServerName(*serverJSON); err != nil {
		return err
	}

	// Validate repository
	if err := validateRepository(&serverJSON.Repository); err != nil {
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

// validateVersion validates the version string
// NB: we decided that we would not enforce strict semver for version strings
func validateVersion(version string) error {
	if version == "latest" {
		return ErrReservedVersionString
	}

	return nil
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
func ValidatePublishRequest(req apiv0.ServerJSON, cfg *config.Config) error {
	// Validate publisher extensions in _meta
	if err := validatePublisherExtensions(req); err != nil {
		return err
	}

	// Validate the server detail (includes all nested validation)
	if err := ValidateServerJSON(&req); err != nil {
		return err
	}

	// Validate registry ownership for all packages if validation is enabled and server is not deleted
	if cfg.EnableRegistryValidation && req.Status != model.StatusDeleted {
		ctx := context.Background()
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

	if req.Meta != nil {
		// Validate that only publisher-provided data is allowed in _meta during publish (no official registry metadata should be present)
		if req.Meta.Official != nil {
			return fmt.Errorf("official registry metadata '_meta.io.modelcontextprotocol.registry/official' is not allowed during publish")
		}
	}

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

	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("server name must be in format 'dns-namespace/name' with non-empty namespace and name parts")
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
