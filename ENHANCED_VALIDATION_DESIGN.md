# Enhanced Server Validation Design

## Overview

This document outlines the design for enhancing the MCP Registry validation system to support comprehensive error collection with JSON path tracking.

## Current State

### Problems with Current Validation
- **Fail-fast behavior**: `ValidateServerJSON()` stops at first error
- **Limited feedback**: Users see only one error at a time
- **No path information**: Errors don't specify where in JSON the problem occurs
- **Manual error fixing**: Users must fix errors one by one

### Current Architecture
```go
func ValidateServerJSON(serverJSON *apiv0.ServerJSON) error {
    if err := validateRepository(&serverJSON.Repository); err != nil {
        return err  // ❌ Stops here
    }
    if err := validateVersion(serverJSON.Version); err != nil {
        return err  // ❌ Never reached if repository validation fails
    }
    // ... more validations
}
```

## Proposed Design

### Design Goals

1. **Comprehensive Feedback**: Collect all validation issues in a single pass, not just the first error
2. **Precise Location**: Provide exact JSON paths for every validation issue
3. **Structured Output**: Return machine-readable validation results with consistent format
4. **Backward Compatibility**: Maintain existing `ValidateServerJSON() error` signature
5. **Extensible**: Support different validation types (json, schema, semantic, linter) and severity levels

### Core Types

```go
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

type ValidationIssue struct {
    Type     ValidationIssueType     `json:"type"`
    Path     string                  `json:"path"`     // JSON path like "packages[0].transport.url"
    Message  string                  `json:"message"`  // Error description (extracted from error.Error())
    Severity ValidationIssueSeverity `json:"severity"`
    Rule     string                  `json:"rule"`     // Rule name like "prefer-transport-configuration"
}

type ValidationResult struct {
    Valid  bool              `json:"valid"`
    Issues []ValidationIssue `json:"issues"`
}

type ValidationContext struct {
    path string
}

// Constructor functions following Go conventions
func NewValidationIssue(issueType ValidationIssueType, path, message string, severity ValidationIssueSeverity, rule string) ValidationIssue
func NewValidationIssueFromError(issueType ValidationIssueType, path string, err error, rule string) ValidationIssue
```

### Validation Types

The `Type` field categorizes validation issues by their source:

- **`ValidationIssueTypeJSON`**: JSON parsing errors (malformed JSON syntax)
- **`ValidationIssueTypeSchema`**: JSON Schema validation errors (structural/format violations)  
- **`ValidationIssueTypeSemantic`**: Logical validation errors not enforceable in schema (business rules)
- **`ValidationIssueTypeLinter`**: Best practice recommendations, security concerns, style guidelines

The `Severity` field indicates the impact level:

- **`ValidationIssueSeverityError`**: Critical issues that must be fixed
- **`ValidationIssueSeverityWarning`**: Issues that should be addressed
- **`ValidationIssueSeverityInfo`**: Suggestions and recommendations

### Context Helper Methods

```go
func (ctx *ValidationContext) Field(name string) *ValidationContext
func (ctx *ValidationContext) Index(i int) *ValidationContext  
func (ctx *ValidationContext) String() string
```

### Backward Compatibility Strategy

The design maintains perfect backward compatibility by leveraging Go's existing error handling patterns:

#### **Error Message Preservation**
- **Current validators** use `fmt.Errorf("%w: %s", ErrInvalidRepositoryURL, obj.URL)` 
- **New validators** use `NewValidationIssueFromError()` which extracts `err.Error()`
- **Result**: Identical error messages, ensuring all existing tests pass

#### **Constructor Pattern**
Following Go conventions used throughout the project:
```go
// Standard constructor for manual field setting (linter rules, etc.)
issue := NewValidationIssue(
    ValidationIssueTypeLinter,
    "name",
    "consider using descriptive server name",
    ValidationIssueSeverityWarning,
    "descriptive-naming",
)

// Constructor that preserves existing error formatting (all current validators)
issue := NewValidationIssueFromError(
    ValidationIssueTypeSemantic, // All existing validation uses "semantic" type
    "repository.url",
    fmt.Errorf("%w: %s", ErrInvalidRepositoryURL, obj.URL), // Same error creation as before
    "invalid-repository-url",
)
```

