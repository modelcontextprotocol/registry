package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"
)

// utf8BOM is the UTF-8 byte order mark. Some Windows tools (e.g. PowerShell's
// Out-File) prepend it when writing UTF-8 files. RFC 8259 permits JSON parsers
// to ignore it, but Go's encoding/json rejects it, so it is stripped before
// parsing.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// stripUTF8BOM returns data without a leading UTF-8 byte order mark.
func stripUTF8BOM(data []byte) []byte {
	return bytes.TrimPrefix(data, utf8BOM)
}

func validateJSONUnicode(filename string, data []byte) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("%s must be encoded as UTF-8", filename)
	}

	if path, ok := findUnpairedSurrogate(data, "$"); ok {
		return fmt.Errorf("%s contains an unpaired UTF-16 surrogate escape at %s", filename, path)
	}

	return nil
}

func findUnpairedSurrogate(raw json.RawMessage, path string) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", false
	}

	switch raw[0] {
	case '"':
		if hasUnpairedSurrogateEscape(raw) {
			return path, true
		}
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return "", false
		}
		for key, value := range obj {
			if badPath, ok := findUnpairedSurrogate(value, path+"."+key); ok {
				return badPath, true
			}
		}
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return "", false
		}
		for i, item := range items {
			if badPath, ok := findUnpairedSurrogate(item, fmt.Sprintf("%s[%d]", path, i)); ok {
				return badPath, true
			}
		}
	}

	return "", false
}

func hasUnpairedSurrogateEscape(s []byte) bool {
	for i := 1; i < len(s)-1; i++ {
		if s[i] != '\\' || i+5 >= len(s) || s[i+1] != 'u' {
			continue
		}

		code, ok := parseHex4(s[i+2 : i+6])
		if !ok {
			continue
		}

		if isLowSurrogate(code) {
			return true
		}

		if !isHighSurrogate(code) {
			i += 5
			continue
		}

		if i+11 >= len(s) || s[i+6] != '\\' || s[i+7] != 'u' {
			return true
		}

		next, ok := parseHex4(s[i+8 : i+12])
		if !ok || !isLowSurrogate(next) {
			return true
		}
		i += 11
	}

	return false
}

func parseHex4(b []byte) (rune, bool) {
	v, err := strconv.ParseInt(string(b), 16, 32)
	if err != nil {
		return 0, false
	}
	return rune(v), true
}

func isHighSurrogate(r rune) bool {
	return r >= 0xD800 && r <= 0xDBFF
}

func isLowSurrogate(r rune) bool {
	return r >= 0xDC00 && r <= 0xDFFF
}
