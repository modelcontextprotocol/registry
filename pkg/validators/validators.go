// Package validators exposes the config-free subset of the registry's
// validation logic as an importable library.
//
// The registry uses these validators to ensure only well-formed, publisher-owned
// entries make it into the registry. Historically the logic lived under
// internal/validators and could not be reused outside of running a full registry.
// This package is a thin facade over that internal implementation, re-exporting
// the parts that do not depend on a running registry's configuration so that
// external tools (for example, a private sub-registry or a preflight check that
// runs in CI before a release tag is cut) can validate a server.json locally.
//
// Two entry points cover the common cases:
//
//   - ValidateServerJSON performs schema and semantic validation of a server.json
//     document and returns every issue it finds.
//   - ValidatePackage checks that a package is allowed on the official registry
//     and is owned by the publisher, by dispatching to the per-registry
//     ownership validators (npm, PyPI, NuGet, OCI, MCPB, Cargo).
//
// The sentinel errors re-exported here can be matched with errors.Is, so
// consumers can distinguish outcomes without matching on error strings.
//
// This is intentionally the config-free surface only. Validators that require a
// running registry's *config.Config (ValidatePublishRequest, ValidateUpdateRequest)
// and the authentication checks currently embedded in the API auth handlers are
// out of scope here and remain in internal/ for now; see issue #1394 for the
// planned follow-up phases.
package validators

import (
	"context"

	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// Result and issue types describing the outcome of validation. These are aliases
// of the internal types so values flow freely between this package and the
// registry's internal call sites.
type (
	// ValidationResult contains the results of validation.
	ValidationResult = validators.ValidationResult
	// ValidationIssue represents a single validation problem.
	ValidationIssue = validators.ValidationIssue
	// ValidationOptions configures which types of validation to perform.
	ValidationOptions = validators.ValidationOptions
	// ValidationIssueType classifies a validation issue (json, schema, semantic, linter).
	ValidationIssueType = validators.ValidationIssueType
	// ValidationIssueSeverity is the severity of a validation issue (error, warning, info).
	ValidationIssueSeverity = validators.ValidationIssueSeverity
	// SchemaVersionPolicy determines how non-current schema versions are handled.
	SchemaVersionPolicy = validators.SchemaVersionPolicy
)

// Validation issue types.
const (
	ValidationIssueTypeJSON     = validators.ValidationIssueTypeJSON
	ValidationIssueTypeSchema   = validators.ValidationIssueTypeSchema
	ValidationIssueTypeSemantic = validators.ValidationIssueTypeSemantic
	ValidationIssueTypeLinter   = validators.ValidationIssueTypeLinter
)

// Validation issue severities.
const (
	ValidationIssueSeverityError   = validators.ValidationIssueSeverityError
	ValidationIssueSeverityWarning = validators.ValidationIssueSeverityWarning
	ValidationIssueSeverityInfo    = validators.ValidationIssueSeverityInfo
)

// Schema version policies.
const (
	SchemaVersionPolicyAllow = validators.SchemaVersionPolicyAllow
	SchemaVersionPolicyWarn  = validators.SchemaVersionPolicyWarn
	SchemaVersionPolicyError = validators.SchemaVersionPolicyError
)

// Common validation configurations for use with ValidateServerJSON.
var (
	// ValidationSemanticOnly performs only semantic validation (no schema checks).
	ValidationSemanticOnly = validators.ValidationSemanticOnly
	// ValidationSchemaVersionOnly checks schema version only (empty, non-current).
	ValidationSchemaVersionOnly = validators.ValidationSchemaVersionOnly
	// ValidationSchemaVersionAndSemantic checks schema version and performs semantic validation.
	ValidationSchemaVersionAndSemantic = validators.ValidationSchemaVersionAndSemantic
	// ValidationAll performs all validation types (schema version, full schema validation, and semantic).
	ValidationAll = validators.ValidationAll
)

// Sentinel validation errors. Match these with errors.Is to distinguish outcomes
// without matching on error strings.
var (
	ErrInvalidRepositoryURL          = validators.ErrInvalidRepositoryURL
	ErrInvalidSubfolderPath          = validators.ErrInvalidSubfolderPath
	ErrPackageNameHasSpaces          = validators.ErrPackageNameHasSpaces
	ErrReservedVersionString         = validators.ErrReservedVersionString
	ErrVersionLooksLikeRange         = validators.ErrVersionLooksLikeRange
	ErrInvalidPackageTransportURL    = validators.ErrInvalidPackageTransportURL
	ErrInvalidRemoteURL              = validators.ErrInvalidRemoteURL
	ErrUnsupportedRegistryBaseURL    = validators.ErrUnsupportedRegistryBaseURL
	ErrMismatchedRegistryTypeAndURL  = validators.ErrMismatchedRegistryTypeAndURL
	ErrNamedArgumentNameRequired     = validators.ErrNamedArgumentNameRequired
	ErrInvalidNamedArgumentName      = validators.ErrInvalidNamedArgumentName
	ErrArgumentValueStartsWithName   = validators.ErrArgumentValueStartsWithName
	ErrArgumentDefaultStartsWithName = validators.ErrArgumentDefaultStartsWithName
	ErrMultipleSlashesInServerName   = validators.ErrMultipleSlashesInServerName
	ErrInvalidServerNameFormat       = validators.ErrInvalidServerNameFormat
)

// ValidateServerJSON performs exhaustive validation of a server.json document and
// returns all issues found. opts specifies which types of validation to perform;
// see the Validation* option presets for common configurations.
func ValidateServerJSON(serverJSON *apiv0.ServerJSON, opts ValidationOptions) *ValidationResult {
	return validators.ValidateServerJSON(serverJSON, opts)
}

// ValidatePackage validates that the package referenced in the server
// configuration is allowed on the official registry (based on its registry type
// and base URL) and is owned by the publisher, by checking for a matching server
// name in the package metadata. It dispatches to the per-registry validator for
// the package's registry type and may perform network calls against the upstream
// package registry.
func ValidatePackage(ctx context.Context, pkg model.Package, serverName string) error {
	return validators.ValidatePackage(ctx, pkg, serverName)
}
