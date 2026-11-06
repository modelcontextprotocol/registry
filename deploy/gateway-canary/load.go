// Isolated private-address HTTPS load driver and deterministic HTTP origin.
// This tool performs only GET requests; it cannot publish registry records.
package main

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	serve := flag.Bool("serve", false, "serve static fixture on :8080")
	payload := flag.String("payload", "payload.json", "origin response body")
	target := flag.String("target", "", "private HTTPS IP:port; never public DNS")
	ca := flag.String("ca", "tls.crt", "test certificate trust root")
	rate := flag.Int("rate", 100, "offered requests/s, max 15000")
	seconds := flag.Int("seconds", 60, "duration, max 900 seconds")
	workers := flag.Int("workers", 128, "bounded concurrency")
	keys := flag.Int("keys", 32, "hot-key count")
	miss := flag.Int("miss-percent", 0, "distinct-key percentage")
	epoch := flag.String("epoch", "warm", "cache-key epoch, same in both arms")
	calibrate := flag.Bool("calibrate", false, "loopback HTTP generator calibration only")
	out := flag.String("out", "result.json", "aggregate results")
	allowPublic := flag.Bool("allow-non-rfc1918-target", false, "explicitly allow a verified canary IP outside RFC1918 (including GKE Service IPs); TLS validation stays enabled")
	http2 := flag.Bool("http2", false, "exercise HTTP/2")
	flag.Parse()
	if *serve {
		data, err := os.ReadFile(*payload)
		if err != nil {
			log.Fatal(err)
		}
		var reads atomic.Uint64
		originStarted := time.Now()
		mux := http.NewServeMux()
		mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
			var ru syscall.Rusage
			syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
			json.NewEncoder(w).Encode(map[string]any{"origin_reads": reads.Load(), "body_bytes": len(data), "cpu_seconds": float64(ru.Utime.Sec+ru.Stime.Sec) + float64(ru.Utime.Usec+ru.Stime.Usec)/1e6, "max_rss_kib": ru.Maxrss, "uptime_seconds": time.Since(originStarted).Seconds()})
		})
		mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]string{"xff": r.Header.Get("X-Forwarded-For"), "request_id": r.Header.Get("X-Request-ID"), "host": r.Host})
		})
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			n := reads.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Origin-Sequence", strconv.FormatUint(n, 10))
			switch r.URL.Query().Get("mode") {
			case "private":
				w.Header().Set("Cache-Control", "private, no-store")
			case "cookie":
				w.Header().Set("Set-Cookie", "benchmark=1; Secure; HttpOnly")
			case "error":
				w.WriteHeader(503)
				w.Write([]byte(`{"error":"controlled fixture"}`))
				return
			}
			w.Write(data)
		})
		log.Fatal((&http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
	}
	if *rate < 1 || *rate > 15000 || *seconds < 1 || *seconds > 900 || *workers < 1 || *workers > 2048 || *keys < 1 || *miss < 0 || *miss > 100 {
		log.Fatal("invalid bounded load parameters")
	}
	u, err := url.Parse(*target)
	if err != nil {
		log.Fatal(err)
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil {
		log.Fatal("target must be a literal private IP")
	}
	if *calibrate {
		if !ip.IsLoopback() || u.Scheme != "http" {
			log.Fatal("calibration is loopback HTTP only")
		}
	} else if (!ip.IsPrivate() && !*allowPublic) || u.Scheme != "https" {
		log.Fatal("comparison requires private-IP HTTPS")
	}
	tr := &http.Transport{MaxIdleConns: *workers, MaxIdleConnsPerHost: *workers, MaxConnsPerHost: *workers, DisableCompression: true, ForceAttemptHTTP2: *http2, IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: 5 * time.Second}
	if !*calibrate {
		cert, err := os.ReadFile(*ca)
		if err != nil {
			log.Fatal(err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cert) {
			log.Fatal("invalid CA")
		}
		tr.TLSClientConfig = &tls.Config{RootCAs: pool, ServerName: "fixture.gateway-canary.invalid", MinVersion: tls.VersionTLS12}
	}
	client := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	jobs := make(chan int, *workers*2)
	var wg sync.WaitGroup
	var mu sync.Mutex
	status := map[string]int{}
	caches := map[string]int{}
	encodings := map[string]int{}
	protocols := map[string]int{}
	errors := map[string]int{}
	var latencies []float64
	var bytes int64
	var firstHash string
	started := time.Now()
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				key := i % *keys
				if i%100 < *miss {
					key = *keys + i
				}
				q := url.Values{"limit": {"100"}, "bench_epoch": {*epoch}, "bench_key": {strconv.Itoa(key)}}
				req, _ := http.NewRequest(http.MethodGet, stringsURL(*target, "/v0.1/servers?"+q.Encode()), nil)
				req.Host = "fixture.gateway-canary.invalid"
				req.Header.Set("Accept-Encoding", "gzip")
				req.Header.Set("User-Agent", "registry-gateway-comparison/1.0")
				begin := time.Now()
				res, e := client.Do(req)
				if e != nil {
					mu.Lock()
					errors["transport"]++
					mu.Unlock()
					continue
				}
				var n int64
				var hash string
				if i == 0 {
					h := sha256.New()
					n, e = io.Copy(h, res.Body)
					hash = hex.EncodeToString(h.Sum(nil))
				} else {
					n, e = io.Copy(io.Discard, res.Body)
				}
				res.Body.Close()
				elapsed := time.Since(begin).Seconds()
				mu.Lock()
				status[strconv.Itoa(res.StatusCode)]++
				caches[res.Header.Get("X-Registry-Cache")]++
				encodings[res.Header.Get("Content-Encoding")]++
				protocols[res.Proto]++
				bytes += n
				latencies = append(latencies, elapsed)
				if hash != "" {
					firstHash = hash
				}
				if e != nil {
					errors["read_body"]++
				}
				mu.Unlock()
			}
		}()
	}
	offered := *rate * *seconds
	dropped := 0
	late := 0
	for i := 0; i < offered; i++ {
		due := started.Add(time.Duration(float64(i) / float64(*rate) * float64(time.Second)))
		if delay := time.Until(due); delay > 0 {
			time.Sleep(delay)
		}
		if time.Since(due) > 100*time.Millisecond {
			late++
			dropped++
			continue
		}
		select {
		case jobs <- i:
		default:
			dropped++
		}
	}
	close(jobs)
	wg.Wait()
	duration := time.Since(started).Seconds()
	tr.CloseIdleConnections()
	sort.Float64s(latencies)
	pct := func(p float64) float64 {
		if len(latencies) == 0 {
			return 0
		}
		return latencies[int(float64(len(latencies)-1)*p)] * 1000
	}
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	report := map[string]any{"started_at": started.UTC().Format(time.RFC3339Nano), "finished_at": time.Now().UTC().Format(time.RFC3339Nano), "generator_cpu_millicores": (float64(ru.Utime.Sec+ru.Stime.Sec) + float64(ru.Utime.Usec+ru.Stime.Usec)/1e6) / duration * 1000, "generator_max_rss_kib": ru.Maxrss, "offered": offered, "attempted": offered - dropped, "generator_dropped": dropped, "scheduler_late": late, "statuses": status, "cache": caches, "encoding": encodings, "protocols": protocols, "errors": errors, "wire_body_bytes": bytes, "first_body_sha256": firstHash, "p50_ms": pct(.5), "p95_ms": pct(.95), "p99_ms": pct(.99), "p999_ms": pct(.999), "max_ms": pct(1), "elapsed_seconds": duration, "successful_rps": float64(status["200"]) / duration, "target_rate": *rate, "seconds": *seconds, "keys": *keys, "miss_percent": *miss, "epoch": *epoch, "calibration": *calibrate, "workers": *workers}
	b, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(*out, append(b, '\n'), 0600); err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(b))
}

func stringsURL(base, path string) string {
	u, _ := url.Parse(base)
	u.Path = ""
	u.RawQuery = ""
	return u.String() + path
}
