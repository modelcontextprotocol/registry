# Enhanced Server Validation Design

NOTE: This document describes a proposed direction for improving validation of server.json data in the Official Registry. This work is in progress (including open PRs and discussions) in a collaborative process and may change significantly or be abandoned.

## Overview

This document outlines the design for implementing comprehensive server validation in the MCP Registry, due to the following concerns: 

- Currently, the MCP Registry project publishes a server.json schema but does not validate servers against it, allowing non-compliant servers to be published. 
- There is existing ad-hoc validation that covers some schema compliance, but not all (there are logical errors not identifiable by schema validation and that are not covered by the existing ad hoc validation). 
- Many servers that do pass validation do not represent best-practices for published servers. 

This design implements a three-tier validation system: **Schema Validation**, **Semantic Validation**, and **Linter Validation**.

## Current State

### Problems with Current Validation
- **No schema validation**: Servers are published without validating against the published schema (and many violate it)
- **Incomplete validation**: Ad hoc validation covers only some schema constraints (many published servers have additional logical errors)
- **Best Practices not indicated**: Many servers that would pass schema and semantic validation do not represent best practices
- **Fail-fast behavior**: Legacy `ValidateServerJSON()` stopped at first error (now replaced with exhaustive validation)
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
- Add `mcp-publisher validate` command that performs exhaustive validation with full schema validation
- Implement schema validation with configurable policy for non-current schemas
- Schema version validation consolidated in `schema.go` with policy support (Allow/Warn/Error)
- `mcp-publisher publish` command validates schema version (rejects empty and non-current schemas) but does not perform full schema validation
- API `/v0/publish` endpoint uses `ValidatePublishRequest` which validates schema version and semantic validation, but not full schema validation
- **All callers migrated**: All code now uses `ValidateServerJSON()` with `ValidationOptions` directly; legacy wrapper removed
- **ValidationResult.FirstError()**: Backward compatibility maintained via `FirstError()` method for code expecting error return type
- This allows experimentation and validation of the new model (including schema validation) without impacting production API

### **Future Stages**
- Enable full schema validation in `mcp-publisher publish` command (currently only validates schema version)
- Enable full schema validation in the `/v0/publish` API endpoint (currently only validates schema version via `ValidatePublishRequest`)
- Add `/v0/validate` API endpoint for programmatic validation without publishing (see Validate API Endpoint section below)
- Enhance production code to use full validation results: Update `importer.go` and `validate-examples/main.go` to log all issues instead of just first error
- Build out comprehensive semantic and linter validation rules (with tests)
- Remove redundant manual validators that duplicate schema constraints
- Consider migrating tests to check all validation issues instead of just first error (where appropriate)

## Proposed Design

### Design Goals

1. **Exhaustive Feedback**: Collect all validation issues in a single pass, not just the first error
2. **Precise Location**: Provide exact JSON paths for every validation issue
3. **Structured Output**: Return machine-readable validation results with consistent format
4. **Backward Compatibility**: Use `ValidationResult.FirstError()` for code expecting error return type
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

// SchemaVersionPolicy determines how non-current schema versions are handled
type SchemaVersionPolicy string

const (
    SchemaVersionPolicyAllow SchemaVersionPolicy = "allow" // Allow non-current schemas silently
    SchemaVersionPolicyWarn  SchemaVersionPolicy = "warn"  // Allow but generate warning
    SchemaVersionPolicyError SchemaVersionPolicy = "error" // Reject non-current schemas
)

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

For code that needs an error return type, use `ValidationResult.FirstError()`:

```go
result := ValidateServerJSON(serverJSON, ValidationSchemaVersionAndSemantic)
if err := result.FirstError(); err != nil {
    return err  // Returns first error-level issue as error
}
```

This maintains compatibility with existing error handling code while providing access to all validation issues.

### New Validation Architecture

#### **All Validators Use Context and Return ValidationResult**

All existing validators are converted to use `ValidationContext` for precise error location tracking and return `ValidationResult` for comprehensive error collection:

