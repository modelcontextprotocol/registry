package usage

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishingSync(t *testing.T) {
	registry := newRegistryFixture(t)
	store := newStore(t, newDatabase(t))
	old := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Second)

	registry.publish(t, "io.github.example/alpha", "1.0.0", old, []string{"npm"}, nil)
	registry.publish(t, "io.github.example/alpha", "1.1.0", old.Add(time.Hour), nil, []string{"streamable-http"})
	registry.publish(t, "com.example/beta", "0.1.0", old.Add(2*time.Hour), []string{"PyPI", "oci"}, []string{"sse", "websocket"})
	registry.publish(t, "com.example/gamma", "2.0.0", old.Add(3*time.Hour), nil, nil)

	result, err := SyncPublishing(t.Context(), registry.reader, store, PublishingOptions{PageSize: 3})
	require.NoError(t, err)
	assert.Equal(t, PublishingResult{Changed: 4, Pages: 2, Full: true}, result)
	rows := publishedRows(t, store)
	require.Len(t, rows, 4)
	assert.Equal(t, publishedRow{Name: "io.github.example/alpha", Version: "1.0.0", Namespace: "io.github.example",
		NamespaceKind: "GitHub account", Status: "active", IsLatest: false, First: true, TypeLabel: "npm", Offers: []string{"npm"}},
		rows["io.github.example/alpha@1.0.0"])
	assert.Equal(t, publishedRow{Name: "io.github.example/alpha", Version: "1.1.0", Namespace: "io.github.example",
		NamespaceKind: "GitHub account", Status: "active", IsLatest: true, First: false, TypeLabel: "Remote only",
		Offers: []string{"Remote (HTTP)"}}, rows["io.github.example/alpha@1.1.0"])
	assert.Equal(t, publishedRow{Name: "com.example/beta", Version: "0.1.0", Namespace: "com.example", NamespaceKind: "Domain",
		Status: "active", IsLatest: true, First: true, TypeLabel: "Remote and package",
		Offers: []string{"Docker (OCI)", "PyPI", "Remote (SSE)", "Remote (websocket)"}}, rows["com.example/beta@0.1.0"])
	assert.Equal(t, "No package or remote", rows["com.example/gamma@2.0.0"].TypeLabel)
	assert.Empty(t, rows["com.example/gamma@2.0.0"].Offers)

	t.Run("nothing changed", func(t *testing.T) {
		result, err := SyncPublishing(t.Context(), registry.reader, store, PublishingOptions{})
		require.NoError(t, err)
		// A run starts a little before the watermark, so it reads the newest version again.
		assert.Equal(t, PublishingResult{Changed: 1, Pages: 1}, result)
		assert.Equal(t, rows, publishedRows(t, store))
	})

	t.Run("a new version takes the latest flag from its sibling", func(t *testing.T) {
		registry.publish(t, "io.github.example/alpha", "2.0.0", time.Now().UTC(), []string{"npm", "pypi"}, nil)
		result, err := SyncPublishing(t.Context(), registry.reader, store, PublishingOptions{})
		require.NoError(t, err)
		assert.Equal(t, 2, result.Changed, "the new version, and the one at the watermark again")
		rows := publishedRows(t, store)
		assert.False(t, rows["io.github.example/alpha@1.1.0"].IsLatest, "the registry cleared this flag without marking the row changed")
		assert.True(t, rows["io.github.example/alpha@2.0.0"].IsLatest)
		assert.Equal(t, "Several package types", rows["io.github.example/alpha@2.0.0"].TypeLabel)
		assert.False(t, rows["io.github.example/alpha@2.0.0"].First)
		assert.True(t, rows["io.github.example/alpha@1.0.0"].First)
	})

	t.Run("deleting the latest version hands the flag back", func(t *testing.T) {
		registry.remove(t, "io.github.example/alpha", "2.0.0", "1.1.0")
		_, err := SyncPublishing(t.Context(), registry.reader, store, PublishingOptions{})
		require.NoError(t, err)
		rows := publishedRows(t, store)
		assert.Equal(t, "deleted", rows["io.github.example/alpha@2.0.0"].Status)
		assert.False(t, rows["io.github.example/alpha@2.0.0"].IsLatest)
		assert.True(t, rows["io.github.example/alpha@1.1.0"].IsLatest)
	})

	t.Run("a change that committed late is still picked up", func(t *testing.T) {
		mark, known, err := store.publishingWatermark(t.Context())
		require.NoError(t, err)
		require.True(t, known)
		// Its timestamp is before the watermark, as if its transaction had been open while the last run read.
		registry.publish(t, "com.example/late", "1.0.0", mark.Add(-5*time.Minute), []string{"mcpb"}, nil)
		registry.publish(t, "com.example/too-late", "1.0.0", mark.Add(-time.Hour), []string{"cargo"}, nil)
		_, err = SyncPublishing(t.Context(), registry.reader, store, PublishingOptions{})
		require.NoError(t, err)
		rows := publishedRows(t, store)
		assert.Equal(t, "MCPB", rows["com.example/late@1.0.0"].TypeLabel)
		assert.NotContains(t, rows, "com.example/too-late@1.0.0", "outside the overlap, only a resync finds it")

		result, err := SyncPublishing(t.Context(), registry.reader, store, PublishingOptions{Resync: true, PageSize: 2})
		require.NoError(t, err)
		assert.True(t, result.Full)
		assert.Equal(t, 7, result.Changed)
		assert.Equal(t, "Cargo", publishedRows(t, store)["com.example/too-late@1.0.0"].TypeLabel)
	})

	t.Run("the catalogue names every server once", func(t *testing.T) {
		catalogue, err := store.Catalogue(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 5, catalogue.Len())
		assert.Equal(t, "Remote only", catalogue.typeOf("io.github.example/alpha"), "the type of the latest version")
		assert.Equal(t, "io.github.example/alpha", catalogue.resolve("IO.GitHub.Example/ALPHA"))
		assert.Equal(t, notListed, catalogue.resolve("io.github.example/unknown"))
	})
}

// TestPublishingPagesThroughTies checks that versions sharing one timestamp
// are each read exactly once when a page boundary falls between them.
func TestPublishingPagesThroughTies(t *testing.T) {
	registry := newRegistryFixture(t)
	store := newStore(t, newDatabase(t))
	at := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	names := []string{"com.example/aa", "com.example/bb", "com.example/cc", "com.example/dd", "com.example/ee", "com.example/ff", "com.example/gg"}
	for _, name := range names {
		registry.publish(t, name, "1.0.0", at, []string{"npm"}, nil)
		registry.publish(t, name, "1.0.1", at, []string{"npm"}, nil)
	}
	result, err := SyncPublishing(t.Context(), registry.reader, store, PublishingOptions{PageSize: 3})
	require.NoError(t, err)
	assert.Equal(t, 14, result.Changed)
	assert.Equal(t, 5, result.Pages)
	rows := publishedRows(t, store)
	require.Len(t, rows, 14)
	for _, name := range names {
		assert.True(t, rows[name+"@1.0.0"].First, name)
		assert.False(t, rows[name+"@1.0.0"].IsLatest, name)
		assert.True(t, rows[name+"@1.0.1"].IsLatest, name)
	}
}

// TestRegistryIsReadOnly checks that the reader cannot change the registry
// database even if a query were written wrongly.
func TestRegistryIsReadOnly(t *testing.T) {
	registry := newRegistryFixture(t)
	err := registry.reader.readOnly(t.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "DELETE FROM servers")
		return err
	})
	require.ErrorContains(t, err, "read-only transaction")
}
