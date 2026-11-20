package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sequence is a fixed stream of numbers between 0 and 1, so that the made-up
// days below are the same on every run.
type sequence uint64

func (s *sequence) next() float64 {
	*s += 0x9e3779b97f4a7c15
	z := uint64(*s)
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return float64((z^(z>>31))>>11) / (1 << 53)
}

// syntheticLimit is the record limit the made-up days are collected with.
const syntheticLimit = 2000

type syntheticEntry struct {
	timestamp string
	payload   map[string]any
	// position decides which samples the entry is in: those of a larger fraction.
	position float64
}

type syntheticRead struct {
	fraction  float64
	delivered int
}

// syntheticDay is a made-up day of traffic: what the gateway counted, and the
// access records that were stored. It stands in for both Cloud Logging and
// the metrics store.
type syntheticDay struct {
	start     time.Time
	entries   []syntheticEntry
	requests  map[[3]string]float64
	listingOK float64
	hourly    [24]float64
	bytes     float64
	// noCounters makes every counter query fail.
	noCounters bool
	// unrecorded means the counters hold no series at all for the day.
	unrecorded bool
	reads      []syntheticRead
}

// newSyntheticDay makes a day of evenly spread requests, nine in ten of them
// successful listings, most of those fast. thinned says in which hours the
// storage rule kept only one in ten of the fast ones.
func newSyntheticDay(requests int, thinned func(hour int) bool) *syntheticDay {
	day := &syntheticDay{start: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), requests: map[[3]string]float64{}}
	random := sequence(20261006)
	for index := range requests {
		at := day.start.Add(time.Duration(index) * daySeconds * time.Second / time.Duration(requests))
		hour := at.Hour()
		kind, drop := random.next(), random.next()
		payload := map[string]any{
			"kind": "registry_access", "host": "registry.example.org", "method": "GET", "status": "200", "request_bytes": "300",
			"client_ip": "192.0.2." + strconv.Itoa(1+index%200), "request_id": fmt.Sprintf("%016x", index), "user_agent": "example-sync/1.0",
		}
		size, route := 400+index%300, "other"
		switch {
		case kind < 0.9:
			size, route = 200000+index%50000, "list_v01"
			payload["uri"], payload["cache"], payload["duration"] = "/v0.1/servers?limit=100", "MISS", "0.012"
			if kind >= 0.85 {
				payload["duration"] = "1.250"
			}
			day.listingOK++
			day.hourly[hour]++
		case kind < 0.97:
			payload["uri"], payload["duration"] = "/v0.1/servers/org.example%2Fweather/versions/latest", "0.004"
		default:
			payload["uri"], payload["duration"], payload["status"] = "/v0.1/servers/org.example%2Fmissing/versions/latest", "0.003", "404"
		}
		payload["response_bytes"] = strconv.Itoa(size)
		day.requests[[3]string{route, "GET", pyField(payload, "status")}]++
		day.bytes += float64(size)
		if kind < 0.85 && thinned(hour) && drop >= 0.1 {
			continue
		}
		day.entries = append(day.entries, syntheticEntry{timestamp: at.Format("2006-01-02T15:04:05.000000000Z"), payload: payload, position: random.next()})
	}
	return day
}

func (d *syntheticDay) Read(_ context.Context, window LogWindow, each func(timestamp string, payload map[string]any) error) error {
	d.reads = append(d.reads, syntheticRead{fraction: window.Fraction})
	read := &d.reads[len(d.reads)-1]
	for _, entry := range d.entries {
		if window.Fraction < 1 && entry.position >= window.Fraction {
			continue
		}
		read.delivered++
		if err := each(entry.timestamp, entry.payload); err != nil {
			return err
		}
	}
	return nil
}

func (d *syntheticDay) Query(_ context.Context, query string, at time.Time) ([]MetricSample, error) {
	switch {
	case d.noCounters:
		return nil, errors.New("unavailable")
	case d.unrecorded:
		return nil, nil
	case strings.Contains(query, "last_over_time"):
		return []MetricSample{{Value: 6}}, nil
	case strings.Contains(query, "sum by (route, method, status)"):
		var rows []MetricSample
		for labels, count := range d.requests {
			rows = append(rows, MetricSample{Labels: map[string]string{"route": labels[0], "method": labels[1], "status": labels[2]}, Value: count})
		}
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].Labels["route"]+rows[i].Labels["status"] < rows[j].Labels["route"]+rows[j].Labels["status"]
		})
		return rows, nil
	case strings.Contains(query, bytesMetric):
		return []MetricSample{{Value: d.bytes}}, nil
	}
	return []MetricSample{{Value: d.hourly[int(at.Sub(d.start)/time.Hour)-1]}}, nil
}

