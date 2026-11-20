package usage

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIdentifyingProductNamesAreWithheld covers the product names that say
// who or where a client is. The fixtures replay many more through whole days;
// these are the ones that cannot be put in a fixture, or that show where the
// rule deliberately withholds too much rather than too little.
func TestIdentifyingProductNamesAreWithheld(t *testing.T) {
	agent := func(text string) *string { return &text }
	for input, expected := range map[string]string{
		// A four-part version number cannot be told from an address.
		"tool-1.2.3.4/beta":   clientAddress,
		"192.0.2.40":          clientAddress,
		"ip-192-0-2-40":       clientAddress,
		"client/192.0.2.40":   "client", // only the product name is kept, and this one is a name
		"Net::HTTP2::Client":  clientAddress,
		"2001:db8::1%eth0":    clientAddress,
		"someone@example.org": clientEmail,
		// A domain that starts with a digit is still a domain, and a handle is still someone.
		"someone@163.example":                           clientEmail,
		"@someone@social.example":                       clientEmail,
		"Mozilla/5.0 (compatible; someone@example.org)": clientEmail,
		"Mozilla/5.0 (compatible; 2001:db8::5; x)":      clientAddress,
		// An address inside a longer word, where it starts and ends cannot be told.
		"host:2001:db8::1":          clientAddress,
		"2001:db8:0:0:0:0:0:1:8080": clientAddress,
		"feed2001:db8::1":           clientAddress,
		"someone@buildhost":         clientEmail,
		"desk-7.corp":               clientHost,
		"0123456789abcdef":          clientIdentifier,

		"tool/1.2.3.4":       "tool",
		"tool-1.2.3":         "tool-1.2.3",
		"tool@1.2.3":         "tool?1.2.3",
		"@scope/tool":        "?scope",
		"Some::Module":       "Some??Module",
		"example.org-bot":    "example.org-bot",
		"build-2026-11-20-1": "build-2026-11-20-1",
	} {
		assert.Equal(t, expected, clientName(agent(input)), input)
	}

	// A withheld name is an unnamed client: it falls back to the network's owner.
	for _, label := range []string{clientAddress, clientEmail, clientHost, clientIdentifier} {
		name, how := organization(label, "Example Network", kindOrganization)
		assert.Equal(t, []string{"Example Network", byNetworkOwner}, []string{name, how}, label)
		name, how = organization(label, "Amazon (AWS)", kindCloud)
		assert.Equal(t, []string{unnamedCloud, ""}, []string{name, how}, label)
	}
}

// TestFixtureHostsMatchTheGateway checks that the hosts this program leaves
// out are exactly the ones the gateway counts as fixture traffic. The counters
// and the access records must describe the same requests; if the gateway's
// rule changes, this fails until the list here follows.
func TestFixtureHostsMatchTheGateway(t *testing.T) {
	config, err := os.ReadFile(filepath.Join("..", "..", "deploy", "pkg", "k8s", "gatewaycanary", "assets", "vector.json"))
	require.NoError(t, err, "the gateway's metrics configuration has moved; point this test at it")
	rule := regexp.MustCompile(`traffic = if \((.*?)\) \{ \\"fixture\\" \}`).FindSubmatch(config)
	require.NotNil(t, rule, "the gateway no longer labels fixture traffic the way this test reads it")
	var hosts []string
	for _, match := range regexp.MustCompile(`p\.host == \\"([^"\\]+)\\"`).FindAllSubmatch(rule[1], -1) {
		hosts = append(hosts, string(match[1]))
	}
	assert.ElementsMatch(t, fixtureHosts, hosts)
	for _, host := range fixtureHosts {
		assert.True(t, isFixtureHost(host))
	}
	assert.False(t, isFixtureHost("other.gateway-canary.invalid"))
	assert.False(t, isFixtureHost(""))
}
