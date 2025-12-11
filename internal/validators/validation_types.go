package validators

import "fmt"

// Validation issue type with constrained values
type ValidationIssueType string

const (
	ValidationIssueTypeJSON     ValidationIssueType = "json"
	ValidationIssueTypeSchema   ValidationIssueType = "schema"
	ValidationIssueTypeSemantic ValidationIssueType = "semantic"
	ValidationIssueTypeLinter   ValidationIssueType = "linter"
)

// Validation issue severity with constrained values
type ValidationIssueSeverity string

const (
	ValidationIssueSeverityError   ValidationIssueSeverity = "error"
	ValidationIssueSeverityWarning ValidationIssueSeverity = "warning"
	ValidationIssueSeverityInfo    ValidationIssueSeverity = "info"
)

// SchemaVersionPolicy determines how non-current schema versions are handled
type SchemaVersionPolicy string

const (
	// SchemaVersionPolicyAllow allows non-current schemas with no warning or error
	SchemaVersionPolicyAllow SchemaVersionPolicy = "allow"
	// SchemaVersionPolicyWarn allows non-current schemas but generates a warning
	SchemaVersionPolicyWarn SchemaVersionPolicy = "warn"
	// SchemaVersionPolicyError rejects non-current schemas with an error
	SchemaVersionPolicyError SchemaVersionPolicy = "error"
)

// ValidationOptions configures which types of validation to perform
// ValidateSchema implies ValidateSchemaVersion (the flag is ignored if ValidateSchema is true)
type ValidationOptions struct {
	ValidateSchemaVersion  bool                // Check schema version (empty, non-current). Ignored if ValidateSchema is true.
	ValidateSchema         bool                // Perform full schema validation (implies ValidateSchemaVersion)
	ValidateSemantic       bool                // Perform semantic validation
	NonCurrentSchemaPolicy SchemaVersionPolicy // Policy for non-current schemas (only used when schema validation is performed)
}

// Common validation configurations
var (
	// ValidationSemanticOnly performs only semantic validation (no schema checks)
	ValidationSemanticOnly = ValidationOptions{
		ValidateSemantic: true,
	}

	// ValidationSchemaVersionOnly checks schema version only (empty, non-current)
	ValidationSchemaVersionOnly = ValidationOptions{
		ValidateSchemaVersion:  true,
		NonCurrentSchemaPolicy: SchemaVersionPolicyError,
	}

	// ValidationSchemaVersionAndSemantic checks schema version and performs semantic validation
	ValidationSchemaVersionAndSemantic = ValidationOptions{
		ValidateSchemaVersion:  true,
		ValidateSemantic:       true,
		NonCurrentSchemaPolicy: SchemaVersionPolicyWarn,
	}

	// ValidationAll performs all validation types (schema version, full schema validation, and semantic)
	ValidationAll = ValidationOptions{
		ValidateSchema:         true, // Implies ValidateSchemaVersion
		ValidateSemantic:       true,
		NonCurrentSchemaPolicy: SchemaVersionPolicyWarn,
	}
)

// ValidationIssue represents a single validation problem
type ValidationIssue struct {
	Type      ValidationIssueType     `json:"type"`
	Path      string                  `json:"path"`    // JSON path like "packages[0].transport.url"
	Message   string                  `json:"message"` // Error description (extracted from error.Error())
	Severity  ValidationIssueSeverity `json:"severity"`
	Reference string                  `json:"reference"` // Reference to validation trigger (schema rule path, named rule, etc.)
}

// ValidationResult contains the results of validation
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Issues []ValidationIssue `json:"issues"`
}

// ValidationContext tracks the current JSON path during validation
type ValidationContext struct {
	path string
}

// NewValidationIssue creates a validation issue with manual field setting
func NewValidationIssue(issueType ValidationIssueType, path, message string, severity ValidationIssueSeverity, reference string) ValidationIssue {
	return ValidationIssue{
		Type:      issueType,
		Path:      path,
		Message:   message,
		Severity:  severity,
		Reference: reference,
	}
}

// NewValidationIssueFromError creates a validation issue from an existing error
func NewValidationIssueFromError(issueType ValidationIssueType, path string, err error, reference string) ValidationIssue {
	return ValidationIssue{
		Type:      issueType,
		Path:      path,
		Message:   err.Error(),                  // Extract string from error
		Severity:  ValidationIssueSeverityError, // Errors are always severity "error"
		Reference: reference,
	}
}

// AddIssue adds a validation issue to the result
func (vr *ValidationResult) AddIssue(issue ValidationIssue) {
	vr.Issues = append(vr.Issues, issue)
	if issue.Severity == ValidationIssueSeverityError {
		vr.Valid = false
	}
}

// Merge combines another validation result into this one
func (vr *ValidationResult) Merge(other *ValidationResult) {
	vr.Issues = append(vr.Issues, other.Issues...)
	if !other.Valid {
		vr.Valid = false
	}
}

// FirstError returns the first error-level issue as an error, or nil if valid
// This provides backward compatibility for code that expects an error return type
func (vr *ValidationResult) FirstError() error {
	if vr.Valid {
		return nil
	}
	for _, issue := range vr.Issues {
		if issue.Severity == ValidationIssueSeverityError {
			return fmt.Errorf("%s", issue.Message)
		}
	}
	return nil
}

// Field adds a field name to the context path
func (ctx *ValidationContext) Field(name string) *ValidationContext {
	if ctx.path == "" {
		return &ValidationContext{path: name}
	}
	return &ValidationContext{path: ctx.path + "." + name}
}

// Index adds an array index to the context path
func (ctx *ValidationContext) Index(i int) *ValidationContext {
	return &ValidationContext{path: ctx.path + fmt.Sprintf("[%d]", i)}
}

// String returns the current path as a string
func (ctx *ValidationContext) String() string {
	return ctx.path
}
