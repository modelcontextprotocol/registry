package usage

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gzipped(t *testing.T, text string) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	_, err := writer.Write([]byte(text))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return out.Bytes()
}

func TestOwnerLookup(t *testing.T) {
	lookup := newOwnerLookup(fixtureNetworks(t, true, true, fixtureAliases))
	for address, expected := range map[string]owner{
		"192.0.2.1":        {"Litware", kindOrganization},
		"192.0.2.95":       {"Litware", kindOrganization},
		"203.0.113.230":    {"Litware", kindOrganization}, // a second key for the same organization, ahead of a hosting provider's
		"192.0.2.127":      {"Small Telecom 15", kindProvider},
		"192.0.2.128":      {"Amazon (AWS)", kindCloud},
		"198.51.100.40":    {"Example Telecom Ltd", kindProvider},
		"198.51.100.100":   {"Example Savings Bank", kindOrganization},
		"198.51.100.130":   {"Northwind Fiber Co-op (AS64502)", kindProvider},
		"198.51.100.200":   {unknownOwner, kindUnknown}, // announced by no one
		"203.0.113.250":    {unknownOwner, kindUnknown}, // past the last range
		"192.0.1.255":      {unknownOwner, kindUnknown}, // before the first range
		"2001:db8::1":      {unknownOwner, kindUnknown},
		"::ffff:192.0.2.1": {unknownOwner, kindUnknown},
		"":                 {unknownOwner, kindUnknown},
	} {
		assert.Equal(t, expected, lookup.lookup(address), address)
	}

	assert.Equal(t, owner{unknownOwner, kindUnknown}, lookup.lookup("192.0.2.1"+" "), "an address is not trimmed")

	// Two networks share a name. Its kind is that of the one seen last.
	assert.Equal(t, owner{"Acme Widgets Inc", kindOrganization}, lookup.lookup("198.51.100.70"))
	assert.Equal(t, kindOrganization, lookup.kinds()["Acme Widgets Inc"])
	assert.Equal(t, owner{"Acme Widgets Inc", kindProvider}, lookup.lookup("203.0.113.40"))
	assert.Equal(t, kindProvider, lookup.kinds()["Acme Widgets Inc"])
	assert.NotContains(t, lookup.kinds(), unknownOwner)
}

// TestOrganizationAliases checks that an organization's networks get one name
// only when an alias says so. No organization is built into the program.
func TestOrganizationAliases(t *testing.T) {
	plain := newOwnerLookup(fixtureNetworks(t, true, true, ""))
	assert.Equal(t, owner{"Litware, Inc.", kindOrganization}, plain.lookup("192.0.2.1"), "named from its registered description")
	assert.Equal(t, owner{"Amazon (AWS)", kindCloud}, plain.lookup("203.0.113.230"), "this description also names a hosting provider")
	for _, known := range knownNetworks {
		assert.Equal(t, kindCloud, known.kind, "%s: only hosting providers are built in", known.name)
	}

	aliases, err := ParseOrganizationAliases(" LITWARE=Litware ;\n\nlit-ware = Litware\r\nwoodgrove=Woodgrove Bank, N.A.;")
	require.NoError(t, err)
	assert.Equal(t, []NetworkAlias{{"LITWARE", "Litware"}, {"LIT-WARE", "Litware"}, {"WOODGROVE", "Woodgrove Bank, N.A."}}, aliases)
	none, err := ParseOrganizationAliases("")
	require.NoError(t, err)
	assert.Empty(t, none)
	for _, invalid := range []string{"LITWARE", "LI=Litware", "LITWARE=", "=Litware", "LITWARE=Lit|ware", "LITWARE=A=B", "LITWARE=Lit\tware",
		"LITWARE=" + strings.Repeat("x", 65)} {
		_, err := ParseOrganizationAliases(invalid)
		assert.Error(t, err, invalid)
	}
}

func TestNetworkTableRejectsMalformedFiles(t *testing.T) {
	table := &NetworkTable{}
	require.Error(t, table.parseOwners([]byte("192.0.2.0\t192.0.2.255\t64496\tUS\tExample\n192.0.2.0\t192.0.2.255\n")))
	assert.False(t, table.OwnersLoaded(), "one malformed row rejects the whole table")
	require.Error(t, table.parseOwners([]byte("192.0.2.0\tnot-an-address\t64496\tUS\tExample\n")))
	require.NoError(t, table.parseOwners([]byte("192.0.2.0\t192.0.2.255\t64496\tUS\tExample\r\n198.51.100.0\t198.51.100.255\t64497\tUS\tOther")))
	assert.Len(t, table.starts, 2)

	require.Error(t, table.parseTypes([]byte(`{"data":[{"name":"No number"}]}`)))
	require.Error(t, table.parseTypes([]byte(`not json`)))
	assert.False(t, table.TypesLoaded())
	require.NoError(t, table.parseTypes([]byte(`{"data":[{"asn":64496,"name":null,"info_type":"NSP"}]}`)))
	assert.Equal(t, listedNetwork{name: "", kind: "NSP"}, table.listed["64496"])
}

