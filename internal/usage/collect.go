package usage

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	requestsMetric = "registry_gateway_canary_requests_total"
	bytesMetric    = "registry_gateway_canary_response_bytes_sum"
	hourSeconds    = 3600
	daySeconds     = 86400

	noteNoCounters       = "Gateway counters were unavailable, so totals are estimated from logs alone."
	noteNoHourlyCounters = "Hourly gateway counters were unavailable, so one scaling factor was used for the whole period."
	noteNoOwners         = "The network-owner table could not be loaded, so networks show as Unknown."
	noteRepeatedID       = "One request ID repeats across many requests, which can skew the log sample."
	noteNotRecorded      = `Some records predate user-agent logging (Nov 12 09:10 UTC) and show as "(not recorded)".`

	// These are recorded with the day but are not part of the report.
	noteNoTypes        = "The network-type list could not be loaded, so fewer networks are recognised as internet providers."
	noteNoCatalogue    = "No published servers were available, so server lookups are not broken down."
	noteEmptyCounters  = "Gateway counters had no data for this day, so totals are estimated from logs alone."
	noteStaleCatalogue = "The catalogue could not be refreshed, so lookups of recently published servers may be counted as unknown names."
	noteLastChance     = "Stored as it is because this was the last run that could collect the day."
	noteUnevenSample   = "The access records could not be sampled evenly, so the shares may not represent the whole day."

	// A day's read is sized by first counting a small sample of its records.
	// For a day that holds exactly MaxRecords records that sample returns
	// about this many, which puts the count within about a tenth of the truth;
	// it is closer for larger days.
	sizingRecords = 100
	// If even that sample returns too many records to count, the next one is
	// this many times smaller.
	sizingStep = 100
	// A read that returns more than this many times MaxRecords is stopped, and
	// one that returns less than MaxRecords divided by thinFactor is too small
	// to settle for. Either is read again at another fraction, at most
	// maxResizes times. Sizing keeps a read near MaxRecords, so this happens
	// only when the sample is very uneven, as when many records share one
	// request ID: those are all in a sample or all out of it.
	readCapFactor = 2
	thinFactor    = 2
	maxResizes    = 4
	minFraction   = 0.000001
	// Two fractions closer together than this return much the same records.
	sameSample = 1.1
	// A sample's count of n records is uncertain by about its square root.
	// Samples are called uneven when one contradicts another by more than this
	// many times that.
	unevenDeviations = 4
)

// ErrNoRecords means Cloud Logging returned no access records for the day.
var ErrNoRecords = errors.New("no access records were found for this period")

// ErrNoTraffic means the gateway's counters were being recorded on the day
// and show no registry request, and no access record shows one either, so
// there is nothing to collect.
var ErrNoTraffic = errors.New("the gateway counted no registry requests and no access record shows one")

// ErrSampling means no sample of the day's access records could be read
// that both fits the limit and holds something to count. The records exist;
// the day cannot be collected at this limit.
var ErrSampling = errors.New("the day's access records could not be sampled")

var (
	// errOverflow stops a read that has returned more records than it may.
	errOverflow = errors.New("the read returned more access records than expected")
	// errStopRead stops a read that has seen what it was looking for.
	errStopRead = errors.New("stop reading")
)

// sizing is what reading a sample of the day showed: how many access records
// came back at a fraction. It says nothing certain about any other fraction.
type sizing struct {
	fraction float64
	seen     int
}

// DegradedError means a day was not read because a source it needs could not
// be used.
type DegradedError struct {
	Missing []string
	// Final says no later scheduled run will look at this day.
	Final bool
}

func (e *DegradedError) Error() string {
	missing := strings.Join(e.Missing, " and ")
	if e.Final {
		return fmt.Sprintf("not collected, because %s could not be used; this was the last scheduled run that looks at the day, "+
			"so it stays missing unless it is named with --day", missing)
	}
	return fmt.Sprintf("not collected, because %s could not be used; the day stays missing and the next run collects it again", missing)
}

// Tolerance says which days are worth reading.
type Tolerance int

