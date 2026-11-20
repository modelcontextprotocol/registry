package usage

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// The collector replaces a Python prototype and must produce the same figures
// from the same access records. The helpers in this file reproduce the parts of
// Python's behaviour that those figures depend on: how a JSON value turns into
// text, how a request target is split and decoded, and how counters order
// their keys.

// fmul multiplies without letting the compiler fuse the product into a
// following addition, which would round differently on some architectures.
func fmul(a, b float64) float64 {
	return float64(a * b)
}

// pySpace reports whether Python's str.isspace() is true for r.
func pySpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// pyWord reports whether Python's \w matches r in a str pattern.
func pyWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

func pyStrip(s string) string {
	return strings.TrimFunc(s, pySpace)
}

// pyFloatRepr formats f the way Python's repr(float) does.
func pyFloatRepr(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	mantissa, exponentText, _ := strings.Cut(strconv.FormatFloat(f, 'e', -1, 64), "e")
	exponent, _ := strconv.Atoi(exponentText)
	sign := ""
	if strings.HasPrefix(mantissa, "-") {
		sign, mantissa = "-", mantissa[1:]
	}
	digits := strings.Replace(mantissa, ".", "", 1)
	if exponent < -4 || exponent >= 16 {
		out := digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		mark := "+"
		if exponent < 0 {
			mark, exponent = "-", -exponent
		}
		if exponent < 10 {
			mark += "0"
		}
		return sign + out + "e" + mark + strconv.Itoa(exponent)
	}
	switch {
	case exponent < 0:
		return sign + "0." + strings.Repeat("0", -exponent-1) + digits
	case len(digits) <= exponent+1:
		return sign + digits + strings.Repeat("0", exponent+1-len(digits)) + ".0"
	default:
		return sign + digits[:exponent+1] + "." + digits[exponent+1:]
	}
}

func isIntegerLiteral(number json.Number) bool {
	return !strings.ContainsAny(string(number), ".eE")
}

// pyStr returns Python's str() of a decoded JSON value. Numbers must have been
// decoded as json.Number so that 0 and 0.0 stay distinguishable.
func pyStr(value any) string {
	switch v := value.(type) {
	case nil:
		return "None"
	case string:
		return v
	case bool:
		if v {
			return "True"
		}
		return "False"
	case json.Number:
		if isIntegerLiteral(v) {
			text := strings.TrimLeft(strings.TrimPrefix(string(v), "-"), "0")
			if text == "" {
				return "0"
			}
			if strings.HasPrefix(string(v), "-") {
				return "-" + text
			}
			return text
		}
		f, err := strconv.ParseFloat(string(v), 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return string(v)
		}
		return pyFloatRepr(f)
	default:
		// Lists and objects never occur in these fields. Any stable text will do,
		// since it is only compared and counted, never shown.
		encoded, err := json.Marshal(v)
		if err != nil {
			return "?"
		}
		return string(encoded)
	}
}

// pyField is str(payload.get(key, "")).
func pyField(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}
	return pyStr(value)
}

func pyTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return v != ""
	case bool:
		return v
	case json.Number:
		f, err := strconv.ParseFloat(string(v), 64)
		return err != nil || f != 0
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}

