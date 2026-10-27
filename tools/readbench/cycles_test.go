package main

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	r := crawl(context.Background(), server.Client(), server.URL, 0, 6, &crawlPacer{step: time.Nanosecond}, func(observation) {})
	if !r.Complete || r.Pages != 2 || r.Entries != 2 || calls != 2 {
		t.Fatalf("invalid walk: %+v calls=%d", r, calls)
	}
}

func TestCrawlRejectsRepeatedCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"servers":[{"server":{"name":"com.example/a","version":"1.0.0"}}],"metadata":{"count":1,"nextCursor":"com.example/a:1.0.0"}}`))
	}))
	defer server.Close()
	r := crawl(context.Background(), server.Client(), server.URL, 0, 0, &crawlPacer{step: time.Nanosecond}, func(observation) {})
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
