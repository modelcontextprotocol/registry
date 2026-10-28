// prepare-listing-json backfills derived HTTP response bytes after migration 018.
// It never changes authoritative server JSON or official metadata.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/registry/internal/database"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	batch := flag.Int("batch", 500, "rows per transaction (1..1000)")
	pause := flag.Duration("pause", 100*time.Millisecond, "delay between batches")
	flag.Parse()
	uri := os.Getenv("DATABASE_URL")
	if uri == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if *pause < 0 {
		return fmt.Errorf("pause must be nonnegative")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	config, err := pgx.ParseConfig(uri)
	if err != nil {
		return fmt.Errorf("invalid DATABASE_URL")
	}
	config.ConnectTimeout = 10 * time.Second
	config.RuntimeParams["statement_timeout"] = "5s"
	config.RuntimeParams["lock_timeout"] = "1s"
	config.RuntimeParams["application_name"] = "registry-listing-backfill"
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("database connection failed")
	}
	defer conn.Close(context.Background())
	name, version := "", ""
	encoder := json.NewEncoder(os.Stdout)
	for {
		batchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		result, err := database.BackfillListingJSONBatch(batchCtx, conn, name, version, *batch)
		cancel()
		if err != nil {
			return fmt.Errorf("backfill batch failed: %w", err)
		}
		if err := encoder.Encode(result); err != nil {
			return err
		}
		if result.Scanned == 0 {
			return nil
		}
		name, version = result.LastName, result.LastVersion
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(*pause):
		}
	}
}
