package validators

// Test-only exports of internal helpers so external test packages can verify them
// directly without going through the full schema validation flow.
var (
	ExtractFieldNameForTest      = extractFieldName
	TruncateForSuggestionForTest = truncateForSuggestion
)
