package usage

import (
	"math"
	"sort"
	"strings"
	"time"
)

// How many names each breakdown keeps per day; the rest are folded into one row.
const (
	topOrganizations = 50
	topClients       = 40
	topNetworks      = 30
	topUnnamed       = 12
	topServers       = 100
	topNamespaces    = 60
	topServerTypes   = 20
)

// Row kinds, as the dashboards expect them.
const (
	rowNamed   = "named"
	rowOther   = "other"
	rowUnnamed = "unnamed"
)

const (
	// Where a day's requests and data sent come from. Mixed means one of the
	// two is from the counters and the other from the scaled access records.
	totalsFromCounters = "counters"
	totalsFromLogs     = "logs"
	totalsMixed        = "mixed"
)

// Daily is one day's totals, as stored in usage_daily.
type Daily struct {
	Day                       time.Time
	Requests                  int64
	GiB                       float64
	CostUSD                   float64
	UserAgents                *int
	NamedOrgs                 int
	NamedRequestsPct          *float64
	NamedDataPct              *float64
	ListingsCompressedPct     *float64
	ListingsCompressedDataPct *float64
	CacheHitPct               *float64
	RecordsSampled            int

	// What the rates above are rates of, as volumes on the scale of Requests
	// and GiB, so that they can be combined across days. Listing requests are
	// the base of the cache rate and of the listing-use and page-size shares.
	// The compression rates are of listing responses whose encoding is known.
	ListingRequests          *float64
	ListingGiB               *float64
	CompressionKnownRequests *float64
	CompressionKnownGiB      *float64

	// How closely the log-based estimate matched the gateway counters: 1 is a
	// perfect match. Nil when the counters were unavailable.
	LogRequestsRatio *float64
	LogDataRatio     *float64
	SampleFraction   float64
	ListingWeightMin float64
	ListingWeightMax float64
	BackgroundCount  int
	TotalsSource     string
	Caveats          []string
}

// Share is one row of a breakdown, as stored in usage_share.
type Share struct {
	Dimension     string
	Name          string
	Kind          string
	Rank          int
	RequestsPct   *float64
	DataPct       *float64
	CompressedPct *float64
	Addresses     *int
	IdentifiedBy  *string
	Category      *string
	RollupCount   *int
	NotFoundPct   *float64
	TopClientPct  *float64
	// CompressionKnownPct is the part of the day's requests that CompressedPct
	// is a rate of: this row's listing responses whose encoding is known.
	CompressionKnownPct *float64
}

func share(part, total float64) *float64 {
	if total <= 0 {
		return nil
	}
	value := 100 * part / total
	return &value
}

// scaled is the part of a day's total that part is of whole.
func scaled(total, part, whole float64) *float64 {
	if whole <= 0 {
		return nil
	}
	value := total * part / whole
	return &value
}

func optional(text string) *string {
	if text == "" {
		return nil
	}
	return &text
}

// group is the traffic of one name within a breakdown.
type group struct {
	tally
	missing float64
	byAgent bool
	byOwner bool
	clients *counter
}

// summarize groups records by key, largest first. Records for which key
// reports false are left out.
func summarize(records []*record, key func(*record) (string, bool), byRequests bool) []*group {
	index := map[string]*group{}
	var groups []*group
	for _, entry := range records {
		name, ok := key(entry)
		if !ok {
			continue
		}
		found, ok := index[name]
		if !ok {
			found = &group{tally: tally{name: name, addresses: addressSet{}}, clients: newCounter()}
			index[name] = found
			groups = append(groups, found)
		}
		found.count(entry)
		found.clients.add(entry.who, entry.weight)
		if entry.outcome == outcomeNotFound {
			found.missing += entry.weight
		}
		found.byAgent = found.byAgent || entry.how == byUserAgent
		found.byOwner = found.byOwner || entry.how == byNetworkOwner
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if byRequests {
			return groups[i].req > groups[j].req
		}
		return groups[i].bytes > groups[j].bytes
	})
	return groups
}

