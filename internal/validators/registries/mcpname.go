package registries

import "strings"

// validServerNameByte reports whether b can appear in an MCP server name
// (namespace/name) per the registry's server name grammar:
// [a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]/[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9].
func validServerNameByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '.', b == '-', b == '_', b == '/':
		return true
	default:
		return false
	}
}

// ReadmeDeclaresMCPName reports whether content contains an
// "mcp-name: <serverName>" ownership marker that matches serverName in full.
//
// The matched name must end at a non-name character or the end of content, so a
// README declaring a longer name that merely has serverName as a prefix — e.g.
// "mcp-name: io.github.alice/foo-evil" for the server "io.github.alice/foo" — is
// not accepted. This mirrors the exact match npm performs against the package's
// mcpName field and the documented contract that the "$SERVER_NAME portion MUST
// match" the server name from server.json.
func ReadmeDeclaresMCPName(content, serverName string) bool {
	const prefix = "mcp-name: "
	for offset := 0; ; {
		idx := strings.Index(content[offset:], prefix)
		if idx < 0 {
			return false
		}
		nameStart := offset + idx + len(prefix)
		if strings.HasPrefix(content[nameStart:], serverName) {
			nameEnd := nameStart + len(serverName)
			if nameEnd == len(content) || !validServerNameByte(content[nameEnd]) {
				return true
			}
		}
		offset = nameStart
	}
}
