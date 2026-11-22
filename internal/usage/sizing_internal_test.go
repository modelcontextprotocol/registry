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
		// Multiplying first would overflow for a large day.
		at := day.start.Add(time.Duration(index) * (daySeconds * time.Second / time.Duration(requests)))
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

// addSharedID adds requests that all carry one request ID, as a client can
// make them by sending its own. They are all in a sample or all out of it;
// position says which: they are in the samples of a larger fraction.
func (d *syntheticDay) addSharedID(id string, count int, position float64, within time.Duration) {
	for index := range count {
		at := d.start.Add(time.Duration(index) * within / time.Duration(count))
		d.entries = append(d.entries, syntheticEntry{timestamp: at.Format("2006-01-02T15:04:05.500000000Z"), position: position, payload: map[string]any{
			"kind": "registry_access", "host": "registry.example.org", "method": "GET", "uri": "/v0.1/health", "status": "429", "duration": "0.001",
			"request_bytes": "200", "response_bytes": "150", "client_ip": "192.0.2.250", "request_id": id, "user_agent": "example-probe/1.0"}})
		d.requests[[3]string{"other", "GET", "429"}]++
		d.bytes += 150
	}
	sort.SliceStable(d.entries, func(i, j int) bool { return d.entries[i].timestamp < d.entries[j].timestamp })
}

// addDashboards adds requests to the dashboards' host, spread over the day.
// They are access records, but not the registry's traffic.
func (d *syntheticDay) addDashboards(count int) {
	random := sequence(7)
	for index := range count {
		at := d.start.Add(time.Duration(index) * daySeconds * time.Second / time.Duration(count))
		d.entries = append(d.entries, syntheticEntry{timestamp: at.Format("2006-01-02T15:04:05.250000000Z"), position: random.next(), payload: map[string]any{
			"kind": "registry_access", "host": "grafana.example.org", "method": "GET", "uri": "/d/usage", "status": "200", "duration": "0.020",
			"request_bytes": "300", "response_bytes": "5000", "client_ip": "192.0.2.200", "request_id": fmt.Sprintf("d%015x", index)}})
	}
	sort.SliceStable(d.entries, func(i, j int) bool { return d.entries[i].timestamp < d.entries[j].timestamp })
}

func emptySyntheticDay() *syntheticDay {
	return &syntheticDay{start: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), requests: map[[3]string]float64{}}
}

func fractions(reads []syntheticRead) []float64 {
	out := make([]float64, len(reads))
	for index, read := range reads {
		out[index] = read.fraction
	}
	return out
}

func rateLimited(sample *Sample) int {
	count := 0
	for _, entry := range sample.records {
		if entry.outcome == "Rate-limited (429)" {
			count++
		}
	}
	return count
}

