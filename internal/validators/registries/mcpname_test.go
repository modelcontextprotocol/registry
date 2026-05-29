package registries_test

import (
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
)

func TestReadmeDeclaresMCPName(t *testing.T) {
	const server = "io.github.alice/foo"

	cases := []struct {
		name    string
		content string
		server  string
		want    bool
	}{
		{
			name:    "html comment marker as documented",
			content: "# My package\n\n<!-- mcp-name: io.github.alice/foo -->\n",
			server:  server,
			want:    true,
		},
		{
			name:    "bare line followed by newline",
			content: "mcp-name: io.github.alice/foo\nmore docs",
			server:  server,
			want:    true,
		},
		{
			name:    "marker at end of content",
			content: "see mcp-name: io.github.alice/foo",
			server:  server,
			want:    true,
		},
		{
			name:    "no marker",
			content: "this readme has no ownership marker",
			server:  server,
			want:    false,
		},
		{
			name:    "server name present without marker prefix",
			content: "install io.github.alice/foo from pip",
			server:  server,
			want:    false,
		},
		{
			// The core fix: a longer name that has the server name as a prefix
			// must not satisfy ownership for the shorter name.
			name:    "hyphen-suffixed longer name is rejected",
			content: "<!-- mcp-name: io.github.alice/foo-evil -->",
			server:  server,
			want:    false,
		},
		{
			name:    "dot-suffixed longer name is rejected",
			content: "mcp-name: io.github.alice/foo.bar\n",
			server:  server,
			want:    false,
		},
		{
			name:    "exact match for a name that itself ends in a suffix",
			content: "<!-- mcp-name: io.github.alice/foo-evil -->",
			server:  "io.github.alice/foo-evil",
			want:    true,
		},
		{
			// A prefix-only occurrence followed by a genuine match still passes.
			name:    "second occurrence is an exact match",
			content: "mcp-name: io.github.alice/foobar then mcp-name: io.github.alice/foo ",
			server:  server,
			want:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := registries.ReadmeDeclaresMCPName(tc.content, tc.server); got != tc.want {
				t.Errorf("ReadmeDeclaresMCPName(%q, %q) = %v, want %v", tc.content, tc.server, got, tc.want)
			}
		})
	}
}
