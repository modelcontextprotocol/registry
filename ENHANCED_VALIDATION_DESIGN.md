# Enhanced Server Validation Design

## Overview

This document outlines the design for implementing comprehensive server validation in the MCP Registry, due to the following concerns: 

- Currently, the MPC Registry project publishes a server.json schema but does not validate servers against it, allowing non-compliant servers to be published. 
- There is existing ad-hoc validation that covers some schema compliance, but not all (there are logical errors not identifiable by schema validation and that are not covered by the existing ad hoc validation). 
- Many servers that do pass validation do not represent best-practices for published servers. 

This design implements a three-tier validation system: **Schema Validation**, **Semantic Validation**, and **Linter Validation**.

## Current State

### Problems with Current Validation
- **No schema validation**: Servers are published without validating against the published schema (and many violate it)
- **Incomplete validation**: Ad hoc validation covers only some schema constraints (many published servers have additional logical errors)
- **Best Practices not indicated**: Many servers that would pass schema and semantic validation do not represent best practices
- **Fail-fast behavior**: `ValidateServerJSON()` stops at first error
- **No path information**: Errors don't specify where in JSON the problem occurs

## Three-Tier Validation System

### Schema Validation (Primary)
- **Validates against published schema**: Ensures servers comply with the official server.json schema
- **Exhaustive coverage**: Catches all structural and format violations defined in the schema
- **Detailed error references**: Shows exact schema rule locations with specific constraint and full path to constraint

### Semantic Validation (Secondary)
- **Business logic validation**: Validates only constraints not expressible in JSON Schema
- **Registry validation**: Enforce validitiy of registry references (as current)
- **Logical Errors**: Enforce logical consistency: format, choices, variable usage, etc

### Linter Validation (Tertiary)
- **Best practice recommendations**: Security concerns, style guidelines, naming conventions
- **Non-blocking**: Warnings and suggestions, not errors
- **Quality improvements**: Helps developers create better servers
- **Educational**: Teaches best practices for MCP server development

## Implementation Approach

The enhanced validation will be implemented in stages to minimize risk and allow for review and experimentation:

### **Stage 1: Schema Validation and Exhaustive Validation Results (Current)**
- Convert existing validators to use and track context and to return exhaustive results
- Add `mcp-publisher validate` command that performs exhaustive validation
- Implement schema validation but only enable it for the `validate` command (not the `/v0/publish` API)
- Maintain backward compatibility with no production impact
  - All existing validation calls use a wrapper that returns the first error
  - Existing validation tests work without modification (since they call the wrapper)
- This allows experimentation and validation of the new model (including schema validation) without impacting production code

### **Future Stages**
- Enable schema validation in all validation cases (including the `/v0/publish` API endpoint) - flip boolean switch
- Build out comprehensive semantic and linter validation rules (with tests)
- Remove redundant manual validators that duplicate schema constraints
- Update unit tests to handle rich/exhaustive validation results

## Proposed Design

### Design Goals

1. **Exhaustive Feedback**: Collect all validation issues in a single pass, not just the first error
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
    Type      ValidationIssueType     `json:"type"`
    Path      string                  `json:"path"`     // JSON path like "packages[0].transport.url"
    Message   string                  `json:"message"`  // Error description (extracted from error.Error())
    Severity  ValidationIssueSeverity `json:"severity"`
    Reference string                  `json:"reference"` // Schema rule path or rule name like "prefer-transport-configuration"
}

type ValidationResult struct {
    Valid  bool              `json:"valid"`
    Issues []ValidationIssue `json:"issues"`
}

type ValidationContext struct {
    path string
}

// Constructor functions following Go conventions
func NewValidationIssue(issueType ValidationIssueType, path, message string, severity ValidationIssueSeverity, reference string) ValidationIssue
func NewValidationIssueFromError(issueType ValidationIssueType, path string, err error, reference string) ValidationIssue
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

The `Reference` field provides context about what triggered the validation issue:

