package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const (
	scenarioPage    = "page"
	scenarioUpdated = "updated"
	scenarioSearch  = "search"
	scenarioSparse  = "sparse"
)

type loadConfig struct {
	targets                          []string
	rows, rate, workers, publishRate int
	duration                         time.Duration
	scenario, output                 string
	timeout                          time.Duration
}
type job struct {
	number    int
	scheduled time.Time
	publish   bool
}
type observation struct {
	kind         string
	status       int
	elapsed, lag float64
	bytes        int64
	cache        string
	errorText    string
	completed    time.Time
}
type distribution struct {
	Count int     `json:"count"`
	P50   float64 `json:"p50_ms"`
	P95   float64 `json:"p95_ms"`
	P99   float64 `json:"p99_ms"`
	Max   float64 `json:"max_ms"`
}
type result struct {
	Scenario                  string                  `json:"scenario"`
	OfferedRPS                int                     `json:"offered_rps"`
	Duration                  float64                 `json:"offering_seconds"`
	Drain                     float64                 `json:"drain_seconds"`
	Scheduled                 int                     `json:"scheduled_reads"`
	Dropped                   int                     `json:"driver_dropped_reads"`
	Completed                 int                     `json:"successful_reads"`
	Goodput                   float64                 `json:"successful_reads_per_second_including_drain"`
	DuringOffer               int                     `json:"successful_reads_during_offering"`
	Status                    map[string]int          `json:"status"`
	Cache                     map[string]int          `json:"cache"`
	Latency                   map[string]distribution `json:"successful_latency_from_scheduled_time"`
	Dispatch                  distribution            `json:"dispatch_lag"`
	Errors                    []string                `json:"error_examples,omitempty"`
	Bytes                     int64                   `json:"response_bytes"`
	CPUSeconds                map[string]float64      `json:"app_cpu_seconds"`
	OriginRequests            map[string]float64      `json:"origin_read_starts"`
	ScheduledPublishes        int                     `json:"scheduled_publishes"`
	DroppedPublishes          int                     `json:"driver_dropped_publishes"`
	ServedAllOffered          bool                    `json:"served_all_offered_requests"`
	ReadWorkers               int                     `json:"read_workers"`
	ReadQueueCapacity         int                     `json:"read_queue_capacity"`
	WallSeconds               float64                 `json:"wall_seconds"`
	ClockDiscontinuitySeconds float64                 `json:"clock_discontinuity_seconds"`
	ValidForComparison        bool                    `json:"valid_for_comparison"`
	InvalidReasons            []string                `json:"invalid_reasons,omitempty"`
}

func quantiles(v []float64) distribution {
	if len(v) == 0 {
		return distribution{}
	}
	sort.Float64s(v)
	q := func(f float64) float64 { return v[int(math.Ceil(f*float64(len(v))))-1] }
	return distribution{len(v), q(.5), q(.95), q(.99), v[len(v)-1]}
}

func requestPath(scenario string, sequence, rows int) (string, string) {
	shape := scenario
	switch scenario {
	case "mixed":
		shape = scenarioPage
		if sequence%100 >= 55 {
			shape = scenarioUpdated
		}
		if sequence%100 >= 98 {
			shape = scenarioSearch
		}
	case "page-heavy":
		shape = scenarioPage
		if sequence%100 >= 85 {
			shape = scenarioUpdated
		}
		if sequence%100 >= 99 {
			shape = scenarioSearch
		}
	}
	index := (sequence * 7919) % (rows/3 - 400)
	if shape == "hot" {
		index = sequence % 8 * 100
		shape = scenarioPage
	}
	q := url.Values{"limit": {"100"}, "cursor": {fmt.Sprintf("io.benchmark/server-%06d:1.0.0", index)}}
	switch shape {
	case scenarioUpdated, scenarioSparse:
		age := 7 * 24 * time.Hour
		if shape == scenarioSparse {
			age = time.Hour
		}
		// Distinct watermarks deliberately defeat exact-URL cache reuse.
		q.Set("updated_since", fixtureEpoch.Add(-age-time.Duration(sequence)*time.Microsecond).Format(time.RFC3339Nano))
	case scenarioSearch:
		q.Del("cursor")
		q.Set(scenarioSearch, fmt.Sprintf("server-%04d", sequence%600))
	}
	return "/v0.1/servers?" + q.Encode(), shape
}

func publishRequest(sequence int, runID string) []byte {
	b, _ := json.Marshal(map[string]any{"$schema": model.CurrentSchemaURL, "name": fmt.Sprintf("io.benchmark/publish-%s-%06d", runID, sequence), "description": "Local benchmark publish", "version": "1.0.0", "remotes": []map[string]string{{"type": "streamable-http", "url": fmt.Sprintf("https://example.invalid/%s/%d/mcp", runID, sequence)}}})
	return b
}

