package usage

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

const (
	ownersFile = "ip2asn-v4.tsv.gz"
	typesFile  = "peeringdb-net.json"
	// Reference files are refreshed weekly; an older copy is used if a refresh fails.
	referenceMaxAge = 7 * 24 * time.Hour
	// Both files are tens of megabytes. The cap only stops a runaway response.
	referenceMaxBytes = 1 << 30
	unknownOwner      = "Unknown"
)

type knownNetwork struct {
	key, name, kind string
}

// knownNetworks names the large hosting providers, whose registered
// descriptions vary. The first key found in the description decides.
// Organizations are not listed here; see NetworkAlias.
var knownNetworks = []knownNetwork{
	{"AMAZON", "Amazon (AWS)", kindCloud}, {"MICROSOFT", "Microsoft (Azure)", kindCloud}, {"GOOGLE", "Google", kindCloud},
	{"CLOUDFLARE", "Cloudflare", kindCloud}, {"DIGITALOCEAN", "DigitalOcean", kindCloud}, {"HETZNER", "Hetzner", kindCloud},
	{"OVH", "OVH", kindCloud}, {"ORACLE", "Oracle", kindCloud}, {"ALIBABA", "Alibaba", kindCloud}, {"TENCENT", "Tencent", kindCloud},
	{"AKAMAI", "Akamai/Linode", kindCloud}, {"LINODE", "Akamai/Linode", kindCloud}, {"FASTLY", "Fastly", kindCloud},
	{"VERCEL", "Vercel", kindCloud}, {"HUAWEI", "Huawei Cloud", kindCloud}, {"CONTABO", "Contabo", kindCloud},
	{"VULTR", "Vultr", kindCloud}, {"CHOOPA", "Vultr", kindCloud}, {"SCALEWAY", "Scaleway", kindCloud},
	{"HOSTINGER", "Hostinger", kindCloud}, {"DATACAMP", "Datacamp/CDN77", kindCloud}, {"CDN77", "Datacamp/CDN77", kindCloud},
	{"LATITUDE.SH", "Latitude.sh", kindCloud}, {"LEASEWEB", "Leaseweb", kindCloud}, {"M247", "M247", kindCloud},
	{"IONOS", "IONOS", kindCloud}, {"RACKSPACE", "Rackspace", kindCloud},
}

// providerTypes are the self-declared network types that mean "carries other people's traffic".
var providerTypes = []string{"Cable/DSL/ISP", "NSP", "Network Services", "Route Server"}

// providerSubstrings and providerWholeWords mark a network as an internet
// provider when its description contains them.
var providerSubstrings = []string{
	"TELECOM", "TELEKOM", "TELEFONICA", "COMMUNICATION", "MOBILE", "BROADBAND", "CABLE", "VODAFONE", "WIRELESS", "FIBER",
	"FIBRE", "INTERNET", "AIRTEL", "UNICOM", "CHINANET", "COMCAST", "CHARTER", "VERIZON", "AT&T", "TELIA", "TELENOR",
	"TELSTRA", "TELUS", "ROGERS", "BELL CANADA", "ORANGE", "BOUYGUES", "KDDI", "SOFTBANK", "BACKBONE", "HYPEROPTIC",
	"SWISSCOM", "PROXIMUS", "ZIGGO", "VIRGIN MEDIA", "SILKNET", "OOREDOO",
}

var providerWholeWords = []string{"ISP", "DSL", "DIGI", "NTT", "SFR", "KPN", "JIO", "TELE2", "COX"}

// ipKey orders addresses the way Python orders int(ip_address(x)).
type ipKey struct {
	high, low uint64
}

func (k ipKey) less(other ipKey) bool {
	return k.high < other.high || (k.high == other.high && k.low < other.low)
}

func parseIPKey(address string) (ipKey, bool) {
	parsed, err := netip.ParseAddr(address)
	if err != nil {
		return ipKey{}, false
	}
	if parsed.Is4() {
		raw := parsed.As4()
		return ipKey{low: uint64(binary.BigEndian.Uint32(raw[:]))}, true
	}
	raw := parsed.As16()
	return ipKey{high: binary.BigEndian.Uint64(raw[:8]), low: binary.BigEndian.Uint64(raw[8:])}, true
}

