package registries

import "strings"

// isServerNameChar reports whether c can appear in an MCP server name.
//
// Server names follow the schema pattern ^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$
// (reverse-DNS namespace + "/" + name), so the full set of characters that may
// continue a server name is [A-Za-z0-9._/-].
func isServerNameChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '.', c == '-', c == '_', c == '/':
		return true
	default:
		return false
	}
}

// containsMCPNameToken reports whether the package README/description contains
// the ownership token "mcp-name: <serverName>" as a complete token — i.e. the
// matched server name is not merely a prefix of a longer declared name.
//
// A bare strings.Contains check is vulnerable to prefix confusion: a README that
// legitimately declares `mcp-name: io.github.acme/widget-pro` would otherwise
// satisfy an ownership claim for the shorter `io.github.acme/widget`, because the
// shorter string is a substring of the longer one. This is contained by namespace
// authorization (a publisher can only claim names within a namespace they own),
// but it still weakens the crate↔server-name binding the token is meant to prove,
// so we require a trailing boundary: the character following the server name must
// be the end of the content or any non-server-name character (whitespace, a
// newline, or an HTML tag delimiter from a rendered README such as `<`).
//
// Shared by the README-token validators (PyPI, NuGet, Cargo). NPM is unaffected
// because it compares an exact metadata field rather than scanning README text.
func containsMCPNameToken(content, serverName string) bool {
	token := "mcp-name: " + serverName
	searchFrom := 0
	for {
		idx := strings.Index(content[searchFrom:], token)
		if idx < 0 {
			return false
		}
		tokenEnd := searchFrom + idx + len(token)
		if tokenEnd >= len(content) || !isServerNameChar(content[tokenEnd]) {
			return true
		}
		// This occurrence is a prefix of a longer name; keep scanning in case a
		// properly-terminated occurrence appears later in the content.
		searchFrom = searchFrom + idx + 1
	}
}
