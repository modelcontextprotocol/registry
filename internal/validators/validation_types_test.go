package validators_test

import (
	"errors"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators"
	"github.com/stretchr/testify/assert"
)

func TestValidationIssueTypes(t *testing.T) {
	tests := []struct {
		name      string
		issueType validators.ValidationIssueType
		expected  string
	}{
		{"JSON type", validators.ValidationIssueTypeJSON, "json"},
		{"Schema type", validators.ValidationIssueTypeSchema, "schema"},
		{"Semantic type", validators.ValidationIssueTypeSemantic, "semantic"},
		{"Linter type", validators.ValidationIssueTypeLinter, "linter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.issueType))
		})
	}
}

func TestValidationIssueSeverity(t *testing.T) {
	tests := []struct {
		name     string
		severity validators.ValidationIssueSeverity
		expected string
	}{
		{"Error severity", validators.ValidationIssueSeverityError, "error"},
		{"Warning severity", validators.ValidationIssueSeverityWarning, "warning"},
		{"Info severity", validators.ValidationIssueSeverityInfo, "info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.severity))
		})
	}
}

func TestNewValidationIssue(t *testing.T) {
	issue := validators.NewValidationIssue(
		validators.ValidationIssueTypeSemantic,
		"repository.url",
		"invalid repository URL",
		validators.ValidationIssueSeverityError,
		"invalid-repository-url",
	)

	assert.Equal(t, validators.ValidationIssueTypeSemantic, issue.Type)
	assert.Equal(t, "repository.url", issue.Path)
	assert.Equal(t, "invalid repository URL", issue.Message)
	assert.Equal(t, validators.ValidationIssueSeverityError, issue.Severity)
	assert.Equal(t, "invalid-repository-url", issue.Reference)
}

func TestNewValidationIssueFromError(t *testing.T) {
	err := errors.New("invalid repository URL: https://bad-url.com")
	issue := validators.NewValidationIssueFromError(
		validators.ValidationIssueTypeSemantic,
		"repository.url",
		err,
		"invalid-repository-url",
	)

	assert.Equal(t, validators.ValidationIssueTypeSemantic, issue.Type)
	assert.Equal(t, "repository.url", issue.Path)
	assert.Equal(t, "invalid repository URL: https://bad-url.com", issue.Message)
	assert.Equal(t, validators.ValidationIssueSeverityError, issue.Severity)
	assert.Equal(t, "invalid-repository-url", issue.Reference)
}

func TestValidationResultAddIssue(t *testing.T) {
	result := &validators.ValidationResult{Valid: true, Issues: []validators.ValidationIssue{}}

	// Add a warning issue - should not affect validity
	warningIssue := validators.NewValidationIssue(
		validators.ValidationIssueTypeLinter,
		"description",
		"consider adding a description",
		validators.ValidationIssueSeverityWarning,
		"descriptive-naming",
	)
	result.AddIssue(warningIssue)

	assert.True(t, result.Valid)
	assert.Len(t, result.Issues, 1)

	// Add an error issue - should make invalid
	errorIssue := validators.NewValidationIssue(
		validators.ValidationIssueTypeSemantic,
		"name",
		"server name is required",
		validators.ValidationIssueSeverityError,
		"missing-server-name",
	)
	result.AddIssue(errorIssue)

	assert.False(t, result.Valid)
	assert.Len(t, result.Issues, 2)
}

func TestValidationResultMerge(t *testing.T) {
	result1 := &validators.ValidationResult{Valid: true, Issues: []validators.ValidationIssue{}}
	result2 := &validators.ValidationResult{Valid: false, Issues: []validators.ValidationIssue{}}

	// Add issues to both
	issue1 := validators.NewValidationIssue(
		validators.ValidationIssueTypeSemantic,
		"name",
		"server name is required",
		validators.ValidationIssueSeverityError,
		"missing-server-name",
	)
	result1.AddIssue(issue1)

	issue2 := validators.NewValidationIssue(
		validators.ValidationIssueTypeSchema,
		"version",
		"version must be a string",
		validators.ValidationIssueSeverityError,
		"schema-validation",
	)
	result2.AddIssue(issue2)

	// Merge result2 into result1
	result1.Merge(result2)

	assert.False(t, result1.Valid)   // Should be invalid because result2 was invalid
	assert.Len(t, result1.Issues, 2) // Should have both issues
}

func TestValidationContext(t *testing.T) {
	// Test empty context
	ctx := &validators.ValidationContext{}
	assert.Equal(t, "", ctx.String())

	// Test field addition
	ctx = ctx.Field("repository")
	assert.Equal(t, "repository", ctx.String())

	// Test nested field
	ctx = ctx.Field("url")
	assert.Equal(t, "repository.url", ctx.String())

	// Test array index
	ctx = &validators.ValidationContext{}
	ctx = ctx.Field("packages").Index(0).Field("transport")
	assert.Equal(t, "packages[0].transport", ctx.String())

	// Test multiple array indices
	ctx = &validators.ValidationContext{}
	ctx = ctx.Field("packages").Index(0).Field("environmentVariables").Index(1).Field("name")
	assert.Equal(t, "packages[0].environmentVariables[1].name", ctx.String())
}

func TestValidationContextImmutability(t *testing.T) {
	// Test that context operations return new instances
	ctx1 := &validators.ValidationContext{}
	ctx2 := ctx1.Field("repository")
	ctx3 := ctx2.Field("url")

	assert.Equal(t, "", ctx1.String())
	assert.Equal(t, "repository", ctx2.String())
	assert.Equal(t, "repository.url", ctx3.String())
}
