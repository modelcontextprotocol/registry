package usage

import (
	"regexp"
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
var genericClient = regexp.MustCompile(`(?i)^(?:\(no user agent\)|\(not recorded\)|web browser|python.*|node.*|curl|libcurl|wget|` +
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
		match := compatibleAgent.FindStringSubmatch(text)
		if match == nil {
			return clientBrowser
		}
		name = match[1]
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