```go
func ValidateServerJSON(serverJSON *apiv0.ServerJSON, opts ValidationOptions) *ValidationResult {
    result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}
    
    // Schema validation based on options
    if opts.ValidateSchemaVersion || opts.ValidateSchema {
        schemaResult := validateServerJSONSchema(serverJSON, opts.ValidateSchema, opts.NonCurrentSchemaPolicy)
        result.Merge(schemaResult)
    }

    // Semantic validation (if requested)
    if opts.ValidateSemantic {
        // Validate server name - using existing error logic
        if _, err := parseServerName(*serverJSON); err != nil {
            issue := NewValidationIssueFromError(
                ValidationIssueTypeSemantic,
                "name",
                err,
                "invalid-server-name",
            )
            result.AddIssue(issue)
        }
        
        // Validate repository with context
        if repoResult := validateRepository(&ValidationContext{}, &serverJSON.Repository); !repoResult.Valid {
            result.Merge(repoResult)
        }
        
        // ... more semantic validation ...
    }
    
    return result
}
```

For backward compatibility with code that expects an error return type, `ValidationResult.FirstError()` can be used:

```go
result := ValidateServerJSON(serverJSON, ValidationSchemaVersionAndSemantic)
if err := result.FirstError(); err != nil {
    return err
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
- **Schema version tracking**: `model.CurrentSchemaURL` provides compile-time constant for current schema version
- **Version validation**: Schema version validation consolidated in `schema.go` with policy support (Allow/Warn/Error)
- **Empty schema handling**: Empty/missing schema fields always generate errors during validation
- **Compile-time validation**: Schema is validated when the binary is built
- **No version drift**: Schema version is locked to the binary version

#### **Performance Benefits**
- **No I/O operations**: Schema is already in memory
- **Faster startup**: No need to read schema files
- **Reduced complexity**: No file path resolution or error handling for missing files

### Rich Error Information

The `jsonschema.ValidationError` provides:
- **InstanceLocation**: JSON Pointer format (RFC 6901) path to the invalid field (e.g., `"/packages/0/transport/url"`)
- **Error**: Detailed error message from schema
- **KeywordLocation**: Schema path with $ref segments (e.g., `"/$ref/properties/transport/$ref/properties/url/format"`)
- **AbsoluteKeywordLocation**: Resolved schema path (e.g., `"file:///server.schema.json#/definitions/SseTransport/properties/url/format"`)

**Path Format Conversion**: JSON Pointer format paths from `InstanceLocation` are converted to bracket notation format to match semantic validation paths. The conversion transforms JSON Pointer paths like `"/packages/0/transport/url"` into bracket notation like `"packages[0].transport.url"`. This ensures consistent path formatting across all validation types (schema, semantic, and linter).

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
- **`InstanceLocation`**: The JSON Pointer format path (e.g., `"/packages/0/transport/url"`) which is converted to bracket notation (e.g., `"packages[0].transport.url"`) for consistency with semantic validation
- **`Message`**: The original schema validation error message
- **Complete reference stack**: The entire resolved path showing how the error was reached

This allows us to build better, more descriptive error messages if needed, while maintaining the current high-quality error references.

### Integration with ValidateServerJSON

```go
// ValidationOptions configures which types of validation to perform
type ValidationOptions struct {
    ValidateSchemaVersion  bool                 // Check schema version (empty, non-current)
    ValidateSchema         bool                 // Perform full schema validation (implies ValidateSchemaVersion)
    ValidateSemantic       bool                 // Perform semantic validation
    NonCurrentSchemaPolicy SchemaVersionPolicy  // Policy for non-current schemas
}

// Common validation configurations
var (
    ValidationSemanticOnly = ValidationOptions{
        ValidateSemantic: true,
    }
    
    ValidationSchemaVersionOnly = ValidationOptions{
        ValidateSchemaVersion: true,
        NonCurrentSchemaPolicy: SchemaVersionPolicyError,
    }
    
    ValidationSchemaVersionAndSemantic = ValidationOptions{
        ValidateSchemaVersion: true,
        ValidateSemantic: true,
        NonCurrentSchemaPolicy: SchemaVersionPolicyWarn,
    }
    
    ValidationAll = ValidationOptions{
        ValidateSchema: true,  // Implies ValidateSchemaVersion
        ValidateSemantic: true,
        NonCurrentSchemaPolicy: SchemaVersionPolicyWarn,
    }
)