func (d *syntheticDay) collect(t *testing.T, require Tolerance) (*Sample, string, error) {
	t.Helper()
	var log strings.Builder
	sources := Sources{Logs: d, Metrics: d, Networks: fixtureNetworks(t, true, true, ""), Logf: func(format string, args ...any) {
		log.WriteString(fmt.Sprintf(format, args...) + "\n")
	}}
	sample, err := CollectDay(t.Context(), sources, Limits{MaxRecords: syntheticLimit, DollarsPerGiB: 0.10, Require: require}, d.start)
	return sample, log.String(), err
}

// assumedRead is how many records the rule this replaced would have read. It
// took for granted that one in ten successful listing records was stored.
func (d *syntheticDay) assumedRead() int {
	total := 0.0
	for _, count := range d.requests {
		total += count
	}
	expected := 0.11*d.listingOK + 1.1*(total-d.listingOK)
	if expected <= float64(syntheticLimit) {
		return len(d.entries)
	}
	return int(math.Trunc(float64(1000*syntheticLimit)/expected) / 1000 * float64(len(d.entries)))
}

// TestReadStaysNearTheLimit checks that about MaxRecords records are read for
// a day whatever share of its records the storage rule thinned out. The
// records that are stored are counted first, on a small sample; nothing about
// the storage rule is assumed.
func TestReadStaysNearTheLimit(t *testing.T) {
	for _, test := range []struct {
		name    string
		thinned func(hour int) bool
		// overRead is by how much the earlier rule would have exceeded the limit.
		overRead float64
	}{
		{"fast listings thinned to one in ten all day", func(int) bool { return true }, 1},
		{"thinned in the first half of the day only", func(hour int) bool { return hour < 12 }, 2.5},
		{"nothing thinned out", func(int) bool { return false }, 4.5},
	} {
		t.Run(test.name, func(t *testing.T) {
			day := newSyntheticDay(60000, test.thinned)
			require.GreaterOrEqual(t, float64(day.assumedRead()), test.overRead*syntheticLimit, "the earlier rule would have read this much")

			sample, _, err := day.collect(t, CompleteDay)
			require.NoError(t, err)
			require.Len(t, day.reads, 2, "one read to size the day, one to collect it")
			sizing, read := day.reads[0], day.reads[1]
			assert.InDelta(t, 0.05, sizing.fraction, 1e-12, "the sizing read asks for 100 records per 2,000 stored")
			assert.InDelta(t, float64(len(day.entries))*0.05, float64(sizing.delivered), 0.06*float64(sizing.delivered))
			assert.InDelta(t, syntheticLimit, read.delivered, 0.08*syntheticLimit, "the read returns about the limit")
			assert.Equal(t, read.delivered, sample.Records())
			assert.Equal(t, read.fraction, sample.fraction)

			// The sample still adds up to what the gateway counted, hour by hour. A
			// sampled record never stands for fewer requests than its place in the
			// sample gives it, so hours that happened to draw more records than their
			// share push the estimate up. With as few records as here that is a few
			// percent; it shrinks as the limit grows.
			assert.InDelta(t, 1, sample.estRequests/sample.counters.requests, 0.04)
			assert.InDelta(t, 1, sample.estBytes/sample.counters.bytes, 0.04)
			assert.Empty(t, sample.notes)
			t.Logf("%d records stored; sized on %d; read %d at fraction %s (the earlier rule: %d); requests %.3f and data %.3f of the counters",
				len(day.entries), sizing.delivered, read.delivered, pyFloatRepr(read.fraction), day.assumedRead(),
				sample.estRequests/sample.counters.requests, sample.estBytes/sample.counters.bytes)
		})
	}

	t.Run("without the counters", func(t *testing.T) {
		day := newSyntheticDay(60000, func(int) bool { return false })
		day.noCounters = true
		sample, _, err := day.collect(t, AnyDay)
		require.NoError(t, err)
		assert.InDelta(t, syntheticLimit, sample.Records(), 0.08*syntheticLimit, "sizing the read does not need the counters")
	})

	t.Run("a day that fits is read whole", func(t *testing.T) {
		day := newSyntheticDay(1500, func(int) bool { return false })
		sample, _, err := day.collect(t, CompleteDay)
		require.NoError(t, err)
		assert.Equal(t, 1.0, day.reads[1].fraction)
		assert.Equal(t, 1500, sample.Records())
	})
}

