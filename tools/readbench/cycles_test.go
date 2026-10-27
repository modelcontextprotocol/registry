package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCrawlFollowsCursorAndPreservesLatest(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("version") != "latest" {
			t.Error("lost latest filter")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "" {
			_, _ = w.Write([]byte(`{"servers":[{"server":{"name":"com.example/a","version":"1.0.2"}}],"metadata":{"count":1,"nextCursor":"com.example/a:1.0.2"}}`))
		} else {
			if r.URL.Query().Get("cursor") != "com.example/a:1.0.2" {
				t.Error("incorrect cursor")
			}
			_, _ = w.Write([]byte(`{"servers":[{"server":{"name":"com.example/a/child","version":"1.0.2"}}],"metadata":{"count":1}}`))
		}
	}))
	defer server.Close()
	r := crawl(context.Background(), server.Client(), server.URL, 0, 6, &crawlPacer{step: time.Nanosecond}, false, func(observation) {})
	if !r.Complete || r.Pages != 2 || r.Entries != 2 || calls != 2 {
		t.Fatalf("invalid walk: %+v calls=%d", r, calls)
	}
}

func TestCrawlRejectsRepeatedCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"servers":[{"server":{"name":"com.example/a","version":"1.0.0"}}],"metadata":{"count":1,"nextCursor":"com.example/a:1.0.0"}}`))
	}))
	defer server.Close()
	r := crawl(context.Background(), server.Client(), server.URL, 0, 0, &crawlPacer{step: time.Nanosecond}, false, func(observation) {})
	if r.Complete || r.Error == "" || r.Pages != 2 {
		t.Fatalf("duplicate walk not rejected: %+v", r)
	}
}

func TestCycleSummarySeparatesPolicyRejection(t *testing.T) {
	start := time.Now()
	c := cycleConfig{load: loadConfig{duration: time.Second, publishRate: 1}}
	r := summarizeCycles(c, start, []observation{
		{kind: "crawl", status: 200, completed: start},
		{kind: scenarioShort, status: 422, completed: start},
		{kind: scenarioShort, status: 200, completed: start},
		{kind: scenarioObserved, status: 500, completed: start},
	}, nil, nil)
	if r.UsefulReadSuccesses != 1 || r.ExpectedShortRejections != 1 || r.UnexpectedFailures != 1 {
		t.Fatalf("rejected work credited as useful throughput: %+v", r)
	}
}

func TestDiverseCrawlPreservesFiltersAndDefaultPageSize(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v0/servers" || r.URL.Query().Get("limit") != "" {
			t.Errorf("unexpected diverse request: %s", r.URL)
		}
		if calls == 1 {
			_, _ = w.Write([]byte(`{"servers":[{"server":{"name":"com.example/a","version":"1.0.0"}}],"metadata":{"count":1,"nextCursor":"com.example/a:1.0.0"}}`))
		} else {
			if r.URL.Query().Get("cursor") != "com.example/a:1.0.0" {
				t.Error("lost cursor")
			}
			_, _ = w.Write([]byte(`{"servers":[],"metadata":{"count":0}}`))
		}
	}))
	defer server.Close()
	r := crawl(context.Background(), server.Client(), server.URL, 0, 0, &crawlPacer{step: time.Nanosecond}, true, func(observation) {})
	if !r.Complete || calls != 2 {
		t.Fatalf("invalid diverse walk: %+v", r)
	}
}

func TestDiverseCacheKeysPreserveQueryMeaning(t *testing.T) {
	q := url.Values{"limit": {"50"}, "cursor": {"io.example/a:1.0.0"}, "version": {"latest"}}
	a, b := crawlPath(q, 0, true), crawlPath(q, 4, true)
	if a == b {
		t.Fatal("parameter order did not vary")
	}
	for _, path := range []string{a, b} {
		u, err := url.Parse(path)
		if err != nil || u.Query().Encode() != q.Encode() {
			t.Fatalf("query meaning changed: %s", path)
		}
	}
	for i := 0; i < 64; i++ {
		path, kind := requestPath(scenarioDiverse, i, 185001)
		u, _ := url.Parse(path)
		if len(u.Query().Get("search")) < 3 || !strings.HasPrefix(kind, scenarioDiverse+"/") {
			t.Fatalf("invalid diverse search %s %s", path, kind)
		}
	}
}

func TestDiverseSearchCategoriesReachBothReplicas(t *testing.T) {
	c := loadConfig{scenario: scenarioDiverse, targets: []string{"http://127.0.0.1:18080", "http://127.0.0.1:18081"}}
	seen := map[string]map[string]int{}
	for n := 0; n < 320; n++ {
		_, kind := requestPath(scenarioDiverse, n, 185001)
		if seen[kind] == nil {
			seen[kind] = map[string]int{}
		}
		seen[kind][requestTarget(c, job{number: n})]++
	}
	for kind, counts := range seen {
		if counts[c.targets[0]] == 0 || counts[c.targets[0]] != counts[c.targets[1]] {
			t.Errorf("imbalanced %s: %v", kind, counts)
		}
	}
}
