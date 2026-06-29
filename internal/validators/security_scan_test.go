package validators_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// securityScanSchemaPath points at the canonical, CI-synced draft schema that the
// io.modelcontextprotocol.registry/security-scan extension is defined in. The same
// file is compiled by tools/validate-examples; keeping the path relative to the
// validators package keeps this test runnable via `go test ./internal/validators/...`.
const securityScanSchemaPath = "../../docs/reference/server-json/draft/server.schema.json"

// compileServerSchema compiles the draft server.schema.json the way the registry's
// validate-examples tool does, so the test exercises the published schema rather
// than a hand-rolled subset.
func compileServerSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7
	schema, err := compiler.Compile(filepath.Clean(securityScanSchemaPath))
	require.NoError(t, err, "draft server.schema.json should compile")
	return schema
}

// serverJSONWithScanReceipt wraps a single security-scan receipt in an otherwise
// valid server.json document under the _meta extension key, so the receipt is
// validated exactly as a downstream client would encounter it.
func serverJSONWithScanReceipt(receipt map[string]any) map[string]any {
	return map[string]any{
		"$schema":     "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
		"name":        "com.example/test-server",
		"description": "A test server",
		"version":     "1.0.0",
		"_meta": map[string]any{
			"io.modelcontextprotocol.registry/security-scan": []any{receipt},
		},
	}
}

// validScanReceipt returns a clean receipt that binds to a well-formed artifact
// digest and a non-empty scan scope. Cases below mutate a copy of this base.
func validScanReceipt() map[string]any {
	return map[string]any{
		"scanner":                 "example-scanner",
		"scanner_version":         "1.2.3",
		"scanned_artifact_digest": "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"scan_scope":              []any{"dependency", "package"},
		"verdict":                 "clean",
		"scanned_at":              "2026-06-28T00:00:00Z",
		"attestation":             "publisher-asserted",
	}
}

// cloneReceipt makes a shallow copy so each case mutates independently.
func cloneReceipt(base map[string]any) map[string]any {
	out := make(map[string]any, len(base))
	for k, v := range base {
		out[k] = v
	}
	return out
}

// TestSecurityScanReceiptSchema is the downstream-client test for the
// io.modelcontextprotocol.registry/security-scan extension added in #1404.
// It confirms the draft schema accepts a clean receipt that binds to a
// matching artifact digest with a non-empty scan_scope, and rejects receipts
// that would let a "clean" claim be surfaced without that binding: a
// mismatched/malformed scanned_artifact_digest, an empty scan_scope, and an
// inconclusive verdict missing its machine-readable inconclusive_reason.
func TestSecurityScanReceiptSchema(t *testing.T) {
	schema := compileServerSchema(t)

	tests := []struct {
		name        string
		mutate      func(r map[string]any)
		expectValid bool
		description string
	}{
		{
			name:        "clean receipt with matching digest and non-empty scope is accepted",
			mutate:      func(_ map[string]any) {},
			expectValid: true,
			description: "A clean verdict that binds to a well-formed artifact digest and lists a scan_scope is the accepted shape",
		},
		{
			name: "mismatched/malformed scanned_artifact_digest is rejected",
			mutate: func(r map[string]any) {
				// Not in algorithm:hex form, so it cannot bind a verdict to exact bytes.
				r["scanned_artifact_digest"] = "not-a-digest"
			},
			expectValid: false,
			description: "A scanned_artifact_digest that violates the algorithm:hex pattern must be rejected so clean never binds to unjoinable bytes",
		},
		{
			name: "missing scanned_artifact_digest is rejected",
			mutate: func(r map[string]any) {
				delete(r, "scanned_artifact_digest")
			},
			expectValid: false,
			description: "scanned_artifact_digest is required so a clean verdict always binds to a specific artifact",
		},
		{
			name: "empty scan_scope is rejected",
			mutate: func(r map[string]any) {
				r["scan_scope"] = []any{}
			},
			expectValid: false,
			description: "An empty scan_scope does not say what was evaluated, so it must be rejected (minItems 1)",
		},
		{
			name: "inconclusive verdict without inconclusive_reason is rejected",
			mutate: func(r map[string]any) {
				r["verdict"] = "inconclusive"
				// inconclusive_reason intentionally omitted
			},
			expectValid: false,
			description: "An inconclusive verdict must carry a machine-readable inconclusive_reason so it never collapses into clean",
		},
		{
			name: "inconclusive verdict with inconclusive_reason is accepted",
			mutate: func(r map[string]any) {
				r["verdict"] = "inconclusive"
				r["inconclusive_reason"] = "artifact_digest_mismatch"
			},
			expectValid: true,
			description: "Once an inconclusive verdict names its reason it is representable rather than collapsing into clean or findings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			receipt := validScanReceipt()
			tt.mutate(receipt)

			doc := serverJSONWithScanReceipt(receipt)

			// Round-trip through JSON to normalize types the way a real client
			// reading a server.json document would.
			raw, err := json.Marshal(doc)
			require.NoError(t, err)
			var instance any
			require.NoError(t, json.Unmarshal(raw, &instance))

			err = schema.Validate(instance)
			if tt.expectValid {
				assert.NoError(t, err, "%s", tt.description)
			} else {
				assert.Error(t, err, "%s", tt.description)
			}
		})
	}
}
