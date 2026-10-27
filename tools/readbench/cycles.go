package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
)

type cycleConfig struct {
	load                                      loadConfig
	metrics, searchTargets                    []string
	searchScenario                            string
	searchDuration                            time.Duration
	searchWorkers                             int
	diverseKeys                               bool
	waves, crawlers, searchRate, shortPercent int
	interval, recovery                        time.Duration
}
type crawlResult struct {
	Wave     int     `json:"wave"`
	Client   int     `json:"client"`
	Shape    string  `json:"shape"`
	Pages    int     `json:"pages"`
	Entries  int     `json:"entries"`
	Complete bool    `json:"complete"`
	Seconds  float64 `json:"seconds"`
	Error    string  `json:"error,omitempty"`
}
type cycleSample struct {
	Second                      float64 `json:"second"`
	Pending, Inflight, Acquired float64
	ObservedTargets             int `json:"observed_targets"`
}
type cycleOutput struct {
	SearchTargets           []string                `json:"search_targets"`
	SearchScenario          string                  `json:"search_scenario"`
	SearchOfferingSeconds   float64                 `json:"search_offering_seconds"`
	SearchWorkers           int                     `json:"search_workers"`
	DiverseKeys             bool                    `json:"diverse_cache_keys"`
	Waves                   int                     `json:"waves"`
	CrawlersPerWave         int                     `json:"crawlers_per_wave"`
	IntervalSeconds         float64                 `json:"interval_seconds"`
	RecoverySeconds         float64                 `json:"recovery_seconds"`
	CrawlRateCeiling        int                     `json:"crawl_rate_ceiling"`
	SearchRate              int                     `json:"search_rate"`
	ShortSearchPercent      int                     `json:"short_search_percent"`
	ScheduledSearches       int                     `json:"scheduled_searches"`
	DroppedSearches         int                     `json:"driver_dropped_searches"`
	ScheduledPublishes      int                     `json:"scheduled_publishes"`
	DroppedPublishes        int                     `json:"driver_dropped_publishes"`
	ExpectedShortRejections int                     `json:"short_search_422"`
	UnexpectedFailures      int                     `json:"unexpected_failures"`
	CompletedCrawls         int                     `json:"completed_crawls"`
	UsefulReadSuccesses     int                     `json:"non_short_read_successes"`
	Status                  map[string]int          `json:"status"`
	Latency                 map[string]distribution `json:"success_latency_ms"`
	PerSecond               map[int]int             `json:"completed_reads_per_second"`
	Cache                   map[string]int          `json:"cache"`
	Crawls                  []crawlResult           `json:"crawls"`
	Samples                 []cycleSample           `json:"samples"`
	CPU                     map[string]float64      `json:"app_cpu_seconds"`
	Origin                  map[string]float64      `json:"origin_reads"`
	ClockDelta              float64                 `json:"clock_discontinuity_seconds"`
	Valid                   bool                    `json:"valid_measurement"`
}
type crawlPage struct {
	Servers []struct {
		Server struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"server"`
	} `json:"servers"`
	Metadata struct {
		Next  string `json:"nextCursor"`
		Count int    `json:"count"`
	} `json:"metadata"`
}

// Global pacing limits crawler request starts; crawl latency excludes intentional pacing.
// Wave arrivals are independent of previous wave completion; each walk follows real cursors.
type crawlPacer struct {
	mu   sync.Mutex
	next time.Time
	step time.Duration
}

func (p *crawlPacer) wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	scheduled := p.next
	if scheduled.Before(now) {
		scheduled = now
	}
	p.next = scheduled.Add(p.step)
	p.mu.Unlock()
	timer := time.NewTimer(time.Until(scheduled))
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Vary real client choices without adding meaningless cache-busting parameters.
func crawlPath(q url.Values, id int, diverse bool) string {
	prefix := "/v0.1/servers?"
	encoded := q.Encode()
	if diverse {
		if (id/2)%2 == 0 {
			prefix = "/v0/servers?"
		}
		if (id/4)%2 == 0 {
			parts := strings.Split(encoded, "&")
			for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
				parts[i], parts[j] = parts[j], parts[i]
			}
			encoded = strings.Join(parts, "&")
		}
	}
	return prefix + encoded
}

func fetchPage(ctx context.Context, client *http.Client, target, path string) (crawlPage, observation) {
	started := time.Now()
	o := observation{kind: "crawl"}
	var page crawlPage
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target+path, nil)
	if err == nil {
		var resp *http.Response
		resp, err = client.Do(req)
		if err == nil {
			o.status = resp.StatusCode
			o.cache = resp.Header.Get("X-Registry-Cache")
			if resp.StatusCode == http.StatusOK {
				err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&page)
			} else {
				_, _ = io.Copy(io.Discard, resp.Body)
			}
			_ = resp.Body.Close()
		}
	}
	if err != nil {
		o.status = 0
		o.errorText = err.Error()
	}
	o.completed = time.Now()
	o.elapsed = float64(o.completed.Sub(started)) / float64(time.Millisecond)
	return page, o
}

func crawlParams(wave, id int, diverse bool) (url.Values, string) {
	shape := "all_versions"
	q := url.Values{"limit": {"100"}}
	if diverse {
		q.Set("limit", []string{"30", "50", "96", "100"}[(id/2)%4])
		if id%8 == 0 {
			q.Del("limit")
		}
	}
	// 60% all-version walks, 30% latest-only walks, 10% unique incremental walks.
	if id%10 >= 6 && id%10 < 9 {
		q.Set("version", "latest")
		shape = "latest"
	}
	if id%10 == 9 {
		q.Set("updated_since", fixtureEpoch.Add(-7*24*time.Hour-time.Duration(wave*10000+id)*time.Microsecond).Format(time.RFC3339Nano))
		shape = "incremental"
	}
	return q, shape
}

func crawl(ctx context.Context, client *http.Client, target string, wave, id int, pacer *crawlPacer, diverse bool, record func(observation)) crawlResult {
	started := time.Now()
	r := crawlResult{Wave: wave, Client: id, Shape: "all_versions"}
	q, shape := crawlParams(wave, id, diverse)
	r.Shape = shape
	previous := ""
	for r.Pages < 10000 {
		if err := pacer.wait(ctx); err != nil {
			r.Error = err.Error()
			break
		}
		page, o := fetchPage(ctx, client, target, crawlPath(q, id, diverse))
		record(o)
		if o.status != http.StatusOK {
			r.Error = fmt.Sprintf("HTTP %d: %s", o.status, o.errorText)
			break
		}
		r.Pages++
		r.Entries += len(page.Servers)
		if page.Metadata.Count != len(page.Servers) {
			r.Error = "metadata count mismatch"
			break
		}
		for _, row := range page.Servers {
			key := row.Server.Name + "\x00" + row.Server.Version
			if previous != "" && key <= previous {
				r.Error = "duplicate or unordered pagination"
				break
			}
			if q.Get("version") == "latest" && row.Server.Version != "1.0.2" && !strings.HasPrefix(row.Server.Name, "io.benchmark/publish-") {
				r.Error = "latest filter mismatch"
				break
			}
			previous = key
		}
		if r.Error != "" {
			break
		}
		if page.Metadata.Next == "" {
			r.Complete = true
			break
		}
		if page.Metadata.Next != strings.Replace(previous, "\x00", ":", 1) {
			r.Error = "cursor does not match last row"
			break
		}
		q.Set("cursor", page.Metadata.Next)
	}
	if !r.Complete && r.Error == "" {
		r.Error = "page safety limit reached"
	}
	r.Seconds = time.Since(started).Seconds()
	return r
}

func sampleCycles(ctx context.Context, client *http.Client, targets []string, start time.Time) cycleSample {
	s := cycleSample{Second: time.Since(start).Seconds()}
	for _, target := range targets {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target+"/metrics", nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			continue
		}
		s.ObservedTargets++
		for line := range strings.SplitSeq(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) != 2 {
				continue
			}
			v, err := strconv.ParseFloat(f[1], 64)
			if err != nil {
				continue
			}
			switch {
			case strings.HasPrefix(f[0], "mcp_registry_db_pool_acquire_pending{"):
				s.Pending += v
			case strings.HasPrefix(f[0], "mcp_registry_http_requests_in_flight{"):
				s.Inflight += v
			case strings.HasPrefix(f[0], "mcp_registry_db_pool_connections{") && strings.Contains(f[0], `state="acquired"`):
				s.Acquired += v
			}
		}
	}
	return s
}

func validateCycleConfig(c cycleConfig) error {
	if c.load.output == "" || len(c.metrics) == 0 {
		return fmt.Errorf("cycles requires output and direct metric targets")
	}
	if err := validateLoad(c.load); err != nil {
		return err
	}
	if c.waves < 2 || c.waves > 5 || c.crawlers < 1 || c.crawlers > 256 || c.interval < time.Second || c.recovery < time.Second || c.searchRate < 0 || c.searchRate > 500 || c.shortPercent < 0 || c.shortPercent > 100 {
		return fmt.Errorf("invalid cyclic workload parameters")
	}
	if c.searchDuration <= 0 || c.searchDuration > c.load.duration || c.searchWorkers < 1 || c.searchWorkers > 256 {
		return fmt.Errorf("invalid search offering or worker count")
	}
	searchConfig := c.load
	searchConfig.targets = c.searchTargets
	searchConfig.scenario = c.searchScenario
	if err := validateLoad(searchConfig); err != nil {
		return err
	}
	metricConfig := c.load
	metricConfig.targets = c.metrics
	if err := validateLoad(metricConfig); err != nil {
		return err
	}
	return nil
}

func runCycles(ctx context.Context, c cycleConfig) error {
	c.load.duration = time.Duration(c.waves)*c.interval + c.recovery
	if c.searchDuration == 0 {
		c.searchDuration = c.load.duration
	}
	if c.searchWorkers == 0 {
		c.searchWorkers = 32
	}
	if c.searchScenario == "" {
		c.searchScenario = scenarioObserved
	}
	if len(c.searchTargets) == 0 {
		c.searchTargets = c.load.targets
	}
	if err := validateCycleConfig(c); err != nil {
		return err
	}
	transport := &http.Transport{MaxIdleConns: 512, MaxIdleConnsPerHost: 256, IdleConnTimeout: time.Minute}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: c.load.timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	token, err := auth.NewJWTManager(&config.Config{JWTPrivateKey: benchmarkKey}).GenerateTokenResponse(ctx, auth.JWTClaims{AuthMethod: auth.MethodGitHubAT, AuthMethodSubject: benchmarkSubject, Permissions: []auth.Permission{{Action: auth.PermissionActionPublish, ResourcePattern: "io.benchmark/*"}}})
	if err != nil {
		return err
	}
	cpuBefore := cpuSnapshot(ctx, client, c.metrics)
	originBefore := originSnapshot(ctx, client, c.metrics)
	start := time.Now()
	runID := strconv.FormatInt(start.UnixNano(), 36)
	ctx, cancel := context.WithTimeout(ctx, c.load.duration+c.load.timeout)
	defer cancel()
	var mu sync.Mutex
	var obs []observation
	var walks []crawlResult
	var samples []cycleSample
	record := func(o observation) { mu.Lock(); obs = append(obs, o); mu.Unlock() }
	var wg sync.WaitGroup
	var droppedSearch, droppedPublish int
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s := sampleCycles(ctx, client, c.metrics, start)
				mu.Lock()
				samples = append(samples, s)
				mu.Unlock()
			}
		}
	}()
	if c.load.publishRate > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			droppedPublish = runSchedule(start, c.load.duration, c.load.publishRate, 16, true, func(j job) { record(execute(ctx, client, c.load, j, token.RegistryToken, runID)) })
		}()
	}
	if c.searchRate > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			droppedSearch = runSchedule(start, c.searchDuration, c.searchRate, c.searchWorkers, false, func(j job) {
				cfg := c.load
				cfg.scenario = c.searchScenario
				cfg.targets = c.searchTargets
				if j.number%100 < c.shortPercent {
					cfg.scenario = scenarioShort
				}
				record(execute(ctx, client, cfg, j, token.RegistryToken, runID))
			})
		}()
	}
	pacer := &crawlPacer{next: start, step: time.Second / time.Duration(c.load.rate)}
	for wave := range c.waves {
		time.Sleep(time.Until(start.Add(time.Duration(wave) * c.interval)))
		for id := range c.crawlers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := crawl(ctx, client, c.load.targets[id%len(c.load.targets)], wave, id, pacer, c.diverseKeys, record)
				mu.Lock()
				walks = append(walks, r)
				mu.Unlock()
			}()
		}
	}
	wg.Wait()
	finish := time.Now()
	cpuAfter := cpuSnapshot(context.WithoutCancel(ctx), client, c.metrics)
	originAfter := originSnapshot(context.WithoutCancel(ctx), client, c.metrics)
	out := summarizeCycles(c, start, obs, walks, samples)
	out.DroppedSearches = droppedSearch
	out.DroppedPublishes = droppedPublish
	out.ClockDelta = float64(finish.UnixNano()-start.UnixNano())/float64(time.Second) - finish.Sub(start).Seconds()
	out.Valid = math.Abs(out.ClockDelta) < .5 && out.Status["publish:401"] == 0 && out.Status["publish:403"] == 0
	for k, v := range cpuBefore {
		out.CPU[k] = cpuAfter[k] - v
	}
	for k, v := range originBefore {
		out.Origin[k] = originAfter[k] - v
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.load.output, append(data, '\n'), 0600)
}

func summarizeCycles(c cycleConfig, start time.Time, obs []observation, walks []crawlResult, samples []cycleSample) cycleOutput {
	r := cycleOutput{SearchTargets: c.searchTargets, SearchScenario: c.searchScenario, SearchOfferingSeconds: c.searchDuration.Seconds(), SearchWorkers: c.searchWorkers, DiverseKeys: c.diverseKeys, Waves: c.waves, CrawlersPerWave: c.crawlers, IntervalSeconds: c.interval.Seconds(), RecoverySeconds: c.recovery.Seconds(), CrawlRateCeiling: c.load.rate, SearchRate: c.searchRate, ShortSearchPercent: c.shortPercent, ScheduledSearches: int(c.searchDuration.Seconds()) * c.searchRate, ScheduledPublishes: int(c.load.duration.Seconds()) * c.load.publishRate, Status: map[string]int{}, Latency: map[string]distribution{}, PerSecond: map[int]int{}, Cache: map[string]int{}, Crawls: walks, Samples: samples, CPU: map[string]float64{}, Origin: map[string]float64{}}
	lat := map[string][]float64{}
	for _, o := range obs {
		r.Status[fmt.Sprintf("%s:%d", o.kind, o.status)]++
		if o.cache != "" {
			r.Cache[o.cache]++
		}
		switch {
		case o.status == http.StatusOK:
			lat[o.kind] = append(lat[o.kind], o.elapsed)
			if o.kind != kindPublish {
				r.PerSecond[int(o.completed.Sub(start).Seconds())]++
				if o.kind != scenarioShort {
					r.UsefulReadSuccesses++
				}
			}
		case o.kind == scenarioShort && o.status == http.StatusUnprocessableEntity:
			r.ExpectedShortRejections++
		default:
			r.UnexpectedFailures++
		}
	}
	for k, v := range lat {
		r.Latency[k] = quantiles(v)
	}
	for _, w := range walks {
		if w.Complete {
			r.CompletedCrawls++
		}
	}
	return r
}
