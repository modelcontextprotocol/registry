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

// isMCPNameBoundary reports whether the content immediately following a matched
// server name terminates the token, so that the matched name is not merely a
// prefix of a longer declared name.
//
// A boundary is the end of content, any non-server-name character (whitespace, a
// newline, or an HTML tag delimiter such as `<`), or the start of an HTML comment
// close (`-->` / `--!>`). The comment-close case matters because PyPI and NuGet
// publishers commonly hide the token in `<!-- mcp-name: NAME -->`, and authors
// (or minifiers) frequently omit the space before `-->`, producing
// `<!-- mcp-name: NAME-->`. There the byte after NAME is `-`, which is a
// server-name character, so without this case the documented hidden-comment form
// would fail to validate.
func isMCPNameBoundary(rest string) bool {
	if rest == "" {
		return true
	}
	if !isServerNameChar(rest[0]) {
		return true
	}
	return strings.HasPrefix(rest, "-->") || strings.HasPrefix(rest, "--!>")
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
// but it still weakens the package↔server-name binding the token is meant to
// prove, so we require a trailing boundary after the server name (see
// isMCPNameBoundary).
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
		if isMCPNameBoundary(content[tokenEnd:]) {
			return true
		}
		// This occurrence is a prefix of a longer name; keep scanning in case a
		// properly-terminated occurrence appears later in the content.
		searchFrom = searchFrom + idx + 1
	}
}

// mcpNameTokenGluedTrailing explains why a visibly-present token failed to match.
// When containsMCPNameToken has already returned false, it reports whether the
// literal "mcp-name: <serverName>" string is nonetheless present and, if so, the
// character immediately following it — i.e. the trailing character that made the
// occurrence look like a prefix of a longer name rather than a complete token.
// Validators use it to turn an unhelpful "token must appear" message into an
// actionable "found it, but it's glued to %q — put it on its own line" message.
// Returns ("", false) when the literal token is absent (a genuinely missing token).
func mcpNameTokenGluedTrailing(content, serverName string) (trailing string, glued bool) {
	token := "mcp-name: " + serverName
	idx := strings.Index(content, token)
	if idx < 0 {
		return "", false
	}
	end := idx + len(token)
	if end >= len(content) {
		// A token at end-of-content is a valid boundary, so containsMCPNameToken
		// would not have failed; be defensive and treat it as not-glued.
		return "", false
	}
	return string(content[end]), true
}