type listedNetwork struct {
	name, kind string
}

// NetworkTable maps address ranges to the network that announces them, and
// networks to their self-declared type. Both come from public reference files.
type NetworkTable struct {
	starts       []ipKey
	ends         []ipKey
	numbers      []string
	descriptions []string
	listed       map[string]listedNetwork
	aliases      []NetworkAlias
}

// NetworkAlias gives one organization's networks a single name. A network
// whose registered description contains Key, without regard to case, is shown
// as Name and counted as that organization's own network.
type NetworkAlias struct {
	Key  string
	Name string
}

const (
	aliasKeyMinLength  = 3
	aliasNameMaxLength = 64
)

// ParseOrganizationAliases reads aliases written as "KEY=Name;KEY=Name".
// Pairs are separated by semicolons or line breaks, so that a name may
// contain a comma. Several keys may share one name.
func ParseOrganizationAliases(text string) ([]NetworkAlias, error) {
	upper := cases.Upper(language.Und)
	var aliases []NetworkAlias
	for _, pair := range strings.FieldsFunc(text, func(r rune) bool { return r == ';' || r == '\n' || r == '\r' }) {
		if strings.TrimSpace(pair) == "" {
			continue
		}
		key, name, found := strings.Cut(pair, "=")
		key, name = strings.TrimSpace(key), strings.TrimSpace(name)
		switch {
		case !found:
			return nil, errors.New("an alias is written KEY=Name")
		case utf8.RuneCountInString(key) < aliasKeyMinLength:
			// A shorter key would match unrelated networks.
			return nil, fmt.Errorf("an alias key needs at least %d characters", aliasKeyMinLength)
		case name == "" || utf8.RuneCountInString(name) > aliasNameMaxLength:
			return nil, fmt.Errorf("an alias name needs 1 to %d characters", aliasNameMaxLength)
		case strings.ContainsFunc(name, func(r rune) bool { return r == '|' || r == '=' || unicode.IsControl(r) }):
			return nil, errors.New("an alias name cannot contain |, = or control characters")
		}
		aliases = append(aliases, NetworkAlias{Key: upper.String(key), Name: name})
	}
	return aliases, nil
}

// OwnersLoaded reports whether the address-range table is available. Without
// it every network shows as Unknown.
func (t *NetworkTable) OwnersLoaded() bool {
	return t != nil && len(t.starts) > 0
}

// TypesLoaded reports whether the network-type list is available. Without it
// fewer networks are recognised as internet providers.
func (t *NetworkTable) TypesLoaded() bool {
	return t != nil && len(t.listed) > 0
}

// NetworkFiles says where the reference files come from and where they are kept.
type NetworkFiles struct {
	CacheDir  string
	OwnersURL string
	TypesURL  string
	UserAgent string
	// Aliases name organizations' own networks. They are checked before the
	// built-in hosting providers, in the order given.
	Aliases []NetworkAlias
	Client  *http.Client
	Logf    func(format string, args ...any)
}

// LoadNetworkTable reads the reference files, refreshing the cached copies
// when they are more than a week old. It never fails: whatever could not be
// loaded is simply absent from the table.
func LoadNetworkTable(ctx context.Context, files NetworkFiles) *NetworkTable {
	table := &NetworkTable{aliases: files.Aliases}
	files.load(ctx, ownersFile, files.OwnersURL, func(path string) error {
		var read NetworkTable
		if err := read.readOwnersFile(path); err != nil {
			return err
		}
		if !read.OwnersLoaded() {
			return errors.New("it holds no address ranges")
		}
		table.starts, table.ends, table.numbers, table.descriptions = read.starts, read.ends, read.numbers, read.descriptions
		return nil
	})
	files.load(ctx, typesFile, files.TypesURL, func(path string) error {
		var read NetworkTable
		if err := read.readTypesFile(path); err != nil {
			return err
		}
		if !read.TypesLoaded() {
			return errors.New("it lists no networks")
		}
		table.listed = read.listed
		return nil
	})
	return table
}

func (f NetworkFiles) logf(format string, args ...any) {
	if f.Logf != nil {
		f.Logf(format, args...)
	}
}

