package validators

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	apiv1 "github.com/modelcontextprotocol/registry/pkg/api/v1"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// ServerValidator validates server details
type ServerValidator struct {
	*RepositoryValidator // Embedded RepositoryValidator for repository validation
}

// Validate checks if the server details are valid
func (v *ServerValidator) Validate(obj *model.ServerJSON) error {
	if err := v.RepositoryValidator.Validate(&obj.Repository); err != nil {
		return err
	}
	return nil
}

// NewServerValidator creates a new ServerValidator instance
func NewServerValidator() *ServerValidator {
	return &ServerValidator{
		RepositoryValidator: NewRepositoryValidator(),
	}
}

// RepositoryValidator validates repository details
type RepositoryValidator struct {
	validSources map[RepositorySource]bool
}

// Validate checks if the repository details are valid
func (rv *RepositoryValidator) Validate(obj *model.Repository) error {
	// Skip validation for empty repository (optional field)
	if obj.URL == "" && obj.Source == "" {
		return nil
	}

	// validate the repository source
	repoSource := RepositorySource(obj.Source)
	if !IsValidRepositoryURL(repoSource, obj.URL) {
		return fmt.Errorf("%w: %s", ErrInvalidRepositoryURL, obj.URL)
	}

	return nil
}

// NewRepositoryValidator creates a new RepositoryValidator instance
func NewRepositoryValidator() *RepositoryValidator {
	return &RepositoryValidator{
		validSources: map[RepositorySource]bool{SourceGitHub: true, SourceGitLab: true},
	}
}

// PackageValidator validates package details
type PackageValidator struct{}

// Validate checks if the package details are valid
func (pv *PackageValidator) Validate(obj *model.Package) error {
	if !HasNoSpaces(obj.Identifier) {
		return ErrPackageNameHasSpaces
	}

	return nil
}

// NewPackageValidator creates a new PackageValidator instance
func NewPackageValidator() *PackageValidator {
	return &PackageValidator{}
}

// RemoteValidator validates remote connection details
type RemoteValidator struct{}

// Validate checks if the remote connection details are valid
func (rv *RemoteValidator) Validate(obj *model.Remote) error {
	if !IsValidURL(obj.URL) {
		return fmt.Errorf("%w: %s", ErrInvalidRemoteURL, obj.URL)
	}
	return nil
}

// NewRemoteValidator creates a new RemoteValidator instance
func NewRemoteValidator() *RemoteValidator {
	return &RemoteValidator{}
}

// ObjectValidator aggregates multiple validators for different object types
// This allows for a single entry point to validate complex objects that may contain multiple fields
// that need validation.
type ObjectValidator struct {
	ServerValidator  *ServerValidator
	PackageValidator *PackageValidator
	RemoteValidator  *RemoteValidator
}

func NewObjectValidator() *ObjectValidator {
	return &ObjectValidator{
		ServerValidator:  NewServerValidator(),
		PackageValidator: NewPackageValidator(),
		RemoteValidator:  NewRemoteValidator(),
	}
}

func (ov *ObjectValidator) Validate(obj *model.ServerJSON) error {
	if err := ov.ServerValidator.Validate(obj); err != nil {
		return err
	}

	for _, pkg := range obj.Packages {
		if err := ov.PackageValidator.Validate(&pkg); err != nil {
			return err
		}
	}

	for _, remote := range obj.Remotes {
		if err := ov.RemoteValidator.Validate(&remote); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePublisherExtensions validates that publisher extensions are within size limits
func ValidatePublisherExtensions(req apiv1.PublishRequest) error {
	const maxExtensionSize = 4 * 1024 // 4KB limit

	// Check size limit for x-publisher extension
	if req.XPublisher != nil {
		extensionsJSON, err := json.Marshal(req.XPublisher)
		if err != nil {
			return fmt.Errorf("failed to marshal x-publisher extension: %w", err)
		}
		if len(extensionsJSON) > maxExtensionSize {
			return fmt.Errorf("x-publisher extension exceeds 4KB limit (%d bytes)", len(extensionsJSON))
		}
	}

	return nil
}

// ValidatePublishRequestExtensions validates that only allowed extension fields are present
func ValidatePublishRequestExtensions(requestData []byte) error {
	// Parse the raw JSON to check for unknown fields
	var rawRequest map[string]interface{}
	if err := json.Unmarshal(requestData, &rawRequest); err != nil {
		return fmt.Errorf("failed to parse request JSON: %w", err)
	}

	// Define allowed top-level fields
	allowedFields := map[string]bool{
		"server":      true,
		"x-publisher": true,
	}

	// Check for any disallowed fields
	var invalidFields []string
	for field := range rawRequest {
		if !allowedFields[field] {
			invalidFields = append(invalidFields, field)
		}
	}

	if len(invalidFields) > 0 {
		return fmt.Errorf("invalid extension fields: %v. Only 'server' and 'x-publisher' fields are allowed", invalidFields)
	}

	return nil
}

// ExtractPublisherExtensions extracts publisher extensions from a apiv1.PublishRequest
func ExtractPublisherExtensions(req apiv1.PublishRequest) map[string]interface{} {
	publisherExtensions := make(map[string]interface{})
	if req.XPublisher != nil {
		// Copy fields directly, avoiding double nesting
		for k, v := range req.XPublisher {
			publisherExtensions[k] = v
		}
	}
	return publisherExtensions
}

// ParseServerName extracts the server name from a model.ServerJSON for validation purposes
func ParseServerName(serverDetail model.ServerJSON) (string, error) {
	name := serverDetail.Name
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

// ValidateRemoteNamespaceMatch validates that remote URLs match the reverse-DNS namespace
func ValidateRemoteNamespaceMatch(serverDetail model.ServerJSON) error {
	namespace := serverDetail.Name

	for _, remote := range serverDetail.Remotes {
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
