package usage

import (
	"regexp"
	"strconv"
	"strings"
)

// Labels shared with the dashboards. Changing one changes what is stored.
const (
	kindCloud        = "cloud hosting"
	kindProvider     = "internet provider"
	kindOrganization = "organization"
	kindUnknown      = "unknown"

	unnamedCloud = "Unnamed clients on cloud hosting"
	unnamedOther = "Unnamed clients on internet providers"

	byUserAgent    = "User agent"
	byNetworkOwner = "Network owner"

	clientNotRecorded = "(not recorded)"
	clientNoAgent     = "(no user agent)"
	clientBrowser     = "Web browser"

	// What a client is called when the product name in its user agent would
	// identify a person or a host instead of a product.
	clientAddress    = "(address withheld)"
	clientEmail      = "(email withheld)"
	clientHost       = "(host name withheld)"
	clientIdentifier = "(identifier withheld)"

	endpointDocs     = "Docs and home page"
	endpointUnknown  = "Unknown paths"
	endpointVersion  = "Get one server version"
	endpointVersions = "List one server's versions"
	endpointOther    = "Other server calls"
	listingPrefix    = "List servers"

	outcomeNotFound = "Not found (404)"
	outcomeOther    = "Other"

	notListed = "Names not in the catalogue"
)

// genericClient matches user agents that name an HTTP library, runtime or
// browser rather than a product.
var genericClient = regexp.MustCompile(`(?i)^(?:\(address withheld\)|\(email withheld\)|\(host name withheld\)|\(identifier withheld\)|` +
	`\(no user agent\)|\(not recorded\)|web browser|python.*|node.*|curl|libcurl|wget|` +
	`go-http-client|java.*|okhttp.*|axios|undici|ruby|rust.*|reqwest|bun|deno|php|guzzlehttp|apache-httpclient|dart|` +
	`libwww-perl|aiohttp|httpx|httpie|got|fetch|postmanruntime|insomnia|powershell|restsharp|unirest|k6|hackney|faraday|` +
	`typhoeus|dotnet|requests|urllib.*|electron|kotlin.*|ktor.*|swift.*|cfnetwork|alamofire|scrapy|colly|lua-resty-http|` +
	`test|.*http-?client.*)$`)

// The whitespace class is Python's \s, which is wider than Go's.
var compatibleAgent = regexp.MustCompile(`compatible;[\t\n\v\f\r \x{1c}-\x{1f}\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]*([A-Za-z0-9._-]+)`)

// serverLookup matches a read of one server: its version list, or one version.
var serverLookup = regexp.MustCompile(`^/v0(?:\.1)?/servers/(.+?)/versions(?:/[^/]+)?/?$`)

func isRead(method string) bool {
	return method == "GET" || method == "HEAD"
}

func isClientNameChar(r rune) bool {
	return r < 0x80 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+-", r))
}

// clientName reduces a user agent to the product it names. A nil agent means
// the access record had no user agent field at all.
func clientName(agent *string) string {
	if agent == nil {
		return clientNotRecorded
	}
	text := pyStrip(*agent)
	if text == "" {
		return clientNoAgent
	}
	name, _, _ := strings.Cut(text, " ")
	name, _, _ = strings.Cut(name, "(")
	name, _, _ = strings.Cut(name, "/")
	if name == "Mozilla" {
		match := compatibleAgent.FindStringSubmatchIndex(text)
		if match == nil {
			return clientBrowser
		}
		name = text[match[2]:match[3]]
		// The name stops at the first unusual character. What follows it may be
		// the rest of an address, so the whole word is judged, not only the name.
		word := text[match[2]:]
		if end := strings.IndexFunc(word, func(r rune) bool { return pySpace(r) || strings.ContainsRune(";)/", r) }); end >= 0 {
			word = word[:end]
		}
		if label := withheld(word); label != "" {
			return label
		}
	}
	if label := withheld(name); label != "" {
		return label
	}
	// A user agent is client-supplied text: keep only harmless characters.
	var out strings.Builder
	kept := 0
	for _, r := range name {
		if kept == 28 {
			break
		}
		if isClientNameChar(r) {
			out.WriteRune(r)
		} else {
			out.WriteByte('?')
		}
		kept++
	}
	if out.Len() == 0 {
		return clientNoAgent
	}
	return out.String()
}

