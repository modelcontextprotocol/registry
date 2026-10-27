package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/registry/internal/database"
)

var fixtureEpoch = time.Date(2026, time.October, 26, 12, 0, 0, 0, time.UTC)

func seed(ctx context.Context, dbURL, fixture string, count int) error {
	if count < 3000 || count%3 != 0 {
		return fmt.Errorf("rows must be >=3000 and a multiple of three")
	}
	raw, err := os.ReadFile(fixture)
	if err != nil {
		return err
	}
	var templates []map[string]any
	if err = json.Unmarshal(raw, &templates); err != nil {
		return err
	}
	if len(templates) == 0 {
		return fmt.Errorf("empty fixture")
	}
	db, err := database.NewPostgreSQL(ctx, dbURL)
	if err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err = conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements"); err != nil {
		return err
	}
	// Never truncate: require an empty, explicitly named benchmark database.
	var existing int
	if err = conn.QueryRow(ctx, "SELECT count(*) FROM servers").Scan(&existing); err != nil {
		return err
	}
	if existing != 0 {
		return fmt.Errorf("registry_bench is not empty (%d rows); use a fresh disposable database", existing)
	}
	index := 0
	source := pgx.CopyFromFunc(func() ([]any, error) {
		if index >= count {
			return nil, nil
		}
		i := index
		index++
		name := fmt.Sprintf("io.benchmark/server-%06d", i/3)
		version := fmt.Sprintf("1.0.%d", i%3)
		value := make(map[string]any)
		for k, v := range templates[i%len(templates)] {
			value[k] = v
		}
		value["name"] = name
		value["version"] = version
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		// Deterministic dates dispersed across name order; no production data/network.
		updated := fixtureEpoch.Add(-time.Duration((int64(i)*48271)%(30*86400)) * time.Second)
		status := "active"
		if i%97 == 0 {
			status = "deleted"
		}
		return []any{name, version, status, updated, "", updated.Add(-24 * time.Hour), updated, i%3 == 2, payload}, nil
	})
	n, err := conn.CopyFrom(ctx, pgx.Identifier{"servers"}, []string{"server_name", "version", "status", "status_changed_at", "status_message", "published_at", "updated_at", "is_latest", "value"}, source)
	if err != nil {
		return err
	}
	if _, err = conn.Exec(ctx, "ANALYZE servers"); err != nil {
		return err
	}
	var heap, total int64
	if err = conn.QueryRow(ctx, "SELECT pg_relation_size('servers'),pg_total_relation_size('servers')").Scan(&heap, &total); err != nil {
		return err
	}
	fmt.Printf("Seeded %d rows; heap=%d bytes total=%d bytes\n", n, heap, total) //nolint:forbidigo // CLI seed summary, not application logging.
	return nil
}
