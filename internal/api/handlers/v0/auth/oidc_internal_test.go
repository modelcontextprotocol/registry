package auth

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/config"
)

func TestClaimMatches(t *testing.T) {
	type group string
	tests := []struct {
		name     string
		actual   any
		expected any
		want     bool
	}{
		{"scalar/scalar match", "x", "x", true},
		{"scalar/scalar mismatch", "x", "y", false},
		{"named string match", group("x"), group("x"), true},
		{"different concrete string types", group("x"), "x", false},

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
		{"bool list overlap", []any{false, true}, []any{true}, true},
		{"number list overlap", []any{1.25, 2.5}, 2.5, true},
		{"no scalar coercion", "42", 42.0, false},
		{"[]string overlaps decoded list", []string{"p", "q"}, []any{"q"}, true},
		{"empty actual array", []any{}, "x", false},
		{"empty expected array", "x", []string{}, false},
		{"nulls", nil, nil, false},
		{"objects", map[string]any{}, map[string]any{}, false},
		{"nested lists", []any{[]any{"x"}}, []any{[]any{"x"}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := claimMatches(tt.actual, tt.expected); got != tt.want {
				t.Errorf("claimMatches(%v, %v) = %v, want %v", tt.actual, tt.expected, got, tt.want)
			}
		})
	}
}

func TestClaimMatchesRejectsMixedLists(t *testing.T) {
	for name, invalid := range map[string]any{
		"object": map[string]any{"key": "value"},
		"list":   []any{"x"},
		"bool":   true,
		"number": 1.0,
		"null":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			for _, mixed := range [][]any{{invalid, "x"}, {"x", invalid}} {
				if claimMatches(mixed, "x") || claimMatches("x", mixed) {
					t.Fatal("mixed list matched a scalar")
				}
			}
		})
	}
}

func TestValidateExtraClaims(t *testing.T) {
	claims := &OIDCClaims{ExtraClaims: map[string]any{
		"group": "token-group", "active": true, "unused": map[string]any{},
	}}
	for _, tt := range []struct {
		name  string
		rules string
		valid bool
	}{
		{"unset", "", true},
		{"empty rules", "[]", true},
		{"all rules match", `[{"group":["token-group"]},{"active":true}]`, true},
		{"missing claim", `[{"group":"token-group"},{"missing":true}]`, false},
		{"mismatch", `[{"group":"configured-group"}]`, false},
		{"invalid rule value", `[{"group":["token-group","configured-group",true]}]`, false},
		{"null configuration", `null`, false},
		{"null rule", `[{"group":"token-group"},null]`, false},
		{"malformed configuration", `[{`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := OIDCHandler{config: &config.Config{OIDCExtraClaims: tt.rules}}
			err := h.validateExtraClaims(claims)
			if (err == nil) != tt.valid {
				t.Fatalf("validateExtraClaims() error = %v, want valid = %v", err, tt.valid)
			}
			if err != nil && (strings.Contains(err.Error(), "token-group") || strings.Contains(err.Error(), "configured-group")) {
				t.Fatal("error includes claim values")
			}
		})
	}
}