#### **Error Interface Compatibility**
- Existing `ValidateServerJSON() error` signature unchanged
- Returns `fmt.Errorf("%s", issue.Message)` - same string format
- All `errors.Is()` and `errors.As()` calls continue to work
- No changes needed to error handling code

### New Validation Architecture

#### 1. All Validators Return ValidationResult

```go
// Every validator becomes exhaustive and returns ValidationResult
func validateRepository(ctx *ValidationContext, obj *model.Repository) *ValidationResult
func validatePackageField(ctx *ValidationContext, obj *model.Package) *ValidationResult  
func validateRemoteTransport(ctx *ValidationContext, obj *model.Transport) *ValidationResult
func validateVersion(ctx *ValidationContext, version string) *ValidationResult
func validateWebsiteURL(ctx *ValidationContext, websiteURL string) *ValidationResult
func validateArgument(ctx *ValidationContext, obj *model.Argument) *ValidationResult
```

#### 2. All Validators Use Context

```go
func ValidateServerJSONDetailed(serverJSON *apiv0.ServerJSON) *ValidationResult {
    result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}
    
    // Validate server name - using existing error logic
    if _, err := parseServerName(*serverJSON); err != nil {
        issue := NewValidationIssueFromError(
            ValidationIssueTypeSemantic, // All existing validation uses "semantic" type
            "name",
            err, // Preserves existing error formatting
            "invalid-server-name",
        )
        result.AddIssue(issue)
    }
    
    // Validate repository with context
    if repoResult := validateRepository(&ValidationContext{}, &serverJSON.Repository); !repoResult.Valid {
        result.Merge(repoResult)
    }
    
    // Validate packages with array context
    for i, pkg := range serverJSON.Packages {
        pkgCtx := &ValidationContext{}.Field("packages").Index(i)
        if pkgResult := validatePackageField(pkgCtx, &pkg); !pkgResult.Valid {
            result.Merge(pkgResult)
        }
    }
    
    // Validate remotes with array context
    for i, remote := range serverJSON.Remotes {
        remoteCtx := &ValidationContext{}.Field("remotes").Index(i)
        if remoteResult := validateRemoteTransport(remoteCtx, &remote); !remoteResult.Valid {
            result.Merge(remoteResult)
        }
    }
    
    return result
}
```

#### 3. Existing Validator Becomes Simple Wrapper

```go
func ValidateServerJSON(serverJSON *apiv0.ServerJSON) error {
    result := ValidateServerJSONDetailed(serverJSON)
    if !result.Valid {
        // Return the first error-level issue
        for _, issue := range result.Issues {
            if issue.Severity == "error" {
                return fmt.Errorf("%s: %s", issue.Path, issue.Message)
            }
        }
    }
    return nil
}
```

## Server Schema Validation

The project already uses `github.com/santhosh-tekuri/jsonschema/v5` for schema validation. We can leverage this library to add comprehensive JSON Schema validation that produces detailed error information.

### Schema Validation Integration

```go
func validateServerJSONSchema(ctx *ValidationContext, serverJSON *apiv0.ServerJSON) *ValidationResult {
    result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}
    
    // Load server.schema.json
    schema, err := loadServerSchema()
    if err != nil {
        // Handle schema loading error
        return result
    }
    
    // Validate against schema
    if err := schema.Validate(serverJSON); err != nil {
        // Convert jsonschema.ValidationError to ValidationIssue
        if validationErr, ok := err.(*jsonschema.ValidationError); ok {
            issue := ValidationIssue{
                Type:     ValidationIssueTypeSchema,
                Path:     validationErr.Field,      // JSON path from library
                Message:  validationErr.Description, // Detailed error message
                Severity: ValidationIssueSeverityError,
                Rule:     "schema-validation",
            }
            result.AddIssue(issue)
        }
    }
    
    return result
}

func ValidateServerJSONDetailed(serverJSON *apiv0.ServerJSON, validateSchema bool) *ValidationResult {
    result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}
    
    // Existing validation (always runs)
    // ... existing validation logic ...
    
    // Optional schema validation
    if validateSchema {
        if schemaResult := validateServerJSONSchema(&ValidationContext{}, serverJSON); !schemaResult.Valid {
            result.Merge(schemaResult)
        }
    }
    
    return result
}
```

