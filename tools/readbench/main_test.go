package main

import (
	"net/url"
	"testing"
	"time"
)

func TestLocalGuards(t *testing.T) {
	for _, raw := range []string{"https://registry.modelcontextprotocol.io", "http://10.36.0.17:8080", "http://localhost:8080", "http://127.0.0.1:8080?target=prod"} {
		if localURL(raw) == nil {
			t.Errorf("accepted nonliteral/nonlocal target %s", raw)
		}
	}
	if err := localDatabase("postgres://mcpregistry@127.0.0.1:5432/registry_bench"); err != nil {
		t.Fatal(err)
	}
	if localDatabase("postgres://mcpregistry@127.0.0.1:5432/app") == nil {
		t.Fatal("accepted non-benchmark database")
	}
}

func TestWorkloadMixAndUniqueWatermarks(t *testing.T) {
	counts := map[string]int{}
	updated := map[string]bool{}
	for i := range 100 {
		raw, shape := requestPath("mixed", i, 185001)
		counts[shape]++
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if u.Query().Get("limit") != "100" {
			t.Fatal("unexpected page size")
		}
		if shape == "updated" {
			stamp := u.Query().Get("updated_since")
			if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
				t.Fatal(err)
			}
			updated[stamp] = true
		}
	}
	if counts["page"] != 55 || counts["updated"] != 43 || counts["search"] != 2 || len(updated) != 43 {
		t.Fatalf("incorrect mix: %v, timestamps=%d", counts, len(updated))
	}
}

func TestSummaryExcludesErrorsFromGoodput(t *testing.T) {
	c := loadConfig{duration: time.Second, rate: 3, scenario: "page"}
	end := time.Now()
	obs := []observation{{kind: "page", status: 200, elapsed: 10, completed: end.Add(-time.Millisecond)}, {kind: "page", status: 503, elapsed: 1, completed: end}, {kind: "publish", status: 200, elapsed: 20, completed: end}}
	r := summarize(c, obs, end, 2*time.Second, 3, 1)
	if r.Completed != 1 || r.Goodput != 0.5 || r.Latency["all_reads"].Count != 1 || r.Dropped != 1 {
		t.Fatalf("incorrect result: %+v", r)
	}
}

func TestScheduleDoesNotSerializePublishes(t *testing.T) {
	started := make(chan job, 3)
	release := make(chan struct{})
	defer close(release)
	go func() {
		runSchedule(time.Now(), 30*time.Millisecond, 100, 3, true, func(j job) {
			started <- j
			<-release
		})
	}()
	for range 3 {
		select {
		case j := <-started:
			if !j.publish {
				t.Fatal("publish scheduler dispatched a read")
			}
		case <-time.After(time.Second):
			t.Fatal("later arrivals waited for earlier responses")
		}
	}
}