func ValidateServerJSON(serverJSON *apiv0.ServerJSON, opts ValidationOptions) *ValidationResult {
    result := &ValidationResult{Valid: true, Issues: []ValidationIssue{}}
    ctx := &ValidationContext{}

    // Schema validation (version check and/or full validation)
    if opts.ValidateSchemaVersion || opts.ValidateSchema {
        schemaResult := validateServerJSONSchema(serverJSON, opts.ValidateSchema, opts.NonCurrentSchemaPolicy)
        result.Merge(schemaResult)
    }

    // Semantic validation (only if requested)
    if !opts.ValidateSemantic {
        return result
    }

    // ... semantic validation logic ...
    
    return result
}
```

### Schema Version Validation

Schema version validation is consolidated in `validateServerJSONSchema()` (now private) in `schema.go`:

- **Empty schema check**: Always performed when schema validation is requested, always generates an error
- **Schema file existence check**: Always performed when schema validation is requested - verifies the schema file exists in embedded schemas, even when not performing full validation
- **Schema version policy**: Controls how non-current schemas are handled (via `ValidationOptions.NonCurrentSchemaPolicy`):
  - `SchemaVersionPolicyAllow`: Non-current schemas are allowed with no warning
  - `SchemaVersionPolicyWarn`: Non-current schemas are allowed but generate a warning
  - `SchemaVersionPolicyError`: Non-current schemas are rejected with an error
- **Full schema validation**: Only performed if `performValidation` is `true`

The `mcp-publisher publish` command validates schema version (rejects empty, non-existent, and non-current schemas) but does not perform full schema validation. The `mcp-publisher validate` command performs full schema validation with `SchemaVersionPolicyWarn` (warns about non-current schemas but doesn't error).

### Request Validation Functions

Two consolidated validation functions in `validators` package handle publish and update requests:

- **`ValidatePublishRequest()`**: Validates publisher extensions, server JSON structure (via `ValidateServerJSON`), and registry ownership (if enabled)
- **`ValidateUpdateRequest()`**: Validates server JSON structure (via `ValidateServerJSON`) and registry ownership (if enabled), with option to skip registry validation for deleted servers

Both functions use `ValidateServerJSON()` with `ValidationSchemaVersionAndSemantic` and `FirstError()` for backward-compatible error handling. Registry ownership validation is extracted into a shared `validateRegistryOwnership()` helper function.

### Testing with Draft or Custom Schemas

The validation system supports testing against draft schemas or custom schema versions by embedding them in the validators package.

#### Setup Steps

1. **Copy the schema file**: Copy your schema file (e.g., `docs/reference/server-json/server.schema.json`) to `internal/validators/schemas/{version}.json`
   - Example: Copy to `internal/validators/schemas/draft.json` for draft schema testing
   - Ensure the schema file's `$id` field matches: `https://static.modelcontextprotocol.io/schemas/{version}/server.schema.json`
   - For draft schema, the `$id` should be: `https://static.modelcontextprotocol.io/schemas/draft/server.schema.json`

2. **Rebuild**: Recompile the Go binary to embed the new schema file (schemas are embedded at compile time)

3. **Use in server.json**: Reference the schema version in your `server.json` file:
   ```json
   {
     "$schema": "https://static.modelcontextprotocol.io/schemas/draft/server.schema.json",
     ...
   }
   ```

#### Schema Version Identifier Rules

Schema version identifiers can contain:
- **Letters**: A-Z, a-z
- **Digits**: 0-9
- **Special characters**: Hyphen (-), underscore (_), tilde (~), period (.)

Examples of valid identifiers: `2025-10-17`, `draft`, `test-v1.0`, `custom_schema~1.2.3`

#### Non-Current Schema Policy

