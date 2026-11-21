package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/registry/internal/api"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/importer"
	"github.com/modelcontextprotocol/registry/internal/malware"
	"github.com/modelcontextprotocol/registry/internal/service"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

// Version info for the MCP Registry application
// These variables are injected at build time via ldflags
var (
	// Version is the current version of the MCP Registry application
	Version = "dev"

	// BuildTime is the time at which the binary was built
	BuildTime = "unknown"

	// GitCommit is the git commit that was compiled
	GitCommit = "unknown"
)

func main() {
	// Parse command line flags
	showVersion := flag.Bool("version", false, "Display version information")
	malwareSweep := flag.Bool("malware-sweep", false, "Check every listed package against OSV malware records, report the matches and exit")
	flag.Parse()

	if *malwareSweep {
		os.Exit(runMalwareSweep())
	}

	// Show version information if requested
	if *showVersion {
		log.Printf("MCP Registry %s\n", Version)
		log.Printf("Git commit: %s\n", GitCommit)
		log.Printf("Build time: %s\n", BuildTime)
		return
	}

	log.Printf("Starting MCP Registry Application v%s (commit: %s)", Version, GitCommit)

	var (
		registryService service.RegistryService
		db              database.Database
		err             error
	)

	// Initialize configuration
	cfg := config.NewConfig()
	poolLimits := database.PoolLimits{MaxConns: cfg.DatabaseMaxConnections, MinConns: cfg.DatabaseMinConnections}
	if err := poolLimits.Validate(); err != nil {
		log.Fatalf("Invalid database pool configuration: %v", err)
	}

	stopProfiling, err := telemetry.StartProfiling(os.Getenv("MCP_REGISTRY_ENABLE_PPROF") == "true")
	if err != nil {
		log.Printf("Failed to start profiling: %v", err)
		return
	}
	defer stopProfiling()

	// Initialize telemetry before opening the instrumented database pool.
	shutdownTelemetry, metrics, err := telemetry.InitMetrics(cfg.Version)
	if err != nil {
		log.Printf("Failed to initialize metrics: %v", err)
		return
	}

	defer func() {
		if err := shutdownTelemetry(context.Background()); err != nil {
			log.Printf("Failed to shutdown telemetry: %v", err)
		}
	}()

	// Connect to PostgreSQL with bounded retry. The DB endpoint can flap briefly
	// during voluntary disruptions (node drains, rollouts) and without retry the
	// pod exits cleanly, restarts, then races with the still-recovering service
	// — turning a transient blip into a multi-minute outage.
	const (
		maxStartupAttempts = 8
		perAttemptTimeout  = 10 * time.Second
		initialBackoff     = 1 * time.Second
		maxBackoff         = 8 * time.Second
	)
	backoff := initialBackoff
	for attempt := 1; ; attempt++ {
		attemptCtx, attemptCancel := context.WithTimeout(context.Background(), perAttemptTimeout)
		db, err = database.NewPostgreSQLWithPoolLimits(attemptCtx, cfg.DatabaseURL, poolLimits)
		attemptCancel()
		if err == nil {
			break
		}
		if attempt >= maxStartupAttempts {
			log.Printf("Failed to connect to PostgreSQL after %d attempts: %v", attempt, err)
			return
		}
		log.Printf("PostgreSQL not reachable (attempt %d/%d): %v, retrying in %s", attempt, maxStartupAttempts, err, backoff)
		time.Sleep(backoff)
		backoff = min(backoff*2, maxBackoff)
	}

	// Store the PostgreSQL instance for later cleanup
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Error closing PostgreSQL connection: %v", err)
		} else {
			log.Println("PostgreSQL connection closed successfully")
		}
	}()

	registryService = service.NewRegistryService(db, cfg)

	// Import seed data if seed source is provided
	if cfg.SeedFrom != "" {
		log.Printf("Importing data from %s...", cfg.SeedFrom)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		importerService := importer.NewService(registryService)
		if err := importerService.ImportFromPath(ctx, cfg.SeedFrom); err != nil {
			log.Printf("Failed to import seed data: %v", err)
		}
	}

	// Prepare version information
	versionInfo := &v0.VersionBody{
		Version:   Version,
		GitCommit: GitCommit,
		BuildTime: BuildTime,
	}

	// Initialize HTTP server
	server := api.NewServer(cfg, registryService, metrics, versionInfo)

	// Start server in a goroutine so it doesn't block signal handling
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Failed to start server: %v", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)

	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Create context with timeout for shutdown
	sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()

	// Gracefully shutdown the server
	if err := server.Shutdown(sctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exiting")
}

const (
	malwareSweepTimeout    = 30 * time.Minute
	malwareSweepOSVTimeout = 60 * time.Second
)

// runMalwareSweep checks every package the registry lists against OSV's
// malware records and returns the process's exit status. It needs no database
// and no credentials: it reads the registry's public listing.
//
// What it prints is what the hosted instance alerts on; see "Malware Checks"
// in docs/administration/admin-operations.md. Matches are a finding, not a
// failure, so the status is non-zero only when the sweep could not finish.
func runMalwareSweep() int {
	cfg := config.NewConfig()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, malwareSweepTimeout)
	defer cancel()

	osv := malware.NewClient(cfg.OSVAPIURL, malwareSweepOSVTimeout)
	result, err := malware.Sweep(ctx, cfg.MalwareSweepRegistryURL, osv, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stdout, "REGISTRY_MALWARE_SWEEP_FAILED registry=%q error=%q\n", cfg.MalwareSweepRegistryURL, err.Error())
		return 1
	}
	fmt.Fprintf(os.Stdout, "REGISTRY_MALWARE_SWEEP_SUCCESS registry=%q versions=%d packages=%d hits=%d\n",
		cfg.MalwareSweepRegistryURL, result.Versions, result.Packages, result.Hits)
	return 0
}