const (
	// AnyDay collects a day whatever is missing, and says so in its caveats.
	AnyDay Tolerance = iota
	// ScalableDay refuses only a day whose request counters could not be used.
	// Without them its records cannot be scaled to the gateway's totals.
	// Anything else that is missing makes the day less detailed, not wrong, and
	// is recorded in its caveats.
	ScalableDay
	// CompleteDay refuses a day when any source it needs could not be used.
	CompleteDay
)

// Sources are what a day is collected from.
type Sources struct {
	Logs     LogReader
	Metrics  MetricQuerier
	Networks *NetworkTable
	// Catalogue is optional. Without it, server lookups are not broken down.
	Catalogue *Catalogue
	// CatalogueStale says the catalogue could not be refreshed for this run.
	CatalogueStale bool
	Logf           func(format string, args ...any)
}

// Limits tune a collection.
type Limits struct {
	// MaxRecords is about how many access records a day's read may return. A
	// day that holds more is read as a sample of about this many.
	MaxRecords int
	// DollarsPerGiB prices the data sent, for the cost estimate.
	DollarsPerGiB float64
	// Require says which days to refuse. A refused day is refused before its
	// access records are read.
	Require Tolerance
}

type compression int8

const (
	compressionUnknown compression = iota
	compressionNone
	compressionGzip
)

// record is one access record reduced to what the report counts. The source
// address is kept only as a number that is meaningless outside this run.
type record struct {
	sampled   bool
	listingOK bool
	bucket    int
	bytes     float64
	client    string
	address   uint32
	network   string
	who       string
	how       string
	endpoint  string
	use       string
	limit     string
	outcome   string
	gzip      compression
	cache     string
	lookup    string
	weight    float64
}

func (r *record) isListing() bool {
	return r.use != ""
}

// Sample is one UTC day of access records, weighted to the gateway's totals.
type Sample struct {
	Start time.Time
	End   time.Time

	records    []record
	kinds      map[string]string
	notes      []string
	recorded   []string
	degraded   []string
	unscalable bool
	background int
	buckets    int
	fraction   float64
	hasLookups bool
	catalogue  *Catalogue
	limits     Limits

	counters    gatewayTotals
	otherWeight float64
	weightMin   float64
	weightMax   float64
	estRequests float64
	estBytes    float64
	requests    float64
	data        float64
}

// gatewayTotals are the day's unsampled counters. A missing value means the
// query failed, which is different from a counter that reads zero.
type gatewayTotals struct {
	requests    float64
	listingOK   float64
	bytes       float64
	hasRequests bool
	hasBytes    bool
}

// Degraded lists the sources that were unavailable. A day collected without
// them is less accurate than the same day collected later.
func (s *Sample) Degraded() []string {
	return s.degraded
}

// Records is how many access records the shares are based on.
func (s *Sample) Records() int {
	return len(s.records)
}

type dayCollector struct {
	sources   Sources
	limits    Limits
	start     time.Time
	owners    *ownerLookup
	addresses map[string]uint32
	requests  *counter
	hourly    []hourlyTotal
	sample    *Sample
	// seen is how many records the current read has returned, kept or not.
	seen int
}

type hourlyTotal struct {
	listingOK float64
	known     bool
}