When testing with draft or custom schemas, they will be treated as **non-current** schemas (since they don't match `model.CurrentSchemaURL`), which triggers the `NonCurrentSchemaPolicy` behavior:

- **`SchemaVersionPolicyAllow`**: Draft schemas are allowed with no warning
- **`SchemaVersionPolicyWarn`**: Draft schemas are allowed but generate a warning (default for `ValidationAll` and `ValidationSchemaVersionAndSemantic`)
- **`SchemaVersionPolicyError`**: Draft schemas are rejected with an error (default for `ValidationSchemaVersionOnly`)

#### Treating Draft as Current Schema

To test with a draft schema as if it were the current schema (no warnings/errors about non-current version):

1. Temporarily update `model.CurrentSchemaVersion` in `pkg/model/constants.go`:
   ```go
   const (
       CurrentSchemaVersion = "draft"  // Temporarily set for testing
       CurrentSchemaURL = "https://static.modelcontextprotocol.io/schemas/" + CurrentSchemaVersion + "/server.schema.json"
   )
   ```

2. Rebuild and test

3. **Important**: Revert the change before committing - `model.CurrentSchemaVersion` should always point to the latest official schema version

#### Example: Testing with Draft Schema

```bash
# 1. Copy draft schema
cp docs/reference/server-json/server.schema.json internal/validators/schemas/draft.json

# 2. Verify the $id field in draft.json is correct
# Should be: "https://static.modelcontextprotocol.io/schemas/draft/server.schema.json"

# 3. Rebuild
go build ./...

# 4. Use in server.json
# Set "$schema": "https://static.modelcontextprotocol.io/schemas/draft/server.schema.json"

# 5. Validate
mcp-publisher validate server.json
```

**Note**: The draft schema will be validated successfully, but you may see a warning about it not being the current schema version unless you temporarily update `model.CurrentSchemaVersion` as described above.

### Discriminated Union Error Consolidation

The schema uses `anyOf` for discriminated unions (transport, argument, remote), which causes noisy error messages when validation fails. When a transport/argument/remote doesn't match its specified type, `anyOf` validation tries all variants and reports errors for each one that doesn't match.

**Problem Example**: If you have an "sse" transport with no url, you get errors for all transport types:

1. [error] packages[0].transport.type (schema)
   value must be "stdio"
   Reference: #/definitions/StdioTransport/properties/type/enum

2. [error] packages[0].transport (schema)
   missing required fields: 'url'
   Reference: #/definitions/StreamableHttpTransport/required

3. [error] packages[0].transport.type (schema)
   value must be "streamable-http"
   Reference: #/definitions/StreamableHttpTransport/properties/type/enum

4. [error] packages[0].transport (schema)
   missing required fields: 'url'
   Reference: #/definitions/SseTransport/required

**Solution Strategy**: Since we cannot modify the schema (it's managed in the static repository), we'll detect and consolidate these `anyOf` error patterns in the validation error processing code (`addDetailedErrors` in `schema.go`). 

**Detection Strategy**:
- Identify groups of errors at the same JSON path (e.g., `packages[0].transport`)
- Detect pattern of multiple "type must be X" errors or multiple "missing required fields" errors from different schema definitions
- Extract the actual `type` value from the JSON being validated
- Filter out errors from non-matching transport/argument/remote definitions
- Consolidate remaining errors into a single, actionable error message

**Implementation Approach**:
- Add logic in `addDetailedErrors()` or a post-processing function to detect `anyOf` error clusters
- Group errors by instance location and analyze error patterns
- Identify the intended type from the JSON data
- Filter/consolidate errors to only show relevant issues for the actual type specified
- Preserve all other validation errors unchanged

This approach allows us to provide clearer error messages without modifying the schema, and can be applied to transport, argument, and remote validation.

**Future Enhancement**: If the schema is updated to use `if/then/else` discriminated unions in the future, this consolidation logic can be removed, but it provides immediate value without requiring schema changes.

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
- [x] **Path format normalization**: JSON Pointer paths converted to bracket notation to match semantic validation format (e.g., `/packages/0/transport` → `packages[0].transport`)

#### **Enhanced Error References**
- [x] **Resolved schema paths**: Shows complete path with `$ref` segments replaced by resolved values
- [x] **Incremental resolution**: Each `$ref` resolved in context of previous resolution
- [x] **Human-readable format**: Clear indication of schema rule location and resolution chain
- [x] **Consistent output**: All schema errors use the same reference format

#### **Testing and Quality**
- [x] **Unit tests**: Comprehensive test coverage for all new functionality
- [x] **Integration tests**: End-to-end validation testing
- [x] **Backward compatibility**: Existing validation continues to work

#### **Caller Migration**
- [x] **Function rename**: `ValidateServerJSONExhaustive` renamed to `ValidateServerJSON` (now takes `ValidationOptions` parameter)
- [x] **Legacy wrapper removed**: Old `ValidateServerJSON()` wrapper that returned `error` removed
- [x] **All callers migrated**: All production code and tests now use `ValidateServerJSON()` with `ValidationOptions` directly
- [x] **FirstError() helper**: `ValidationResult.FirstError()` method added for backward compatibility with error return types
- [x] **Request validators consolidated**: `ValidatePublishRequest` and `ValidateUpdateRequest` moved to validators package with shared `validateRegistryOwnership` helper

### 🔄 In Progress

#### **Schema-First Validation Strategy**
- [x] **Schema validation integration**: `ValidateServerJSON()` runs schema validation first
- [x] **CLI integration**: Schema validation enabled in `mcp-publisher validate` command
- [x] **Schema version validation**: Consolidated in `schema.go` with policy support (Allow/Warn/Error)
- [x] **Schema file existence check**: Schema version validation verifies schema file exists in embedded schemas
- [x] **Publish command schema checks**: `mcp-publisher publish` validates schema version (rejects empty, non-existent, and non-current schemas)
- [x] **API endpoint validation**: `/v0/publish` uses `ValidatePublishRequest` which validates schema version and semantic validation
- [ ] **Full schema validation in publish**: Enable full schema validation in `mcp-publisher publish` command
- [ ] **Full schema validation in API**: Enable full schema validation in `/v0/publish` API endpoint
- [ ] **Discriminated union error consolidation**: Detect and filter/consolidate noisy `anyOf` errors for transport, argument, and remote validation to show only relevant errors for the actual type
- [ ] **Error message mapping**: Map technical schema errors to user-friendly messages (if needed)
- [ ] **Validator migration**: Move from manual validators to schema-first approach

### 📋 Pending

#### **Migration Strategy**
- [ ] **Phase 1: Identify Schema Coverage**: Audit existing manual validators against schema constraints
- [ ] **Phase 2: Implement Error Mapping (Optional)**: Create mapping function for schema error messages (only if current messages are insufficient)
- [ ] **Phase 3: Error Consolidation**: Implement logic to detect and consolidate noisy `anyOf` errors from discriminated unions (transport, argument, remote)
- [ ] **Phase 4: Enable Schema-First Validation**: Update tests to expect schema validation errors instead of semantic errors; Enable schema validation in publish API
- [ ] **Phase 5: Clean Up Redundant Validators**: Remove manual validators that duplicate schema constraints
- [ ] **Phase 6: Add Enhanced Semantic and Linter Rules**: Review and implement specific rules from [MCP Registry Validator linter guidelines](https://github.com/TeamSparkAI/ToolCatalog/blob/main/packages/mcp-registry-validator/linter.md)

#### **Command Integration**
- [x] **CLI updates**: `mcp-publisher validate` command uses detailed validation with full schema validation
- [x] **Publish command**: `mcp-publisher publish` validates schema version (rejects empty, non-existent, and non-current schemas)
- [x] **Shared validation logic**: Both commands use `runValidationAndPrintIssues` to eliminate duplication
- [x] **Caller migration**: All callers migrated to use `ValidateServerJSON()` with `ValidationOptions` directly
- [x] **Request validation consolidation**: `ValidatePublishRequest` and `ValidateUpdateRequest` consolidated in validators package
- [ ] **Enhanced error reporting**: Update production code (importer, validate-examples tool) to log all issues instead of just first error
- [ ] **Output formatting**: Add JSON output format options
- [ ] **Filtering options**: Add severity and type filtering

#### **Validate API Endpoint**
- [ ] **POST /v0/validate endpoint**: API endpoint for validating server.json without publishing

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
- **Backward compatibility**: Use `ValidationResult.FirstError()` for code expecting error return type
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
- **API endpoint**: Validation as a service (see Validate API Endpoint section below)

## Validate API Endpoint

### Overview

A REST API endpoint (`POST /v0/validate`) that validates `server.json` files without publishing them to the registry. This endpoint provides programmatic access to the same validation logic used by the CLI commands, returning structured validation results in JSON format.

### Use Cases

- **CI/CD Pipelines**: Validate server.json files before attempting to publish
- **Editor/IDE Integrations**: Real-time validation feedback in development tools
- **Web UIs**: Validate files in browser-based interfaces
- **Pre-publish Checks**: Validate before authentication/publishing workflow
- **Validation as a Service**: Allow external tools to validate server.json format

### Implementation

#### Endpoint Specification

**Endpoint**: `POST /v0/validate`  
**Authentication**: None required (read-only operation)  
**Content-Type**: `application/json`

#### Request

Request body should be a valid `ServerJSON` object:

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
  "name": "io.example/server",
  "version": "1.0.0",
  ...
}
```

#### Response

Returns a `ValidationResult` in JSON format:

```json
{
  "valid": false,
  "issues": [
    {
      "type": "schema",
      "path": "packages[0].transport.url",
      "message": "missing required field: 'url'",
      "severity": "error",
      "reference": "#/definitions/SseTransport/required"
    },
    {
      "type": "semantic",
      "path": "name",
      "message": "server name must be in format 'dns-namespace/name'",
      "severity": "error",
      "reference": "invalid-server-name"
    }
  ]
}
```

**HTTP Status Codes**:
- `200 OK`: Validation completed successfully (regardless of whether valid or invalid)
- `400 Bad Request`: Malformed JSON or invalid request format

Note: A `200 OK` status does not mean the server.json is valid - check the `valid` field in the response body.

#### Implementation Details

**Location**: `internal/api/handlers/v0/validate.go`

**Handler Function**:
- Accepts `ServerJSON` in request body
- Calls `validators.ValidateServerJSON(serverJSON, validators.ValidationAll)`
- Returns `ValidationResult` as JSON response
- Uses Huma framework (same as publish endpoint) for request/response handling

**Key Differences from Publish Endpoint**:
- No authentication required (read-only)
- Does not save to database
- Returns structured validation results instead of published server response
- Returns warnings, not just errors (useful for comprehensive feedback)

**Reuses Existing Infrastructure**:
- Same validation functions as CLI commands
- Same `ValidationResult` type
- Same issue types and severity levels
- Consistent validation behavior across CLI and API

### Testing Strategy

#### Unit Tests

Test handler function with mocked dependencies:
- Valid server.json → `valid: true, issues: []`
- Invalid server.json → `valid: false` with specific issues
- Schema errors → issues with `type: "schema"`
- Semantic errors → issues with `type: "semantic"`
- Empty schema → `schema-field-required` issue
- Non-current schema → `schema-version-deprecated` issue
- Multiple issues → all issues returned in response
- Malformed JSON → proper error handling

#### Integration Tests

Follow patterns from `publish_integration_test.go`:
- Start test server
- Send HTTP POST requests with various `server.json` payloads
- Assert response JSON matches expected `ValidationResult` structure
- Verify HTTP status codes (200 for valid requests, 400 for malformed)
- Test both valid and invalid inputs
- Reuse test fixtures from `validation_detailed_test.go`

#### Test Infrastructure

- Reuse existing test server setup
- Use same patterns as `test_endpoints.sh` for manual testing
- Leverage existing validation test cases

### Future Enhancements

- **Query Parameters**: Optional parameters to filter by issue type or severity
- **Partial Validation**: Validate specific sections (e.g., only schema, only semantic)
- **Format Options**: Request different output formats (detailed vs. summary)
- **Batch Validation**: Validate multiple server.json files in one request