// load reads one reference file with the given function. A copy older than a
// week is downloaded again first, but the download only replaces the cached
// copy once it has been read successfully, so a bad response never costs the
// copy that worked. A cached copy that cannot be read is removed, so that the
// next run fetches a new one instead of trusting it for a week.
func (f NetworkFiles) load(ctx context.Context, name, source string, read func(path string) error) {
	path := filepath.Join(f.CacheDir, name)
	info, err := os.Stat(path)
	cached := err == nil
	if !cached || time.Since(info.ModTime()) > referenceMaxAge {
		fetched := path + ".tmp"
		err := f.download(ctx, source, fetched)
		if err == nil {
			if err = read(fetched); err == nil {
				if err = os.Rename(fetched, path); err == nil {
					return
				}
			}
		}
		_ = os.Remove(fetched)
		f.logf("could not refresh %s: %v", name, err)
	}
	if !cached {
		return
	}
	if err := read(path); err != nil {
		f.logf("cached %s could not be read and was removed: %v", name, err)
		_ = os.Remove(path)
	}
}

func (f NetworkFiles) download(ctx context.Context, source, path string) error {
	if source == "" {
		return errors.New("no source configured")
	}
	if err := os.MkdirAll(f.CacheDir, 0o750); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	// These sites reject requests without a descriptive user agent.
	request.Header.Set("User-Agent", f.UserAgent)
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, io.LimitReader(response.Body, referenceMaxBytes))
	if closeErr := file.Close(); copyErr == nil {
		copyErr = closeErr
	}
	return copyErr
}

func (t *NetworkTable) readOwnersFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(reader, referenceMaxBytes))
	if err != nil {
		return err
	}
	return t.parseOwners(data)
}

// parseOwners reads tab-separated rows of range start, range end, network
// number, country and description. One malformed row rejects the whole table.
func (t *NetworkTable) parseOwners(data []byte) error {
	text := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(pyDecodeUTF8(data))
	lines := strings.Split(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	starts, ends := make([]ipKey, 0, len(lines)), make([]ipKey, 0, len(lines))
	numbers, descriptions := make([]string, 0, len(lines)), make([]string, 0, len(lines))
	shared := map[string]string{}
	share := func(value string) string {
		if kept, ok := shared[value]; ok {
			return kept
		}
		value = strings.Clone(value)
		shared[value] = value
		return value
	}
	for index, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			return fmt.Errorf("row %d has %d fields, not 5", index+1, len(fields))
		}
		first, okFirst := parseIPKey(fields[0])
		last, okLast := parseIPKey(fields[1])
		if !okFirst || !okLast {
			return fmt.Errorf("row %d has an invalid address range", index+1)
		}
		starts, ends = append(starts, first), append(ends, last)
		numbers, descriptions = append(numbers, share(fields[2])), append(descriptions, share(fields[4]))
	}
	t.starts, t.ends, t.numbers, t.descriptions = starts, ends, numbers, descriptions
	return nil
}

func (t *NetworkTable) readTypesFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return t.parseTypes(data)
}