### Benefits of Schema Validation

#### **Comprehensive Coverage**
- **JSON Schema validation** catches structural issues not covered by Go validators
- **Detailed error messages** with exact JSON paths from the schema library
- **Standards compliance** ensures server.json follows the official schema

#### **Rich Error Information**
The `jsonschema.ValidationError` provides:
- **Field**: Exact JSON path (e.g., `"packages[0].transport.url"`)
- **Description**: Detailed error message from schema
- **Type**: Error type (e.g., `"required"`, `"format"`, `"type"`)

#### **Integration with Existing Library**
- **No new dependencies**: Uses existing `jsonschema/v5` library
- **Consistent with project**: Same library used in `tools/validate-examples/`
- **Proven reliability**: Already tested and used in the project

## Implementation Plan

### Phase 1: Add Core Types
- [ ] Create `ValidationIssue`, `ValidationResult`, `ValidationContext` types
- [ ] Add helper methods for context building and result merging
- [ ] Add unit tests for new types

### Phase 2: Migrate Individual Validators
- [ ] Update `validateRepository()` to use context and return `*ValidationResult`
  - Use `NewValidationIssueFromError()` to preserve existing error formatting
  - Maintain same error messages for backward compatibility
- [ ] Update `validatePackageField()` to use context and return `*ValidationResult`
- [ ] Update `validateRemoteTransport()` to use context and return `*ValidationResult`
- [ ] Update `validateVersion()` to use context and return `*ValidationResult`
- [ ] Update `validateWebsiteURL()` to use context and return `*ValidationResult`
- [ ] Update all other individual validators
- [ ] Verify all existing tests continue to pass

### Phase 3: Implement Main Validators
- [ ] Create `ValidateServerJSONDetailed()` function
- [ ] Update `ValidateServerJSON()` to be a simple wrapper
- [ ] Add comprehensive tests for path building

### Phase 4: Add Server Schema Validation
- [ ] Add optional server schema validation using existing `jsonschema` library
- [ ] Convert schema validation errors to `ValidationIssue` format
- [ ] Add schema validation to `ValidateServerJSONDetailed()` with optional parameter

### Phase 5: Update Commands
- [ ] Update `mcp-publisher validate` command to use detailed validation
- [ ] Add JSON output format option
- [ ] Add filtering options (errors only, warnings, etc.)

### Phase 6: Testing and Documentation
- [ ] Add comprehensive test coverage
- [ ] Update documentation
- [ ] Performance testing
- [ ] Backward compatibility verification

## Example Usage

### JSON Output Format
```json
{
  "valid": false,
  "issues": [
    {
      "type": "json",
      "path": "",
      "message": "invalid JSON syntax at line 5, column 12",
      "severity": "error",
      "rule": "json-syntax-error"
    },
    {
      "type": "semantic",
      "path": "name",
      "message": "server name must be in format 'dns-namespace/name'",
      "severity": "error",
      "rule": "invalid-server-name"
    },
    {
      "type": "semantic", 
      "path": "packages[0].transport.url",
      "message": "url is required for streamable-http transport type",
      "severity": "error",
      "rule": "missing-transport-url"
    },
    {
      "type": "schema",
      "path": "packages[1].environmentVariables[0].name",
      "message": "string does not match required pattern",
      "severity": "error",
      "rule": "schema-validation"
    },
    {
      "type": "linter",
      "path": "packages[1].description",
      "message": "consider adding a more descriptive package description",
      "severity": "warning",
      "rule": "descriptive-package-description"
    }
  ]
}
```

