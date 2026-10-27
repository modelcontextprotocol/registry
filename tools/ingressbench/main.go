// ingressbench drives only the explicitly acknowledged staging registry. It runs
// outside the tested node pool, through the public ingress and its real caches.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const limitParam = "limit"
const target = "https://staging.registry.modelcontextprotocol.io"

type result struct {
	StartedAt, FinishedAt, CatalogSHA256                                string
	ReadRate, UpdatedRate, SearchRate, HotPercent                       int
	Scheduled, ScheduledPublishes, Completed, Dropped, DroppedPublishes int
	Bytes                                                               int64
	Status, Cache, Errors, Protocols                                    map[string]int
	ErrorDetails                                                        map[string]string
	LatencyMS                                                           map[string]float64
	ElapsedSeconds                                                      float64
	Failures                                                            []requestFailure
	OmittedFailures                                                     int
}
type requestFailure struct {
	ScheduledAt, FinishedAt time.Time
	Kind, Class             string
	Status                  int
}
type point struct {
	kind, cache, failure, protocol, detail string
	status                                 int
	elapsed                                float64
	bytes                                  int64
	scheduledAt, finishedAt                time.Time
}

func main() {
	ack := flag.Bool("staging", false, "acknowledge load against staging only")
	catalogMode := flag.Bool("catalog", false, "collect real pagination URLs before timing")
	keysFile := flag.String("keys", "keys.json", "saved real pagination URLs")
	output := flag.String("out", "result.json", "output")
	rate := flag.Int("rate", 1500, "scheduled reads per second")
	updatedRate := flag.Int("updated-rate", 0, "uncached incremental-sync reads per second, within total rate")
	searchRate := flag.Int("search-rate", 3, "varied search reads per second, within total rate")
	duration := flag.Duration("duration", 10*time.Minute, "offering duration")
	workers := flag.Int("workers", 512, "bounded request concurrency and queue")
	tokenFile := flag.String("token-file", "", "optional scoped staging publish token file")
	hot := flag.Int("hot-percent", 80, "percent of requests using the first 128 keys")
	http2 := flag.Bool("http2", false, "allow HTTP/2 negotiation (HTTP/1.1 only by default)")
	flag.Parse()
	validateArguments(*ack, *rate, *updatedRate, *searchRate, *workers, *duration, *hot)
	tr := newTransport(*http2)
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if *catalogMode {
		collectCatalog(client, *keysFile)
		return
	}
	raw, keys := loadKeys(*keysFile)
	token := loadToken(*tokenFile)
	results := make(chan point, 4096)
	var wg sync.WaitGroup
	r := result{Status: map[string]int{}, Cache: map[string]int{}, Errors: map[string]int{}, Protocols: map[string]int{}, ErrorDetails: map[string]string{}, LatencyMS: map[string]float64{}}
	samples := map[string][]float64{}
	done := make(chan struct{})
	go collectResults(results, &r, samples, done)
	request := makeRequester(client, token, results)
	type job struct {
		n  int
		at time.Time
	}
	jobs := make(chan job, *workers)
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				path, kind := readPath(j.n, *hot, *rate, *updatedRate, *searchRate, keys)
				request(path, kind, j.at, nil)
			}
		}()
	}
	start := time.Now()
	r.StartedAt = start.UTC().Format(time.RFC3339Nano)
	r.CatalogSHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
	r.ReadRate = *rate
	r.UpdatedRate = *updatedRate
	r.SearchRate = *searchRate
	r.HotPercent = *hot
	runID := fmt.Sprint(start.UnixNano())
	total := int(duration.Seconds() * float64(*rate))
	if token != "" {
		pubJobs := make(chan job, 16)
		for w := 0; w < 16; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range pubJobs {
					i := j.n
					body, _ := json.Marshal(map[string]any{"$schema": "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json", "name": fmt.Sprintf("io.benchmark/autoscale-%s-%d", runID, i), "description": "Autoscaling staging benchmark", "version": "1.0.0", "remotes": []map[string]string{{"type": "streamable-http", "url": fmt.Sprintf("https://example.invalid/autoscale-%s-%d/mcp", runID, i)}}})
					request("/v0.1/publish", "publish", j.at, body)
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(pubJobs)
			for i := 0; i < int(duration.Seconds()); i++ {
				at := start.Add(time.Duration(i) * time.Second)
				time.Sleep(time.Until(at))
				r.ScheduledPublishes++
				select {
				case pubJobs <- job{i, at}:
				default:
					r.DroppedPublishes++
				}
			}
		}()
	}
	// Rotate idle connections to model independent clients arriving during a burst
	// and let newly ready ingress replicas receive connections. Same rule in A/B.
	stopRotation := make(chan struct{})
	defer close(stopRotation)
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				tr.CloseIdleConnections()
			case <-stopRotation:
				return
			}
		}
	}()

	for i := 0; i < total; i++ {
		at := start.Add(time.Duration(float64(i) / float64(*rate) * float64(time.Second)))
		time.Sleep(time.Until(at))
		r.Scheduled++
		select {
		case jobs <- job{i, at}:
		default:
			r.Dropped++
		}
	}
	close(jobs)
	wg.Wait()
	close(results)
	<-done
	r.ElapsedSeconds = time.Since(start).Seconds()
	r.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	for kind, v := range samples {
		sort.Float64s(v)
		for _, q := range []float64{.5, .95, .99} {
			r.LatencyMS[fmt.Sprintf("%s_p%.0f", kind, q*100)] = v[int(math.Ceil(float64(len(v))*q))-1]
		}
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(*output, b, 0600); err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stdout, string(b))
}

