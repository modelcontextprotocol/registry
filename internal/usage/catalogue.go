package usage

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// CatalogueServer is one published server and the type of its latest version.
type CatalogueServer struct {
	Name string
	Type string
}

// Catalogue is the set of published server names. A requested name is only
// ever reported if it is in here; anything else a client asked for is counted
// without being kept.
type Catalogue struct {
	types     map[string]string
	spellings map[string]string
	lower     cases.Caser
}

// NewCatalogue builds a catalogue. When two names differ only by case, the
// later one answers requests that match neither exactly.
func NewCatalogue(servers []CatalogueServer) *Catalogue {
	catalogue := &Catalogue{
		types:     make(map[string]string, len(servers)),
		spellings: make(map[string]string, len(servers)),
		lower:     cases.Lower(language.Und),
	}
	for _, server := range servers {
		if _, known := catalogue.types[server.Name]; !known {
			catalogue.types[server.Name] = server.Type
		}
	}
	for _, server := range servers {
		catalogue.spellings[catalogue.lower.String(server.Name)] = server.Name
	}
	return catalogue
}

// Len is the number of published servers.
func (c *Catalogue) Len() int {
	if c == nil {
		return 0
	}
	return len(c.types)
}

// resolve returns the published name a request refers to, or notListed.
func (c *Catalogue) resolve(requested string) string {
	if _, ok := c.types[requested]; ok {
		return requested
	}
	if name, ok := c.spellings[c.lower.String(requested)]; ok {
		return name
	}
	return notListed
}

func (c *Catalogue) typeOf(name string) string {
	return c.types[name]
}

func namespaceOf(name string) string {
	namespace, _, _ := strings.Cut(name, "/")
	return namespace
}

func namespaceKind(namespace string) string {
	if strings.HasPrefix(namespace, "io.github.") {
		return "GitHub account"
	}
	return "Domain"
}

var packageNames = map[string]string{
	"npm": "npm", "pypi": "PyPI", "oci": "Docker (OCI)", "nuget": "NuGet", "mcpb": "MCPB", "cargo": "Cargo",
}

var remoteNames = map[string]string{"streamable-http": "Remote (HTTP)", "sse": "Remote (SSE)"}

func packageName(kind string) string {
	if name, ok := packageNames[kind]; ok {
		return name
	}
	return kind
}

// typeLabel gives a version one label, so that stacked charts add up to the
// number of versions. packages and remotes are the distinct types it offers.
func typeLabel(packages, remotes []string) string {
	switch {
	case len(packages) > 0 && len(remotes) > 0:
		return "Remote and package"
	case len(remotes) > 0:
		return "Remote only"
	case len(packages) == 1:
		return packageName(packages[0])
	case len(packages) > 1:
		return "Several package types"
	}
	return "No package or remote"
}

// offerLabels lists every package type and remote a version has.
func offerLabels(packages, remotes []string) []string {
	out := make([]string, 0, len(packages)+len(remotes))
	for _, kind := range packages {
		out = append(out, packageName(kind))
	}
	for _, kind := range remotes {
		if name, ok := remoteNames[kind]; ok {
			out = append(out, name)
		} else {
			out = append(out, "Remote ("+kind+")")
		}
	}
	return out
}