// breakdown describes how one dimension's groups become rows.
type breakdown struct {
	dimension  string
	top        int
	otherLabel string
	kind       string
	category   func(name string) string
	identified bool
	lookups    bool
}

func (s *Sample) rowsFor(b breakdown, groups []*group) []Share {
	kind := b.kind
	if kind == "" {
		kind = rowNamed
	}
	var rows []Share
	for _, member := range groups[:min(b.top, len(groups))] {
		rows = append(rows, s.row(b, member.name, []*group{member}, kind, false))
	}
	if len(groups) > b.top {
		rows = append(rows, s.row(b, b.otherLabel, groups[b.top:], rowOther, true))
	}
	return rows
}

func (s *Sample) row(b breakdown, name string, members []*group, kind string, rollup bool) Share {
	var req, size, gz, gzAll, missing float64
	byAgent, byOwner := false, false
	sets := make([]addressSet, len(members))
	clients := newCounter()
	for i, member := range members {
		req += member.req
		size += member.bytes
		gz += member.gz
		gzAll += member.gzAll
		missing += member.missing
		byAgent, byOwner = byAgent || member.byAgent, byOwner || member.byOwner
		sets[i] = member.addresses
		if b.lookups {
			clients.merge(member.clients)
		}
	}
	addresses := unionSize(sets)
	out := Share{
		Dimension:   b.dimension,
		Name:        name,
		Kind:        kind,
		RequestsPct: share(req, s.estRequests),
		DataPct:     share(size, s.estBytes),
		Addresses:   &addresses,
	}
	if gzAll != 0 {
		out.CompressedPct = share(gz, gzAll)
		out.CompressionKnownPct = share(gzAll, s.estRequests)
	}
	if rollup {
		count := len(members)
		out.RollupCount = &count
	} else if b.category != nil {
		out.Category = optional(b.category(members[0].name))
	}
	if b.identified && kind == rowNamed {
		out.IdentifiedBy = optional(identifiedLabel(byAgent, byOwner))
	}
	if b.lookups {
		// For lookups: the share that came back not found, and the client that asked most.
		out.NotFoundPct = share(missing, req)
		if most, ok := clients.top(); ok {
			out.IdentifiedBy = optional(most.key)
			out.TopClientPct = share(most.value, req)
		}
	}
	return out
}

// idleTotals records zero volumes for a day verified against counters and the
// full access log. Percentages and drift ratios have no denominator.
func (s *Sample) idleTotals() Daily {
	zero, agents := 0.0, 0
	source := totalsMixed // Requests from the counter, zero bytes from the full log.
	if s.counters.hasBytes {
		source = totalsFromCounters
	}
	return Daily{
		Day: s.Start, UserAgents: &agents, TotalsSource: source, SampleFraction: 1,
		BackgroundCount: s.background, ListingRequests: &zero, ListingGiB: &zero,
		CompressionKnownRequests: &zero, CompressionKnownGiB: &zero,
	}
}

// Rows turns the day into what is stored: its totals and every breakdown.
func (s *Sample) Rows() (Daily, []Share) {
	a := s.analyse()
	daily := s.daily(a)
	shares := s.howShares(a)
	shares = append(shares, s.whoShares()...)
	shares = append(shares, s.lookupShares()...)

	// Before user-agent logging every request shows as one client, which is not a real count.
	if len(a.clients) == 1 && a.clients[0].name == clientNotRecorded {
		daily.UserAgents = nil
		kept := shares[:0]
		for _, row := range shares {
			if row.Dimension != "client" && row.Dimension != "unnamed_client" {
				kept = append(kept, row)
			}
		}
		shares = kept
	}
	ranks := map[string]int{}
	for i := range shares {
		ranks[shares[i].Dimension]++
		shares[i].Rank = ranks[shares[i].Dimension]
	}
	return daily, shares
}

