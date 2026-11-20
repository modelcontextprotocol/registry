package usage

import (
	"bytes"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prototypeCases holds, for a list of made-up inputs, what the Python
// prototype's own functions returned. Each entry is one call: the arguments
// followed by the result. See testdata/README.md.
type prototypeCases map[string][][]json.RawMessage

func loadPrototypeCases(t *testing.T) prototypeCases {
	t.Helper()
	var cases prototypeCases
	require.NoError(t, json.Unmarshal(readFixture(t, "prototype_cases.json"), &cases))
	return cases
}

func decode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var value T
	require.NoError(t, json.Unmarshal(raw, &value))
	return value
}

// literal decodes JSON text the way an access record's field is decoded.
func literal(t *testing.T, text string) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader([]byte(text)))
	decoder.UseNumber()
	var value any
	require.NoError(t, decoder.Decode(&value))
	return value
}

// TestPrototypeFunctions replays the prototype's classification and parsing
// functions against their ports, one input at a time.
func TestPrototypeFunctions(t *testing.T) {
	cases := loadPrototypeCases(t)
	text := func(raw json.RawMessage) string { return decode[string](t, raw) }
	optional := func(raw json.RawMessage) *string { return decode[*string](t, raw) }

	for _, c := range cases["client_name"] {
		assert.Equal(t, text(c[1]), clientName(optional(c[0])), "client_name(%s)", c[0])
	}
	for _, c := range cases["generic_client"] {
		assert.Equal(t, decode[bool](t, c[1]), genericClient.MatchString(text(c[0])), "generic client %s", c[0])
	}
	for _, c := range cases["endpoint"] {
		assert.Equal(t, text(c[2]), endpoint(text(c[0]), text(c[1])), "endpoint(%s, %s)", c[0], c[1])
	}
	for _, c := range cases["lookup"] {
		name, found := requestedServer(text(c[0]), text(c[1]))
		expected := optional(c[2])
		if assert.Equal(t, expected != nil, found, "lookup(%s, %s)", c[0], c[1]) && found {
			assert.Equal(t, *expected, name, "lookup(%s, %s)", c[0], c[1])
		}
	}
	for _, c := range cases["parse_qs"] {
		assert.Equal(t, decode[map[string][]string](t, c[1]), pyParseQS(text(c[0])), "parse_qs(%s)", c[0])
	}
	for _, c := range cases["listing_use"] {
		assert.Equal(t, text(c[1]), listingUse(pyParseQS(text(c[0]))), "listing_use(%s)", c[0])
	}
	for _, c := range cases["outcome"] {
		assert.Equal(t, text(c[1]), outcome(text(c[0])), "outcome(%s)", c[0])
	}
	for _, c := range cases["organization"] {
		name, how := organization(text(c[0]), text(c[1]), text(c[2]))
		assert.Equal(t, text(c[3]), name, "organization(%s, %s, %s)", c[0], c[1], c[2])
		expected := ""
		if by := optional(c[4]); by != nil {
			expected = *by
		}
		assert.Equal(t, expected, how, "organization(%s, %s, %s)", c[0], c[1], c[2])
	}
	for _, c := range cases["unquote"] {
		assert.Equal(t, text(c[1]), pyUnquote(text(c[0])), "unquote(%s)", c[0])
	}
	for _, c := range cases["urlsplit"] {
		path, query, err := pyURLSplit(text(c[0]))
		if expected := optional(c[1]); expected == nil {
			assert.Error(t, err, "urlsplit(%s) fails in the prototype", c[0])
		} else if assert.NoError(t, err, "urlsplit(%s)", c[0]) {
			assert.Equal(t, []string{*expected, text(c[2])}, []string{path, query}, "urlsplit(%s)", c[0])
		}
	}
	for _, c := range cases["page_size"] {
		if limit := text(c[0]); limit == defaultLimit || limit[:min(len(limit), 7)] != "default" {
			assert.Equal(t, text(c[1]), limitLabel(limit), "page size label for %s", c[0])
		}
	}
	for _, c := range cases["type_label"] {
		assert.Equal(t, text(c[2]), typeLabel(decode[[]string](t, c[0]), decode[[]string](t, c[1])), "type_label(%s, %s)", c[0], c[1])
	}
}

// TestPrototypeValues replays how the prototype reads a field of an access
// record: as text, as a number, and as true or false.
func TestPrototypeValues(t *testing.T) {
	cases := loadPrototypeCases(t)
	for _, c := range cases["str"] {
		input := decode[string](t, c[0])
		assert.Equal(t, decode[string](t, c[1]), pyStr(literal(t, input)), "str(%s)", input)
	}
	for _, c := range cases["truthy"] {
		input := decode[string](t, c[0])
		assert.Equal(t, decode[bool](t, c[1]), pyTruthy(literal(t, input)), "bool(%s)", input)
	}
	for _, c := range cases["int"] {
		input := decode[string](t, c[0])
		value, ok := pyInt(literal(t, input))
		expected := decode[*float64](t, c[1])
		if assert.Equal(t, expected != nil, ok, "int(%s)", input) && ok {
			assert.Equal(t, *expected, value, "int(%s)", input)
		}
	}
	for _, c := range cases["pct"] {
		assert.Equal(t, decode[string](t, c[2]), pct(decode[float64](t, c[0]), decode[float64](t, c[1])), "pct(%s, %s)", c[0], c[1])
	}
	for _, c := range cases["count"] {
		assert.Equal(t, decode[string](t, c[1]), approximate(decode[float64](t, c[0])), "count(%s)", c[0])
	}
}

// TestPrototypeNetworks replays how the prototype names a network and decides
// what kind it is, and how it orders source addresses.
func TestPrototypeNetworks(t *testing.T) {
	cases := loadPrototypeCases(t)
	for _, c := range cases["describe"] {
		description, listedName, listedKind := decode[string](t, c[0]), decode[string](t, c[1]), decode[string](t, c[2])
		lookup := newOwnerLookup(&NetworkTable{listed: map[string]listedNetwork{"64496": {name: listedName, kind: listedKind}}})
		assert.Equal(t, owner{decode[string](t, c[3]), decode[string](t, c[4])}, lookup.describe("64496", description),
			"describe(%q, %q, %q)", description, listedName, listedKind)
	}
	for _, c := range cases["describe_alias"] {
		aliases, err := ParseOrganizationAliases(decode[string](t, c[0]))
		require.NoError(t, err)
		description, listedName, listedKind := decode[string](t, c[1]), decode[string](t, c[2]), decode[string](t, c[3])
		lookup := newOwnerLookup(&NetworkTable{aliases: aliases, listed: map[string]listedNetwork{"64496": {name: listedName, kind: listedKind}}})
		assert.Equal(t, owner{decode[string](t, c[4]), decode[string](t, c[5])}, lookup.describe("64496", description),
			"describe(%q, %q, %q) with aliases", description, listedName, listedKind)
	}
	for _, c := range cases["tidy"] {
		assert.Equal(t, decode[string](t, c[1]), tidyName(decode[string](t, c[0])), "tidy(%s)", c[0])
	}
	for _, c := range cases["address"] {
		input := decode[string](t, c[0])
		key, ok := parseIPKey(input)
		expected := decode[*string](t, c[1])
		if assert.Equal(t, expected != nil, ok, "ip_address(%q)", input) && ok {
			value := new(big.Int).Lsh(new(big.Int).SetUint64(key.high), 64)
			value.Add(value, new(big.Int).SetUint64(key.low))
			assert.Equal(t, *expected, value.String(), "int(ip_address(%q))", input)
		}
	}
}