// TestReferenceFilesAreCached checks that the public reference files are
// fetched when missing or old, and that an older copy keeps working when a
// refresh fails.
func TestReferenceFilesAreCached(t *testing.T) {
	ctx := t.Context()
	owners := "192.0.2.0\t192.0.2.255\t64496\tUS\tEXAMPLE-NET - Example Network\n"
	available := true
	requests := map[string]int{}
	var agents []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		agents = append(agents, r.Header.Get("User-Agent"))
		switch {
		case !available:
			http.Error(w, "down", http.StatusBadGateway)
		case r.URL.Path == "/owners":
			_, _ = w.Write(gzipped(t, owners))
		default:
			_, _ = io.WriteString(w, `{"data":[{"asn":64496,"name":"Example Network","info_type":"Content"}]}`)
		}
	}))
	t.Cleanup(server.Close)
	dir := filepath.Join(t.TempDir(), "cache")
	files := NetworkFiles{CacheDir: dir, OwnersURL: server.URL + "/owners", TypesURL: server.URL + "/types", UserAgent: "collector-test",
		Client: server.Client()}

	table := LoadNetworkTable(ctx, files)
	require.True(t, table.OwnersLoaded())
	require.True(t, table.TypesLoaded())
	assert.Equal(t, owner{"Example Network", kindOrganization}, newOwnerLookup(table).lookup("192.0.2.9"))
	assert.Equal(t, map[string]int{"/owners": 1, "/types": 1}, requests)
	assert.Equal(t, "collector-test", agents[0])

	LoadNetworkTable(ctx, files)
	assert.Equal(t, map[string]int{"/owners": 1, "/types": 1}, requests, "fresh copies are not fetched again")

	old := time.Now().Add(-8 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, ownersFile), old, old))
	require.NoError(t, os.Chtimes(filepath.Join(dir, typesFile), old, old))
	available = false
	table = LoadNetworkTable(ctx, files)
	assert.Equal(t, 2, requests["/owners"], "a week-old copy is refreshed")
	assert.True(t, table.OwnersLoaded() && table.TypesLoaded(), "the old copy is used when the refresh fails")

	// A refresh that answers with something unusable does not cost the copy that worked.
	available, owners = true, "an error page, not a table\n"
	table = LoadNetworkTable(ctx, files)
	assert.Equal(t, 3, requests["/owners"])
	assert.Equal(t, owner{"Example Network", kindOrganization}, newOwnerLookup(table).lookup("192.0.2.9"))
	assert.NoFileExists(t, filepath.Join(dir, ownersFile+".tmp"))

	// A cached copy that cannot be read is removed, so the next run fetches a new one.
	require.NoError(t, os.WriteFile(filepath.Join(dir, typesFile), []byte(`{"data": []}`), 0o600))
	table = LoadNetworkTable(ctx, files)
	assert.False(t, table.TypesLoaded())
	assert.NoFileExists(t, filepath.Join(dir, typesFile))
	assert.True(t, LoadNetworkTable(ctx, files).TypesLoaded())

	available = false
	empty := LoadNetworkTable(ctx, NetworkFiles{CacheDir: filepath.Join(t.TempDir(), "none"), OwnersURL: server.URL + "/owners",
		TypesURL: server.URL + "/types", Client: server.Client()})
	assert.False(t, empty.OwnersLoaded())
	assert.Equal(t, owner{unknownOwner, kindUnknown}, newOwnerLookup(empty).lookup("192.0.2.9"))
}

func TestSettingsValidation(t *testing.T) {
	valid := Settings{GCPProject: "example-project", ClusterName: "example-cluster", LogNamespace: "ingress-nginx", LogContainer: "nginx",
		MetricsURL: "http://metrics.monitoring:8428", DatabaseURL: "postgres://usage", LookbackDays: 13, MaxRecords: 100000, LogReadsPerMinute: 50,
		DollarsPerGiB: 0.1, OrganizationAliases: fixtureAliases, ReaderRole: "dashboards@example-project.iam"}
	require.NoError(t, valid.Validate(true))

	withoutDatabase := valid
	withoutDatabase.DatabaseURL = ""
	require.NoError(t, withoutDatabase.Validate(false))
	require.ErrorContains(t, withoutDatabase.Validate(true), "USAGE_COLLECTOR_DATABASE_URL is required")

	// Values that end up inside the log filter cannot break out of their quotes.
	injected := valid
	injected.ClusterName = `x" OR resource.type="gce_instance`
	require.ErrorContains(t, injected.Validate(true), "USAGE_COLLECTOR_CLUSTER_NAME may only contain")

	broken := Settings{MetricsURL: "metrics.monitoring:8428", LookbackDays: 45, MaxRecords: 10, DollarsPerGiB: -1,
		OrganizationAliases: "LITWARE", ReaderRole: strings.Repeat("r", 64)}
	err := broken.Validate(true)
	for _, problem := range []string{"GCP_PROJECT is required", "CLUSTER_NAME is required", "LOG_NAMESPACE is required", "METRICS_URL must be",
		"DATABASE_URL is required", "LOOKBACK_DAYS must be", "MAX_RECORDS must be", "LOG_READS_PER_MINUTE must be", "DOLLARS_PER_GIB cannot be",
		"ORGANIZATION_ALIASES: an alias is written KEY=Name", "READER_ROLE is not a role name"} {
		assert.ErrorContains(t, err, problem)
	}
}

func TestSettingsFromEnvironment(t *testing.T) {
	t.Setenv("USAGE_COLLECTOR_GCP_PROJECT", "example-project")
	t.Setenv("USAGE_COLLECTOR_CLUSTER_NAME", "example-cluster")
	t.Setenv("USAGE_COLLECTOR_METRICS_URL", "http://metrics.monitoring:8428")
	t.Setenv("USAGE_COLLECTOR_LOOKBACK_DAYS", "5")
	settings, err := LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, "example-project", settings.GCPProject)
	assert.Equal(t, "ingress-nginx", settings.LogNamespace)
	assert.Equal(t, 5, settings.LookbackDays)
	assert.Equal(t, 100000, settings.MaxRecords)
	assert.InDelta(t, 0.10, settings.DollarsPerGiB, 1e-12)
	assert.NotEmpty(t, settings.CacheDir)
	require.NoError(t, settings.Validate(false))
}