func (s *Sample) daily(a *analysis) Daily {
	gib := s.data / gibibyte
	agents := len(a.clients)
	daily := Daily{
		Day:                       s.Start,
		Requests:                  int64(math.Round(s.requests)),
		GiB:                       gib,
		CostUSD:                   gib * s.limits.DollarsPerGiB,
		UserAgents:                &agents,
		NamedOrgs:                 len(a.named),
		NamedRequestsPct:          share(a.namedReq, s.estRequests),
		NamedDataPct:              share(a.namedBytes, s.estBytes),
		ListingsCompressedPct:     share(a.gz, a.gzAll),
		ListingsCompressedDataPct: share(a.gzBytes, a.gzBytesAll),
		CacheHitPct:               share(a.cacheHits(), a.listingRequests),
		RecordsSampled:            len(s.records),
		SampleFraction:            s.fraction,
		ListingWeightMin:          s.weightMin,
		ListingWeightMax:          s.weightMax,
		BackgroundCount:           s.background,
		Caveats:                   append(append([]string{}, s.notes...), s.recorded...),
	}
	daily.ListingRequests = scaled(s.requests, a.listingRequests, s.estRequests)
	daily.ListingGiB = scaled(gib, a.listingBytes, s.estBytes)
	daily.CompressionKnownRequests = scaled(s.requests, a.gzAll, s.estRequests)
	daily.CompressionKnownGiB = scaled(gib, a.gzBytesAll, s.estBytes)
	fromCounters := 0
	if truthy(s.counters.requests, s.counters.hasRequests) {
		fromCounters++
		ratio := s.estRequests / s.counters.requests
		daily.LogRequestsRatio = &ratio
	}
	if truthy(s.counters.bytes, s.counters.hasBytes) {
		fromCounters++
		ratio := s.estBytes / s.counters.bytes
		daily.LogDataRatio = &ratio
	}
	daily.TotalsSource = [...]string{totalsFromLogs, totalsMixed, totalsFromCounters}[fromCounters]
	return daily
}

// howShares are the "how it is used" breakdowns. Endpoint and response shares
// are of all traffic; listing-use and page-size shares are of listing traffic.
func (s *Sample) howShares(a *analysis) []Share {
	var rows []Share
	for _, item := range a.endpointReq.mostCommon() {
		rows = append(rows, Share{Dimension: "endpoint", Name: item.key, Kind: rowNamed,
			RequestsPct: share(item.value, s.estRequests), DataPct: share(a.endpointSize.get(item.key), s.estBytes)})
	}
	for _, item := range a.useSize.mostCommon() {
		rows = append(rows, Share{Dimension: "listing_use", Name: item.key, Kind: rowNamed,
			RequestsPct: share(a.useReq.get(item.key), a.listingRequests), DataPct: share(item.value, a.listingBytes)})
	}
	common, rest := a.commonLimits()
	for _, item := range common {
		rows = append(rows, Share{Dimension: "page_size", Name: limitLabel(item.key), Kind: rowNamed,
			RequestsPct: share(item.value, a.listingRequests)})
	}
	if others := a.limits.len() - len(common); others > 0 {
		rows = append(rows, Share{Dimension: "page_size", Name: "other", Kind: rowOther,
			RequestsPct: share(rest, a.listingRequests), RollupCount: &others})
	}
	for _, item := range a.outcomes.mostCommon() {
		rows = append(rows, Share{Dimension: "response", Name: strings.ToLower(item.key), Kind: rowNamed,
			RequestsPct: share(item.value, s.estRequests)})
	}
	return rows
}

