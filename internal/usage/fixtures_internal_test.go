package usage

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures under testdata are synthetic: addresses come only from the
// ranges reserved for documentation, and every name is made up. The golden
// files beside each scenario were produced by the Python prototype this
// package replaces, run on these same fixtures. See testdata/README.md.

type scenario struct {
	Day          string   `json:"day"`
	MaxRecords   int      `json:"max_records"`
	Entries      string   `json:"entries"`
	Counters     string   `json:"counters"`
	Fail         []string `json:"fail"`
	Networks     bool     `json:"networks"`
	NetworkTypes bool     `json:"network_types"`
	Catalogue    bool     `json:"catalogue"`
	DropFields   []string `json:"drop_fields"`
	DropBefore   string   `json:"drop_before"`
	// Rows says whether the scenario has a rows.json beside its report.
	Rows bool `json:"rows"`
	// OrganizationAliases is the alias setting, as an operator would write it.
	OrganizationAliases string `json:"organization_aliases"`
}

const fixtureAliases = "LITWARE=Litware;lit-ware = Litware"

func scenarioNames(t *testing.T) []string {
	t.Helper()
	found, err := os.ReadDir(filepath.Join("testdata", "scenarios"))
	require.NoError(t, err)
	names := make([]string, 0, len(found))
	for _, item := range found {
		names = append(names, item.Name())
	}
	return names
}

func readFixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, parts...)...))
	require.NoError(t, err)
	return data
}

func loadScenario(t *testing.T, name string) scenario {
	t.Helper()
	var loaded scenario
	require.NoError(t, json.Unmarshal(readFixture(t, "scenarios", name, "scenario.json"), &loaded))
	return loaded
}

func (s scenario) day(t *testing.T) time.Time {
	t.Helper()
	day, err := time.Parse("2006-01-02", s.Day)
	require.NoError(t, err)
	return day
}

func (s scenario) fails(kind string) bool {
	for _, failing := range s.Fail {
		if failing == kind {
			return true
		}
	}
	return false
}

// entries returns the scenario's log entries as raw JSON, with the fields the
// scenario leaves out removed. Numbers keep the exact text they had.
func (s scenario) entries(t *testing.T) []json.RawMessage {
	t.Helper()
	var out []json.RawMessage
	for _, line := range bytes.Split(readFixture(t, "entries", s.Entries+".jsonl"), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		if len(s.DropFields) == 0 {
			out = append(out, json.RawMessage(line))
			continue
		}
		var item map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(line, &item))
		var stamp string
		require.NoError(t, json.Unmarshal(item["timestamp"], &stamp))
		if s.DropBefore == "" || stamp < s.DropBefore {
			var payload map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(item["jsonPayload"], &payload))
			for _, field := range s.DropFields {
				delete(payload, field)
			}
			encoded, err := json.Marshal(payload)
			require.NoError(t, err)
			item["jsonPayload"] = encoded
		}
		encoded, err := json.Marshal(item)
		require.NoError(t, err)
		out = append(out, encoded)
	}
	return out
}

// fakeGateway stands in for Cloud Logging and the metrics store.
type fakeGateway struct {
	server   *httptest.Server
	mu       sync.Mutex
	filters  []string
	queries  []string
	logCalls int
}

func noSleep(context.Context, time.Duration) error {
	return nil
}

