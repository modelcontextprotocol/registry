package usage

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	// The report lists this many clients and organizations individually.
	reportTopClients = 8
	reportTopOrgs    = 12
	gibibyte         = 1 << 30
	defaultLimit     = "default (30)"

	columnRequests  = "Requests"
	columnData      = "Data sent"
	columnAddresses = "Addresses seen"
)

type addressSet map[uint32]struct{}

func (s addressSet) add(id uint32) {
	s[id] = struct{}{}
}

func unionSize(sets []addressSet) int {
	if len(sets) == 1 {
		return len(sets[0])
	}
	all := addressSet{}
	for _, set := range sets {
		for id := range set {
			all[id] = struct{}{}
		}
	}
	return len(all)
}

// tally is the traffic of one client, organization, network or other group.
type tally struct {
	name      string
	req       float64
	bytes     float64
	gz        float64
	gzAll     float64
	addresses addressSet
}

func (t *tally) count(entry *record) {
	t.req += entry.weight
	t.bytes += fmul(entry.weight, entry.bytes)
	t.addresses.add(entry.address)
	if entry.gzip != compressionUnknown {
		t.gzAll += entry.weight
		if entry.gzip == compressionGzip {
			t.gz += entry.weight
		}
	}
}

type clientTally struct {
	tally
	networks *counter
	does     *counter
}

type orgTally struct {
	tally
	byAgent bool
	byOwner bool
}

func (o *orgTally) named() bool {
	return o.byAgent || o.byOwner
}

// identifiedLabel says how a name was identified, in the report's wording.
func identifiedLabel(byAgent, byOwner bool) string {
	switch {
	case byAgent && byOwner:
		return "User agent and network owner"
	case byAgent:
		return byUserAgent
	case byOwner:
		return byNetworkOwner
	}
	return ""
}

// analysis holds the day's figures that the report and the stored rows share.
type analysis struct {
	clients []*clientTally
	ranked  []*clientTally
	named   []*orgTally
	unnamed []*orgTally

	endpointReq, endpointSize *counter
	useReq, useSize           *counter
	limits, cache, outcomes   *counter
	networkReq, networkSize   *counter
	networkAddresses          map[string]addressSet

	listingRequests float64
	listingBytes    float64
	gz              float64
	gzAll           float64
	gzBytes         float64
	gzBytesAll      float64
	namedReq        float64
	namedBytes      float64
}

func (s *Sample) analyse() *analysis {
	a := &analysis{
		endpointReq: newCounter(), endpointSize: newCounter(),
		useReq: newCounter(), useSize: newCounter(),
		limits: newCounter(), cache: newCounter(), outcomes: newCounter(),
		networkReq: newCounter(), networkSize: newCounter(),
		networkAddresses: map[string]addressSet{},
	}
	clients, orgs := map[string]*clientTally{}, map[string]*orgTally{}
	var orgOrder []*orgTally
	for i := range s.records {
		entry := &s.records[i]
		size := fmul(entry.weight, entry.bytes)

		client, ok := clients[entry.client]
		if !ok {
			client = &clientTally{tally: tally{name: entry.client, addresses: addressSet{}}, networks: newCounter(), does: newCounter()}
			clients[entry.client] = client
			a.clients = append(a.clients, client)
		}
		client.count(entry)
		client.networks.add(entry.network, entry.weight)
		if entry.isListing() {
			client.does.add(entry.use, entry.weight)
		} else {
			client.does.add(entry.endpoint, entry.weight)
		}

		// Organizations are merged without regard to case and shown under the first spelling seen.
		key := strings.ToLower(entry.who)
		org, ok := orgs[key]
		if !ok {
			org = &orgTally{tally: tally{name: entry.who, addresses: addressSet{}}}
			orgs[key] = org
			orgOrder = append(orgOrder, org)
		}
		org.byAgent = org.byAgent || entry.how == byUserAgent
		org.byOwner = org.byOwner || entry.how == byNetworkOwner
		org.count(entry)

		a.endpointReq.add(entry.endpoint, entry.weight)
		a.endpointSize.add(entry.endpoint, size)
		a.outcomes.add(entry.outcome, entry.weight)
		a.networkReq.add(entry.network, entry.weight)
		a.networkSize.add(entry.network, size)
		if _, ok := a.networkAddresses[entry.network]; !ok {
			a.networkAddresses[entry.network] = addressSet{}
		}
		a.networkAddresses[entry.network].add(entry.address)
		if entry.isListing() {
			a.useReq.add(entry.use, entry.weight)
			a.useSize.add(entry.use, size)
			a.limits.add(entry.limit, entry.weight)
			a.cache.add(entry.cache, entry.weight)
		}
		if entry.gzip != compressionUnknown {
			a.gzBytesAll += size
			if entry.gzip == compressionGzip {
				a.gzBytes += size
			}
		}
	}
	a.ranked = append([]*clientTally(nil), a.clients...)
	sort.SliceStable(a.ranked, func(i, j int) bool { return a.ranked[i].bytes > a.ranked[j].bytes })
	for _, org := range orgOrder {
		if org.named() {
			a.named = append(a.named, org)
		} else {
			a.unnamed = append(a.unnamed, org)
		}
	}
	sort.SliceStable(a.named, func(i, j int) bool { return a.named[i].bytes > a.named[j].bytes })
	sort.SliceStable(a.unnamed, func(i, j int) bool { return a.unnamed[i].bytes > a.unnamed[j].bytes })
	for _, client := range a.clients {
		a.gzAll += client.gzAll
		a.gz += client.gz
	}
	for _, org := range a.named {
		a.namedReq += org.req
		a.namedBytes += org.bytes
	}
	a.listingRequests, a.listingBytes = a.useReq.sum(), a.useSize.sum()
	return a
}