// CollectDay reads one full UTC day. It returns ErrNoRecords or ErrNoTraffic,
// together with a sample that can still be rendered, when the day has no
// access records.
func CollectDay(ctx context.Context, sources Sources, limits Limits, day time.Time) (*Sample, error) {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	collector := &dayCollector{
		sources:   sources,
		limits:    limits,
		start:     start,
		owners:    newOwnerLookup(sources.Networks),
		addresses: map[string]uint32{},
		requests:  newCounter(),
		sample: &Sample{
			Start:      start,
			End:        start.Add(daySeconds * time.Second),
			buckets:    daySeconds / hourSeconds,
			fraction:   1,
			limits:     limits,
			hasLookups: sources.Catalogue.Len() > 0,
			catalogue:  sources.Catalogue,
		},
	}
	sample := collector.sample
	collector.readCounters(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The counters were being recorded and show no registry request. If no
	// access record says otherwise, the gateway served the registry nothing
	// that day, which is not a failure.
	if collector.idle(ctx) {
		absent, err := collector.noRegistryRecord(ctx)
		if err != nil {
			return nil, err
		}
		if absent {
			sample.kinds = collector.owners.kinds()
			return sample, ErrNoTraffic
		}
	}

	if !sources.Networks.OwnersLoaded() {
		sample.notes = append(sample.notes, noteNoOwners)
		sample.degraded = append(sample.degraded, "the network-owner table")
	} else if !sources.Networks.TypesLoaded() {
		sample.recorded = append(sample.recorded, noteNoTypes)
		sample.degraded = append(sample.degraded, "the network-type list")
	}
	if sources.CatalogueStale {
		sample.recorded = append(sample.recorded, noteStaleCatalogue)
		sample.degraded = append(sample.degraded, "an up-to-date catalogue")
	}
	if !sample.hasLookups {
		sample.recorded = append(sample.recorded, noteNoCatalogue)
	}
	// Everything that can make a day incomplete is known by now. Reading its
	// access records takes minutes, so a day that will not be kept is not read
	// at all.
	switch {
	case len(sample.degraded) == 0:
	case limits.Require == CompleteDay, limits.Require == ScalableDay && sample.unscalable:
		return nil, &DegradedError{Missing: sample.degraded, Final: limits.Require == ScalableDay}
	case limits.Require == ScalableDay:
		sample.recorded = append(sample.recorded, noteLastChance)
	}
	if err := collector.read(ctx); err != nil {
		return nil, err
	}
	sample.kinds = collector.owners.kinds()
	// read returns without records only after reading the whole day.
	if len(sample.records) == 0 {
		if truthy(sample.counters.requests, sample.counters.hasRequests) {
			return sample, fmt.Errorf("%w, although the gateway counted %.0f requests; "+
				"the log settings may not name where the gateway wrote its access records that day", ErrNoRecords, sample.counters.requests)
		}
		return sample, ErrNoRecords
	}
	collector.weigh()
	collector.review()
	return sample, nil
}

func (c *dayCollector) logf(format string, args ...any) {
	if c.sources.Logf != nil {
		c.sources.Logf(format, args...)
	}
}

func truthy(value float64, known bool) bool {
	return known && value != 0
}

// readCounters asks the gateway's counters for the day's totals and, hour by
// hour, for the successful listing requests that Cloud Logging samples.
func (c *dayCollector) readCounters(ctx context.Context) {
	sample := c.sample
	totals, err := c.dayTotals(ctx)
	sample.counters = totals
	if err != nil {
		c.logf("gateway counters unavailable: %v", err)
		sample.notes = append(sample.notes, noteNoCounters)
		sample.degraded = append(sample.degraded, "the gateway counters")
	} else if !truthy(totals.requests, totals.hasRequests) || !truthy(totals.bytes, totals.hasBytes) {
		// The counters answered but hold nothing for this day, as happens once it
		// is older than their retention. The report says nothing about this.
		sample.recorded = append(sample.recorded, noteEmptyCounters)
		sample.degraded = append(sample.degraded, "the gateway counters (no data for this day)")
	}
	c.hourly = make([]hourlyTotal, sample.buckets)
	if !truthy(totals.requests, totals.hasRequests) {
		sample.unscalable = true
		return
	}
	// The share of listing records that Cloud Logging keeps can change during a
	// day, so listing records are scaled to the counters hour by hour.
	selector := `traffic="registry",route=~"list_v0|list_v01",method=~"GET|HEAD",status=~"200|304"`
	query := fmt.Sprintf("sum(increase(%s{%s}[%ds]))", requestsMetric, selector, hourSeconds)
	hourly := make([]hourlyTotal, sample.buckets)
	for index := range hourly {
		at := sample.Start.Add(time.Duration(index+1) * hourSeconds * time.Second)
		rows, err := c.sources.Metrics.Query(ctx, query, at)
		if err != nil {
			c.logf("hourly gateway counters unavailable: %v", err)
			sample.notes = append(sample.notes, noteNoHourlyCounters)
			sample.degraded = append(sample.degraded, "the hourly gateway counters")
			return
		}
		hourly[index].known = true
		if len(rows) > 0 {
			hourly[index].listingOK = rows[0].Value
		}
	}
	c.hourly = hourly
}

func (c *dayCollector) dayTotals(ctx context.Context) (gatewayTotals, error) {
	var totals gatewayTotals
	at := c.sample.End
	rows, err := c.sources.Metrics.Query(ctx,
		fmt.Sprintf(`sum by (route, method, status) (increase(%s{traffic="registry"}[%ds]))`, requestsMetric, daySeconds), at)
	if err != nil {
		return totals, err
	}
	for _, row := range rows {
		totals.requests += row.Value
	}
	for _, row := range rows {
		route, status := row.Labels["route"], row.Labels["status"]
		if (route == "list_v0" || route == "list_v01") && isRead(row.Labels["method"]) && (status == "200" || status == "304") {
			totals.listingOK += row.Value
		}
	}
	totals.hasRequests = true
	rows, err = c.sources.Metrics.Query(ctx,
		fmt.Sprintf(`sum(increase(%s{traffic="registry"}[%ds]))`, bytesMetric, daySeconds), at)
	if err != nil {
		return totals, err
	}
	if len(rows) > 0 {
		totals.bytes, totals.hasBytes = rows[0].Value, true
	}
	return totals, nil
}

// idle reports whether the gateway's counters were being recorded on the day
// and show no registry request. Counters with no data at all are a different
// thing: that cannot be told from looking in the wrong place.
func (c *dayCollector) idle(ctx context.Context) bool {
	if !c.sample.counters.hasRequests || c.sample.counters.requests != 0 {
		return false
	}
	rows, err := c.sources.Metrics.Query(ctx, fmt.Sprintf("count(last_over_time(%s[%ds]))", requestsMetric, daySeconds), c.sample.End)
	return err == nil && len(rows) > 0 && rows[0].Value > 0
}

// noRegistryRecord reports whether none of the day's access records is a
// request to the registry. Absence can only be shown by looking at every
// record, so this reads the day whole, keeping nothing; a sample that holds
// no such record would prove nothing. It gives up, and reports false, on a day
// with more records than a read may return.
func (c *dayCollector) noRegistryRecord(ctx context.Context) (bool, error) {
	limit := readCapFactor * c.limits.MaxRecords
	seen, found := 0, false
	window := LogWindow{Start: c.sample.Start, End: c.sample.End, Fraction: 1}
	err := c.sources.Logs.Read(ctx, window, func(_ string, payload map[string]any) error {
		seen++
		if registry, _ := registryRecord(payload); registry {
			found = true
			return errStopRead
		}
		if limit > 0 && seen > limit {
			return errOverflow
		}
		return nil
	})
	switch {
	case errors.Is(err, errStopRead):
		return false, nil
	case errors.Is(err, errOverflow):
		c.logf("the counters show no registry request, but the first %d access records of the day do not settle whether there was one", limit)
		return false, nil
	case err != nil:
		return false, err
	}
	return !found, nil
}

// read reads the day's access records, whole if they fit the limit and as a
// sample of about MaxRecords if not.
//
// How many records Cloud Logging holds for a day depends on how much of the
// traffic its storage rule thinned out, which changes from day to day and
// within a day, so it is counted on a small sample and never assumed. That
// count, like any sample's, only ever chooses the next fraction to try.
// Whether a read is kept is decided by what that read itself returned: it
// must fit, and unless it is the whole day it must not be far smaller than
// intended. An empty sample in particular says nothing about the day.
func (c *dayCollector) read(ctx context.Context) error {
	sample := c.sample
	limit := c.limits.MaxRecords
	if limit <= 0 {
		_, err := c.attempt(ctx, 1)
		return err
	}
	// low is the largest sample known to fit, and high the smallest fraction
	// known to return too many records, or zero. basis is the latest sample
	// that fit, whatever its size, and target the size to aim for.
	low, high, err := c.countSample(ctx)
	if err != nil {
		return err
	}
	basis, target := low, limit
	// The fraction of the sizing sample is not among those tried: it was
	// counted, not read.
	var tried []float64
	next := nextFraction(basis, low.fraction, high, target)
	c.logf("window %s..%s; Cloud Logging holds about %.0f access records; reading logs at fraction %s", sample.Start.Format(stampLayout),
		sample.End.Format(stampLayout), float64(low.seen)/low.fraction, pyFloatRepr(next))
	// held says whether the records in memory are the sample at low, and
	// spent that a sample without a registry request left no room for a larger one.
	var held, spent bool
	for resizes := 0; ; resizes++ {
		overflow, err := c.attempt(ctx, next)
		if err != nil {
			return err
		}
		fraction, returned := next, c.seen
		tried = append(tried, fraction)
		switch {
		case overflow:
			if high == 0 || fraction < high {
				high = fraction
			}
			held = false
		case fraction == 1 || (returned*thinFactor >= limit && len(sample.records) > 0):
			if c.contradicted(sizing{fraction: fraction, seen: returned}, high) {
				sample.recorded = append(sample.recorded, noteUnevenSample)
			}
			return nil
		default:
			basis = sizing{fraction: fraction, seen: returned}
			if held = fraction >= low.fraction; held {
				low = basis
			}
			if returned*thinFactor >= limit {
				// The size intended, but without a registry request in it.
				// There is room below the cap for a larger one, unless this
				// one has used it.
				target = limit + limit/2
				spent = returned >= target
			}
		}
		next = nextFraction(basis, low.fraction, high, target)
		// The whole day is always worth reading: it is the one read whose answer is final.
		if resizes == maxResizes || spent || (next < 1 && sameAsOneOf(tried, next)) {
			break
		}
		if overflow {
			c.logf("  more than %d access records at fraction %s; reading again at fraction %s",
				readCapFactor*limit, pyFloatRepr(fraction), pyFloatRepr(next))
		} else {
			c.logf("  only %d access records at fraction %s, %d of them registry requests; reading again at fraction %s",
				returned, pyFloatRepr(fraction), len(sample.records), pyFloatRepr(next))
		}
	}
	return c.settle(ctx, low.fraction, held)
}

// contradicted reports whether a sample that is about to be kept is at odds
// with what another read showed: a fraction returned too many records,
// although by this sample's own count, and allowing for how uncertain that
// count is, it would have fit. Records that come and go together between
// samples, as those sharing a request ID do, look like that; a day that is
// simply large does not. The whole day is at odds with nothing, because
// nothing is left out of it.
func (c *dayCollector) contradicted(kept sizing, high float64) bool {
	if kept.fraction >= 1 || high <= 0 || kept.seen == 0 {
		return false
	}
	seen := float64(kept.seen)
	expected := seen * high / kept.fraction
	return expected*(1+unevenDeviations/math.Sqrt(seen)) < float64(readCapFactor*c.limits.MaxRecords)
}

// settle ends a search that found no sample near the limit. The largest
// sample known to fit is used, as long as it holds something to count. held
// says whether the records in memory are that sample.
func (c *dayCollector) settle(ctx context.Context, fraction float64, held bool) error {
	limit := c.limits.MaxRecords
	if !held {
		overflow, err := c.attempt(ctx, fraction)
		if err != nil {
			return err
		}
		if overflow {
			return fmt.Errorf("%w: the sample at fraction %s did not return the same records twice", ErrSampling, pyFloatRepr(fraction))
		}
	}
	if len(c.sample.records) == 0 {
		return fmt.Errorf("%w: the largest sample that returned no more than %d records, at fraction %s, held %d and none of them a registry request",
			ErrSampling, readCapFactor*limit, pyFloatRepr(fraction), c.seen)
	}
	c.logf("  no sample near %d access records was found; using the %d at fraction %s", limit, c.seen, pyFloatRepr(fraction))
	c.sample.recorded = append(c.sample.recorded, noteUnevenSample)
	return nil
}

// countSample counts a small sample of the day's access records, keeping
// none. If even that returns more than a read may, it is taken again smaller.
// It returns the sample that could be counted, and the smallest fraction that
// could not, or zero.
func (c *dayCollector) countSample(ctx context.Context) (sizing, float64, error) {
	limit := c.limits.MaxRecords
	fraction, tooMany := math.Min(1, sizingRecords/float64(limit)), 0.0
	for {
		count := 0
		window := LogWindow{Start: c.sample.Start, End: c.sample.End, Fraction: fraction}
		err := c.sources.Logs.Read(ctx, window, func(string, map[string]any) error {
			count++
			if count > readCapFactor*limit {
				return errOverflow
			}
			return nil
		})
		switch {
		case err == nil:
			return sizing{fraction: fraction, seen: count}, tooMany, nil
		case !errors.Is(err, errOverflow):
			return sizing{}, 0, err
		case fraction <= minFraction:
			return sizing{}, 0, fmt.Errorf("%w: even at fraction %s the day returns more than %d records",
				ErrSampling, pyFloatRepr(fraction), readCapFactor*limit)
		}
		tooMany = fraction
		fraction = math.Max(minFraction, fraction/sizingStep)
		c.logf("  more than %d access records at fraction %s; counting again at fraction %s",
			readCapFactor*limit, pyFloatRepr(tooMany), pyFloatRepr(fraction))
	}
}

// attempt reads the day's sample at a fraction, forgetting any earlier
// attempt. It reports whether the read was stopped for returning too many
// records.
func (c *dayCollector) attempt(ctx context.Context, fraction float64) (bool, error) {
	c.seen = 0
	c.owners = newOwnerLookup(c.sources.Networks)
	c.addresses = map[string]uint32{}
	c.requests = newCounter()
	c.sample.records = nil
	c.sample.background = 0
	c.sample.fraction = fraction
	err := c.sources.Logs.Read(ctx, LogWindow{Start: c.sample.Start, End: c.sample.End, Fraction: fraction}, c.accept)
	if errors.Is(err, errOverflow) {
		return true, nil
	}
	return false, err
}

// nextFraction chooses the fraction to try next: the one at which a sample
// that fit, by its own count, would return about target records. If that is
// as good as known to return too many, it goes halfway between the largest
// fraction known to fit and the smallest known not to (zero if none is).
// Nothing is assumed about how the records are spread over the day or over
// the samples; the read that follows shows whether the choice was right.
func nextFraction(basis sizing, low, high float64, target int) float64 {
	// A sample that came back empty leads to the whole day.
	next := readFraction(float64(basis.seen)/basis.fraction, target)
	if high > 0 && next*sameSample > high {
		next = math.Max(minFraction, math.Trunc(1e6*math.Sqrt(low*high))/1e6)
	}
	return next
}

func sameAsOneOf(tried []float64, fraction float64) bool {
	for _, earlier := range tried {
		if fraction > earlier/sameSample && fraction < earlier*sameSample {
			return true
		}
	}
	return false
}

// readFraction is the share of a day's stored records to read so that about
// maxRecords are returned.
func readFraction(stored float64, maxRecords int) float64 {
	if maxRecords <= 0 || stored <= float64(maxRecords) {
		return 1
	}
	if fraction := math.Trunc(float64(1000*maxRecords)/stored) / 1000; fraction >= 0.001 {
		return fraction
	}
	return math.Max(minFraction, math.Trunc(1e6*float64(maxRecords)/stored)/1e6)
}

func (c *dayCollector) bucket(timestamp string) int {
	if len(timestamp) > 19 {
		timestamp = timestamp[:19]
	}
	at, err := time.Parse(stampLayout, timestamp+"Z")
	if err != nil {
		return 0
	}
	bucket := int(math.Floor(at.Sub(c.start).Seconds() / hourSeconds))
	return min(max(bucket, 0), c.sample.buckets-1)
}

func (c *dayCollector) addressID(address string) uint32 {
	id, ok := c.addresses[address]
	if !ok {
		id = uint32(len(c.addresses)) //nolint:gosec // G115: a day never has four billion distinct sources
		c.addresses[address] = id
	}
	return id
}

// registryRecord reports whether an access record is a request to the
// registry, which is what the gateway's counters count, and if not, whether
// it is a background request.
func registryRecord(payload map[string]any) (registry, background bool) {
	// Records for the dashboards' host and for the gateway's test routes are
	// not the registry's traffic.
	if host := pyField(payload, "host"); strings.HasPrefix(host, "grafana.") || isFixtureHost(host) {
		return false, false
	}
	if pyStr(payload["request_bytes"]) == "0" {
		return false, true
	}
	return true, false
}

// accept turns one access record into a record, or drops it.
func (c *dayCollector) accept(timestamp string, payload map[string]any) error {
	c.seen++
	if limit := readCapFactor * c.limits.MaxRecords; limit > 0 && c.seen > limit {
		return errOverflow
	}
	registry, background := registryRecord(payload)
	if background {
		c.sample.background++
	}
	if !registry {
		return nil
	}
	method, status := pyField(payload, "method"), pyField(payload, "status")
	path, rawQuery, err := pyURLSplit(pyField(payload, "uri"))
	if err != nil {
		path, rawQuery = "", ""
	}
	kind := endpoint(method, path)
	isListing := strings.HasPrefix(kind, listingPrefix)
	success := status == "200" || status == "304"
	address := pyField(payload, "client_ip")
	network := c.owners.lookup(address)
	client := clientName(agentOf(payload))
	c.requests.add(pyField(payload, "request_id"), 1)

	entry := record{
		sampled:   isListing && success && strings.HasPrefix(pyField(payload, "duration"), "0."),
		listingOK: isListing && success,
		bucket:    c.bucket(timestamp),
		bytes:     responseSize(payload),
		client:    client,
		address:   c.addressID(address),
		network:   network.name,
		endpoint:  kind,
		outcome:   outcome(status),
	}
	entry.who, entry.how = organization(client, network.name, network.kind)
	if isListing {
		query := pyParseQS(rawQuery)
		entry.use = listingUse(query)
		entry.limit = "default (30)"
		if limit := query["limit"]; len(limit) > 0 {
			entry.limit = limit[0]
		}
		entry.cache = pyField(payload, "cache")
		if encoding, ok := payload["content_encoding"]; status == "200" && ok && encoding != nil {
			entry.gzip = compressionNone
			if encoding == "gzip" {
				entry.gzip = compressionGzip
			}
		}
	}
	if c.sample.hasLookups {
		if name, ok := requestedServer(method, path); ok {
			entry.lookup = c.sources.Catalogue.resolve(name)
		}
	}
	c.sample.records = append(c.sample.records, entry)
	return nil
}

// agentOf returns the user agent, or nil when the record has none.
func agentOf(payload map[string]any) *string {
	value := payload["user_agent"]
	if value == nil {
		return nil
	}
	text := pyStr(value)
	return &text
}

// responseSize is the size of the response in bytes, or zero when the record
// does not say.
func responseSize(payload map[string]any) float64 {
	for _, key := range []string{"response_bytes", "body_bytes"} {
		if value := payload[key]; pyTruthy(value) {
			size, ok := pyInt(value)
			if !ok {
				return 0
			}
			return size
		}
	}
	return 0
}

// weigh scales each record so that the sample adds up to the day's traffic.
// Records that Cloud Logging samples get an hourly weight derived from the
// counters; all others stand for themselves, or for 1/fraction of themselves.
func (c *dayCollector) weigh() {
	sample := c.sample
	other := 1.0 / sample.fraction
	sampled, kept := make([]float64, sample.buckets), make([]float64, sample.buckets)
	totalSampled, totalKept := 0.0, 0.0
	for i := range sample.records {
		entry := &sample.records[i]
		switch {
		case entry.sampled:
			sampled[entry.bucket]++
			totalSampled++
		case entry.listingOK:
			kept[entry.bucket]++
			totalKept++
		}
	}
	fallback := fmul(10.0, other)
	if truthy(sample.counters.listingOK, sample.counters.hasRequests) && totalSampled > 0 {
		fallback = math.Max(other, (sample.counters.listingOK-fmul(totalKept, other))/totalSampled)
	}
	weights := c.hourlyWeights(sampled, kept, other, fallback)
	sample.otherWeight, sample.weightMin, sample.weightMax = other, weights[0], weights[0]
	for _, weight := range weights {
		sample.weightMin, sample.weightMax = math.Min(sample.weightMin, weight), math.Max(sample.weightMax, weight)
	}
	for i := range sample.records {
		entry := &sample.records[i]
		entry.weight = other
		if entry.sampled {
			entry.weight = weights[entry.bucket]
		}
		sample.estRequests += entry.weight
		sample.estBytes += fmul(entry.weight, entry.bytes)
	}
	sample.requests, sample.data = sample.estRequests, sample.estBytes
	if truthy(sample.counters.requests, sample.counters.hasRequests) {
		sample.requests = sample.counters.requests
	}
	if truthy(sample.counters.bytes, sample.counters.hasBytes) {
		sample.data = sample.counters.bytes
	}
}

// hourlyWeights is how many requests one sampled record of each hour stands
// for, given how many sampled and other listing records each hour has.
func (c *dayCollector) hourlyWeights(sampled, kept []float64, other, fallback float64) []float64 {
	// share is how many requests an hour's sampled records stand for. When
	// every such record of an hour was thinned out, nothing is left to carry
	// that hour's requests, so they go to the nearest hour that kept some.
	// On a quiet day most hours are like that.
	share := make([]float64, len(sampled))
	for index := range share {
		if c.hourly[index].known {
			share[index] = c.hourly[index].listingOK - fmul(kept[index], other)
		}
	}
	for index := range share {
		if !c.hourly[index].known || sampled[index] > 0 || share[index] <= 0 {
			continue
		}
		if nearest := nearestSampled(sampled, index); nearest >= 0 {
			share[nearest] += share[index]
		}
	}
	weights := make([]float64, len(sampled))
	for index := range weights {
		weights[index] = fallback
		if c.hourly[index].known && sampled[index] > 0 {
			weights[index] = math.Max(other, share[index]/sampled[index])
		}
	}
	return weights
}

// nearestSampled is the hour closest to the given one that has sampled
// records, the earlier one if two are equally close, or -1 if none has.
func nearestSampled(sampled []float64, hour int) int {
	for distance := 1; distance < len(sampled); distance++ {
		if before := hour - distance; before >= 0 && sampled[before] > 0 {
			return before
		}
		if after := hour + distance; after < len(sampled) && sampled[after] > 0 {
			return after
		}
	}
	return -1
}

// review compares the log-based estimate with the counters and notes anything
// that makes the day's shares less trustworthy.
func (c *dayCollector) review() {
	sample := c.sample
	c.logf("records %d (+%d background); listing weight %.1f to %.1f over %d buckets, other %.2f; log estimate vs counters: requests %s, bytes %s",
		len(sample.records), sample.background, sample.weightMin, sample.weightMax, sample.buckets, sample.otherWeight,
		ratioText(sample.estRequests, sample.requests), ratioText(sample.estBytes, sample.data))
	if repeated, ok := c.requests.top(); ok && repeated.key != "" && repeated.value > 0.01*float64(len(sample.records)) {
		sample.notes = append(sample.notes, noteRepeatedID)
	}
	for _, check := range []struct {
		label    string
		estimate float64
		counter  float64
		known    bool
	}{
		{"request count", sample.estRequests, sample.counters.requests, sample.counters.hasRequests},
		{"data volume", sample.estBytes, sample.counters.bytes, sample.counters.hasBytes},
	} {
		if !truthy(check.counter, check.known) {
			continue
		}
		if difference := check.estimate/check.counter - 1; math.Abs(difference) > 0.1 {
			sample.notes = append(sample.notes, fmt.Sprintf(
				"Log-based %s differs from the gateway counters by %+.0f%%; treat the shares as approximate.", check.label, 100*difference))
		}
	}
	for i := range sample.records {
		if sample.records[i].client == clientNotRecorded {
			sample.notes = append(sample.notes, noteNotRecorded)
			break
		}
	}
}

func ratioText(part, whole float64) string {
	if whole == 0 {
		return "n/a"
	}
	return strconv.FormatFloat(part/whole, 'f', 3, 64)
}