// TestUnevenSamples covers days whose samples are far from even, because many
// records share one request ID. What a sample returned is never taken for
// more than it shows: a read that returns too many records is not projected
// onto the rest of the day, and a sample that returns few or none is not
// taken as the day being small or empty.
func TestUnevenSamples(t *testing.T) {
	const tooMany = readCapFactor*syntheticLimit + 1

	// 3,400 requests spread over the day, and 6,000 with one request ID in its
	// first second, which the sizing sample misses. No sample that holds the
	// 6,000 fits, so the day is read at the largest fraction found below them.
	t.Run("a burst that no sample can hold", func(t *testing.T) {
		day := newSyntheticDay(3400, func(int) bool { return false })
		day.addSharedID("shared", 6000, 0.25, time.Second)
		sample, log, err := day.collect(t, CompleteDay)
		require.NoError(t, err)

		assert.Less(t, day.reads[0].delivered, 250, "the sizing sample did not hold the shared request ID")
		assert.Greater(t, day.reads[1].fraction, 0.25)
		assert.Equal(t, tooMany, day.reads[1].delivered, "the first read is stopped at the record that crosses twice the limit")
		for _, read := range day.reads {
			assert.GreaterOrEqual(t, read.fraction, day.reads[0].fraction,
				"a read stopped in the first second is not taken to mean the whole day is that busy")
			assert.LessOrEqual(t, read.delivered, tooMany)
		}
		assert.LessOrEqual(t, len(day.reads), maxResizes+3, "the sizing sample, at most five attempts, and the kept sample again")

		last := day.reads[len(day.reads)-1]
		assert.Equal(t, last.fraction, sample.fraction)
		assert.Equal(t, last.delivered, sample.Records(), "nothing is kept from an attempt that was stopped")
		assert.Less(t, sample.fraction, 0.25)
		assert.Greater(t, sample.Records(), 500, "the sample is as large as could be found below the burst")
		assert.Zero(t, rateLimited(sample))
		assert.Contains(t, sample.recorded, noteUnevenSample)
		assert.Contains(t, log, "no sample near 2000 access records was found")
		assert.NotEmpty(t, sample.notes, "the burst is missing from the sample, which the comparison with the counters shows")
	})

	// 40 requests spread over the day and 3,000 with one request ID, which the
	// sizing sample does hold. That makes the day look twenty times larger than
	// it is, and the fraction chosen for it returns next to nothing.
	t.Run("a sample that comes back nearly empty is not believed", func(t *testing.T) {
		day := newSyntheticDay(40, func(int) bool { return false })
		day.addSharedID("shared", 3000, 0.045, 6*time.Hour)
		sample, log, err := day.collect(t, CompleteDay)
		require.NoError(t, err)
		require.Len(t, day.reads, 3)
		assert.Greater(t, day.reads[0].delivered, 3000)
		assert.Less(t, day.reads[1].fraction, 0.045)
		assert.Less(t, day.reads[1].delivered, 10, "the fraction sized on that sample returns almost nothing")
		assert.Contains(t, log, "access records at fraction "+pyFloatRepr(day.reads[1].fraction))
		assert.Contains(t, log, "reading again at fraction 1.0")
		assert.Equal(t, 1.0, sample.fraction, "so the day is read again, and turns out to fit whole")
		assert.Equal(t, 3040, sample.Records())
		assert.Equal(t, 3000, rateLimited(sample))
		assert.NotContains(t, sample.recorded, noteUnevenSample)
	})

	// The same burst, but it leaves room for a sample of about half the limit
	// below it. That sample is kept, and the day says that it is not even: the
	// read that was stopped had returned more than this sample accounts for.
	t.Run("a sample kept after another returned too many says so", func(t *testing.T) {
		day := newSyntheticDay(3400, func(int) bool { return false })
		day.addSharedID("shared", 6000, 0.5, time.Second)
		sample, _, err := day.collect(t, AnyDay)
		require.NoError(t, err)
		require.Len(t, day.reads, 4)
		assert.Equal(t, tooMany, day.reads[1].delivered, "the first read holds the burst and is stopped")
		assert.Less(t, day.reads[2].delivered, syntheticLimit/thinFactor, "halfway down is too small to settle for")
		assert.Less(t, sample.fraction, 0.5)
		assert.Greater(t, sample.fraction, day.reads[2].fraction)
		assert.GreaterOrEqual(t, sample.Records(), syntheticLimit/thinFactor)
		assert.Contains(t, sample.recorded, noteUnevenSample)
	})

	// 30,000 requests spread over the day and 3,000 with one request ID that
	// the sizing sample holds, so that it is the sizing sample that returns too
	// many. The sample kept in the end is below the 3,000, and its own count
	// says the sizing sample should have fit: the day is marked as uneven.
	t.Run("a sample at odds with the sizing sample says so", func(t *testing.T) {
		day := newSyntheticDay(30000, func(int) bool { return false })
		day.addSharedID("shared", 3000, 0.04, 6*time.Hour)
		sample, _, err := day.collect(t, AnyDay)
		require.NoError(t, err)
		assert.Equal(t, syntheticRead{fraction: 0.05, delivered: tooMany}, day.reads[0])
		assert.Less(t, sample.fraction, 0.04)
		assert.GreaterOrEqual(t, sample.Records(), syntheticLimit/thinFactor)
		assert.Zero(t, rateLimited(sample))
		assert.Contains(t, sample.recorded, noteUnevenSample)
	})

	// The same, with so many spread requests that the kept sample puts the
	// sizing sample only a little under the cap. Its count is precise enough
	// to tell that apart from the cap all the same.
	t.Run("a sample narrowly at odds with the sizing sample says so too", func(t *testing.T) {
		day := newSyntheticDay(70000, func(int) bool { return false })
		day.addSharedID("shared", 100000, 0.04, 6*time.Hour)
		sample, _, err := day.collect(t, AnyDay)
		require.NoError(t, err)
		assert.Equal(t, syntheticRead{fraction: 0.05, delivered: tooMany}, day.reads[0])
		assert.Less(t, sample.fraction, 0.04)
		assert.InDelta(t, syntheticLimit, sample.Records(), 0.2*syntheticLimit)
		assert.Contains(t, sample.recorded, noteUnevenSample)
	})

	// 2,175 records, which fit whole. 75 share a request ID that the sizing
	// sample holds, so the day looks a little larger than the limit; 1,500
	// share one that only the largest samples hold, so the first read comes
	// back small. The next step up is the whole day, however short the step.
	t.Run("the whole day is read whenever it may fit", func(t *testing.T) {
		day := newSyntheticDay(600, func(int) bool { return false })
		day.addSharedID("one", 75, 0.01, time.Hour)
		day.addSharedID("another", 1500, 0.97, time.Hour)
		sample, _, err := day.collect(t, CompleteDay)
		require.NoError(t, err)
		require.Len(t, day.reads, 3)
		assert.Greater(t, day.reads[1].fraction, 0.92)
		assert.Less(t, day.reads[1].delivered, syntheticLimit/thinFactor)
		assert.Equal(t, 1.0, sample.fraction)
		assert.Equal(t, 2175, sample.Records())
		assert.NotContains(t, sample.recorded, noteUnevenSample)
	})

	// 3,900 requests share a request ID that the sizing sample holds, among
	// 1,900 dashboard records. The fraction sized on that sample returns only
	// dashboard records, and everything larger than the sizing sample returns
	// too many. The sizing sample is known to fit, and that is not forgotten.
	t.Run("a sample known to fit is fallen back on", func(t *testing.T) {
		day := emptySyntheticDay()
		day.addDashboards(1900)
		day.addSharedID("shared", 3900, 0.045, daySeconds*time.Second)
		sample, _, err := day.collect(t, AnyDay)
		require.NoError(t, err)
		tried := fractions(day.reads)
		require.Len(t, tried, maxResizes+3)
		assert.Equal(t, []float64{0.05, 0.025, 1}, tried[:3], "the sizing sample, the fraction sized on it, and then the whole day")
		for index, fraction := range tried[3:6] {
			assert.Greater(t, fraction, 0.05)
			assert.Equal(t, tooMany, day.reads[3+index].delivered)
		}
		assert.Equal(t, 0.05, sample.fraction)
		assert.Equal(t, 3900, sample.Records())
		assert.Contains(t, sample.recorded, noteUnevenSample)
		assert.Contains(t, sample.notes, noteRepeatedID)
	})

	// 200,000 evenly spread records are too many even for the sizing sample,
	// which is taken again smaller. Nothing about that is uneven.
	t.Run("a day too large for the sizing sample", func(t *testing.T) {
		day := newSyntheticDay(200000, func(int) bool { return false })
		sample, _, err := day.collect(t, CompleteDay)
		require.NoError(t, err)
		require.Len(t, day.reads, 3)
		assert.Equal(t, syntheticRead{fraction: 0.05, delivered: tooMany}, day.reads[0])
		assert.Equal(t, 0.0005, day.reads[1].fraction)
		assert.Less(t, day.reads[2].fraction, 0.05)
		assert.InDelta(t, syntheticLimit, sample.Records(), 0.25*syntheticLimit)
		assert.NotContains(t, sample.recorded, noteUnevenSample)
	})

	// 40,000 dashboard records and 60 registry requests. A sample of the size
	// intended holds none of the 60, so one is read with the room left below
	// the cap. If that holds none either, the day cannot be collected at this
	// limit, which is what the error says.
	for name, test := range map[string]struct {
		position float64
		collects bool
	}{"within reach": {0.06, true}, "out of reach": {0.5, false}} {
		t.Run("registry requests that are rare among other records, "+name, func(t *testing.T) {
			day := emptySyntheticDay()
			day.addDashboards(40000)
			day.addSharedID("shared", 60, test.position, daySeconds*time.Second)
			sample, _, err := day.collect(t, AnyDay)
			require.Len(t, day.reads, 3)
			assert.InDelta(t, 0.05, day.reads[1].fraction, 0.005)
			assert.InDelta(t, 0.075, day.reads[2].fraction, 0.007, "half as large again, which still fits")
			if test.collects {
				require.NoError(t, err)
				assert.Equal(t, 60, sample.Records())
				return
			}
			require.ErrorIs(t, err, ErrSampling)
			require.NotErrorIs(t, err, ErrNoRecords)
			assert.ErrorContains(t, err, "none of them a registry request")
		})
	}

	// Every record carries the same request ID, so every sample holds all of
	// the day or none of it.
	for name, position := range map[string]float64{"in every sample": 0, "in the larger samples only": 0.5} {
		t.Run("a day that cannot be sampled fails as that, "+name, func(t *testing.T) {
			day := emptySyntheticDay()
			day.addSharedID("shared", 10000, position, daySeconds*time.Second)
			_, _, err := day.collect(t, CompleteDay)
			require.ErrorIs(t, err, ErrSampling)
			require.NotErrorIs(t, err, ErrNoRecords, "the records exist; an empty sample does not say otherwise")
			assert.LessOrEqual(t, len(day.reads), maxResizes+3)
			for _, read := range day.reads {
				assert.LessOrEqual(t, read.delivered, tooMany, "no attempt takes in more than twice the limit")
			}
		})
	}
}