func (a *analysis) cacheHits() float64 {
	return a.cache.get("HIT") + a.cache.get("STALE") + a.cache.get("UPDATING")
}

// isMinorEndpoint reports whether an endpoint is under 0.1% of both requests and data.
func (s *Sample) isMinorEndpoint(a *analysis, name string) bool {
	return a.endpointReq.get(name) < 0.001*s.estRequests && a.endpointSize.get(name) < 0.001*s.estBytes
}

// limitLabel names a page size. The requested value is client-supplied text,
// so only its first few letters and digits are kept.
func limitLabel(limit string) string {
	if limit == defaultLimit {
		return limit
	}
	var out strings.Builder
	out.WriteString("limit=")
	kept := 0
	for _, r := range limit {
		if kept == 8 {
			break
		}
		if r < 0x80 && (r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			out.WriteRune(r)
		} else {
			out.WriteByte('?')
		}
		kept++
	}
	return out.String()
}

// pct formats a share the way the report shows it.
func pct(part, total float64) string {
	if total <= 0 {
		return "–"
	}
	return pctText(100.0 * part / total)
}

// pctText formats a percentage that has already been worked out.
func pctText(value float64) string {
	switch {
	case value == 0:
		return "0%"
	case value < 0.1:
		return "<0.1%"
	case value >= 99.5 && value < 100:
		// Avoid showing "100%" beside other non-zero shares.
		return strconv.FormatFloat(min(value, 99.9), 'f', 1, 64) + "%"
	case value < 10:
		return strconv.FormatFloat(value, 'f', 1, 64) + "%"
	}
	return strconv.FormatFloat(value, 'f', 0, 64) + "%"
}

// grouped formats a whole number with thousands separators.
func grouped(value float64) string {
	digits := strconv.FormatFloat(value, 'f', 0, 64)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var out strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	return sign + out.String()
}

func approximate(value float64) string {
	switch {
	case value >= 1e6:
		return strconv.FormatFloat(value/1e6, 'f', 2, 64) + "M"
	case value >= 1e4:
		return strconv.FormatFloat(value/1e3, 'f', 0, 64) + "K"
	}
	return grouped(value)
}