// Use the sequence within each class. Indexing by the global request number
// couples modulo 100 to the catalog size and silently visits only a subset.
func pageIndex(n, hotPercent, keyCount int) int {
	position := n % 100
	if position < hotPercent {
		return ((n/100*hotPercent + position) * 31) % 128
	}
	coldSequence := n/100*(100-hotPercent) + position - hotPercent
	return (coldSequence * 7919) % keyCount
}

func errorClass(err error) string {
	var n net.Error
	switch {
	case errors.As(err, &n) && n.Timeout():
		return "timeout"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected_eof"
	case errors.Is(err, io.EOF):
		return "eof"
	case strings.Contains(err.Error(), "connection reset"):
		return "connection_reset"
	case strings.Contains(err.Error(), "connection refused"):
		return "connection_refused"
	default:
		return fmt.Sprintf("%T", err)
	}
}

func newTransport(allowHTTP2 bool) *http.Transport {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(allowHTTP2)
	return &http.Transport{Protocols: protocols, MaxIdleConns: 2048, MaxIdleConnsPerHost: 1024, MaxConnsPerHost: 2048, IdleConnTimeout: 15 * time.Second}
}
func errorDetail(err error) string {
	var u *url.Error
	if errors.As(err, &u) {
		err = u.Err
	} // Exclude request URLs/cursors from diagnostics.
	return err.Error()
}

func collectCatalog(client *http.Client, keysFile string) {
	var keys []string
	for _, prefix := range []string{"/v0/servers", "/v0.1/servers"} {
		for _, limit := range []string{"50", "100"} {
			q := url.Values{limitParam: {limit}}
			for i := 0; i < 1000; i++ {
				path := prefix + "?" + q.Encode()
				req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target+path, nil)
				if err != nil {
					panic(err)
				}
				resp, err := client.Do(req)
				if err != nil {
					panic(err)
				}
				var body struct {
					Metadata struct {
						Next string `json:"nextCursor"`
					} `json:"metadata"`
				}
				err = json.NewDecoder(resp.Body).Decode(&body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK || err != nil {
					panic(fmt.Sprintf("catalog failed status=%d err=%v", resp.StatusCode, err))
				}
				keys = append(keys, path)
				if body.Metadata.Next == "" {
					break
				}
				q.Set("cursor", body.Metadata.Next)
			}
		}
	}
	b, _ := json.Marshal(keys)
	if err := os.WriteFile(keysFile, b, 0600); err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stdout, "catalog keys", len(keys))
}

func loadKeys(keysFile string) ([]byte, []string) {
	raw, err := os.ReadFile(keysFile)
	if err != nil {
		panic(err)
	}
	var keys []string
	if err = json.Unmarshal(raw, &keys); err != nil {
		panic(err)
	}
	if len(keys) < 128 {
		panic("need at least 128 real keys")
	}
	for _, k := range keys {
		u, e := url.Parse(k)
		if e != nil || u.IsAbs() || u.Host != "" || (u.Path != "/v0/servers" && u.Path != "/v0.1/servers") {
			panic("invalid key")
		}
	}
	return raw, keys
}