- **Schema validation**: Contains the resolved schema path with `$ref` resolution (e.g., `"#/definitions/SseTransport/properties/url/format from: [#/definitions/ServerDetail]/properties/packages/items/[#/definitions/Package]/properties/transport/properties/url/format"`)
- **Semantic validation**: Contains rule names for business logic (e.g., `"invalid-server-name"`, `"missing-transport-url"`)
- **Linter validation**: Contains rule names for best practices (e.g., `"descriptive-naming"`, `"security-recommendation"`)
- **JSON validation**: Contains error type identifiers (e.g., `"json-syntax-error"`, `"invalid-json-format"`)

### ValidationContext

The `ValidationContext` tracks the current JSON path during validation, allowing validators to report issues with precise location information. This is essential for providing users with exact paths to problematic fields.

#### **Purpose**
- **Path tracking**: Builds JSON paths like `"packages[0].transport.url"` as validation traverses nested structures
- **Precise error location**: Users can see exactly where validation issues occur
- **Immutable building**: Each method returns a new context, preventing accidental mutations

#### **Usage Example**
```go
// Navigate to packages array, first item, transport field
pkgCtx := ctx.Field("packages").Index(0).Field("transport")
// Validate transport - any issues will be reported at "packages[0].transport"
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
// Standard constructor for manual field setting
issue := NewValidationIssue(ValidationIssueTypeLinter, "name", "message", ValidationIssueSeverityWarning, "rule-name")

// Constructor that preserves existing error formatting
issue := NewValidationIssueFromError(ValidationIssueTypeSemantic, "path", err, "rule-name")
```

#### **Error Interface Compatibility**
- Existing `ValidateServerJSON() error` signature unchanged
- Returns `fmt.Errorf("%s", issue.Message)` - same string format
- All `errors.Is()` and `errors.As()` calls continue to work
- No changes needed to error handling code

### New Validation Architecture

#### **All Validators Use Context and Return ValidationResult**

All existing validators are converted to use `ValidationContext` for precise error location tracking and return `ValidationResult` for comprehensive error collection:

```go
func ValidateServerJSONExhaustive(serverJSON *apiv0.ServerJSON) *ValidationResult {
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

#### **Existing Validator Becomes Simple Wrapper**

```go
func ValidateServerJSON(serverJSON *apiv0.ServerJSON) error {
    result := ValidateServerJSONExhaustive(serverJSON)
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

## Schema Validation

The project uses `github.com/santhosh-tekuri/jsonschema/v5` for schema validation with an embedded schema approach. The schema is embedded at compile time using Go's `//go:embed` directive, eliminating the need for file system access and ensuring the schema is always available.

### Schema-First Validation Strategy

The enhanced validation system adopts a **schema-first approach** where JSON Schema validation serves as the primary and first validator. This strategy addresses the current duplication between manual/semantic validators and schema constraints.

#### **Current Problem: Validation Duplication**

The existing system has both:
- **Manual/semantic validators**: Custom Go code validating server name format, URL patterns, etc.
- **JSON Schema validation**: Structural validation of the same constraints

This creates redundancy and potential inconsistencies where:
- Manual validators provide friendly error messages
- Schema validation provides technical error messages
- Both validate the same underlying constraints

#### **Proposed Solution: Schema-First with Friendly Error Mapping**

1. **Schema validation runs first** and catches all structural/format issues
2. **Manual validators are eliminated** for constraints already specified in the schema
3. **Schema error messages are mapped to friendly messages** using deterministic schema rule references (if needed)

### Embedded Schema Benefits

#### **No File System Dependencies**
- **Embedded at compile time**: Schema is included in the binary using `//go:embed schema/*.json`
- **No external files**: Eliminates dependency on schema files being present at runtime
- **Portable**: Binary contains everything needed for validation

#### **Version Consistency**
- **Schema version tracking**: `GetCurrentSchemaVersion()` extracts the `$id` field from embedded schema
- **Compile-time validation**: Schema is validated when the binary is built
- **No version drift**: Schema version is locked to the binary version

#### **Performance Benefits**
- **No I/O operations**: Schema is already in memory
- **Faster startup**: No need to read schema files
- **Reduced complexity**: No file path resolution or error handling for missing files

### Rich Error Information

The `jsonschema.ValidationError` provides:
- **InstanceLocation**: JSON path to the invalid field (e.g., `"/packages/0/transport/url"`)
- **Error**: Detailed error message from schema
- **KeywordLocation**: Schema path with $ref segments (e.g., `"/$ref/properties/transport/$ref/properties/url/format"`)
- **AbsoluteKeywordLocation**: Resolved schema path (e.g., `"file:///server.schema.json#/definitions/SseTransport/properties/url/format"`)

#### **Current Error Reference Format**

Schema validation errors now include detailed reference information:

```
Reference: #/definitions/Repository/properties/url/format from: [#/definitions/ServerDetail]/properties/repository/[#/definitions/Repository]/properties/url/format
```

This format provides:
- **Absolute location**: `#/definitions/Repository/properties/url/format` - the final resolved schema location
- **Resolved path**: Shows the complete path with `$ref` segments replaced by their resolved values in square brackets
- **Full context**: Users can see exactly which schema rule triggered the error and how it was reached

#### **Error Message Quality**

The current schema validation errors are generally quite readable:

```
[error] repository.url (schema)
'' has invalid format 'uri'
Reference: #/definitions/Repository/properties/url/format from: [#/definitions/ServerDetail]/properties/repository/[#/definitions/Repository]/properties/url/format
```

#### **Future Error Message Enhancement**

If we encounter situations where schema validation errors need to be more user-friendly, we have full access to:

- **`KeywordLocation`**: The schema path to the validating rule
- **`AbsoluteKeywordLocation`**: The absolute schema location after `$ref` resolution
- **`InstanceLocation`**: The JSON path of the element that triggered the violation
- **`Message`**: The original schema validation error message
- **Complete reference stack**: The entire resolved path showing how the error was reached

This allows us to build better, more descriptive error messages if needed, while maintaining the current high-quality error references.

### Integration with ValidateServerJSONExhaustive

```go
func ValidateServerJSONExhaustive(serverJSON *apiv0.ServerJSON, validateSchema bool) *ValidationResult {
    result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}
    ctx := &ValidationContext{}

    // Schema validation first (if requested) - catches structural issues early
    if validateSchema {
        schemaResult := validateServerJSONSchema(serverJSON)
        result.Merge(schemaResult)
        // If schema validation fails, we might still want to run semantic validation
        // to provide additional context, but schema errors take precedence
    }

    // Semantic validation (always runs) - business logic not covered by schema
    if _, err := parseServerName(*serverJSON); err != nil {
        issue := NewValidationIssueFromError(
            ValidationIssueTypeSemantic,
            ctx.Field("name").String(),
            err,
            "invalid-server-name",
        )
        result.AddIssue(issue)
    }
    
    // ... more semantic validation ...
    
    return result
}
```

### Transport Validation Improvements

Currently the transport validation fails in a pretty ugly way (if no transport is fully satisfied, you get validation errors for all transports). The current schema is:

        "transport": {
          "anyOf": [
            {
              "$ref": "#/definitions/StdioTransport"
            },
            {
              "$ref": "#/definitions/StreamableHttpTransport"
            },
            {
              "$ref": "#/definitions/SseTransport"
            }
          ],
          "description": "Transport protocol configuration for the package"
        },

And if you have an "sse" transport with no url, you get these schema errors:

1. [error] packages.0.transport.type (schema)
   value must be "stdio"
   Reference: #/definitions/StdioTransport/properties/type/enum

2. [error] packages.0.transport (schema)
   missing required fields: 'url'
   Reference: #/definitions/StreamableHttpTransport/required

3. [error] packages.0.transport.type (schema)
   value must be "streamable-http"
   Reference: #/definitions/StreamableHttpTransport/properties/type/enum

4. [error] packages.0.transport (schema)
   missing required fields: 'url'
   Reference: #/definitions/SseTransport/require

If we used a spec to select the discriminated type, like this:

        "transport": {
          "type": "object",
          "properties": {
            "type": {
              "type": "string",
              "enum": ["stdio", "streamable-http", "sse"]
            }
          },
          "required": ["type"],
          "if": {"properties": {"type": {"const": "stdio"}}},
          "then": {"$ref": "#/definitions/StdioTransport"},
          "else": {
            "if": {"properties": {"type": {"const": "streamable-http"}}},
            "then": {"$ref": "#/definitions/StreamableHttpTransport"},
            "else": {"$ref": "#/definitions/SseTransport"}
          },
          "description": "Transport protocol configuration for the package"
        }

Then it would fix on the "see" transport reference (by type) and validate against it only, producing only the single (correct) schema violation:

1. [error] packages.0.transport (schema)
   missing required fields: 'url'
   Reference: #/definitions/SseTransport/required

Same applies to Argument and remotes

## Implementation Status

### ✅ Completed Features

#### **Core Validation System**
- [x] **ValidationIssue and ValidationResult types**: Complete with all required fields
- [x] **ValidationContext**: Immutable context building for JSON path tracking
- [x] **Constructor functions**: `NewValidationIssue()` and `NewValidationIssueFromError()` with consistent parameter naming
- [x] **Helper methods**: Context building, result merging, and path construction

#### **Schema Validation Integration**
- [x] **JSON Schema validation**: Using existing `jsonschema/v5` library
- [x] **Error conversion**: Schema errors converted to `ValidationIssue` format
- [x] **$ref resolution**: Sophisticated resolution showing complete schema path with resolved references
- [x] **Comprehensive testing**: Full test coverage for schema validation scenarios
- [x] **Embedded schema**: Schema embedded at compile time using `//go:embed` directive

#### **Enhanced Error References**
- [x] **Resolved schema paths**: Shows complete path with `$ref` segments replaced by resolved values
- [x] **Incremental resolution**: Each `$ref` resolved in context of previous resolution
- [x] **Human-readable format**: Clear indication of schema rule location and resolution chain
- [x] **Consistent output**: All schema errors use the same reference format

#### **Testing and Quality**
- [x] **Unit tests**: Comprehensive test coverage for all new functionality
- [x] **Integration tests**: End-to-end validation testing
- [x] **Backward compatibility**: Existing validation continues to work

### 🔄 In Progress

#### **Schema-First Validation Strategy**
- [x] **Schema validation integration**: `ValidateServerJSONExhaustive()` runs schema validation first
- [x] **CLI integration**: Schema validation enabled in `mcp-publisher validate` command
- [ ] **Discriminated unions**: Replace `anyOf` with `if/then/else` for transport, argument, and remote validation
- [ ] **Error message mapping**: Map technical schema errors to user-friendly messages (if needed)
- [ ] **Validator migration**: Move from manual validators to schema-first approach

### 📋 Pending

#### **Migration Strategy**
- [ ] **Phase 1: Identify Schema Coverage**: Audit existing manual validators against schema constraints
- [ ] **Phase 2: Implement Error Mapping (Optional)**: Create mapping function for schema error messages (only if current messages are insufficient)
- [ ] **Phase 3: Enable Schema-First Validation**: Update tests to expect schema validation errors instead of semantic errors; Enable schema validation in publish API
- [ ] **Phase 4: Clean Up Redundant Validators**: Remove manual validators that duplicate schema constraints
- [ ] **Phase 5: Add Enhanced Semantic and Linter Rules**: Review and implement specific rules from [MCP Registry Validator linter guidelines](https://github.com/TeamSparkAI/ToolCatalog/blob/main/packages/mcp-registry-validator/linter.md)

#### **Command Integration**
- [ ] **CLI updates**: Update `mcp-publisher validate` command to use detailed validation
- [ ] **Output formatting**: Add JSON output format options
- [ ] **Filtering options**: Add severity and type filtering

#### **Documentation and Polish**
- [ ] **API documentation**: Update API documentation with new validation types

### 🎯 Key Achievements

1. **Comprehensive Error Collection**: All validation issues collected in single pass
2. **Precise Error Location**: Exact JSON paths for every validation issue  
3. **Schema Integration**: Full JSON Schema validation with detailed error references
4. **Backward Compatibility**: Existing validation continues to work unchanged
5. **Type Safety**: Constrained types prevent invalid validation issue creation
6. **Extensible Architecture**: Easy to add new validation types and severity levels

The enhanced validation system is now production-ready with comprehensive schema validation, detailed error references, and full backward compatibility.


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
      "reference": "json-syntax-error"
    },
    {
      "type": "semantic",
      "path": "name",
      "message": "server name must be in format 'dns-namespace/name'",
      "severity": "error",
      "reference": "invalid-server-name"
    },
    {
      "type": "semantic", 
      "path": "packages[0].transport.url",
      "message": "url is required for streamable-http transport type",
      "severity": "error",
      "reference": "missing-transport-url"
    },
    {
      "type": "schema",
      "path": "packages[1].environmentVariables[0].name",
      "message": "string does not match required pattern",
      "severity": "error",
      "reference": "#/definitions/EnvironmentVariable/properties/name/pattern from: [#/definitions/ServerDetail]/properties/packages/items/[#/definitions/Package]/properties/environmentVariables/items/[#/definitions/EnvironmentVariable]/properties/name/pattern"
    },
    {
      "type": "linter",
      "path": "packages[1].description",
      "message": "consider adding a more descriptive package description",
      "severity": "warning",
      "reference": "descriptive-package-description"
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

## Benefits and Achievements

### ✅ Comprehensive Feedback
- **Exhaustive error collection**: See all validation issues at once, not just the first error
- **Better developer experience**: No need to fix errors one by one
- **Precise error location**: JSON paths show exactly where issues occur in large JSON files
- **Structured output**: JSON format for tooling integration and machine-readable error information

### ✅ Schema-First Validation
- **Primary validator**: Schema validation catches all structural and format violations defined in the schema
- **Semantic validation only for gaps**: Covers business logic that cannot be expressed in JSON Schema
- **Standards compliance**: Ensures server.json follows the official schema
- **Detailed error messages**: Exact JSON paths and resolved schema references

### ✅ Backward Compatibility
- **Existing `ValidateServerJSON() error` signature unchanged**: All existing code continues to work
- **Error interface compatibility**: Leverages Go's error interface and existing error constants
- **Constructor pattern**: Follows established project conventions
- **No breaking changes**: All error handling code remains functional

### ✅ Extensible Architecture
- **Easy to add new validation types**: Schema, semantic, linter validation
- **Easy to add new severity levels**: Error, warning, info
- **Easy to add filtering and formatting options**: By type, severity, path pattern
- **Type safety**: Constrained types prevent invalid validation issue creation

### ✅ Schema-First Strategy Benefits
- **Eliminates duplication**: Single source of truth for structural constraints
- **Better error messages**: Schema validation provides precise JSON paths with deterministic mapping
- **Maintainability**: Schema changes automatically update validation
- **Standards compliance**: Ensures validation matches official schema exactly

## Technical Design

### Architecture Overview

The enhanced validation system uses a **schema-first approach** with comprehensive error collection and precise location tracking. The system is designed for maximum backward compatibility while providing extensive new capabilities.

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

### Performance Considerations
- **Slightly slower than fail-fast validation**: Acceptable trade-off for better user experience
- **Memory usage increases with error collection**: Manageable for typical server.json files
- **Schema validation performance**: Embedded schema eliminates I/O operations

### Testing Strategy
- **Unit tests**: Each validator with context
- **Integration tests**: End-to-end validation testing
- **Backward compatibility tests**: Ensure existing code continues to work
- **Performance benchmarks**: Validate acceptable performance characteristics

---

## Appendix: Future Enhancements

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