func markdownTable(headers []string, rows [][]string) string {
	rules := make([]string, len(headers))
	for i := range rules {
		rules[i] = "---"
	}
	lines := []string{"| " + strings.Join(headers, " | ") + " |", "| " + strings.Join(rules, " | ") + " |"}
	for _, row := range rows {
		lines = append(lines, "| "+strings.Join(row, " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

func (s *Sample) title() string {
	return fmt.Sprintf("# Registry usage, %s %d, %d (UTC day)", s.Start.Format("Mon Jan"), s.Start.Day(), s.Start.Year())
}

// Markdown renders the day as the report people read.
func (s *Sample) Markdown() string {
	if len(s.records) == 0 {
		return s.title() + "\n\nNo access records were found for this period.\n"
	}
	a := s.analyse()
	out := []string{s.title(), ""}
	out = append(out, s.summaryLines(a)...)
	out = append(out, s.howLines(a)...)
	out = append(out, s.whoLines(a)...)
	out = append(out, "_Totals are from the gateway's request counters; shares are from "+grouped(float64(len(s.records)))+
		" access records scaled to those totals. An organization is named from its user agent, or from the owner of its network "+
		`when the client is generic. User agents are self-reported, and "addresses seen" counts source addresses in the sample._`)
	if len(s.notes) > 0 {
		out = append(out, "", "**Caveats for this period:**", "")
		for _, note := range s.notes {
			out = append(out, "- "+note)
		}
	}
	return strings.Join(out, "\n") + "\n"
}

func (s *Sample) summaryLines(a *analysis) []string {
	lead := a.ranked[0].tally
	if len(a.named) > 0 {
		lead = a.named[0].tally
	}
	gib := s.data / gibibyte
	return []string{
		fmt.Sprintf("**%s requests, %s GiB sent** (about $%s at roughly $%s per GiB), from %d distinct user agents.",
			approximate(s.requests), strconv.FormatFloat(gib, 'f', 1, 64), grouped(gib*s.limits.DollarsPerGiB),
			strconv.FormatFloat(s.limits.DollarsPerGiB, 'f', 2, 64), len(a.clients)),
		"",
		fmt.Sprintf("- **%s** made %s of requests and received %s of the data; %s of its listing responses were compressed.",
			lead.name, pct(lead.req, s.estRequests), pct(lead.bytes, s.estBytes), pct(lead.gz, lead.gzAll)),
		fmt.Sprintf("- %d named organizations and products account for %s of requests and %s of the data. The rest cannot be tied to a name.",
			len(a.named), pct(a.namedReq, s.estRequests), pct(a.namedBytes, s.estBytes)),
		fmt.Sprintf("- Across all clients, %s of listing responses were compressed, carrying %s of listing data.",
			pct(a.gz, a.gzAll), pct(a.gzBytes, a.gzBytesAll)),
		"",
	}
}

func (s *Sample) howLines(a *analysis) []string {
	out := []string{"## How it is used", ""}
	var rows [][]string
	var minor []string
	for _, item := range a.endpointReq.mostCommon() {
		if s.isMinorEndpoint(a, item.key) {
			minor = append(minor, strings.ToLower(item.key)+" (about "+approximate(item.value)+" requests)")
			continue
		}
		rows = append(rows, []string{item.key, pct(item.value, s.estRequests), pct(a.endpointSize.get(item.key), s.estBytes)})
	}
	out = append(out, markdownTable([]string{"Endpoint", columnRequests, columnData}, rows), "")
	if len(minor) > 0 {
		out = append(out, "Under 0.1% each: "+strings.Join(minor, ", ")+".", "")
	}

	rows = nil
	for _, item := range a.useSize.mostCommon() {
		rows = append(rows, []string{item.key, pct(a.useReq.get(item.key), a.listingRequests), pct(item.value, a.listingBytes)})
	}
	out = append(out, "Listing requests, by what the client was doing:", "",
		markdownTable([]string{"Listing use", columnRequests, columnData}, rows), "")

	common, rest := a.commonLimits()
	sizes := make([]string, len(common))
	for i, item := range common {
		sizes[i] = limitLabel(item.key) + " " + pct(item.value, a.listingRequests)
	}
	pageSize := "- **Page size:** " + strings.Join(sizes, ", ")
	if rest > 0.0005*a.listingRequests {
		pageSize += ", other " + pct(rest, a.listingRequests)
	}
	responses := []string{}
	for _, item := range a.outcomes.mostCommon() {
		responses = append(responses, strings.ToLower(item.key)+" "+pct(item.value, s.estRequests))
	}
	return append(out,
		pageSize+".",
		"- **Gateway cache:** "+pct(a.cacheHits(), a.listingRequests)+" of listing requests were served from cache.",
		"- **Responses:** "+strings.Join(responses, ", ")+".",
		"")
}

// commonLimits returns the three most requested page sizes and the listing
// requests that used any other.
func (a *analysis) commonLimits() ([]entry, float64) {
	common := a.limits.mostCommon()
	if len(common) > 3 {
		common = common[:3]
	}
	used := 0.0
	for _, item := range common {
		used += item.value
	}
	return common, a.listingRequests - used
}

func (s *Sample) orgRow(label, how string, group []*orgTally) []string {
	var req, size, gz, gzAll float64
	sets := make([]addressSet, len(group))
	for i, org := range group {
		req += org.req
		size += org.bytes
		gz += org.gz
		gzAll += org.gzAll
		sets[i] = org.addresses
	}
	return []string{label, how, pct(req, s.estRequests), pct(size, s.estBytes), pct(gz, gzAll), strconv.Itoa(unionSize(sets))}
}

func (s *Sample) whoLines(a *analysis) []string {
	out := []string{"## Who uses it", "", "By organization or product:", ""}
	var rows [][]string
	var hidden []*orgTally
	for i, org := range a.named {
		if i < reportTopOrgs && (org.bytes >= 0.001*s.estBytes || org.req >= 0.005*s.estRequests) {
			rows = append(rows, s.orgRow(org.name, identifiedLabel(org.byAgent, org.byOwner), []*orgTally{org}))
		} else {
			hidden = append(hidden, org)
		}
	}
	if len(hidden) > 0 {
		rows = append(rows, s.orgRow(strconv.Itoa(len(hidden))+" other named", "", hidden))
	}
	for _, org := range a.unnamed {
		rows = append(rows, s.orgRow(org.name, "Not identifiable", []*orgTally{org}))
	}
	out = append(out,
		markdownTable([]string{"Organization or product", "Identified by", columnRequests, columnData, "Listings compressed", columnAddresses}, rows),
		"", "By client software, as named in the user agent:", "",
		markdownTable([]string{"Client", columnRequests, columnData, "Listings compressed", columnAddresses, "Main network", "Mostly does"}, s.clientRows(a)),
		"", "By network owner of the source address:", "",
		markdownTable([]string{"Network", columnRequests, columnData, columnAddresses}, s.networkRows(a)),
		"")
	return out
}

func (s *Sample) clientRows(a *analysis) [][]string {
	var rows [][]string
	listed := min(reportTopClients, len(a.ranked))
	for _, client := range a.ranked[:listed] {
		network := "Mixed"
		if main, ok := client.networks.top(); ok && client.req != 0 {
			if share := main.value / client.req; share >= 0.5 {
				network = main.key + " (" + strconv.FormatFloat(share*100, 'f', 0, 64) + "%)"
			}
		}
		does, _ := client.does.top()
		rows = append(rows, []string{client.name, pct(client.req, s.estRequests), pct(client.bytes, s.estBytes), pct(client.gz, client.gzAll),
			strconv.Itoa(len(client.addresses)), network, does.key})
	}
	others := a.ranked[listed:]
	if len(others) == 0 {
		return rows
	}
	var req, size, gz, gzAll float64
	sets := make([]addressSet, len(others))
	for i, client := range others {
		req += client.req
		size += client.bytes
		gz += client.gz
		gzAll += client.gzAll
		sets[i] = client.addresses
	}
	return append(rows, []string{strconv.Itoa(len(others)) + " other clients", pct(req, s.estRequests), pct(size, s.estBytes), pct(gz, gzAll),
		strconv.Itoa(unionSize(sets)), "", ""})
}

func (s *Sample) networkRows(a *analysis) [][]string {
	var rows [][]string
	leading := map[string]bool{}
	largest := a.networkSize.mostCommon()
	for _, item := range largest[:min(6, len(largest))] {
		if item.value >= 0.01*s.estBytes || a.networkReq.get(item.key) >= 0.01*s.estRequests {
			leading[item.key] = true
			rows = append(rows, []string{item.key, pct(a.networkReq.get(item.key), s.estRequests), pct(item.value, s.estBytes),
				strconv.Itoa(len(a.networkAddresses[item.key]))})
		}
	}
	if a.networkSize.len() <= len(leading) {
		return rows
	}
	var req, size float64
	var sets []addressSet
	for _, item := range a.networkReq.entries() {
		if !leading[item.key] {
			req += item.value
			sets = append(sets, a.networkAddresses[item.key])
		}
	}
	for _, item := range a.networkSize.entries() {
		if !leading[item.key] {
			size += item.value
		}
	}
	return append(rows, []string{strconv.Itoa(a.networkSize.len()-len(leading)) + " other networks", pct(req, s.estRequests),
		pct(size, s.estBytes), strconv.Itoa(unionSize(sets))})
}