// whoShares are the "who uses it" breakdowns: organizations, clients,
// networks, and what the unnamed traffic is made of.
func (s *Sample) whoShares() []Share {
	var all, named, unnamed []*record
	spelling := map[string]string{}
	for i := range s.records {
		entry := &s.records[i]
		all = append(all, entry)
		if entry.how == "" {
			unnamed = append(unnamed, entry)
			continue
		}
		named = append(named, entry)
		// Names are merged without regard to case and shown under the first spelling seen.
		if key := strings.ToLower(entry.who); spelling[key] == "" {
			spelling[key] = entry.who
		}
	}
	byWho := func(entry *record) (string, bool) { return entry.who, true }
	byClient := func(entry *record) (string, bool) { return entry.client, true }
	byNetwork := func(entry *record) (string, bool) { return entry.network, true }

	rows := s.rowsFor(breakdown{dimension: "organization", top: topOrganizations, otherLabel: "Other named", identified: true},
		summarize(named, func(entry *record) (string, bool) { return spelling[strings.ToLower(entry.who)], true }, false))
	for _, bucket := range summarize(unnamed, byWho, false) {
		row := s.row(breakdown{dimension: "organization"}, bucket.name, []*group{bucket}, rowUnnamed, false)
		row.IdentifiedBy = optional("Not identifiable")
		rows = append(rows, row)
	}
	rows = append(rows, s.rowsFor(breakdown{dimension: "client", top: topClients, otherLabel: "Other clients"},
		summarize(all, byClient, false))...)
	rows = append(rows, s.rowsFor(breakdown{dimension: "network", top: topNetworks, otherLabel: "Other networks", category: s.networkKind},
		summarize(all, byNetwork, false))...)

	for _, bucket := range []struct{ name, label string }{{unnamedCloud, kindCloud}, {unnamedOther, "internet providers"}} {
		var members []*record
		for _, entry := range unnamed {
			if entry.who == bucket.name {
				members = append(members, entry)
			}
		}
		part := s.rowsFor(breakdown{dimension: "unnamed_network", top: topUnnamed, otherLabel: "Other " + bucket.label},
			summarize(members, byNetwork, false))
		for i := range part {
			part[i].Category = optional(bucket.label)
		}
		rows = append(rows, part...)
	}
	return append(rows, s.rowsFor(breakdown{dimension: "unnamed_client", top: topUnnamed, otherLabel: "Other clients"},
		summarize(unnamed, byClient, false))...)
}

func (s *Sample) networkKind(name string) string {
	if kind, ok := s.kinds[name]; ok {
		return kind
	}
	return kindUnknown
}

// lookupShares say which servers, namespaces and server types were looked up.
// Requested names are client-supplied text, so only names that exist in the
// catalogue appear; everything else is one group.
func (s *Sample) lookupShares() []Share {
	if !s.hasLookups {
		return nil
	}
	var known, unknown []*record
	for i := range s.records {
		switch entry := &s.records[i]; entry.lookup {
		case "":
		case notListed:
			unknown = append(unknown, entry)
		default:
			known = append(known, entry)
		}
	}
	catalogue := s.catalogue
	dimensions := []struct {
		breakdown
		key func(*record) (string, bool)
	}{
		{breakdown{dimension: "server", top: topServers, category: catalogue.typeOf},
			func(entry *record) (string, bool) { return entry.lookup, true }},
		{breakdown{dimension: "server_namespace", top: topNamespaces, category: namespaceKind},
			func(entry *record) (string, bool) { return namespaceOf(entry.lookup), true }},
		{breakdown{dimension: "server_type", top: topServerTypes},
			func(entry *record) (string, bool) { return catalogue.typeOf(entry.lookup), true }},
	}
	var rows []Share
	for _, dimension := range dimensions {
		dimension.otherLabel, dimension.lookups = "Other", true
		rows = append(rows, s.rowsFor(dimension.breakdown, summarize(known, dimension.key, true))...)
		if len(unknown) > 0 {
			all := summarize(unknown, func(*record) (string, bool) { return notListed, true }, true)
			rows = append(rows, s.row(breakdown{dimension: dimension.dimension, lookups: true}, notListed, all, rowUnnamed, false))
		}
	}
	return rows
}
