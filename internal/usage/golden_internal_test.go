package usage

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goldenRows is what the prototype's wrapper wrote for a scenario, after the
// one rule the preview's loader applied to it: on a day where no user agent
// was recorded, the single "(not recorded)" client is not a real count, so the
// client rows are left out.
type goldenRows struct {
	Requests float64     `json:"requests"`
	GiB      float64     `json:"gib"`
	Records  int         `json:"records"`
	Fraction float64     `json:"fraction"`
	Rows     []goldenRow `json:"rows"`
}

type goldenRow struct {
	Dimension     string   `json:"dimension"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	RollupCount   *int     `json:"rollup_count,omitempty"`
	Category      *string  `json:"category,omitempty"`
	IdentifiedBy  *string  `json:"identified_by,omitempty"`
	RequestsPct   *float64 `json:"requests_pct,omitempty"`
	DataPct       *float64 `json:"data_pct,omitempty"`
	CompressedPct *float64 `json:"compressed_pct,omitempty"`
	Addresses     *int     `json:"addresses,omitempty"`
	NotFoundPct   *float64 `json:"not_found_pct,omitempty"`
	TopClientPct  *float64 `json:"top_client_pct,omitempty"`
}

func asGolden(row Share) goldenRow {
	return goldenRow{
		Dimension: row.Dimension, Name: row.Name, Kind: row.Kind, RollupCount: row.RollupCount, Category: row.Category,
		IdentifiedBy: row.IdentifiedBy, RequestsPct: row.RequestsPct, DataPct: row.DataPct, CompressedPct: row.CompressedPct,
		Addresses: row.Addresses, NotFoundPct: row.NotFoundPct, TopClientPct: row.TopClientPct,
	}
}

// howDimensions are the breakdowns the prototype only ever printed in the report.
var howDimensions = map[string]bool{"endpoint": true, "listing_use": true, "page_size": true, "response": true}

var update = flag.Bool("update", false, "rewrite the golden files under testdata/scenarios from this package's output")

// TestGoldenParity checks that, for the same records, this package produces
// the prototype's report byte for byte and its rows digit for digit.
//
// The golden files were written by the prototype. Now that it is retired, an
// intended change to the figures is recorded by running this test with -update
// and reviewing the difference.
func TestGoldenParity(t *testing.T) {
	for _, name := range scenarioNames(t) {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, name)
			sample, err := f.collect(t)
			if f.scenario.Entries == "empty" {
				require.ErrorIs(t, err, ErrNoRecords)
			} else {
				require.NoError(t, err)
			}
			reportPath := filepath.Join("testdata", "scenarios", name, "report.md")
			rowsPath := filepath.Join("testdata", "scenarios", name, "rows.json")
			if *update {
				require.NoError(t, os.WriteFile(reportPath, []byte(sample.Markdown()), 0o600))
				if f.scenario.Rows {
					require.NoError(t, os.WriteFile(rowsPath, encodeGolden(f.scenario, sample), 0o600))
				}
			}
			report, err := os.ReadFile(reportPath)
			require.NoError(t, err)
			assert.Equal(t, string(report), sample.Markdown())
			if !f.scenario.Rows {
				return
			}

			var golden goldenRows
			data, err := os.ReadFile(rowsPath)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(data, &golden))
			daily, shares := sample.Rows()
			assert.Equal(t, golden.Fraction, daily.SampleFraction, "sampling fraction")
			assert.Equal(t, golden.Records, daily.RecordsSampled, "records")
			assert.Equal(t, golden.GiB, daily.GiB, "data sent")
			assert.InDelta(t, golden.Requests, float64(daily.Requests), 0.5, "requests")

			expected, got := golden.Rows, whoRows(shares)
			require.Len(t, got, len(expected))
			for i := range expected {
				assert.Equal(t, expected[i], got[i], "row %d (%s / %s)", i, expected[i].Dimension, expected[i].Name)
			}
		})
	}
}

// whoRows are the rows the prototype's wrapper wrote: everything except the
// breakdowns it only printed in the report.
func whoRows(shares []Share) []goldenRow {
	var rows []goldenRow
	for _, row := range shares {
		if !howDimensions[row.Dimension] {
			rows = append(rows, asGolden(row))
		}
	}
	return rows
}

// goldenFloat is written the way the prototype wrote its figures, so that
// rewriting a golden file changes only the figures that really changed.
type goldenFloat float64

func (f goldenFloat) MarshalJSON() ([]byte, error) {
	return []byte(pyFloatRepr(float64(f))), nil
}

func asGoldenFloat(value *float64) *goldenFloat {
	if value == nil {
		return nil
	}
	converted := goldenFloat(*value)
	return &converted
}

// encodeGolden writes rows in the layout of the existing golden files: one
// row per line, without the fields that are empty.
func encodeGolden(s scenario, sample *Sample) []byte {
	var out bytes.Buffer
	daily, shares := sample.Rows()
	fmt.Fprintf(&out, "{\n\"day\": %q,\n\"requests\": %s,\n\"gib\": %s,\n\"records\": %d,\n\"fraction\": %s,\n\"rows\": [\n",
		s.Day, pyFloatRepr(sample.requests), pyFloatRepr(daily.GiB), daily.RecordsSampled, pyFloatRepr(daily.SampleFraction))
	rows := whoRows(shares)
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	for i, row := range rows {
		_ = encoder.Encode(struct {
			Dimension     string       `json:"dimension"`
			Name          string       `json:"name"`
			Kind          string       `json:"kind"`
			RollupCount   *int         `json:"rollup_count,omitempty"`
			Category      *string      `json:"category,omitempty"`
			IdentifiedBy  *string      `json:"identified_by,omitempty"`
			RequestsPct   *goldenFloat `json:"requests_pct,omitempty"`
			DataPct       *goldenFloat `json:"data_pct,omitempty"`
			CompressedPct *goldenFloat `json:"compressed_pct,omitempty"`
			Addresses     *int         `json:"addresses,omitempty"`
			NotFoundPct   *goldenFloat `json:"not_found_pct,omitempty"`
			TopClientPct  *goldenFloat `json:"top_client_pct,omitempty"`
		}{row.Dimension, row.Name, row.Kind, row.RollupCount, row.Category, row.IdentifiedBy, asGoldenFloat(row.RequestsPct),
			asGoldenFloat(row.DataPct), asGoldenFloat(row.CompressedPct), row.Addresses, asGoldenFloat(row.NotFoundPct),
			asGoldenFloat(row.TopClientPct)})
		if i < len(rows)-1 {
			out.Truncate(out.Len() - 1)
			out.WriteString(",\n")
		}
	}
	out.WriteString("]\n}\n")
	return out.Bytes()
}

// TestGoldenFilter checks that the records are asked for with the prototype's
// filter, since both must read the same deterministic sample.
func TestGoldenFilter(t *testing.T) {
	f := newFixture(t, "subsampled")
	_, err := f.collect(t)
	require.NoError(t, err)
	require.NotEmpty(t, f.gateway.filters)
	assert.Equal(t, `resource.type="k8s_container" AND resource.labels.cluster_name="example-cluster" AND `+
		`resource.labels.namespace_name="ingress-nginx" AND resource.labels.container_name="nginx" AND `+
		`jsonPayload.kind="registry_access" AND timestamp>="2026-10-06T00:00:00Z" AND timestamp<"2026-10-07T00:00:00Z" AND `+
		`sample(jsonPayload.request_id, 0.365)`, f.gateway.filters[0])
	assert.Greater(t, f.gateway.logCalls, 1, "the fixture should span several pages")

	require.GreaterOrEqual(t, len(f.gateway.queries), 26)
	assert.Equal(t, `sum by (route, method, status) (increase(registry_gateway_canary_requests_total{traffic="registry"}[86400s]))`, f.gateway.queries[0])
	assert.Equal(t, `sum(increase(registry_gateway_canary_response_bytes_sum{traffic="registry"}[86400s]))`, f.gateway.queries[1])
	assert.Equal(t, `sum(increase(registry_gateway_canary_requests_total{traffic="registry",route=~"list_v0|list_v01",method=~"GET|HEAD",status=~"200|304"}[3600s]))`,
		f.gateway.queries[2])

	whole := newFixture(t, "typical")
	_, err = whole.collect(t)
	require.NoError(t, err)
	assert.NotContains(t, whole.gateway.filters[0], "sample(", "a day that fits is read whole")
}

// TestStoredSharesMatchReport checks that the "how it is used" rows, which the
// prototype only printed, are the same numbers the report shows.
func TestStoredSharesMatchReport(t *testing.T) {
	for _, name := range []string{"typical", "subsampled", "hostile", "before-user-agents"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, name)
			sample, err := f.collect(t)
			require.NoError(t, err)
			_, shares := sample.Rows()
			report := sample.Markdown()
			cell := func(value *float64) string {
				if value == nil {
					return "–"
				}
				return pctText(*value)
			}
			var pageSizes, responses []string
			for _, row := range shares {
				switch row.Dimension {
				case "endpoint":
					line := "| " + row.Name + " | " + cell(row.RequestsPct) + " | " + cell(row.DataPct) + " |"
					minor := strings.ToLower(row.Name) + " (about "
					assert.True(t, strings.Contains(report, line) || strings.Contains(report, minor), "endpoint %q", row.Name)
				case "listing_use":
					assert.Contains(t, report, "| "+row.Name+" | "+cell(row.RequestsPct)+" | "+cell(row.DataPct)+" |")
				case "page_size":
					if row.Kind == rowNamed {
						pageSizes = append(pageSizes, row.Name+" "+cell(row.RequestsPct))
					}
				case "response":
					responses = append(responses, row.Name+" "+cell(row.RequestsPct))
				}
			}
			assert.Contains(t, report, "- **Page size:** "+strings.Join(pageSizes, ", "))
			assert.Contains(t, report, "- **Responses:** "+strings.Join(responses, ", ")+".")
		})
	}
}