**Note**: The JSON output still uses string values for `type` and `severity` fields for JSON serialization compatibility, but the Go code uses the typed constants for type safety.

### CLI Usage
```bash
# Basic validation
mcp-publisher validate server.json

# JSON output format
mcp-publisher validate --format json server.json

# Filter by severity
mcp-publisher validate --severity error server.json

# Include schema validation
mcp-publisher validate --schema server.json
```

## Benefits

### ✅ Comprehensive Feedback
- See all validation issues at once
- No need to fix errors one by one
- Better developer experience

### ✅ Precise Error Location
- JSON paths show exactly where issues occur
- Easy to locate problems in large JSON files
- Structured error format with rule names

### ✅ Structured Output
- JSON format for tooling integration
- Machine-readable error information
- Easy to parse and process programmatically

### ✅ Backward Compatibility
- Existing `ValidateServerJSON() error` signature unchanged
- All existing code continues to work
- Leverages Go's error interface and existing error constants
- Constructor pattern follows established project conventions

### ✅ Extensible Architecture
- Easy to add new validation types (schema, linter, warning)
- Easy to add new severity levels
- Easy to add filtering and formatting options

## Technical Considerations

### Go-Specific Design Rationale

#### **Error Interface Compatibility**
- **Leverages existing error constants**: `ErrInvalidRepositoryURL`, `ErrVersionLooksLikeRange`, etc.
- **Preserves error wrapping**: Uses `fmt.Errorf("%w: %s", err, context)` pattern
- **Maintains error.Is() compatibility**: Existing error checking continues to work
- **No breaking changes**: All error handling code remains functional

#### **Constructor Pattern**
Following established Go conventions in the project:
- **`NewValidationIssue()`**: Standard constructor following `NewXxx()` pattern
- **`NewValidationIssueFromError()`**: Specialized constructor for error conversion
- **Consistent with project**: Matches patterns used in `NewConfig()`, `NewServer()`, etc.
- **Type safety**: Compile-time validation of required fields

#### **Context Passing Architecture**
- **Immutable context building**: `ctx.Field("name").Index(0)` pattern
- **Clean composition**: Validators focus on validation, not path building
- **Reusable validators**: Same validator can be called with different contexts
- **No global state**: Thread-safe validation with explicit context

#### **Type Safety with Constrained Values**
Following Go best practices used throughout the project:
- **Typed string constants**: `ValidationIssueType`, `ValidationIssueSeverity` prevent invalid values
- **Compile-time validation**: IDE autocomplete and error checking
- **JSON compatibility**: Still serializes as strings for API compatibility
- **Refactoring safety**: Rename constants without breaking code
- **Consistent with project**: Matches patterns used in `Status`, `Format`, `ArgumentType`

### Performance
- Slightly slower than fail-fast validation
- Memory usage increases with error collection
- Acceptable trade-off for better user experience

### Testing Strategy
- Unit tests for each validator with context
- Integration tests for path building
- Backward compatibility tests
- Performance benchmarks

### Migration Strategy
- Implement alongside existing validators
- Gradual migration of individual validators
- Thorough testing at each phase
- Rollback plan if issues arise

## Future Enhancements

### Additional Validation Types
- **Linter rules**: Best practices and style guidelines
- **Warning level**: Non-critical issues
- **Info level**: Suggestions and improvements

### Advanced Features
- **Error filtering**: By type, severity, path pattern
- **Output formatting**: Human-readable, JSON, XML
- **Configuration**: Custom validation rules
- **IDE integration**: Real-time validation feedback

### Tooling Integration
- **WASM package**: Browser-based validation
- **VS Code extension**: Real-time validation
- **CI/CD integration**: Automated validation in pipelines
- **API endpoint**: Validation as a service