// scriptedDay returns a set number of access records for each fraction it is
// asked for, each with its own request ID, and fails on any other fraction.
type scriptedDay struct {
	counts map[float64]int
	reads  []syntheticRead
}

func (d *scriptedDay) Read(_ context.Context, window LogWindow, each func(timestamp string, payload map[string]any) error) error {
	count, known := d.counts[window.Fraction]
	if !known {
		return fmt.Errorf("a sample at fraction %v was not expected", window.Fraction)
	}
	d.reads = append(d.reads, syntheticRead{fraction: window.Fraction})
	read := &d.reads[len(d.reads)-1]
	for index := range count {
		at := window.Start.Add(time.Duration(index) * (daySeconds * time.Second / time.Duration(count)))
		read.delivered++
		if err := each(at.Format("2006-01-02T15:04:05.000000000Z"), map[string]any{
			"kind": "registry_access", "host": "registry.example.org", "method": "GET", "uri": "/v0.1/servers?limit=100", "status": "200",
			"duration": "1.250", "request_bytes": "300", "response_bytes": "200000", "client_ip": "192.0.2." + strconv.Itoa(1+index%200),
			"request_id": fmt.Sprintf("%016x", index), "user_agent": "example-sync/1.0", "cache": "MISS"}); err != nil {
			return err
		}
	}
	return nil
}