var (
	hardwareToken   = regexp.MustCompile(`(?:^|[^0-9A-Za-z])(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}(?:[^0-9A-Za-z]|\z)`)
	identifierToken = regexp.MustCompile(`[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}|[0-9A-Fa-f]{16,}`)
	numericRun      = regexp.MustCompile(`[0-9._-]+`)
	numericBreak    = regexp.MustCompile(`[._-]`)
	hexRun          = regexp.MustCompile(`[0-9A-Fa-f:.]+`)
	versionOnly     = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+)*(?:[-+][0-9A-Za-z.-]*)?$`)
)

// privateSuffixes end host names that only resolve inside someone's network.
var privateSuffixes = map[string]bool{
	"local": true, "localdomain": true, "localhost": true, "internal": true, "intranet": true, "lan": true, "corp": true,
	"home": true, "private": true, "arpa": true,
}

// withheld decides whether the product name taken from a user agent is
// something else: an address, an email address, a host name inside a private
// network, or a long identifier. Such a value says who or where the client is,
// so a fixed label is kept in its place. It returns "" for an ordinary name.
//
// The check errs towards withholding. A four-part version number, for one,
// cannot be told from an IPv4 address and is withheld too.
func withheld(token string) string {
	switch {
	case isMailbox(token):
		return clientEmail
	case hasIPv4(token) || hasIPv6(token) || hardwareToken.MatchString(token):
		return clientAddress
	case isPrivateHost(token):
		return clientHost
	case identifierToken.MatchString(token):
		return clientIdentifier
	}
	return ""
}

// isMailbox reports whether the token reads as someone at somewhere:
// jane@example.org, operator@buildhost, or the handle @jane@example.org. A
// package name with its version, tool@1.2.3, and a scope, @example/tool, are not.
func isMailbox(token string) bool {
	token = strings.TrimLeft(token, "@")
	at := strings.IndexByte(token, '@')
	return at > 0 && at+1 < len(token) && !versionOnly.MatchString(token[at+1:])
}

// hasIPv4 looks for four numbers of at most 255 in a row, separated by dots,
// hyphens or underscores: 192.0.2.7, and also the ip-192-0-2-7 of host names.
func hasIPv4(token string) bool {
	for _, run := range numericRun.FindAllString(token, -1) {
		parts := numericBreak.Split(run, -1)
		for first := 0; first+4 <= len(parts); first++ {
			if isOctet(parts[first]) && isOctet(parts[first+1]) && isOctet(parts[first+2]) && isOctet(parts[first+3]) {
				return true
			}
		}
	}
	return false
}

func isOctet(text string) bool {
	if len(text) < 1 || len(text) > 3 {
		return false
	}
	value, err := strconv.Atoi(text)
	return err == nil && value <= 255
}

// hasIPv6 looks for an IPv6 address anywhere in the token, as in
// [2001:db8::7]:443 or host:2001:db8::7. Rather than find where an address
// starts and ends inside a longer word, it takes any stretch of hexadecimal
// digits and colons that has a digit and either "::" or three colons. The
// digit keeps a name such as Some::Module from being taken for an address.
func hasIPv6(token string) bool {
	for _, run := range hexRun.FindAllString(token, -1) {
		if strings.ContainsAny(run, "0123456789") && (strings.Contains(run, "::") || strings.Count(run, ":") >= 3) {
			return true
		}
	}
	return false
}

func isPrivateHost(token string) bool {
	host := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, token)
	host, _, _ = strings.Cut(host, ":")
	host = strings.TrimRight(host, ".")
	if host == "localhost" {
		return true
	}
	dot := strings.LastIndexByte(host, '.')
	return dot > 0 && privateSuffixes[host[dot+1:]]
}

// organization returns the name traffic is attributed to and how it was
// identified. The second value is empty when no one can be named.
func organization(client, network, networkKind string) (name, identifiedBy string) {
	if !genericClient.MatchString(client) {
		return client, byUserAgent
	}
	if networkKind == kindOrganization {
		return network, byNetworkOwner
	}
	if networkKind == kindCloud {
		return unnamedCloud, ""
	}
	return unnamedOther, ""
}

func endpoint(method, path string) string {
	version := "v0.1"
	rest, found := strings.CutPrefix(path, "/v0.1/")
	if !found {
		version = "v0"
		rest, found = strings.CutPrefix(path, "/v0/")
	}
	if !found {
		if path == "/" || path == "/openapi.yaml" || strings.HasPrefix(path, "/docs") {
			return endpointDocs
		}
		return endpointUnknown
	}
	switch {
	case rest == "servers":
		if isRead(method) {
			return listingPrefix + " (" + version + ")"
		}
		return endpointUnknown
	case strings.HasPrefix(rest, "servers/"):
		return serverEndpoint(method, rest)
	case strings.HasPrefix(rest, "publish"):
		return "Publish"
	case strings.HasPrefix(rest, "auth/"):
		return "Sign in"
	case rest == "health" || rest == "ping" || rest == "version":
		return "Health and version checks"
	}
	return endpointUnknown
}

func serverEndpoint(method, rest string) string {
	if !isRead(method) {
		return endpointOther
	}
	if i := strings.LastIndexByte(rest, '/'); i >= 0 && i+1 < len(rest) && strings.HasSuffix(rest[:i], "/versions") {
		return endpointVersion
	}
	if strings.HasSuffix(rest, "/versions") {
		return endpointVersions
	}
	return endpointOther
}

func listingUse(query map[string][]string) string {
	if _, ok := query["search"]; ok {
		return "Search by name"
	}
	if _, ok := query["updated_since"]; ok {
		return "Incremental sync"
	}
	if _, ok := query["cursor"]; ok {
		if version := query["version"]; len(version) == 1 && version[0] == "latest" {
			return "Paging through latest versions"
		}
		return "Paging through all versions"
	}
	return "First page only"
}

func outcome(status string) string {
	switch status {
	case "404":
		return outcomeNotFound
	case "429":
		return "Rate-limited (429)"
	}
	if status == "" {
		return outcomeOther
	}
	switch status[0] {
	case '2':
		return "Success"
	case '3':
		return "Redirect or not modified"
	case '4':
		return "Other client error"
	case '5':
		return "Server error"
	}
	return outcomeOther
}

// requestedServer returns the server name a read asked for, or false when the
// request is not a read of one server.
func requestedServer(method, path string) (string, bool) {
	if !isRead(method) {
		return "", false
	}
	match := serverLookup.FindStringSubmatch(path)
	if match == nil {
		return "", false
	}
	return pyUnquote(match[1]), true
}