// pyInt is Python's int() of a decoded JSON value, as a float64 because the
// result is only ever multiplied by a weight. It reports false where Python
// would raise.
func pyInt(value any) (float64, bool) {
	switch v := value.(type) {
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case json.Number:
		f, err := strconv.ParseFloat(string(v), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		if isIntegerLiteral(v) {
			return f, true
		}
		return math.Trunc(f), true
	case string:
		return pyIntText(v)
	default:
		return 0, false
	}
}

// pyIntText parses what Python's int(str) accepts: surrounding whitespace, an
// optional sign, and decimal digits with single underscores between them.
func pyIntText(text string) (float64, bool) {
	// Python treats the separators 0x1c to 0x1f as whitespace here only when
	// the text also contains a character outside ASCII.
	trimmed := strings.Trim(text, " \t\n\v\f\r")
	if strings.IndexFunc(text, func(r rune) bool { return r >= 0x80 }) >= 0 {
		trimmed = pyStrip(text)
	}
	runes := []rune(trimmed)
	var digits strings.Builder
	if len(runes) > 0 && (runes[0] == '+' || runes[0] == '-') {
		if runes[0] == '-' {
			digits.WriteByte('-')
		}
		runes = runes[1:]
	}
	if len(runes) == 0 {
		return 0, false
	}
	lastUnderscore := true
	for _, r := range runes {
		switch {
		case r == '_':
			if lastUnderscore {
				return 0, false
			}
			lastUnderscore = true
		case unicode.IsDigit(r):
			digits.WriteByte(digitValue(r))
			lastUnderscore = false
		default:
			return 0, false
		}
	}
	if lastUnderscore {
		return 0, false
	}
	f, err := strconv.ParseFloat(digits.String(), 64)
	if err != nil || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// digitValue returns a Unicode decimal digit as its ASCII equivalent. Every
// block of decimal digits is ten consecutive code points starting at zero,
// and blocks that touch each other still start at a multiple of ten.
func digitValue(r rune) byte {
	start := r
	for unicode.IsDigit(start - 1) {
		start--
	}
	return "0123456789"[(r-start)%10]
}

// pyDecodeUTF8 is bytes.decode('utf-8', 'replace'): each maximal invalid
// sequence becomes one replacement character.
func pyDecodeUTF8(data []byte) string {
	var out strings.Builder
	for i := 0; i < len(data); {
		lead := data[i]
		if lead < 0x80 {
			out.WriteByte(lead)
			i++
			continue
		}
		need, low, high := 0, byte(0x80), byte(0xBF)
		switch {
		case lead >= 0xC2 && lead <= 0xDF:
			need = 1
		case lead == 0xE0:
			need, low = 2, 0xA0
		case lead == 0xED:
			need, high = 2, 0x9F
		case lead >= 0xE1 && lead <= 0xEF:
			need = 2
		case lead == 0xF0:
			need, low = 3, 0x90
		case lead == 0xF4:
			need, high = 3, 0x8F
		case lead >= 0xF1 && lead <= 0xF3:
			need = 3
		}
		end, valid := i+1, need > 0
		for ; valid && need > 0; need-- {
			if end >= len(data) || data[end] < low || data[end] > high {
				valid = false
				break
			}
			low, high = 0x80, 0xBF
			end++
		}
		if valid {
			out.Write(data[i:end])
		} else {
			out.WriteRune(unicode.ReplacementChar)
		}
		i = end
	}
	return out.String()
}

func hexValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func unquoteASCII(run string) string {
	parts := strings.Split(run, "%")
	if len(parts) == 1 {
		return run
	}
	out := []byte(parts[0])
	for _, item := range parts[1:] {
		if len(item) >= 2 {
			high, okHigh := hexValue(item[0])
			low, okLow := hexValue(item[1])
			if okHigh && okLow {
				out = append(out, high<<4|low)
				out = append(out, item[2:]...)
				continue
			}
		}
		out = append(out, '%')
		out = append(out, item...)
	}
	return pyDecodeUTF8(out)
}

// pyUnquote is urllib.parse.unquote: percent escapes are decoded within each
// run of ASCII characters, and anything else is kept as it is.
func pyUnquote(text string) string {
	if !strings.Contains(text, "%") {
		return text
	}
	var out strings.Builder
	start, ascii := 0, true
	flush := func(end int) {
		if ascii {
			out.WriteString(unquoteASCII(text[start:end]))
		} else {
			out.WriteString(text[start:end])
		}
		start = end
	}
	for i, r := range text {
		if isASCII := r < 0x80; isASCII != ascii {
			flush(i)
			ascii = isASCII
		}
	}
	flush(len(text))
	return out.String()
}

var errUnsplittable = errors.New("request target cannot be split")

func isSchemeChar(r rune) bool {
	return r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' || r == '-' || r == '.')
}

// pyURLSplit returns the path and query that urllib.parse.urlsplit finds in a
// request target. It fails where Python raises.
func pyURLSplit(target string) (path, query string, err error) {
	target = strings.NewReplacer("\t", "", "\r", "", "\n", "").Replace(target)
	if i := strings.IndexByte(target, ':'); i > 0 {
		first := rune(target[0])
		if first < 0x80 && unicode.IsLetter(first) && strings.IndexFunc(target[:i], func(r rune) bool { return !isSchemeChar(r) }) < 0 {
			target = target[i+1:]
		}
	}
	netloc := ""
	if strings.HasPrefix(target, "//") {
		end := len(target)
		if i := strings.IndexAny(target[2:], "/?#"); i >= 0 {
			end = i + 2
		}
		netloc, target = target[2:end], target[end:]
		if strings.Contains(netloc, "[") != strings.Contains(netloc, "]") {
			return "", "", errUnsplittable
		}
	}
	target, _, _ = strings.Cut(target, "#")
	path, query, _ = strings.Cut(target, "?")
	if !netlocAllowed(netloc) {
		return "", "", errUnsplittable
	}
	return path, query, nil
}

// netlocAllowed mirrors urlsplit's check for characters that turn into URL
// delimiters under NFKC normalization.
func netlocAllowed(netloc string) bool {
	if strings.IndexFunc(netloc, func(r rune) bool { return r >= 0x80 }) < 0 {
		return true
	}
	stripped := strings.NewReplacer("@", "", ":", "", "#", "", "?", "").Replace(netloc)
	normalized := norm.NFKC.String(stripped)
	return stripped == normalized || !strings.ContainsAny(normalized, "/?#@:")
}

// pyParseQS is urllib.parse.parse_qs(query, keep_blank_values=True).
func pyParseQS(query string) map[string][]string {
	values := map[string][]string{}
	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		name, value, _ := strings.Cut(pair, "=")
		name = pyUnquote(strings.ReplaceAll(name, "+", " "))
		value = pyUnquote(strings.ReplaceAll(value, "+", " "))
		values[name] = append(values[name], value)
	}
	return values
}

// counter is collections.Counter with float values: keys keep the order in
// which they were first seen, which decides every tie.
type counter struct {
	index  map[string]int
	keys   []string
	values []float64
}

type entry struct {
	key   string
	value float64
}

func newCounter() *counter {
	return &counter{index: map[string]int{}}
}

func (c *counter) add(key string, value float64) {
	i, ok := c.index[key]
	if !ok {
		i = len(c.keys)
		c.index[key] = i
		c.keys = append(c.keys, key)
		c.values = append(c.values, 0)
	}
	c.values[i] += value
}

func (c *counter) get(key string) float64 {
	if i, ok := c.index[key]; ok {
		return c.values[i]
	}
	return 0
}

func (c *counter) len() int {
	return len(c.keys)
}

func (c *counter) entries() []entry {
	out := make([]entry, len(c.keys))
	for i, key := range c.keys {
		out[i] = entry{key, c.values[i]}
	}
	return out
}

// sum adds the values in key order, as sum(counter.values()) does.
func (c *counter) sum() float64 {
	total := 0.0
	for _, value := range c.values {
		total += value
	}
	return total
}

// mostCommon is Counter.most_common(): largest first, ties in key order.
func (c *counter) mostCommon() []entry {
	out := c.entries()
	sort.SliceStable(out, func(i, j int) bool { return out[i].value > out[j].value })
	return out
}

// top is the first key with the largest value.
func (c *counter) top() (entry, bool) {
	if len(c.keys) == 0 {
		return entry{}, false
	}
	best := 0
	for i, value := range c.values {
		if value > c.values[best] {
			best = i
		}
	}
	return entry{c.keys[best], c.values[best]}, true
}

// merge is Counter addition: existing keys keep their place, new keys follow.
func (c *counter) merge(other *counter) {
	for i, key := range other.keys {
		c.add(key, other.values[i])
	}
}
