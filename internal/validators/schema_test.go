package validators_test

import (
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators"
	"github.com/stretchr/testify/assert"
)

func TestConvertJSONPointerToBracketNotation(t *testing.T) {
	tests := []struct {
		name           string
		jsonPointer    string
		expectedOutput string
		description    string
	}{
		{
			name:           "single array index in middle",
			jsonPointer:    "/packages/0/transport",
			expectedOutput: "packages[0].transport",
			description:    "JSON Pointer with single array index converts to bracket notation",
		},
		{
			name:           "multiple array indices",
			jsonPointer:    "/packages/0/transport/1/url",
			expectedOutput: "packages[0].transport[1].url",
			description:    "JSON Pointer with multiple array indices converts correctly",
		},
		{
			name:           "leading array index",
			jsonPointer:    "/0/name",
			expectedOutput: "[0].name",
			description:    "JSON Pointer starting with array index converts to leading bracket",
		},
		{
			name:           "trailing array index",
			jsonPointer:    "/packages/0",
			expectedOutput: "packages[0]",
			description:    "JSON Pointer ending with array index converts correctly",
		},
		{
			name:           "no array indices",
			jsonPointer:    "/name/version",
			expectedOutput: "name.version",
			description:    "JSON Pointer without array indices converts to dot notation only",
		},
		{
			name:           "complex nested path",
			jsonPointer:    "/packages/0/runtimeArguments/1/name",
			expectedOutput: "packages[0].runtimeArguments[1].name",
			description:    "Complex nested JSON Pointer with multiple indices converts correctly",
		},
		{
			name:           "multiple consecutive indices",
			jsonPointer:    "/a/0/1/2",
			expectedOutput: "a[0][1][2]",
			description:    "JSON Pointer with consecutive array indices converts to consecutive brackets",
		},
		{
			name:           "single character path with index",
			jsonPointer:    "/a/0",
			expectedOutput: "a[0]",
			description:    "Simple JSON Pointer with single field and index converts correctly",
		},
		{
			name:           "empty string",
			jsonPointer:    "",
			expectedOutput: "",
			description:    "Empty JSON Pointer returns empty string",
		},
		{
			name:           "root path",
			jsonPointer:    "/",
			expectedOutput: "",
			description:    "Root JSON Pointer (just slash) converts to empty string",
		},
		{
			name:           "only index",
			jsonPointer:    "/0",
			expectedOutput: "[0]",
			description:    "JSON Pointer with only array index converts to bracket notation",
		},
		{
			name:           "two digit index",
			jsonPointer:    "/packages/10/transport",
			expectedOutput: "packages[10].transport",
			description:    "JSON Pointer with multi-digit array index converts correctly",
		},
		{
			name:           "three digit index",
			jsonPointer:    "/packages/123/transport",
			expectedOutput: "packages[123].transport",
			description:    "JSON Pointer with three-digit array index converts correctly",
		},
		{
			name:           "remotes array index",
			jsonPointer:    "/remotes/0/url",
			expectedOutput: "remotes[0].url",
			description:    "JSON Pointer for remotes array converts correctly",
		},
		{
			name:           "package arguments nested",
			jsonPointer:    "/packages/0/packageArguments/0/format",
			expectedOutput: "packages[0].packageArguments[0].format",
			description:    "JSON Pointer with nested array structures converts correctly",
		},
		{
			name:           "repository url",
			jsonPointer:    "/repository/url",
			expectedOutput: "repository.url",
			description:    "JSON Pointer without array indices converts to simple dot notation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validators.ConvertJSONPointerToBracketNotation(tt.jsonPointer)
			assert.Equal(t, tt.expectedOutput, result, "%s: JSON Pointer format should convert to bracket notation", tt.description)
		})
	}
}
