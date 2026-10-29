package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestMeasuredPageMixIgnoresCatalogOrdering(t *testing.T) {
	keys := []string{"/v0/servers?limit=50&cursor=a", "/v0/servers?limit=50&cursor=b", "/v0.1/servers?limit=100&cursor=a", "/v0.1/servers?limit=100&cursor=b"}
	p := PageWorkload{Classes: []PageClass{
		{APIVersion: "v0.1", Limit: 100, KeyCounts: []int{7, 2}},
		{APIVersion: "v0", Limit: 50, KeyCounts: []int{1}},
	}}
	if err := p.prepare(keys); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for n := range 10 {
		u, err := url.Parse(p.path(n))
		if err != nil {
			t.Fatal(err)
		}
		counts[u.Path+"|"+u.Query().Get("limit")]++
	}
	if counts["/v0.1/servers|100"] != 9 || counts["/v0/servers|50"] != 1 {
		t.Fatalf("catalog ordering changed measured mix: %v", counts)
	}
}

func TestMeasuredPageMixRejectsInsufficientDiversity(t *testing.T) {
	p := PageWorkload{Classes: []PageClass{{APIVersion: "v0.1", Limit: 100, KeyCounts: []int{2, 1}}}}
	err := p.prepare([]string{"/v0.1/servers?limit=100&cursor=a", "/v0.1/servers?cursor=a&limit=100"})
	if err == nil || !strings.Contains(err.Error(), "catalog has 1") {
		t.Fatalf("must not silently collapse distinct production keys: %v", err)
	}
}

func TestMeasuredPageMixExcludesOtherFiltersAndExternalURLs(t *testing.T) {
	p := PageWorkload{Classes: []PageClass{{APIVersion: "v0.1", Limit: 100, KeyCounts: []int{1}}}}
	err := p.prepare([]string{"https://example.invalid/v0.1/servers?limit=100", "/v0.1/servers?limit=100&search=private", "/v0.1/servers?limit=100&version=latest"})
	if err == nil {
		t.Fatal("matched an external URL or a different query class")
	}
}

func TestPageSequenceSurvivesSpecialQuerySlots(t *testing.T) {
	keys := make([]string, 128)
	var want int
	for n := range 9000 {
		_, kind := readPath(n, 80, 4500, 36, 3, keys)
		if kind != "page" {
			continue
		}
		if got := pageSequence(n, 4500, 39); got != want {
			t.Fatalf("slot %d: got %d want %d", n, got, want)
		}
		want++
	}
}

func TestRequestShapeDoesNotRetainParameterValues(t *testing.T) {
	got := benchmarkRequestShape("/v0.1/servers?limit=100&search=private&cursor=private&version=private", "search")
	if got != "search|v0.1|100|exact|cursor=true" {
		t.Fatal(got)
	}
	if got := benchmarkRequestShape("/v0.1/servers?limit=private", "page"); strings.Contains(got, "private") {
		t.Fatal("unbounded label")
	}
}