// TestReadIsRepeatedSmallerWhenItOverflows covers a sample that is far from
// even: many records share one request ID, so they are all in a sample or all
// out of it, and the sizing read can miss them. The read is stopped at twice
// the limit and repeated at a smaller fraction, keeping nothing from the
// first attempt.
func TestReadIsRepeatedSmallerWhenItOverflows(t *testing.T) {
	day := newSyntheticDay(3400, func(int) bool { return false })
	for index := range 6000 {
		at := day.start.Add(time.Duration(index) * daySeconds * time.Second / 6000)
		day.entries = append(day.entries, syntheticEntry{timestamp: at.Format("2006-01-02T15:04:05.500000000Z"), position: 0.3, payload: map[string]any{
			"kind": "registry_access", "host": "registry.example.org", "method": "GET", "uri": "/v0.1/health", "status": "429", "duration": "0.001",
			"request_bytes": "200", "response_bytes": "150", "client_ip": "192.0.2.250", "request_id": "shared", "user_agent": "example-probe/1.0"}})
	}
	sort.SliceStable(day.entries, func(i, j int) bool { return day.entries[i].timestamp < day.entries[j].timestamp })

	sample, log, err := day.collect(t, CompleteDay)
	require.NoError(t, err)
	require.Len(t, day.reads, 3)
	assert.Less(t, day.reads[0].delivered, 250, "the sizing read did not see the shared request ID")
	assert.Greater(t, day.reads[1].fraction, 0.3)
	assert.Equal(t, 2*syntheticLimit+1, day.reads[1].delivered, "the read stops at the record that crosses twice the limit")
	assert.Less(t, day.reads[2].fraction, 0.3, "the second attempt is sized from what the first one returned")
	assert.Contains(t, log, "more than 4000 access records at fraction "+pyFloatRepr(day.reads[1].fraction)+
		"; reading again at fraction "+pyFloatRepr(day.reads[2].fraction))
	assert.Equal(t, day.reads[2].delivered, sample.Records(), "nothing is kept from the attempt that was stopped")
	assert.Equal(t, day.reads[2].fraction, sample.fraction)
	for _, entry := range sample.records {
		assert.NotEqual(t, "Rate-limited (429)", entry.outcome)
	}

	t.Run("a day that cannot be sampled at all is given up on", func(t *testing.T) {
		day := &syntheticDay{start: day.start, requests: map[[3]string]float64{{"other", "GET", "200"}: 10000}, bytes: 1}
		for index := range 10000 {
			at := day.start.Add(time.Duration(index) * daySeconds * time.Second / 10000)
			day.entries = append(day.entries, syntheticEntry{timestamp: at.Format("2006-01-02T15:04:05.000000000Z"), payload: map[string]any{
				"host": "registry.example.org", "method": "GET", "uri": "/v0.1/health", "status": "200", "request_bytes": "200", "request_id": "shared"}})
		}
		_, _, err := day.collect(t, CompleteDay)
		require.ErrorContains(t, err, "the day's read returned more than 4000 access records at fraction")
		require.ErrorContains(t, err, "a smaller sample could not be sized")
		assert.LessOrEqual(t, len(day.reads), maxResizes+2)
		for _, read := range day.reads {
			assert.LessOrEqual(t, read.delivered, 2*syntheticLimit+1, "no attempt takes in more than twice the limit")
		}
	})
}

