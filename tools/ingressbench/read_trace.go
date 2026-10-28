package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	v0ListingPath  = "/v0/servers"
	pageKind       = "page"
	searchKind     = "search"
	versionParam   = "version"
	v01ListingPath = "/v0.1/servers"
)

// ReadTrace contains only catalog references and relative request start times.
// Keep the source catalog private; results expose its hash, never its values.
type ReadTrace struct {
	DurationMS int64       `json:"duration_ms"`
	Requests   []TraceRead `json:"requests"`
	SHA256     string      `json:"-"`
}

type TraceRead struct {
	AtMS int64  `json:"at_ms"`
	Key  int    `json:"key"`
	Kind string `json:"kind"`
}

func loadReadTrace(filename string, keys []string, duration time.Duration, pageProfile, token string) (*ReadTrace, error) {
	t := &ReadTrace{}
	if filename == "" {
		return t, nil
	}
	if pageProfile != "" || token != "" {
		return nil, fmt.Errorf("read trace cannot combine with a page profile or publishing")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, t); err != nil {
		return nil, err
	}
	if t.DurationMS != duration.Milliseconds() || len(t.Requests) == 0 || len(t.Requests) > 3_000_000 {
		return nil, fmt.Errorf("trace duration/count outside bounds")
	}
	kinds := make([]string, len(keys))
	for i, key := range keys {
		kinds[i] = traceReadKind(key)
	}
	var previous int64 = -1
	perSecond := map[int64]int{}
	for _, entry := range t.Requests {
		if entry.AtMS < previous || entry.AtMS < 0 || entry.AtMS >= t.DurationMS || entry.Key < 0 || entry.Key >= len(keys) {
			return nil, fmt.Errorf("invalid trace time or catalog index")
		}
		if kinds[entry.Key] == "" || kinds[entry.Key] != entry.Kind {
			return nil, fmt.Errorf("trace must reference a supported public GET read")
		}
		perSecond[entry.AtMS/1000]++
		if perSecond[entry.AtMS/1000] > 10000 {
			return nil, fmt.Errorf("trace exceeds 10000 reads per second")
		}
		previous = entry.AtMS
	}
	t.SHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
	return t, nil
}

func traceReadKind(path string) string {
	u, err := url.Parse(path)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" {
		return ""
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return ""
	}
	for key, values := range q {
		if len(values) != 1 {
			return ""
		}
		switch key {
		case "cursor", "limit", versionParam, searchKind, "updated_since", "include_deleted":
		default:
			return ""
		}
	}
	if u.Path == v0ListingPath || u.Path == v01ListingPath {
		if q.Get(searchKind) != "" {
			return searchKind
		}
		if q.Get("updated_since") != "" {
			return "updated"
		}
		return pageKind
	}
	if (strings.HasPrefix(u.Path, "/v0/servers/") || strings.HasPrefix(u.Path, "/v0.1/servers/")) && strings.Contains(u.Path, "/versions") {
		return "detail"
	}
	return ""
}

func (t *ReadTrace) count(defaultCount int) int {
	if len(t.Requests) > 0 {
		return len(t.Requests)
	}
	return defaultCount
}
func (t *ReadTrace) offset(n, rate int) time.Duration {
	if len(t.Requests) > 0 {
		return time.Duration(t.Requests[n].AtMS) * time.Millisecond
	}
	return time.Duration(float64(n) / float64(rate) * float64(time.Second))
}
func (t *ReadTrace) read(n int, keys []string, path, kind string) (string, string) {
	if len(t.Requests) > 0 {
		e := t.Requests[n]
		return keys[e.Key], e.Kind
	}
	return path, kind
}
func (t *ReadTrace) describe(r *result) {
	if len(t.Requests) > 0 {
		r.ReadTraceSHA256 = t.SHA256
		r.ReadRate = -1
		r.SearchRate = -1
		r.UpdatedRate = -1
		r.HotPercent = -1
	}
}

func selectedRead(n, hot, rate, updated, search int, keys []string, profile *PageWorkload, trace *ReadTrace) (string, string) {
	if len(trace.Requests) > 0 {
		return trace.read(n, keys, "", "")
	}
	path, kind := readPath(n, hot, rate, updated, search, keys)
	if kind == pageKind && len(profile.paths) > 0 {
		path = profile.path(pageSequence(n, rate, updated+search))
	}
	return path, kind
}

func validCatalogKey(u *url.URL, err error, traced bool, key string) bool {
	if traced {
		return traceReadKind(key) != ""
	}
	return err == nil && !u.IsAbs() && u.Host == "" && (u.Path == v0ListingPath || u.Path == v01ListingPath)
}