func newFakeGateway(t *testing.T, s scenario) *fakeGateway {
	t.Helper()
	entries := s.entries(t)
	var counters struct {
		Requests json.RawMessage   `json:"requests"`
		Bytes    json.RawMessage   `json:"bytes"`
		Hourly   []json.RawMessage `json:"hourly"`
	}
	require.NoError(t, json.Unmarshal(readFixture(t, "entries", s.Counters+".counters.json"), &counters))
	start := s.day(t).Unix()
	gateway := &fakeGateway{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/entries:list", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Filter    string `json:"filter"`
			PageToken string `json:"pageToken"`
			PageSize  int    `json:"pageSize"`
			OrderBy   string `json:"orderBy"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || r.Header.Get("Authorization") != "Bearer test-token" || body.OrderBy != "timestamp asc" {
			http.Error(w, `{"error":{"status":"INVALID_ARGUMENT"}}`, http.StatusBadRequest)
			return
		}
		gateway.mu.Lock()
		gateway.filters = append(gateway.filters, body.Filter)
		gateway.logCalls++
		gateway.mu.Unlock()
		// The fixture holds one day. Any other day has no access records.
		available := entries
		if !strings.Contains(body.Filter, `timestamp>="`+s.Day+`T00:00:00Z"`) {
			available = nil
		}
		offset, _ := strconv.Atoi(body.PageToken)
		// Smaller pages than asked for, as Cloud Logging may return, to exercise paging.
		end := min(offset+min(body.PageSize, 400), len(available))
		page := map[string]any{"entries": available[offset:end]}
		if end < len(available) {
			page["nextPageToken"] = strconv.Itoa(end)
		}
		_ = json.NewEncoder(w).Encode(page)
	})
	mux.HandleFunc("GET /api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		gateway.mu.Lock()
		gateway.queries = append(gateway.queries, query)
		gateway.mu.Unlock()
		kind, result := "hourly", json.RawMessage("[]")
		switch {
		case strings.Contains(query, "sum by (route, method, status)"):
			kind, result = "requests", counters.Requests
		case strings.Contains(query, "response_bytes_sum"):
			kind, result = "bytes", counters.Bytes
		default:
			at, _ := strconv.ParseInt(r.URL.Query().Get("time"), 10, 64)
			if hour := int((at-start)/hourSeconds) - 1; hour >= 0 && hour < len(counters.Hourly) {
				result = counters.Hourly[hour]
			}
		}
		if s.fails(kind) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": result}})
	})
	gateway.server = httptest.NewServer(mux)
	t.Cleanup(gateway.server.Close)
	return gateway
}

func (g *fakeGateway) logs() *CloudLogging {
	return &CloudLogging{
		Project: "example-project", Cluster: "example-cluster", Namespace: "ingress-nginx", Container: "nginx",
		Token:    func(context.Context) (string, error) { return "test-token", nil },
		Endpoint: g.server.URL, Client: g.server.Client(), sleep: noSleep,
	}
}

func (g *fakeGateway) metrics() *Prometheus {
	return &Prometheus{BaseURL: g.server.URL, Client: g.server.Client(), sleep: noSleep}
}

// fixtureNetworks loads the synthetic reference files through the same code
// that reads the real ones from the cache directory.
func fixtureNetworks(t *testing.T, owners, types bool, aliasSetting string) *NetworkTable {
	t.Helper()
	aliases, err := ParseOrganizationAliases(aliasSetting)
	require.NoError(t, err)
	dir := t.TempDir()
	if owners {
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		_, err := writer.Write(readFixture(t, "networks", "ip2asn-v4.tsv"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		require.NoError(t, os.WriteFile(filepath.Join(dir, ownersFile), compressed.Bytes(), 0o600))
		if types {
			require.NoError(t, os.WriteFile(filepath.Join(dir, typesFile), readFixture(t, "networks", "peeringdb-net.json"), 0o600))
		}
	}
	return LoadNetworkTable(t.Context(), NetworkFiles{CacheDir: dir, Aliases: aliases})
}

type catalogueVersion struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	PublishedAt string   `json:"published_at"`
	UpdatedAt   string   `json:"updated_at"`
	Status      string   `json:"status"`
	IsLatest    bool     `json:"is_latest"`
	Packages    []string `json:"packages"`
	Remotes     []string `json:"remotes"`
}

func fixtureVersions(t *testing.T) []catalogueVersion {
	t.Helper()
	var document struct {
		Versions []catalogueVersion `json:"versions"`
	}
	require.NoError(t, json.Unmarshal(readFixture(t, "catalogue.json"), &document))
	return document.Versions
}

// fixtureCatalogue builds the catalogue the way the prototype's wrapper read
// its catalogue file: a name's type is that of its latest version, and names
// keep the order in which they first appear.
func fixtureCatalogue(t *testing.T) *Catalogue {
	t.Helper()
	types := map[string]string{}
	var order []string
	for _, version := range fixtureVersions(t) {
		_, known := types[version.Name]
		if !known {
			order = append(order, version.Name)
		}
		if version.IsLatest || !known {
			types[version.Name] = typeLabel(version.Packages, version.Remotes)
		}
	}
	servers := make([]CatalogueServer, len(order))
	for i, name := range order {
		servers[i] = CatalogueServer{Name: name, Type: types[name]}
	}
	return NewCatalogue(servers)
}

type fixture struct {
	scenario scenario
	gateway  *fakeGateway
	sources  Sources
	limits   Limits
	log      *bytes.Buffer
}

func newFixture(t *testing.T, name string) *fixture {
	t.Helper()
	loaded := loadScenario(t, name)
	gateway := newFakeGateway(t, loaded)
	log := &bytes.Buffer{}
	logf := func(format string, args ...any) {
		log.WriteString(strings.TrimRight(fmt.Sprintf(format, args...), "\n") + "\n")
	}
	logs := gateway.logs()
	logs.Logf = logf
	sources := Sources{Logs: logs, Metrics: gateway.metrics(), Networks: fixtureNetworks(t, loaded.Networks, loaded.NetworkTypes, loaded.OrganizationAliases),
		Logf: logf}
	if loaded.Catalogue {
		sources.Catalogue = fixtureCatalogue(t)
	}
	return &fixture{scenario: loaded, gateway: gateway, sources: sources, log: log,
		limits: Limits{MaxRecords: loaded.MaxRecords, DollarsPerGiB: 0.10}}
}

func (f *fixture) collect(t *testing.T) (*Sample, error) {
	t.Helper()
	return CollectDay(t.Context(), f.sources, f.limits, f.scenario.day(t))
}

var documentationRanges = []netip.Prefix{
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// isSynthetic reports whether an address is reserved for documentation, or is
// one of the special addresses that belong to no one.
func isSynthetic(address netip.Addr) bool {
	address = address.Unmap()
	if raw := address.As16(); address.Is6() && bytes.Equal(raw[:12], make([]byte, 12)) && !address.IsLoopback() && !address.IsUnspecified() {
		// The deprecated form that embeds an IPv4 address after zeros.
		address = netip.AddrFrom4([4]byte(raw[12:]))
	}
	for _, reserved := range documentationRanges {
		if reserved.Contains(address) {
			return true
		}
	}
	return address.IsLoopback() || address.IsUnspecified() || address == netip.MustParseAddr("255.255.255.255")
}

// TestFixturesAreSynthetic checks that no fixture contains an address outside
// the ranges reserved for documentation.
func TestFixturesAreSynthetic(t *testing.T) {
	dottedQuad := regexp.MustCompile(`(?:\d{1,3}\.){3}\d{1,3}`)
	colonGroups := regexp.MustCompile(`[0-9A-Fa-f:]*:[0-9A-Fa-f:]*:[0-9A-Fa-f:.]*`)
	checked := 0
	fixtures := os.DirFS("testdata")
	err := fs.WalkDir(fixtures, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(fixtures, path)
		if err != nil {
			return err
		}
		for _, pattern := range []*regexp.Regexp{dottedQuad, colonGroups} {
			for _, span := range pattern.FindAllIndex(data, -1) {
				candidate := data[span[0]:span[1]]
				// Without a digit it is part of a name such as Some::Module, as the collector also judges.
				if pattern == colonGroups && !bytes.ContainsAny(candidate, "0123456789") {
					continue
				}
				address, err := netip.ParseAddr(string(candidate))
				if err != nil {
					continue // not an address: a timestamp, a version, or a deliberately malformed value
				}
				checked++
				assert.True(t, isSynthetic(address), "%s contains %s, which is not a documentation address", path, candidate)
			}
		}
		return nil
	})
	require.NoError(t, err)
	assert.Greater(t, checked, 1000, "the fixtures should contain addresses to check")
}
