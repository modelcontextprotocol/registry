package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const latestVersion = "latest"

// PageWorkload models a measured page mix. It is not a replay of client crawl
// sessions: temporal ordering and connection reuse need separate validation.
type PageWorkload struct {
	Classes []PageClass `json:"classes"`
	SHA256  string      `json:"-"`
	paths   []string
}

type PageClass struct {
	APIVersion    string `json:"api_version"`
	Limit         int    `json:"limit"`
	VersionFilter string `json:"version_filter"`
	// Each count is the observed frequency of one distinct URI in this class.
	// These anonymous ranks map to distinct real staging URLs, not client IDs.
	KeyCounts []int `json:"key_counts"`
}

func loadPageWorkload(path string, keys []string) (*PageWorkload, error) {
	if path == "" {
		return &PageWorkload{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var profile PageWorkload
	if err := json.Unmarshal(raw, &profile); err != nil {
		return nil, err
	}
	if err := profile.prepare(keys); err != nil {
		return nil, err
	}
	profile.SHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
	return &profile, nil
}

func (p *PageWorkload) prepare(keys []string) error {
	if len(p.Classes) == 0 || len(p.Classes) > 200 {
		return fmt.Errorf("page workload needs 1–200 measured classes")
	}
	p.paths = nil
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // Reproducible workload permutation, not security-sensitive randomness.
	seenClasses := map[string]bool{}
	for _, class := range p.Classes {
		if (class.APIVersion != "v0" && class.APIVersion != "v0.1") || class.Limit < 1 || class.Limit > 100 ||
			(class.VersionFilter != "" && class.VersionFilter != latestVersion) || len(class.KeyCounts) == 0 {
			return fmt.Errorf("invalid page workload class")
		}
		identity := fmt.Sprintf("%s/%d/%s", class.APIVersion, class.Limit, class.VersionFilter)
		if seenClasses[identity] {
			return fmt.Errorf("duplicate page workload class %s", identity)
		}
		seenClasses[identity] = true
		candidates := matchingPageKeys(class, keys)
		if len(candidates) < len(class.KeyCounts) {
			return fmt.Errorf("class %s needs %d distinct staging keys; catalog has %d", identity, len(class.KeyCounts), len(candidates))
		}
		// Catalog ordering must not choose the API version or concentrate hot
		// ranks near the beginning of the registry.
		rng.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
		for i, count := range class.KeyCounts {
			if count <= 0 || count > 1_000_000-len(p.paths) {
				return fmt.Errorf("page workload counts must be positive and total at most 1000000")
			}
			for range count {
				p.paths = append(p.paths, candidates[i])
			}
		}
	}
	rng.Shuffle(len(p.paths), func(i, j int) { p.paths[i], p.paths[j] = p.paths[j], p.paths[i] })
	return nil
}

func (p *PageWorkload) path(sequence int) string {
	return p.paths[sequence%len(p.paths)]
}

// Count page slots preceding n, including pages dropped by the bounded queue.
// Special query slots must not bias which measured page classes are exercised.
func pageSequence(n, rate, specialRate int) int {
	return n/rate*(rate-specialRate) + n%rate - n%rate*specialRate/rate
}

func matchingPageKeys(class PageClass, keys []string) []string {
	var candidates []string
	seen := map[string]bool{}
	for _, key := range keys {
		u, err := url.Parse(key)
		if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || u.Path != "/"+class.APIVersion+"/servers" {
			continue
		}
		q := u.Query()
		valid := q.Get("limit") == fmt.Sprint(class.Limit) && q.Get(versionParam) == class.VersionFilter
		for name, values := range q {
			if (name != "cursor" && name != "limit" && name != versionParam) || len(values) != 1 {
				valid = false
			}
		}
		canonical := u.Path + "?" + q.Encode()
		if valid && !seen[canonical] {
			seen[canonical] = true
			candidates = append(candidates, canonical)
		}
	}
	return candidates
}

func validateConnectionRotation(interval time.Duration) {
	if interval < 0 || interval > 10*time.Minute {
		panic("connection rotation must be between 0 and 10 minutes")
	}
}

func rotateIdleConnections(transport *http.Transport, interval time.Duration) func() {
	done := make(chan struct{})
	if interval == 0 {
		return func() { close(done) }
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				transport.CloseIdleConnections()
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}

// Bounded structural labels let calibration compare delivered mix, without
// retaining search terms, cursor values or server names in results.
func benchmarkRequestShape(path, kind string) string {
	u, err := url.Parse(path)
	if err != nil {
		return kind + "|invalid"
	}
	api := "other"
	if strings.HasPrefix(u.Path, "/v0.1/") {
		api = "v0.1"
	} else if strings.HasPrefix(u.Path, "/v0/") {
		api = "v0"
	}
	q := u.Query()
	limit := "default"
	if value := q.Get("limit"); value != "" {
		count, parseErr := strconv.Atoi(value)
		limit = "invalid"
		if parseErr == nil && count > 0 && count <= 100 {
			limit = strconv.Itoa(count)
		}
	}
	version := "none"
	if value := q.Get(versionParam); value == latestVersion {
		version = latestVersion
	} else if value != "" {
		version = "exact"
	}
	return fmt.Sprintf("%s|%s|%s|%s|cursor=%t", kind, api, limit, version, q.Get("cursor") != "")
}
