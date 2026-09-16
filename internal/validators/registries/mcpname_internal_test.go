package registries

import (
	"strings"
	"testing"
)

// TestContainsMCPNameToken covers the boundary-anchored ownership-token match
// shared by the PyPI, NuGet, and Cargo validators — in particular that a server
// name which is a prefix of a longer declared name does not satisfy the match.
func TestContainsMCPNameToken(t *testing.T) {
	const name = "io.github.acme/widget"

	cases := []struct {
		desc    string
		content string
		want    bool
	}{
		{"exact on its own line", "intro text\nmcp-name: io.github.acme/widget\nmore text", true},
		{"exact at end of content", "mcp-name: io.github.acme/widget", true},
		{"followed by HTML tag", "<p>mcp-name: io.github.acme/widget</p>", true},
		{"followed by space", "mcp-name: io.github.acme/widget is the name", true},
		{"longer hyphenated name not a match", "mcp-name: io.github.acme/widget-pro\n", false},
		{"longer dotted name not a match", "mcp-name: io.github.acme/widget.core\n", false},
		{"longer slashed name not a match", "mcp-name: io.github.acme/widget/sub\n", false},
		{"absent", "nothing to see here", false},
		{"different name", "mcp-name: io.github.other/thing\n", false},
		{"prefix occurrence before a real one still matches", "mcp-name: io.github.acme/widget-pro then mcp-name: io.github.acme/widget\n", true},

		// HTML hidden-comment form (documented for PyPI/NuGet). The canonical
		// spaced form has always passed; the no-space variants must pass too,
		// since the byte after the name is the `-` of the comment close.
		{"comment, spaced (canonical)", "<!-- mcp-name: io.github.acme/widget -->", true},
		{"comment, no trailing space", "<!-- mcp-name: io.github.acme/widget-->", true},
		{"comment, no spaces at all", "<!--mcp-name: io.github.acme/widget-->", true},
		{"legacy comment close --!>", "<!-- mcp-name: io.github.acme/widget--!>", true},
		// A genuine longer name with a double hyphen is still NOT a match for the
		// shorter claim (it is not an HTML comment close).
		{"double-hyphen longer name not a match", "mcp-name: io.github.acme/widget--pro\n", false},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			if got := containsMCPNameToken(tc.content, name); got != tc.want {
				t.Fatalf("containsMCPNameToken(%q, %q) = %v, want %v", tc.content, name, got, tc.want)
			}
		})
	}
}

// TestMCPNameTokenGluedTrailing covers the diagnostic helper that explains why a
// visibly-present token failed: it reports the trailing character gluing the
// token to a longer name, or ("", false) when the token is genuinely absent.
func TestMCPNameTokenGluedTrailing(t *testing.T) {
	const name = "io.github.acme/widget"
	cases := []struct {
		desc         string
		content      string
		wantTrailing string
		wantGlued    bool
	}{
		{"absent", "no token here", "", false},
		{"glued period", "mcp-name: io.github.acme/widget.", ".", true},
		{"glued hyphen (longer name)", "mcp-name: io.github.acme/widget-pro", "-", true},
		{"glued slash", "mcp-name: io.github.acme/widget/x", "/", true},
		{"at end of content (boundary, not glued)", "mcp-name: io.github.acme/widget", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			gotTrailing, gotGlued := mcpNameTokenGluedTrailing(tc.content, name)
			if gotGlued != tc.wantGlued || gotTrailing != tc.wantTrailing {
				t.Fatalf("mcpNameTokenGluedTrailing(%q) = (%q, %v), want (%q, %v)",
					tc.content, gotTrailing, gotGlued, tc.wantTrailing, tc.wantGlued)
			}
		})
	}
}

// FuzzContainsMCPNameToken pins the core safety property of the boundary-anchored
// matcher: it is strictly stricter than a bare substring check. A true result
// must imply the literal token is present (strings.Contains). This guards against
// a future edit to isServerNameChar/isMCPNameBoundary accidentally accepting
// something the old behavior rejected — i.e. it can only ever flip pass→fail,
// never fail→pass. Runs the seed corpus under `go test`; exhaustively under
// `go test -fuzz`.
func FuzzContainsMCPNameToken(f *testing.F) {
	seeds := []struct{ content, name string }{
		{"mcp-name: io.github.acme/widget", "io.github.acme/widget"},
		{"mcp-name: io.github.acme/widget-pro", "io.github.acme/widget"},
		{"<!-- mcp-name: io.github.acme/widget-->", "io.github.acme/widget"},
		{"prefix mcp-name: a/b then mcp-name: a/b-c", "a/b"},
		{"", ""},
		{"mcp-name: ", ""},
		{"random text with no token", "io.github.x/y"},
	}
	for _, s := range seeds {
		f.Add(s.content, s.name)
	}
	f.Fuzz(func(t *testing.T, content, name string) {
		if containsMCPNameToken(content, name) && !strings.Contains(content, "mcp-name: "+name) {
			t.Fatalf("matcher accepted but literal token absent: content=%q name=%q", content, name)
		}
	})
}