func execute(ctx context.Context, client *http.Client, c loadConfig, j job, token, runID string) observation {
	path, kind := requestPath(c.scenario, j.number, c.rows)
	method := http.MethodGet
	var body io.Reader
	if j.publish {
		kind = "publish"
		method = http.MethodPost
		path = "/v0.1/publish"
		body = bytes.NewReader(publishRequest(j.number, runID))
	}
	started := time.Now()
	ctx, cancel := context.WithDeadline(ctx, j.scheduled.Add(c.timeout))
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.targets[j.number%len(c.targets)], "/")+path, body)
	o := observation{kind: kind, lag: float64(started.Sub(j.scheduled)) / float64(time.Millisecond)}
	if err == nil {
		req.Header.Set("X-Request-ID", fmt.Sprintf("readbench-%s-%d", runID, j.number))
		if j.publish {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
		}
		var resp *http.Response
		resp, err = client.Do(req)
		if err == nil {
			o.status = resp.StatusCode
			o.cache = resp.Header.Get("X-Registry-Cache")
			if resp.StatusCode >= 400 {
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
				o.errorText = string(b)
			}
			o.bytes, err = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}
	if err != nil {
		o.errorText = err.Error()
		o.status = 0
	}
	o.completed = time.Now()
	o.elapsed = float64(o.completed.Sub(j.scheduled)) / float64(time.Millisecond)
	return o
}

func cpuSnapshot(ctx context.Context, client *http.Client, targets []string) map[string]float64 {
	out := map[string]float64{}
	for _, target := range targets {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(target, "/")+"/metrics", nil)
		if err != nil {
			continue
		}
		r, err := client.Do(req)
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		_ = r.Body.Close()
		if err != nil {
			continue
		}
		for line := range strings.SplitSeq(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "process_cpu_seconds_total" {
				v, err := strconv.ParseFloat(fields[1], 64)
				if err == nil {
					out[target] = v
				}
			}
		}
	}
	return out
}

func validateLoad(c loadConfig) error {
	if c.timeout <= 0 || c.timeout > time.Minute {
		return fmt.Errorf("timeout must be positive and at most one minute")
	}
	if c.rate <= 0 || c.rate > 100000 || c.workers <= 0 || c.rows < 3000 || c.duration <= 0 || c.duration > 2*time.Minute || c.publishRate < 0 || c.publishRate > 100 {
		return fmt.Errorf("invalid rate, duration, workers, rows, or publish rate")
	}
	switch c.scenario {
	case scenarioPage, scenarioUpdated, scenarioSparse, scenarioSearch, "mixed", "page-heavy", "hot":
	default:
		return fmt.Errorf("unknown scenario %q", c.scenario)
	}
	for _, target := range c.targets {
		if err := localURL(target); err != nil {
			return err
		}
		u, _ := url.Parse(target)
		if u.Scheme != "http" || u.Path != "" {
			return fmt.Errorf("target must be an http loopback origin without a path")
		}
	}
	return nil
}