// TestEvenDayNeedsNoCaveat covers an ordinary day that takes a second read.
// A fraction is cut to a thousandth, so just above 500 times the limit the
// first read can come back a little under half the limit. The next one is at
// the fraction of the sizing sample, which is read like any other; the day
// is not uneven and does not say so.
func TestEvenDayNeedsNoCaveat(t *testing.T) {
	day := &scriptedDay{counts: map[float64]int{0.002: 50060, 0.001: 24960}}
	var log strings.Builder
	sources := Sources{Logs: day, Metrics: emptyMetrics{}, Networks: fixtureNetworks(t, true, true, ""), Logf: func(format string, args ...any) {
		log.WriteString(fmt.Sprintf(format, args...) + "\n")
	}}
	sample, err := CollectDay(t.Context(), sources, Limits{MaxRecords: 50000, DollarsPerGiB: 0.10, Require: AnyDay}, time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Equal(t, []syntheticRead{{0.002, 50060}, {0.001, 24960}, {0.002, 50060}}, day.reads)
	assert.Equal(t, 50060, sample.Records())
	assert.NotContains(t, sample.recorded, noteUnevenSample)
	assert.NotContains(t, log.String(), "no sample near")
}

func TestNextFraction(t *testing.T) {
	pilot := sizing{fraction: 0.05, seen: 170}
	assert.Equal(t, 0.588, nextFraction(pilot, 0.05, 0, 2000), "from the sample's own count")
	assert.Equal(t, 0.882, nextFraction(pilot, 0.05, 0, 3000), "aiming higher")
	assert.Equal(t, 1.0, nextFraction(sizing{fraction: 0.05, seen: 90}, 0.05, 0, 2000))
	assert.Equal(t, 1.0, nextFraction(sizing{fraction: 0.03, seen: 0}, 0.05, 0, 2000), "an empty sample leads to the whole day")
	assert.Equal(t, 0.171464, nextFraction(pilot, 0.05, 0.588, 2000), "halfway between a fraction that fits and one that does not")
	assert.Equal(t, 0.333588, nextFraction(sizing{fraction: 0.1772, seen: 568}, 0.1772, 0.628, 2000),
		"also when its own count points just below the fraction that did not fit")
	assert.Equal(t, 0.01, nextFraction(sizing{fraction: 0.0005, seen: 100}, 0.0005, 0.05, 2000), "its own count again, while that stays below what is known not to fit")
	assert.Equal(t, minFraction, nextFraction(sizing{fraction: minFraction, seen: 0}, minFraction, 0.000002, 2000))

	// Whether a kept sample is at odds with a fraction that returned more than 4,000 records.
	collector := &dayCollector{limits: Limits{MaxRecords: 2000}}
	assert.False(t, collector.contradicted(sizing{fraction: 0.01, seen: 2000}, 0.05), "it puts 10,000 there: the day is simply large")
	assert.True(t, collector.contradicted(sizing{fraction: 0.0375, seen: 1125}, 0.05), "it puts 1,500 there")
	assert.True(t, collector.contradicted(sizing{fraction: 0.028, seen: 1960}, 0.05), "it puts 3,500 there, give or take 80")
	assert.False(t, collector.contradicted(sizing{fraction: 0.028, seen: 2150}, 0.05), "3,840, give or take 83, could be 4,000")
	assert.False(t, collector.contradicted(sizing{fraction: 1, seen: 100}, 0.05), "the whole day")
	assert.False(t, collector.contradicted(sizing{fraction: 0.01, seen: 1000}, 0), "nothing returned too many")

	assert.True(t, sameAsOneOf([]float64{0.05, 0.3}, 0.32))
	assert.False(t, sameAsOneOf([]float64{0.05, 0.3}, 0.34))
}

// TestDayWithoutRegistryTraffic covers a day on which the gateway ran but
// served the registry nothing. Its only access records are the ones the
// collector leaves out anyway: the dashboards' host and background requests.
// Such a day is skipped only after every one of its records has been looked
// at; a sample without a registry request would not show that there was none.
func TestDayWithoutRegistryTraffic(t *testing.T) {
	idleDay := func(records int) *syntheticDay {
		day := emptySyntheticDay()
		random := sequence(7)
		for index := range records {
			at := day.start.Add(time.Duration(index) * daySeconds * time.Second / time.Duration(records))
			payload := map[string]any{"kind": "registry_access", "host": "grafana.example.org", "method": "GET", "uri": "/d/usage", "status": "200",
				"duration": "0.020", "request_bytes": "300", "response_bytes": "5000", "client_ip": "192.0.2.200", "request_id": fmt.Sprintf("%016x", index)}
			if index%2 == 1 {
				payload["host"], payload["uri"], payload["request_bytes"] = "registry.example.org", "/v0/health", "0"
			}
			day.entries = append(day.entries, syntheticEntry{timestamp: at.Format("2006-01-02T15:04:05.000000000Z"), payload: payload, position: random.next()})
		}
		return day
	}
	// asRegistryRequest turns one of the day's records into a request to the
	// registry that no sample of a third of the day holds.
	asRegistryRequest := func(day *syntheticDay, index int) {
		day.entries[index].payload["host"], day.entries[index].payload["request_bytes"] = "registry.example.org", "300"
		day.entries[index].position = 0.9
	}
	requireRefused := func(t *testing.T, day *syntheticDay) {
		t.Helper()
		_, _, err := day.collect(t, CompleteDay)
		var degraded *DegradedError
		require.ErrorAs(t, err, &degraded, "the access records and the counters disagree, so the day is not skipped")
		require.Len(t, day.reads, 1)
		assert.Equal(t, 1.0, day.reads[0].fraction, "the decision rests on an unsampled read")
	}

	t.Run("nothing to collect", func(t *testing.T) {
		day := idleDay(300)
		sample, _, err := day.collect(t, CompleteDay)
		require.ErrorIs(t, err, ErrNoTraffic)
		assert.Equal(t, 150, sample.idleTotals().BackgroundCount)
		assert.Contains(t, sample.Markdown(), "No access records were found for this period.")
		assert.Equal(t, []syntheticRead{{fraction: 1, delivered: 300}}, day.reads, "every record of the day is looked at, once")
	})

	t.Run("positive byte counter rules out a zero-traffic day", func(t *testing.T) {
		day := idleDay(300)
		day.bytes = 5000
		_, _, err := day.collect(t, CompleteDay)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrNoTraffic)
	})

	t.Run("counters without any data are not proof of an idle day", func(t *testing.T) {
		day := idleDay(300)
		day.unrecorded = true
		_, _, err := day.collect(t, CompleteDay)
		var degraded *DegradedError
		require.ErrorAs(t, err, &degraded, "this cannot be told from a collector that looks in the wrong place")
		assert.Equal(t, []string{"the gateway counters (no data for this day)"}, degraded.Missing)
		assert.Empty(t, day.reads)
	})

	t.Run("a registry request the counters did not count", func(t *testing.T) {
		day := idleDay(300)
		asRegistryRequest(day, 10)
		requireRefused(t, day)
		assert.Equal(t, 11, day.reads[0].delivered, "reading stops at the first registry request")

		sample, _, err := day.collect(t, AnyDay)
		require.NoError(t, err, "it can still be collected on request")
		assert.Equal(t, 1, sample.Records())
	})

	// More records than fit the limit, so a read of the day would be a sample
	// of a third of it. The one registry request is not in that sample.
	t.Run("a registry request that a sample of the day would miss", func(t *testing.T) {
		day := idleDay(6000)
		asRegistryRequest(day, 3000)
		requireRefused(t, day)
		assert.Equal(t, 3001, day.reads[0].delivered)
	})

	t.Run("too many records to look at them all", func(t *testing.T) {
		day := idleDay(6000)
		asRegistryRequest(day, 5500)
		requireRefused(t, day)
		assert.Equal(t, readCapFactor*syntheticLimit+1, day.reads[0].delivered, "looking stops at the cap; what was not seen is not taken as absent")

		// The same when there is no registry request at all: it was not shown, so the day is not skipped.
		requireRefused(t, idleDay(6000))
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
