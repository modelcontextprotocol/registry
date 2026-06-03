package registries

import "testing"

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
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			if got := containsMCPNameToken(tc.content, name); got != tc.want {
				t.Fatalf("containsMCPNameToken(%q, %q) = %v, want %v", tc.content, name, got, tc.want)
			}
		})
	}
}