// parseTypes reads the network list: {"data": [{"asn": 64496, "name": "...", "info_type": "..."}]}.
func (t *NetworkTable) parseTypes(data []byte) error {
	var document struct {
		Data []map[string]any `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return err
	}
	listed := make(map[string]listedNetwork, len(document.Data))
	for _, item := range document.Data {
		number, ok := item["asn"]
		if !ok {
			return errors.New("an entry has no network number")
		}
		listed[pyStr(number)] = listedNetwork{name: textOrEmpty(item["name"]), kind: textOrEmpty(item["info_type"])}
	}
	t.listed = listed
	return nil
}

func textOrEmpty(value any) string {
	if !pyTruthy(value) {
		return ""
	}
	return pyStr(value)
}

type owner struct {
	name, kind string
}

// ownerLookup resolves addresses for one day. It remembers the networks it
// has seen, in order, because that order decides which type a shared name gets.
type ownerLookup struct {
	table *NetworkTable
	seen  map[string]owner
	order []string
	upper cases.Caser
}

func newOwnerLookup(table *NetworkTable) *ownerLookup {
	if table == nil {
		table = &NetworkTable{}
	}
	return &ownerLookup{table: table, seen: map[string]owner{}, upper: cases.Upper(language.Und)}
}

// lookup returns the owner of the network a source address belongs to.
func (l *ownerLookup) lookup(address string) owner {
	unknown := owner{unknownOwner, kindUnknown}
	table := l.table
	if len(table.starts) == 0 {
		return unknown
	}
	key, ok := parseIPKey(address)
	if !ok {
		return unknown
	}
	index := sort.Search(len(table.starts), func(i int) bool { return key.less(table.starts[i]) }) - 1
	if index < 0 || table.ends[index].less(key) || table.numbers[index] == "0" {
		return unknown
	}
	number := table.numbers[index]
	found, ok := l.seen[number]
	if !ok {
		found = l.describe(number, table.descriptions[index])
		l.seen[number] = found
		l.order = append(l.order, number)
	}
	return found
}

// kinds maps each network name seen so far to its type.
func (l *ownerLookup) kinds() map[string]string {
	out := make(map[string]string, len(l.order))
	for _, number := range l.order {
		found := l.seen[number]
		out[found.name] = found.kind
	}
	return out
}

func (l *ownerLookup) describe(number, description string) owner {
	listed := l.table.listed[number]
	text := l.upper.String(description + " " + listed.name)
	for _, alias := range l.table.aliases {
		if strings.Contains(text, alias.Key) {
			return owner{alias.Name, kindOrganization}
		}
	}
	for _, known := range knownNetworks {
		if strings.Contains(text, known.key) {
			return owner{known.name, known.kind}
		}
	}
	name := stripNumberPrefix(description)
	if _, after, found := strings.Cut(description, " - "); found {
		name = after
	}
	if handle, remainder, _ := strings.Cut(name, " "); remainder != "" && strings.ContainsAny(handle, "-_") {
		name = remainder
	}
	name = tidyName(name)
	if listedName := tidyName(listed.name); listedName != "" {
		name = listedName
	}
	if name == "" {
		name = unknownOwner
	}
	kind := kindOrganization
	if isProviderType(listed.kind) || hasProviderWords(text) {
		kind = kindProvider
	}
	return owner{name, kind}
}

// stripNumberPrefix removes a leading "AS64496 " from a description.
func stripNumberPrefix(description string) string {
	rest, found := strings.CutPrefix(description, "AS")
	if !found {
		return description
	}
	afterDigits := strings.TrimLeftFunc(rest, unicode.IsDigit)
	afterSpace := strings.TrimLeftFunc(afterDigits, pySpace)
	if len(afterDigits) == len(rest) || len(afterSpace) == len(afterDigits) {
		return description
	}
	return afterSpace
}

// tidyName reduces a registered network name to plain characters, at most 32.
func tidyName(name string) string {
	var out strings.Builder
	for _, r := range norm.NFKD.String(strings.ReplaceAll(name, "_", " ")) {
		if r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" .,&()/-", r)) {
			out.WriteRune(r)
		}
	}
	tidy := strings.Trim(out.String(), " ")
	if len(tidy) > 32 {
		tidy = tidy[:32]
	}
	return strings.Trim(tidy, " ")
}

func isProviderType(kind string) bool {
	for _, known := range providerTypes {
		if kind == known {
			return true
		}
	}
	return false
}

func hasProviderWords(text string) bool {
	for _, word := range providerSubstrings {
		if strings.Contains(text, word) {
			return true
		}
	}
	for _, word := range providerWholeWords {
		if containsWholeWord(text, word) {
			return true
		}
	}
	return false
}

func containsWholeWord(text, word string) bool {
	for offset := 0; ; {
		found := strings.Index(text[offset:], word)
		if found < 0 {
			return false
		}
		start := offset + found
		end := start + len(word)
		before, _ := lastRune(text[:start])
		after, _ := firstRune(text[end:])
		if !pyWord(before) && !pyWord(after) {
			return true
		}
		offset = start + 1
	}
}

func firstRune(text string) (rune, bool) {
	if text == "" {
		return ' ', false
	}
	r, _ := utf8.DecodeRuneInString(text)
	return r, true
}

func lastRune(text string) (rune, bool) {
	if text == "" {
		return ' ', false
	}
	r, _ := utf8.DecodeLastRuneInString(text)
	return r, true
}