func load(ctx context.Context, c loadConfig) error {
	if err := validateLoad(c); err != nil {
		return err
	}
	transport := &http.Transport{MaxIdleConns: c.workers + 8, MaxIdleConnsPerHost: c.workers + 8, IdleConnTimeout: 30 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: c.timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	tok, err := auth.NewJWTManager(&config.Config{JWTPrivateKey: benchmarkKey}).GenerateTokenResponse(ctx, auth.JWTClaims{AuthMethod: auth.MethodGitHubAT, AuthMethodSubject: "benchmark", Permissions: []auth.Permission{{Action: auth.PermissionActionPublish, ResourcePattern: "io.benchmark/*"}}})
	if err != nil {
		return err
	}
	cpuBefore := cpuSnapshot(ctx, client, c.targets)
	originBefore := originSnapshot(ctx, client, c.targets)
	start := time.Now()
	end := start.Add(c.duration)
	runID := strconv.FormatInt(start.UnixNano(), 36)
	observations := make([]observation, 0, int(c.duration.Seconds()*float64(c.rate+c.publishRate)))
	var mu sync.Mutex
	record := func(o observation) { mu.Lock(); observations = append(observations, o); mu.Unlock() }
	// Publishing has its own open-loop scheduler and bounded worker pool.
	var publishes sync.WaitGroup
	droppedPublishes := 0
	if c.publishRate > 0 {
		publishes.Add(1)
		go func() {
			defer publishes.Done()
			workers := min(c.workers, max(1, c.publishRate*int(math.Ceil(c.timeout.Seconds()))))
			droppedPublishes = runSchedule(start, c.duration, c.publishRate, workers, true, func(j job) {
				record(execute(ctx, client, c, j, tok.RegistryToken, runID))
			})
		}()
	}
	count := int(c.duration.Seconds() * float64(c.rate))
	dropped := runSchedule(start, c.duration, c.rate, c.workers, false, func(j job) {
		record(execute(ctx, client, c, j, tok.RegistryToken, runID))
	})
	publishes.Wait()
	finished := time.Now()
	elapsed := finished.Sub(start)
	wallSeconds := float64(finished.UnixNano()-start.UnixNano()) / float64(time.Second)
	cpuAfter := cpuSnapshot(ctx, client, c.targets)
	originAfter := originSnapshot(ctx, client, c.targets)
	r := summarize(c, observations, end, elapsed, count, dropped)
	r.WallSeconds = wallSeconds
	r.ClockDiscontinuitySeconds = wallSeconds - elapsed.Seconds()
	r.ValidForComparison = true
	if math.Abs(r.ClockDiscontinuitySeconds) > .5 {
		r.ValidForComparison = false
		r.InvalidReasons = append(r.InvalidReasons, "wall clock changed relative to monotonic clock")
	}
	if r.Status["publish:401"]+r.Status["publish:403"] > 0 {
		r.ValidForComparison = false
		r.InvalidReasons = append(r.InvalidReasons, "publish authentication failed")
	}
	for target, before := range cpuBefore {
		if after, ok := cpuAfter[target]; ok {
			r.CPUSeconds[target] = after - before
		}
	}
	r.OriginRequests = map[string]float64{}
	for target, before := range originBefore {
		if after, ok := originAfter[target]; ok {
			r.OriginRequests[target] = after - before
		}
	}
	r.ScheduledPublishes = int(c.duration.Seconds() * float64(c.publishRate))
	r.DroppedPublishes = droppedPublishes
	r.ReadWorkers = c.workers
	r.ReadQueueCapacity = c.workers
	r.ServedAllOffered = r.Completed == count && r.Latency["publish"].Count == r.ScheduledPublishes && dropped == 0 && droppedPublishes == 0
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if c.output != "" {
		return os.WriteFile(c.output, append(data, '\n'), 0600)
	}
	fmt.Println(string(data)) //nolint:forbidigo // CLI machine-readable output, not application logging.
	return nil
}

// runSchedule preserves offered arrivals even when earlier requests are still running.
func runSchedule(start time.Time, duration time.Duration, rate, workers int, publish bool, executeJob func(job)) int {
	jobs := make(chan job, workers)
	var wg sync.WaitGroup
	var ready sync.WaitGroup
	ready.Add(workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready.Done()
			for j := range jobs {
				executeJob(j)
			}
		}()
	}
	ready.Wait()
	dropped := 0
	for i := range int(duration.Seconds() * float64(rate)) {
		scheduled := start.Add(time.Duration(float64(i) * float64(time.Second) / float64(rate)))
		time.Sleep(time.Until(scheduled))
		select {
		case jobs <- job{i, scheduled, publish}:
		default:
			dropped++
		}
	}
	close(jobs)
	wg.Wait()
	return dropped
}

func summarize(c loadConfig, observations []observation, end time.Time, elapsed time.Duration, count, dropped int) result {
	r := result{Scenario: c.scenario, OfferedRPS: c.rate, Duration: c.duration.Seconds(), Drain: max(0, elapsed.Seconds()-c.duration.Seconds()), Scheduled: count, Dropped: dropped, Status: map[string]int{}, Cache: map[string]int{}, Latency: map[string]distribution{}, CPUSeconds: map[string]float64{}}
	latency := map[string][]float64{}
	var lags []float64
	for _, o := range observations {
		r.Status[fmt.Sprintf("%s:%d", o.kind, o.status)]++
		r.Bytes += o.bytes
		lags = append(lags, o.lag)
		if o.cache != "" {
			r.Cache[o.cache]++
		}
		if o.status >= 200 && o.status < 300 {
			latency[o.kind] = append(latency[o.kind], o.elapsed)
			if o.kind != "publish" {
				r.Completed++
				latency["all_reads"] = append(latency["all_reads"], o.elapsed)
				if o.completed.Before(end) {
					r.DuringOffer++
				}
			}
		} else if len(r.Errors) < 5 {
			r.Errors = append(r.Errors, o.errorText)
		}
	}
	for kind, values := range latency {
		r.Latency[kind] = quantiles(values)
	}
	r.Dispatch = quantiles(lags)
	r.Goodput = float64(r.Completed) / max(c.duration.Seconds(), elapsed.Seconds())
	return r
}

func originSnapshot(ctx context.Context, client *http.Client, targets []string) map[string]float64 {
	out := map[string]float64{}
	for _, target := range targets {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target+"/_readbench/stats", nil)
		if err != nil {
			continue
		}
		r, err := client.Do(req)
		if err != nil {
			continue
		}
		var stats struct {
			ReadStarts float64 `json:"read_starts"`
		}
		err = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&stats)
		_ = r.Body.Close()
		if err == nil && r.StatusCode == http.StatusOK {
			out[target] = stats.ReadStarts
		}
	}
	return out
}
