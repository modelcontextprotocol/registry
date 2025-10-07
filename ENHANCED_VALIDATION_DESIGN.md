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

The project uses `github.com/santhosh-tekuri/jsonschema/v5` for schema validation with an embedded schema approach. The schema is embedded at compile time using Go's `//go:embed` directive, eliminating the need for file system access and ensuring the schema is always available.

### Schema Validation Integration

```go
func validateServerJSONSchema(serverJSON *apiv0.ServerJSON) *ValidationResult {
    result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}
    
    // Use embedded schema - no file system access needed
    schemaData := embeddedSchema
    
    // Parse the schema
    var schema map[string]any
    if err := json.Unmarshal(schemaData, &schema); err != nil {
        // Handle schema parsing error
        issue := NewValidationIssue(
            ValidationIssueTypeSchema,
            "",
            fmt.Sprintf("failed to parse schema file: %v", err),
            ValidationIssueSeverityError,
            "schema-parse-error",
        )
        result.AddIssue(issue)
        return result
    }
    
    // Convert server JSON to map for validation
    serverData, err := json.Marshal(serverJSON)
    if err != nil {
        // Handle JSON marshaling error
        return result
    }
    
    var serverMap map[string]any
    if err := json.Unmarshal(serverData, &serverMap); err != nil {
        // Handle JSON unmarshaling error
        return result
    }
    
    // Validate against schema using jsonschema library
    compiler := jsonschema.NewCompiler()
    if err := compiler.AddResource("file:///server.schema.json", bytes.NewReader(schemaData)); err != nil {
        // Handle schema resource error
        return result
    }
    
    schemaInstance, err := compiler.Compile("file:///server.schema.json")
    if err != nil {
        // Handle schema compilation error
        return result
    }
    
    // Validate the server JSON against the schema
    if err := schemaInstance.Validate(serverMap); err != nil {
        // Convert jsonschema.ValidationError to ValidationIssue with $ref resolution
        if validationErr, ok := err.(*jsonschema.ValidationError); ok {
            addValidationError(result, validationErr, schema)
        }
    }
    
    return result
}
```

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

### Integration with ValidateServerJSONDetailed

```go
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

## Schema-First Validation Strategy

### Primary Validation Approach

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
3. **Schema error messages are mapped to friendly messages** using deterministic schema rule references

### Enhanced Schema Error References

The current implementation provides comprehensive error context through sophisticated `$ref` resolution, making schema validation errors highly readable and informative.

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

### Validation Order and Scope

#### **Schema Validation (Primary)**
- **Runs first** and catches all structural/format violations
- **Comprehensive coverage** of all schema-defined constraints
- **Friendly error messages** via deterministic mapping
- **JSON path precision** for exact error location

#### **Semantic Validation (Secondary)**
- **Runs after schema validation** for business logic not expressible in schema
- **Focused scope**: Only validates constraints not covered by schema
- **Examples**: Namespace matching rules, transport configuration logic, registry-specific constraints

#### **Linter Validation (Tertiary)**
- **Runs last** for best practice recommendations
- **Non-blocking**: Warnings and suggestions, not errors
- **Examples**: Descriptive naming suggestions, security recommendations

### Migration Strategy

#### **Phase 1: Identify Schema Coverage**
- Audit existing manual validators against schema constraints
- Identify validators that duplicate schema validation
- Document which validators can be eliminated

#### **Phase 2: Implement Error Mapping (Optional)**
- Create mapping function for schema error messages (only if current messages are insufficient)
- Test mapping with existing validation scenarios
- Ensure friendly messages match current manual validator messages
- **Note**: Current schema error messages with `$ref` resolution are generally readable and may not need additional mapping

#### **Phase 3: Enable Schema-First Validation**
- [x] Update `ValidateServerJSONDetailed()` to run schema validation first (with optional parameter)
- [x] Schema validation is enabled in `mcp-publisher validate` command
- [ ] Update tests to expect schema validation errors instead of semantic errors
- [ ] Enable schema validation in publish API (currently uses `ValidateServerJSON()` without schema validation)

#### **Phase 4: Clean Up Redundant Validators**
- Remove manual validators that duplicate schema constraints
- Keep only semantic validators for business logic
- Update documentation to reflect new validation strategy

#### **Phase 5: Add Enhanced Semantic and Linter Rules**
- [ ] Review and implement specific rules from [MCP Registry Validator linter guidelines](https://github.com/TeamSparkAI/ToolCatalog/blob/main/packages/mcp-registry-validator/linter.md)
- [ ] Create comprehensive test coverage for new validation rules


### Benefits of Schema-First Strategy

#### **Eliminates Duplication**
- Single source of truth for structural constraints
- No conflicting validation logic between manual and schema validators
- Consistent validation behavior across all tools

#### **Better Error Messages**
- Schema validation provides precise JSON paths
- Deterministic mapping ensures consistent friendly messages
- No dependency on error message text parsing

#### **Maintainability**
- Schema changes automatically update validation
- No need to maintain parallel validation logic
- Clear separation between structural and business logic validation

#### **Standards Compliance**
- Ensures validation matches official schema exactly
- Schema is the authoritative specification
- Reduces risk of validation drift


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


## Schema Validation Improvements

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

## Current Implementation Status

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
- [ ] **Discriminated unions**: Replace `anyOf` with `if/then/else` for transport, argument, and remote validation
- [ ] **Error message mapping**: Map technical schema errors to user-friendly messages
- [ ] **Validator migration**: Move from manual validators to schema-first approach

### 📋 Pending

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


