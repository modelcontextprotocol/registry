package usage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// These tests use the PostgreSQL that `make test-unit` starts on localhost:5432.
const testDatabaseServer = "postgres://mcpregistry:mcpregistry@localhost:5432/" //nolint:gosec // G101: local test database credentials

// newDatabase creates an empty database for one test and returns its URL.
func newDatabase(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, testDatabaseServer+"postgres?sslmode=disable")
	require.NoError(t, err, "PostgreSQL is not running; start it with: docker compose up -d postgres")
	// By the time a cleanup runs the test's context is cancelled.
	cleanup := context.WithoutCancel(t.Context())
	defer func() { _ = admin.Close(ctx) }()
	var suffix [8]byte
	_, err = rand.Read(suffix[:])
	require.NoError(t, err)
	name := "usage_test_" + hex.EncodeToString(suffix[:])
	_, err = admin.Exec(ctx, "CREATE DATABASE "+name)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(cleanup, 15*time.Second)
		defer cancel()
		admin, err := pgx.Connect(ctx, testDatabaseServer+"postgres?sslmode=disable")
		if err != nil {
			return
		}
		defer func() { _ = admin.Close(ctx) }()
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return testDatabaseServer + name + "?sslmode=disable"
}

func newStore(t *testing.T, url string) *Store {
	t.Helper()
	store, err := OpenStore(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	return store
}

func newPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// registryFixture is a database with the registry's real schema, written to
// through the registry's own code, and a reader pointed at it.
type registryFixture struct {
	url    string
	db     *database.PostgreSQL
	reader *Registry
	pool   *pgxpool.Pool
}

func newRegistryFixture(t *testing.T) *registryFixture {
	t.Helper()
	url := newDatabase(t)
	db, err := database.NewPostgreSQL(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	reader, err := OpenRegistry(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(reader.Close)
	return &registryFixture{url: url, db: db, reader: reader, pool: newPool(t, url)}
}

// publish adds a version the way the registry does: the previous latest
// version loses its flag without being marked as changed.
func (r *registryFixture) publish(t *testing.T, name, version string, at time.Time, packages, remotes []string) {
	t.Helper()
	ctx := t.Context()
	server := &apiv0.ServerJSON{
		Schema: model.CurrentSchemaURL, Name: name, Version: version, Description: "A made-up server",
	}
	for _, kind := range packages {
		server.Packages = append(server.Packages, model.Package{RegistryType: kind, Identifier: "example", Version: version,
			Transport: model.Transport{Type: "stdio"}})
	}
	for _, kind := range remotes {
		server.Remotes = append(server.Remotes, model.Transport{Type: kind, URL: "https://mcp.example.org/" + kind})
	}
	require.NoError(t, r.db.UnmarkAsLatest(ctx, nil, name))
	_, err := r.db.CreateServer(ctx, nil, server, &apiv0.RegistryExtensions{
		Status: model.StatusActive, StatusChangedAt: at, PublishedAt: at, UpdatedAt: at, IsLatest: true,
	})
	require.NoError(t, err)
}

// remove marks a version deleted and hands the latest flag to another one,
// which the registry does without marking that other version as changed.
func (r *registryFixture) remove(t *testing.T, name, version, newLatest string) {
	t.Helper()
	ctx := t.Context()
	_, err := r.db.SetServerStatus(ctx, nil, name, version, model.StatusDeleted, nil, false)
	require.NoError(t, err)
	require.NoError(t, r.db.SetLatestVersion(ctx, nil, name, newLatest))
}

type publishedRow struct {
	Name, Version, Namespace, NamespaceKind string
	Status                                  string
	IsLatest, First                         bool
	TypeLabel                               string
	Offers                                  []string
}

func publishedRows(t *testing.T, store *Store) map[string]publishedRow {
	t.Helper()
	rows, err := store.pool.Query(t.Context(), `SELECT name, version, namespace, namespace_kind, status, is_latest,
		first_for_server, type_label, offers FROM publish_version`)
	require.NoError(t, err)
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (publishedRow, error) {
		var out publishedRow
		err := row.Scan(&out.Name, &out.Version, &out.Namespace, &out.NamespaceKind, &out.Status, &out.IsLatest, &out.First,
			&out.TypeLabel, &out.Offers)
		return out, err
	})
	require.NoError(t, err)
	byKey := map[string]publishedRow{}
	for _, row := range found {
		byKey[fmt.Sprintf("%s@%s", row.Name, row.Version)] = row
	}
	return byKey
}

// seedCatalogue fills the store's copy of the catalogue from the fixture, as a
// sync from the registry database would.
func seedCatalogue(t *testing.T, store *Store) {
	t.Helper()
	var versions []registryVersion
	for _, version := range fixtureVersions(t) {
		published, err := time.Parse(time.RFC3339Nano, version.PublishedAt)
		require.NoError(t, err)
		versions = append(versions, registryVersion{Name: version.Name, Version: version.Version, PublishedAt: published,
			UpdatedAt: published, Status: version.Status, IsLatest: version.IsLatest, Packages: version.Packages, Remotes: version.Remotes})
	}
	require.NoError(t, store.savePublishing(t.Context(), versions, nil, time.Now().UTC()))
}