func makeRequester(client *http.Client, token string, results chan<- point) func(path, kind string, scheduled time.Time, body []byte) {
	return func(path, kind string, scheduled time.Time, body []byte) {
		method := "GET"
		if body != nil {
			method = "POST"
		}
		req, e := http.NewRequestWithContext(context.Background(), method, target+path, bytes.NewReader(body))
		p := point{kind: kind}
		if e == nil {
			if body != nil {
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
			}
			resp, e := client.Do(req)
			if e == nil {
				p.status = resp.StatusCode
				p.protocol = resp.Proto
				p.cache = resp.Header.Get("X-Registry-Cache")
				p.bytes, e = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if e != nil {
					p.status = 0
					p.failure = "body:" + errorClass(e)
					p.detail = errorDetail(e)
				}
			} else {
				p.failure = "request:" + errorClass(e)
				p.detail = errorDetail(e)
			}
		}
		p.elapsed = float64(time.Since(scheduled)) / float64(time.Millisecond)
		if p.failure != "" || p.status >= http.StatusBadRequest {
			p.scheduledAt, p.finishedAt = scheduled.UTC(), time.Now().UTC()
		}
		results <- p
	}
}

func loadToken(tokenFile string) string {
	token := ""
	if tokenFile != "" {
		b, e := os.ReadFile(tokenFile)
		if e != nil {
			panic(e)
		}
		token = strings.TrimSpace(string(b))
	}
	return token
}

func readPath(n, hot, rate, updatedRate, searchRate int, keys []string) (string, string) {
	idx := pageIndex(n, hot, len(keys))
	path := keys[idx]
	kind := "page"
	slot := n % rate
	// Spread the low-rate query classes across each second.
	specialRate := updatedRate + searchRate
	before := slot * specialRate / rate
	after := (slot + 1) * specialRate / rate
	search := after > before && before < searchRate
	updated := after > before && !search
	if search {
		sequence := n/rate*searchRate + before
		q := url.Values{"search": {[]string{"github", "mcp", "postgres", "server", "cloud"}[sequence%5]}, limitParam: {fmt.Sprint(50 + sequence%51)}}
		path = "/v0.1/servers?" + q.Encode()
		kind = "search"
	} else if updated {
		age := []time.Duration{7 * 24 * time.Hour, 24 * time.Hour, time.Hour}[n%3]
		stamp := time.Now().UTC().Add(-age - time.Duration(n)*time.Microsecond).Format(time.RFC3339Nano)
		path = "/v0.1/servers?" + url.Values{"updated_since": {stamp}, limitParam: {"100"}}.Encode()
		kind = "updated"
	}
	return path, kind
}

func validateArguments(ack bool, rate, updatedRate, searchRate, workers int, duration time.Duration, hot int) {
	if updatedRate < 0 || searchRate < 0 || updatedRate+searchRate > rate || !ack || rate < 1 || rate > 10000 || workers < 1 || workers > 2048 || duration <= 0 || duration > 30*time.Minute || hot < 0 || hot > 100 {
		panic("explicit staging acknowledgement and bounded arguments required")
	}
}

func collectResults(results <-chan point, r *result, samples map[string][]float64, done chan<- struct{}) {
	defer close(done)
	for p := range results {
		r.Completed++
		if !p.finishedAt.IsZero() {
			// Bounded, credential-free timing evidence for correlating failures
			// with ingress/node transitions. Never retain URLs or request bodies.
			if len(r.Failures) < 100 {
				r.Failures = append(r.Failures, requestFailure{p.scheduledAt, p.finishedAt, p.kind, p.failure, p.status})
			} else {
				r.OmittedFailures++
			}
		}
		if p.failure != "" {
			r.Errors[p.kind+":"+p.failure]++
			if _, ok := r.ErrorDetails[p.failure]; !ok {
				r.ErrorDetails[p.failure] = p.detail
			}
		}
		r.Bytes += p.bytes
		r.Status[fmt.Sprintf("%s:%d", p.kind, p.status)]++
		r.Cache[p.cache]++
		r.Protocols[p.protocol]++
		samples[p.kind] = append(samples[p.kind], p.elapsed)
	}
}