// TestDayWithoutRegistryTraffic covers a day on which the gateway ran but
// served the registry nothing. Its only access records are the ones the
// collector leaves out anyway: the dashboards' host and background requests.
func TestDayWithoutRegistryTraffic(t *testing.T) {
	idleDay := func() *syntheticDay {
		day := &syntheticDay{start: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), requests: map[[3]string]float64{}}
		for index := range 300 {
			at := day.start.Add(time.Duration(index) * daySeconds * time.Second / 300)
			payload := map[string]any{"kind": "registry_access", "host": "grafana.example.org", "method": "GET", "uri": "/d/usage", "status": "200",
				"duration": "0.020", "request_bytes": "300", "response_bytes": "5000", "client_ip": "192.0.2.200", "request_id": fmt.Sprintf("%016x", index)}
			if index%2 == 1 {
				payload["host"], payload["uri"], payload["request_bytes"] = "registry.example.org", "/v0/health", "0"
			}
			day.entries = append(day.entries, syntheticEntry{timestamp: at.Format("2006-01-02T15:04:05.000000000Z"), payload: payload, position: float64(index) / 300})
		}
		return day
	}

	t.Run("nothing to collect", func(t *testing.T) {
		day := idleDay()
		sample, _, err := day.collect(t, CompleteDay)
		require.ErrorIs(t, err, ErrNoTraffic)
		assert.Contains(t, sample.Markdown(), "No access records were found for this period.")
		assert.Len(t, day.reads, 2, "the day's few records are read, to see that none is a registry request")
	})

	t.Run("counters without any data are not proof of an idle day", func(t *testing.T) {
		day := idleDay()
		day.unrecorded = true
		_, _, err := day.collect(t, CompleteDay)
		var degraded *DegradedError
		require.ErrorAs(t, err, &degraded, "this cannot be told from a collector that looks in the wrong place")
		assert.Equal(t, []string{"the gateway counters (no data for this day)"}, degraded.Missing)
		assert.Empty(t, day.reads)
	})

	t.Run("a registry request the counters did not count", func(t *testing.T) {
		day := idleDay()
		day.entries[10].payload["host"] = "registry.example.org"
		_, _, err := day.collect(t, CompleteDay)
		var degraded *DegradedError
		require.ErrorAs(t, err, &degraded, "the access records and the counters disagree, so the day is not skipped")

		sample, _, err := day.collect(t, AnyDay)
		require.NoError(t, err, "it can still be collected on request")
		assert.Equal(t, 1, sample.Records())
	})
}

func TestReadFraction(t *testing.T) {
	assert.Equal(t, 1.0, readFraction(0, 100000))
	assert.Equal(t, 1.0, readFraction(100000, 100000))
	assert.Equal(t, 0.249, readFraction(400300, 100000), "truncated to a thousandth, never rounded up")
	assert.Equal(t, 0.001, readFraction(99999999, 100000))
	assert.Equal(t, 0.000123, readFraction(812000000, 100000), "finer steps below a thousandth")
	assert.Equal(t, minFraction, readFraction(1e15, 100000))
	assert.Equal(t, 1.0, readFraction(1e15, 0), "no limit")
}

// TestQuietHoursPassTheirRequestsOn covers a day with so little traffic that
// in some hours every fast listing record was thinned out. The requests the
// counters saw in such an hour are carried by the nearest hour that kept a
// record, so that the day still adds up.
func TestQuietHoursPassTheirRequestsOn(t *testing.T) {
	f := newFixture(t, "quiet")
	sample, err := f.collect(t)
	require.NoError(t, err)

	sampled, counted, carried := make([]int, sample.buckets), 0.0, 0.0
	for _, entry := range sample.records {
		if entry.sampled {
			sampled[entry.bucket]++
		}
		if entry.listingOK {
			carried += entry.weight
		}
	}
	var counters struct {
		Hourly [][]struct {
			Value []any `json:"value"`
		} `json:"hourly"`
	}
	require.NoError(t, json.Unmarshal(readFixture(t, "entries", "quiet.counters.json"), &counters))
	empty := 0
	for hour, rows := range counters.Hourly {
		if len(rows) == 0 {
			continue
		}
		requests, err := strconv.ParseFloat(rows[0].Value[1].(string), 64)
		require.NoError(t, err)
		counted += requests
		if requests > 0 && sampled[hour] == 0 {
			empty++
		}
	}
	require.GreaterOrEqual(t, empty, 4, "the fixture should have hours that kept no fast listing record")
	assert.InDelta(t, counted, carried, 1e-9, "the listing records stand for every listing request the counters saw")
	assert.Empty(t, sample.notes, "the estimate is close enough to the counters to need no caveat")

	assert.Equal(t, 0, nearestSampled([]float64{2, 0, 0, 0, 1}, 2), "the earlier hour, when two are equally close")
	assert.Equal(t, 4, nearestSampled([]float64{2, 0, 0, 0, 1}, 3))
	assert.Equal(t, -1, nearestSampled([]float64{0, 0, 0}, 1))
}
