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

// ValidationIssue represents a single validation problem
type ValidationIssue struct {
	Type     ValidationIssueType     `json:"type"`
	Path     string                  `json:"path"`    // JSON path like "packages[0].transport.url"
	Message  string                  `json:"message"` // Error description (extracted from error.Error())
	Severity ValidationIssueSeverity `json:"severity"`
	Rule     string                  `json:"rule"` // Rule name like "prefer-transport-configuration"
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
func NewValidationIssue(issueType ValidationIssueType, path, message string, severity ValidationIssueSeverity, rule string) ValidationIssue {
	return ValidationIssue{
		Type:     issueType,
		Path:     path,
		Message:  message,
		Severity: severity,
		Rule:     rule,
	}
}

// NewValidationIssueFromError creates a validation issue from an existing error
func NewValidationIssueFromError(issueType ValidationIssueType, path string, err error, rule string) ValidationIssue {
	return ValidationIssue{
		Type:     issueType,
		Path:     path,
		Message:  err.Error(),                  // Extract string from error
		Severity: ValidationIssueSeverityError, // Errors are always severity "error"
		Rule:     rule,
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
