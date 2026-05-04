package auth

import "testing"

func TestClaimMatches(t *testing.T) {
	tests := []struct {
		name     string
		actual   any
		expected any
		want     bool
	}{
		{"scalar/scalar match", "x", "x", true},
		{"scalar/scalar mismatch", "x", "y", false},

		{"scalar actual in array expected", "x", []any{"x", "y"}, true},
		{"scalar actual not in array expected", "z", []any{"x", "y"}, false},

		{"array actual contains scalar expected", []any{"x", "y"}, "x", true},
		{"array actual missing scalar expected", []any{"y", "z"}, "x", false},

		// Pre-#1238 array/array comparison panicked with "comparing uncomparable type []interface {}".
		{"array/array overlap", []any{"a", "b"}, []any{"b", "c"}, true},
		{"array/array disjoint", []any{"a", "b"}, []any{"c", "d"}, false},

		// Exercises the []string branch of toAnySlice (concrete typed slice, not []any).
		{"[]string actual matches scalar", []string{"p", "q"}, "p", true},
		{"scalar matches []string expected", "p", []string{"p", "q"}, true},

		{"non-string scalar match", 42, 42, true},
		{"bool scalar match", true, true, true},
		{"empty actual array", []any{}, "x", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := claimMatches(tt.actual, tt.expected); got != tt.want {
				t.Errorf("claimMatches(%v, %v) = %v, want %v", tt.actual, tt.expected, got, tt.want)
			}
		})
	}
}
