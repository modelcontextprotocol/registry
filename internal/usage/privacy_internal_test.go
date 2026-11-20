package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var plainLimit = regexp.MustCompile(`^limit=\d+$`)

// sensitive collects what the access records contain that must never leave
// the process: source addresses, request IDs, full user agents, query
// strings, and requested server names that are not in the catalogue.
func sensitive(t *testing.T, s scenario, catalogue *Catalogue) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	for _, raw := range s.entries(t) {
		var item struct {
			Payload map[string]any `json:"jsonPayload"`
		}
		require.NoError(t, json.Unmarshal(raw, &item))
		for _, value := range sensitiveIn(item.Payload, catalogue) {
			if value != "" && !seen[value] {
				seen[value] = true
				out = append(out, value)
			}
		}
	}
	return out
}

func sensitiveIn(payload map[string]any, catalogue *Catalogue) []string {
	var out []string
	if address, ok := payload["client_ip"].(string); ok && len(address) >= 7 {
		out = append(out, address)
	}
	if id, ok := payload["request_id"].(string); ok && len(id) >= 6 {
		out = append(out, id)
	}
	// A full user agent, as opposed to the product name taken from it. A few
	// fixture agents spell out one of the report's own labels; those prove nothing.
	if agent, ok := payload["user_agent"].(string); ok && strings.ContainsAny(strings.TrimSpace(agent), "/ ") && len(agent) >= 8 &&
		agent != clientBrowser && agent != clientNotRecorded && agent != unnamedCloud {
		out = append(out, agent)
	}
	uri, _ := payload["uri"].(string)
	// A query string, unless it is only a page size, which the report shows as "limit=100".
	if _, query, found := strings.Cut(uri, "?"); found && len(query) >= 8 && !plainLimit.MatchString(query) {
		out = append(out, query)
	}
	path, _, err := pyURLSplit(uri)
	if err != nil {
		return out
	}
	// A requested name that is not published. Names without a dot or hyphen
	// in the fixture are ordinary words such as "versions".
	if name, ok := requestedServer("GET", path); ok && strings.ContainsAny(name, ".-") && catalogue.resolve(name) == notListed {
		out = append(out, name)
	}
	return out
}

// TestNothingSensitiveLeavesTheProcess runs whole days, including records
// crafted to be awkward, and searches everything the collector produced: the
// stored rows, the report and its own log.
func TestNothingSensitiveLeavesTheProcess(t *testing.T) {
	documentationAddress := regexp.MustCompile(`192\.0\.2\.\d|198\.51\.100\.\d|203\.0\.113\.\d|2001:db8`)
	for _, name := range []string{"typical", "hostile", "mixed-user-agents"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, name)
			store := newStore(t, newDatabase(t))
			seedCatalogue(t, store)
			reports := t.TempDir()
			collector := newCollector(t, f, store, Options{
				Days: []time.Time{f.scenario.day(t)}, MarkdownDir: reports, AllowDegraded: true, Now: at("2026-10-09T03:00:00Z")})
			require.NoError(t, collector.Run(t.Context()))

			report, err := os.ReadFile(filepath.Join(reports, f.scenario.Day+".md"))
			require.NoError(t, err)
			rows, err := store.pool.Query(t.Context(), `SELECT row_to_json(d)::text FROM usage_daily d
				UNION ALL SELECT row_to_json(s)::text FROM usage_share s`)
			require.NoError(t, err)
			stored, err := pgx.CollectRows(rows, pgx.RowTo[string])
			require.NoError(t, err)
			require.Greater(t, len(stored), 50)
			produced := map[string]string{"report": string(report), "log": f.log.String(), "stored rows": strings.Join(stored, "\n")}

			catalogue, err := store.Catalogue(t.Context())
			require.NoError(t, err)
			secrets := sensitive(t, f.scenario, catalogue)
			require.Greater(t, len(secrets), 100, "the fixture should contain plenty to leak")
			for where, text := range produced {
				assert.NotEmpty(t, text, where)
				assert.False(t, documentationAddress.MatchString(text), "%s contains a source address", where)
				for _, secret := range secrets {
					assert.False(t, strings.Contains(text, secret), "%s contains %q", where, secret)
				}
			}
			assert.Contains(t, produced["stored rows"], notListed, "requests for unknown names are counted as one group")
		})
	}
}
