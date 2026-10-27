// readbench exercises real registry HTTP handlers against an isolated local database.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/registry/internal/api"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

// This public, deterministic key is exclusively for the loopback-only benchmark.
const benchmarkKey = "0000000000000000000000000000000000000000000000000000000000000000"

func localURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("benchmark requires a literal loopback address without query/fragment")
	}
	return nil
}

func localDatabase(raw string) error {
	if err := localURL(raw); err != nil {
		return err
	}
	u, _ := url.Parse(raw)
	if u.Scheme != "postgres" || u.Path != "/registry_bench" {
		return fmt.Errorf("database must be postgres://127.0.0.1:<port>/registry_bench (optional user/password)")
	}
	return nil
}

func main() {
	mode := flag.String("mode", "load", "seed, serve, or load")
	dbURL := flag.String("db", "postgres://mcpregistry@127.0.0.1:5432/registry_bench", "isolated loopback benchmark database")
	addr := flag.String("listen", "127.0.0.1:18080", "benchmark HTTP listen address")
	target := flag.String("target", "http://127.0.0.1:18080", "comma-separated loopback targets")
	rows := flag.Int("rows", 185001, "fixture rows (multiple of three)")
	fixture := flag.String("fixture", "data/seed.json", "local development seed templates")
	delay := flag.Duration("listing-delay", 0, "serve: artificial listing delay for cache-coordination experiment only")
	profile := flag.String("cpuprofile", "", "serve: CPU profile output")
	timeout := flag.Duration("timeout", 10*time.Second, "load: per-request deadline measured from scheduled arrival")
	duration := flag.Duration("duration", 30*time.Second, "load: offering duration; requests drain afterward")
	rate := flag.Int("rate", 200, "load: offered requests per second (open loop)")
	workers := flag.Int("workers", 256, "load: maximum active requests; excess counted as dropped")
	scenario := flag.String("scenario", "mixed", "page, updated, sparse, search, mixed, or hot")
	publishRate := flag.Int("publish-rate", 1, "independent publish requests/sec; zero disables")
	output := flag.String("out", "", "load: JSON result file (otherwise stdout)")
	flag.Parse()
	ctx := context.Background()
	var err error
	switch *mode {
	case "seed":
		if err = localDatabase(*dbURL); err == nil {
			err = seed(ctx, *dbURL, *fixture, *rows)
		}
	case "serve":
		if err = localDatabase(*dbURL); err == nil {
			err = serve(ctx, *dbURL, *addr, *profile, *delay)
		}
	case "load":
		err = load(ctx, loadConfig{targets: strings.Split(*target, ","), rows: *rows, duration: *duration, rate: *rate, workers: *workers, scenario: *scenario, publishRate: *publishRate, output: *output, timeout: *timeout})
	default:
		err = fmt.Errorf("unknown mode %q", *mode)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func serve(ctx context.Context, dbURL, addr, profile string, delay time.Duration) error {
	if delay < 0 || delay > 30*time.Second {
		return fmt.Errorf("listing-delay must be between zero and 30s")
	}
	if err := localURL("http://" + addr); err != nil {
		return err
	}
	shutdown, metrics, err := telemetry.InitMetrics("readbench") //nolint:contextcheck // Process-wide initializer has no context argument.
	if err != nil {
		return err
	}
	defer shutdown(ctx) //nolint:errcheck // Best effort cleanup in the disposable benchmark.
	db, err := database.NewPostgreSQL(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	cfg := &config.Config{ServerAddress: addr, JWTPrivateKey: benchmarkKey, EnableRegistryValidation: false}
	registry := api.NewServer(cfg, service.NewRegistryService(db, cfg), metrics, &v0.VersionBody{}) //nolint:contextcheck // Production router constructor has no context argument.
	var reads atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_readbench/stats" {
			_ = json.NewEncoder(w).Encode(map[string]int64{"read_starts": reads.Load()})
			return
		}
		if r.Method == http.MethodGet && (r.URL.Path == "/v0.1/servers" || r.URL.Path == "/v0/servers") {
			reads.Add(1)
			if delay > 0 {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-r.Context().Done():
					return
				}
			}
		}
		registry.Handler().ServeHTTP(w, r)
	})
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second}
	if profile != "" {
		f, err := os.Create(profile)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}
	signals, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-signals.Done()
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(c)
	}()
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
