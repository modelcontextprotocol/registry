package validators

import "testing"

// SetHostResolverForTest overrides the package-level DNS resolver used by
// ValidateWriteRequest for the duration of tb.
func SetHostResolverForTest(tb testing.TB, r HostResolver) {
	tb.Helper()
	prev := defaultHostResolver
	defaultHostResolver = r
	tb.Cleanup(func() {
		defaultHostResolver = prev
	})
}
